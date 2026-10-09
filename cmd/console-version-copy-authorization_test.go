// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

type consoleVersionCopyOptions struct {
	sourceKey string
	headers   map[string]string
	query     url.Values
}

// Exercise the production Fiber routing table and real erasure storage. A
// helper-only authorization test cannot establish which source bytes a copy
// actually used, or whether a rejected copy persisted a destination part.
func TestConsoleVersionAuthorizationCopySourceHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testConsoleVersionAuthorizationCopySourceHTTPStorage, nil)
}

func testConsoleVersionAuthorizationCopySourceHTTPStorage(obj ObjectLayer, instanceType, destination string, router http.Handler, _ auth.Credentials, t *testing.T) {
	if instanceType == FSTestStr {
		return // FS cannot enable bucket versioning.
	}
	ctx := context.Background()
	source := getRandomBucketName()
	const key = "source"
	const nullData, oldData, currentData, prodData = "null source bytes", "old source bytes", "current source bytes", "prod source bytes"
	if err := obj.MakeBucketWithLocation(ctx, source, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	put := func(object, body, tagging string, versioned bool) ObjectInfo {
		t.Helper()
		info, err := obj.PutObject(ctx, source, object, mustGetPutObjReader(t, strings.NewReader(body), int64(len(body)), "", ""), ObjectOptions{Versioned: versioned, UserDefined: map[string]string{xhttp.AmzObjectTagging: tagging}})
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	put(key, nullData, "", false)
	for _, bucket := range []string{source, destination} {
		if err := globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)); err != nil {
			t.Fatal(err)
		}
	}
	old := put(key, oldData, "environment=dev", true)
	prod := put(key, prodData, "environment=prod", true)
	current := put(key, currentData, "environment=dev", true)
	prodCurrent := put("prod-current", prodData, "environment=prod", true)
	put("untagged", oldData, "", true)
	if old.VersionID == "" || current.VersionID == "" || old.VersionID == current.VersionID {
		t.Fatal("fixture did not persist distinct historical/current versions")
	}

	globalIAMSys.InitStore(obj)
	user := auth.Credentials{AccessKey: "version-copy-user", SecretKey: "version-copy-secret"}
	if err := globalIAMSys.CreateUser(user.AccessKey, madmin.UserInfo{SecretKey: user.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	policies := map[string]struct {
		actions, condition, deny, tag, targetTag string
	}{
		"current":        {actions: `"s3:GetObject"`},
		"history":        {actions: `"s3:GetObjectVersion"`},
		"both":           {actions: `"s3:GetObject","s3:GetObjectVersion"`},
		"deny-history":   {actions: `"s3:GetObject","s3:GetObjectVersion"`, deny: "s3:GetObjectVersion"},
		"deny-current":   {actions: `"s3:GetObject","s3:GetObjectVersion"`, deny: "s3:GetObject"},
		"condition":      {actions: `"s3:GetObject","s3:GetObjectVersion"`, condition: old.VersionID},
		"condition-null": {actions: `"s3:GetObjectVersion"`, condition: nullVersionID},
		"tag-current":    {actions: `"s3:GetObject"`, tag: "prod"},
		"tag-history":    {actions: `"s3:GetObjectVersion"`, tag: "prod"},
		"tag-both":       {actions: `"s3:GetObject","s3:GetObjectVersion"`, tag: "prod"},
		"request-tag":    {actions: `"s3:GetObject","s3:GetObjectVersion"`, targetTag: "prod"},
		"tag-version":    {actions: `"s3:GetObjectVersion"`, condition: prod.VersionID, tag: "prod"},
	}
	for name, config := range policies {
		condition, deny := "", ""
		var conditions []string
		if config.condition != "" {
			conditions = append(conditions, `"s3:versionid":"`+config.condition+`"`)
		}
		if config.tag != "" {
			conditions = append(conditions, `"s3:ExistingObjectTag/environment":"`+config.tag+`"`)
		}
		if len(conditions) != 0 {
			condition = `,"Condition":{"StringEquals":{` + strings.Join(conditions, ",") + `}}`
		}
		if config.deny != "" {
			deny = `,{"Effect":"Deny","Action":["` + config.deny + `"],"Resource":["arn:aws:s3:::` + source + `/*"]}`
		}
		targetCondition := ""
		if config.targetTag != "" {
			targetCondition = `,"Condition":{"StringEquals":{"s3:RequestObjectTag/environment":"` + config.targetTag + `"}}`
		}
		document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::` + destination + `/*"]` + targetCondition + `},{"Effect":"Allow","Action":[` + config.actions + `],"Resource":["arn:aws:s3:::` + source + `/*"]` + condition + `}` + deny + `]}`
		parsed, err := iampolicy.ParseConfig(strings.NewReader(document))
		if err != nil {
			t.Fatal(err)
		}
		if err = globalIAMSys.SetPolicy("copy-"+name, *parsed); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name, policy, version, destinationVersion, want string
		options                                         *consoleVersionCopyOptions
	}{
		{"current-current", "current", "", "", currentData, nil},
		{"current-old-denied", "current", old.VersionID, "", "", nil},
		{"current-null-denied", "current", nullVersionID, "", "", nil},
		{"current-blank", "current", " \t\u2003", "", currentData, nil},
		{"current-target-version", "current", "", old.VersionID, currentData, nil},
		{"history-current-denied", "history", "", "", "", nil},
		{"history-old", "history", old.VersionID, "", oldData, nil},
		{"history-null", "history", nullVersionID, "", nullData, nil},
		{"history-blank-denied", "history", " \t\u2003", "", "", nil},
		{"history-padded-old", "history", " \t" + old.VersionID + "\u2003", "", oldData, nil},
		{"history-padded-null", "history", " null\t", "", nullData, nil},
		{"history-old-target-current", "history", old.VersionID, current.VersionID, oldData, nil},
		{"both-current", "both", "", "", currentData, nil},
		{"both-old", "both", old.VersionID, "", oldData, nil},
		{"both-null", "both", nullVersionID, "", nullData, nil},
		{"deny-history-current", "deny-history", "", "", currentData, nil},
		{"deny-history-old", "deny-history", old.VersionID, "", "", nil},
		{"deny-history-null", "deny-history", nullVersionID, "", "", nil},
		{"deny-history-old-target-null", "deny-history", old.VersionID, nullVersionID, "", nil},
		{"deny-current-current", "deny-current", "", "", "", nil},
		{"deny-current-old", "deny-current", old.VersionID, "", oldData, nil},
		{"condition-old", "condition", old.VersionID, "", oldData, nil},
		{"condition-padded-old", "condition", " \t" + old.VersionID + "\u2003", current.VersionID, oldData, nil},
		{"condition-other-denied", "condition", current.VersionID, "", "", nil},
		{"condition-other-target-allowed-denied", "condition", current.VersionID, old.VersionID, "", nil},
		{"condition-current-target-allowed-denied", "condition", "", old.VersionID, "", nil},
		{"condition-blank-target-allowed-denied", "condition", " \t\u2003", old.VersionID, "", nil},
		{"condition-null-target-allowed-denied", "condition", nullVersionID, old.VersionID, "", nil},
		{"condition-null", "condition-null", nullVersionID, "", nullData, nil},
		{"condition-padded-null", "condition-null", " null\t", old.VersionID, nullData, nil},
		{"condition-old-target-null-denied", "condition-null", old.VersionID, nullVersionID, "", nil},
		{"tag-history-prod-no-header", "tag-history", prod.VersionID, "", prodData, nil},
		{"tag-history-dev-no-header", "tag-history", old.VersionID, "", "", nil},
		{"tag-history-dev-forged-prod", "tag-history", old.VersionID, "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-history-prod-request-dev", "tag-history", prod.VersionID, "", prodData, &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=dev"}}},
		{"tag-history-null-forged-prod", "tag-history", nullVersionID, "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-history-padded-prod", "tag-history", " \t" + prod.VersionID + "\u2003", "", prodData, nil},
		{"tag-current-prod-no-header", "tag-current", "", "", prodData, &consoleVersionCopyOptions{sourceKey: "prod-current"}},
		{"tag-current-prod-request-dev", "tag-current", "", "", prodData, &consoleVersionCopyOptions{sourceKey: "prod-current", headers: map[string]string{xhttp.AmzObjectTagging: "environment=dev"}}},
		{"tag-current-dev-no-header", "tag-current", "", "", "", nil},
		{"tag-current-dev-forged-prod", "tag-current", "", "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-current-untagged-forged-prod", "tag-current", "", "", "", &consoleVersionCopyOptions{sourceKey: "untagged", headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-current-history-denied", "tag-current", prodCurrent.VersionID, "", "", &consoleVersionCopyOptions{sourceKey: "prod-current", headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-both-prod-history-no-header", "tag-both", prod.VersionID, "", prodData, nil},
		{"tag-both-prod-current-no-header", "tag-both", "", "", prodData, &consoleVersionCopyOptions{sourceKey: "prod-current"}},
		{"tag-both-dev-history-forged-prod", "tag-both", old.VersionID, "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-both-dev-current-forged-prod", "tag-both", "", "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-version-prod-allowed", "tag-version", prod.VersionID, "", prodData, nil},
		{"tag-version-dev-target-prod-forged-prod", "tag-version", old.VersionID, prod.VersionID, "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}}},
		{"tag-precondition-dev-forged-prod", "tag-history", old.VersionID, "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod", xhttp.AmzCopySourceIfMatch: `"wrong-etag"`}}},
		{"tag-precondition-current-dev-forged-prod", "tag-current", "", "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod", xhttp.AmzCopySourceIfMatch: `"wrong-etag"`}}},
		{"tag-history-null-query-injection", "tag-history", nullVersionID, "", "", &consoleVersionCopyOptions{query: url.Values{"ExistingObjectTag/environment": {"prod"}, "Existingobjecttag/environment": {"prod"}}}},
		{"tag-history-dev-query-injection", "tag-history", old.VersionID, "", "", &consoleVersionCopyOptions{query: url.Values{"ExistingObjectTag/environment": {"prod"}, "Existingobjecttag/environment": {"prod"}}}},
		{"tag-current-untagged-query-injection", "tag-current", "", "", "", &consoleVersionCopyOptions{sourceKey: "untagged", query: url.Values{"ExistingObjectTag/environment": {"prod"}, "Existingobjecttag/environment": {"prod"}}}},
		{"tag-current-dev-query-injection", "tag-current", "", "", "", &consoleVersionCopyOptions{query: url.Values{"ExistingObjectTag/environment": {"prod"}, "Existingobjecttag/environment": {"prod"}}}},
		{"request-tag-current-dev-target-prod", "request-tag", "", "", currentData, &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod", xhttp.AmzTagDirective: "REPLACE"}}},
		{"request-tag-old-dev-target-prod", "request-tag", old.VersionID, "", oldData, &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=prod", xhttp.AmzTagDirective: "REPLACE"}}},
		{"request-tag-source-prod-copied-tags", "request-tag", "", "", prodData, &consoleVersionCopyOptions{sourceKey: "prod-current"}},
		{"request-tag-source-prod-target-dev-denied", "request-tag", prod.VersionID, "", "", &consoleVersionCopyOptions{headers: map[string]string{xhttp.AmzObjectTagging: "environment=dev", xhttp.AmzTagDirective: "REPLACE"}}},
	}
	read := func(object, want string) {
		t.Helper()
		reader, err := obj.GetObjectNInfo(ctx, destination, object, nil, nil, readLock, ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || string(data) != want {
			t.Fatalf("copied bytes=%q, err=%v, want=%q", data, err, want)
		}
	}
	for _, signature := range []string{"v4", "v2", "presigned"} {
		for _, operation := range []string{"CopyObject", "UploadPartCopy"} {
			for index, test := range cases {
				t.Run(signature+"/"+operation+"/"+test.name, func(t *testing.T) {
					want := test.want
					if operation == "UploadPartCopy" && test.policy == "request-tag" {
						want = "" // UploadPartCopy cannot set final object tags.
					}
					if err := globalIAMSys.PolicyDBSet(user.AccessKey, "copy-"+test.policy, false); err != nil {
						t.Fatal(err)
					}
					object := fmt.Sprintf("copy-%s-%s-%d", signature, operation, index)
					query := url.Values{}
					if test.options != nil {
						for name, values := range test.options.query {
							query[name] = values
						}
					}
					var uploadID string
					if operation == "UploadPartCopy" {
						var err error
						uploadID, err = obj.NewMultipartUpload(ctx, destination, object, ObjectOptions{Versioned: true, VersionID: test.destinationVersion})
						if err != nil {
							t.Fatal(err)
						}
						query.Set("partNumber", "1")
						query.Set("uploadId", uploadID)
					}
					if test.destinationVersion != "" {
						query.Set("versionId", test.destinationVersion)
					}
					target := "http://otterio.test/" + destination + "/" + object
					if len(query) > 0 {
						target += "?" + query.Encode()
					}
					sourceKey := key
					if test.options != nil && test.options.sourceKey != "" {
						sourceKey = test.options.sourceKey
					}
					copySource := "/" + source + "/" + sourceKey
					if test.version != "" {
						copySource += "?" + url.Values{"versionId": {test.version}}.Encode()
					}
					headers := map[string]string{xhttp.AmzCopySource: copySource}
					if test.options != nil {
						for name, value := range test.options.headers {
							headers[name] = value
						}
					}
					var request *http.Request
					var err error
					switch signature {
					case "v2":
						request, err = newTestSignedRequestV2(http.MethodPut, target, 0, bytes.NewReader(nil), user.AccessKey, user.SecretKey, headers)
					case "presigned":
						request, err = newTestRequest(http.MethodPut, target, 0, bytes.NewReader(nil))
						if err == nil {
							for name, value := range headers {
								request.Header.Set(name, value)
							}
							request = signer.PreSignV4(*request, user.AccessKey, user.SecretKey, "", globalServerRegion, 60)
						}
					default:
						request, err = newTestSignedRequestV4(http.MethodPut, target, 0, bytes.NewReader(nil), user.AccessKey, user.SecretKey, headers)
					}
					if err != nil {
						t.Fatal(err)
					}
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					if want == "" {
						var response APIErrorResponse
						if recorder.Code != http.StatusForbidden || xml.Unmarshal(recorder.Body.Bytes(), &response) != nil || response.Code != "AccessDenied" {
							t.Fatalf("want AccessDenied, got %d %s", recorder.Code, recorder.Body.String())
						}
						if operation == "UploadPartCopy" {
							parts, err := obj.ListObjectParts(ctx, destination, object, uploadID, 0, 100, ObjectOptions{})
							if err != nil || len(parts.Parts) != 0 {
								t.Fatalf("rejected copy persisted parts: %+v, %v", parts.Parts, err)
							}
						} else if _, err := obj.GetObjectInfo(ctx, destination, object, ObjectOptions{}); !isErrObjectNotFound(err) {
							t.Fatalf("rejected copy persisted a destination: %v", err)
						}
					} else {
						if recorder.Code != http.StatusOK {
							t.Fatalf("want successful copy, got %d %s", recorder.Code, recorder.Body.String())
						}
						if operation == "UploadPartCopy" {
							var response CopyObjectPartResponse
							if err := xml.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.ETag == "" {
								t.Fatalf("invalid successful copy part result: %s, %v", recorder.Body.String(), err)
							}
							if _, err := obj.CompleteMultipartUpload(ctx, destination, object, uploadID, []CompletePart{{PartNumber: 1, ETag: canonicalizeETag(response.ETag)}}, ObjectOptions{Versioned: true, VersionID: test.destinationVersion}); err != nil {
								t.Fatal(err)
							}
						}
						read(object, want)
					}
					if operation == "UploadPartCopy" && want == "" {
						if err := obj.AbortMultipartUpload(ctx, destination, object, uploadID, ObjectOptions{}); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}

	readCases := []struct {
		name, policy, object, version, requestTags, conditionHeader, conditionValue string
		query                                                                       url.Values
		status                                                                      int
		want                                                                        string
	}{
		{name: "prod-current", policy: "tag-current", object: "prod-current", status: 200, want: prodData},
		{name: "prod-current-request-dev", policy: "tag-current", object: "prod-current", requestTags: "environment=dev", status: 200, want: prodData},
		{name: "dev-current", policy: "tag-current", object: key, status: 403},
		{name: "dev-current-forged-prod", policy: "tag-current", object: key, requestTags: "environment=prod", status: 403},
		{name: "prod-history", policy: "tag-history", object: key, version: prod.VersionID, status: 200, want: prodData},
		{name: "prod-history-request-dev", policy: "tag-history", object: key, version: prod.VersionID, requestTags: "environment=dev", status: 200, want: prodData},
		{name: "dev-history", policy: "tag-history", object: key, version: old.VersionID, status: 403},
		{name: "dev-history-forged-prod", policy: "tag-history", object: key, version: old.VersionID, requestTags: "environment=prod", status: 403},
		{name: "null-forged-prod", policy: "tag-history", object: key, version: nullVersionID, requestTags: "environment=prod", status: 403},
		{name: "null-query-injection", policy: "tag-history", object: key, version: nullVersionID, query: url.Values{"ExistingObjectTag/environment": {"prod"}, "Existingobjecttag/environment": {"prod"}}, status: 403},
		{name: "dev-query-injection", policy: "tag-current", object: key, query: url.Values{"ExistingObjectTag/environment": {"prod"}, "Existingobjecttag/environment": {"prod"}}, status: 403},
		{name: "untagged-query-injection", policy: "tag-current", object: "untagged", query: url.Values{"ExistingObjectTag/environment": {"prod"}, "Existingobjecttag/environment": {"prod"}}, status: 403},
		{name: "prod-not-modified", policy: "tag-history", object: key, version: prod.VersionID, conditionHeader: "If-None-Match", conditionValue: prod.ETag, status: 304},
		{name: "dev-not-modified-denied", policy: "tag-history", object: key, version: old.VersionID, requestTags: "environment=prod", conditionHeader: "If-None-Match", conditionValue: old.ETag, status: 403},
		{name: "prod-precondition", policy: "tag-history", object: key, version: prod.VersionID, conditionHeader: "If-Match", conditionValue: `"wrong-etag"`, status: 412},
		{name: "dev-precondition-denied", policy: "tag-history", object: key, version: old.VersionID, requestTags: "environment=prod", conditionHeader: "If-Match", conditionValue: `"wrong-etag"`, status: 403},
		{name: "missing-tag-policy", policy: "tag-current", object: "missing", requestTags: "environment=prod", status: 403},
		{name: "missing-no-list", policy: "history", object: "missing", status: 403},
		{name: "missing-with-list", policy: "history", object: "missing", status: 404},
	}
	for _, signature := range []string{"v4", "v2", "presigned", "anonymous"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, test := range readCases {
				t.Run("StoredTagRead/"+signature+"/"+method+"/"+test.name, func(t *testing.T) {
					if err := globalIAMSys.PolicyDBSet(user.AccessKey, "copy-"+test.policy, false); err != nil {
						t.Fatal(err)
					}
					if signature == "anonymous" {
						config := policies[test.policy]
						condition := ""
						if config.tag != "" {
							condition = `,"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"` + config.tag + `"}}`
						}
						list := ""
						if test.name == "missing-with-list" {
							list = `,{"Effect":"Allow","Principal":"*","Action":["s3:ListBucket"],"Resource":["arn:aws:s3:::` + source + `"]}`
						}
						document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":[` + config.actions + `],"Resource":["arn:aws:s3:::` + source + `/*"]` + condition + `}` + list + `]}`
						if err := globalBucketMetadataSys.Update(source, bucketPolicyConfig, []byte(document)); err != nil {
							t.Fatal(err)
						}
					}
					query := url.Values{}
					for name, values := range test.query {
						query[name] = values
					}
					if test.version != "" {
						query.Set("versionId", test.version)
					}
					target := "http://otterio.test/" + source + "/" + test.object
					if len(query) > 0 {
						target += "?" + query.Encode()
					}
					headers := map[string]string{}
					if test.requestTags != "" {
						headers[xhttp.AmzObjectTagging] = test.requestTags
					}
					if test.conditionHeader != "" {
						headers[test.conditionHeader] = test.conditionValue
					}
					var request *http.Request
					var err error
					switch signature {
					case "v2":
						request, err = newTestSignedRequestV2(method, target, 0, bytes.NewReader(nil), user.AccessKey, user.SecretKey, headers)
					case "presigned", "anonymous":
						request, err = newTestRequest(method, target, 0, bytes.NewReader(nil))
						if err == nil {
							for name, value := range headers {
								request.Header.Set(name, value)
							}
							if signature == "presigned" {
								request = signer.PreSignV4(*request, user.AccessKey, user.SecretKey, "", globalServerRegion, 60)
							}
						}
					default:
						request, err = newTestSignedRequestV4(method, target, 0, bytes.NewReader(nil), user.AccessKey, user.SecretKey, headers)
					}
					if err != nil {
						t.Fatal(err)
					}
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					status := test.status
					if signature != "anonymous" && test.name == "missing-with-list" {
						status = 403
					}
					if recorder.Code != status {
						t.Fatalf("want %d got %d %s", status, recorder.Code, recorder.Body.String())
					}
					if status == http.StatusOK && method == http.MethodGet && recorder.Body.String() != test.want {
						t.Fatalf("read bytes %q want %q", recorder.Body.String(), test.want)
					}
					if status == http.StatusForbidden {
						for name := range recorder.Header() {
							for _, private := range []string{xhttp.ETag, xhttp.LastModified, xhttp.AmzVersionID, xhttp.AmzDeleteMarker} {
								if strings.EqualFold(name, private) {
									t.Fatalf("denied read exposed %s", name)
								}
							}
						}
					}
				})
			}
		}
	}

}
