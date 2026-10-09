// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/policy"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

func versionAuthorizationRequest(t *testing.T, method, rawQuery, signature string, user auth.Credentials, headers map[string]string) *http.Request {
	t.Helper()
	target := "http://localhost/bucket/key"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	var req *http.Request
	var err error
	switch signature {
	case "v2":
		req, err = newTestSignedRequestV2(method, target, 0, bytes.NewReader(nil), user.AccessKey, user.SecretKey, headers)
	case "presigned":
		req, err = newTestRequest(method, target, 0, bytes.NewReader(nil))
		if err == nil {
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			req = signer.PreSignV4(*req, user.AccessKey, user.SecretKey, user.SessionToken, globalServerRegion, 60)
		}
	default:
		req, err = newTestSignedRequestV4(method, target, 0, bytes.NewReader(nil), user.AccessKey, user.SecretKey, headers)
	}
	if err != nil {
		t.Fatal(err)
	}
	req.RequestURI = req.URL.RequestURI()
	return req
}

func TestConsoleVersionAuthorizationPolicyMatrix(t *testing.T) {
	user := selfCredentialsFixture(t)
	policies := map[string]string{
		"current":      `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:ListBucket"],"Resource":["arn:aws:s3:::bucket","arn:aws:s3:::bucket/*"]}]}`,
		"history":      `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObjectVersion","s3:ListBucketVersions"],"Resource":["arn:aws:s3:::bucket","arn:aws:s3:::bucket/*"]}]}`,
		"condition":    `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObjectVersion"],"Resource":["arn:aws:s3:::bucket/*"],"Condition":{"StringEquals":{"s3:versionid":"allowed"}}}]}`,
		"deny-history": `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:GetObjectVersion"],"Resource":["arn:aws:s3:::bucket/*"]},{"Effect":"Deny","Action":["s3:GetObjectVersion"],"Resource":["arn:aws:s3:::bucket/*"]}]}`,
	}
	for name, document := range policies {
		parsed, err := iampolicy.ParseConfig(strings.NewReader(document))
		if err != nil {
			t.Fatal(err)
		}
		globalIAMSys.iamPolicyDocsMap[name] = *parsed
	}
	for _, signature := range []string{"v4", "v2", "presigned"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, test := range []struct {
				policy, version string
				want            APIErrorCode
			}{
				{"current", "", ErrNone}, {"current", "old", ErrAccessDenied}, {"current", "null", ErrAccessDenied},
				{"history", "", ErrAccessDenied}, {"history", "old", ErrNone}, {"history", "null", ErrNone},
				{"condition", "allowed", ErrNone}, {"condition", "other", ErrAccessDenied},
				{"deny-history", "", ErrNone}, {"deny-history", "old", ErrAccessDenied},
			} {
				t.Run(signature+"/"+method+"/"+test.policy+"/"+test.version, func(t *testing.T) {
					globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy(test.policy)
					query := ""
					if test.version != "" {
						query = url.Values{"versionId": {test.version}}.Encode()
					}
					r := versionAuthorizationRequest(t, method, query, signature, user, nil)
					if got := checkRequestAuthType(context.Background(), r, policy.GetObjectAction, "bucket", "key"); got != test.want {
						t.Fatalf("authorization=%v want=%v", got, test.want)
					}
				})
			}
		}
	}
	for _, name := range []string{"current", "history"} {
		globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy(name)
		r := versionAuthorizationRequest(t, http.MethodGet, "versions=", "v4", user, nil)
		want := ErrAccessDenied
		if name == "history" {
			want = ErrNone
		}
		if got := checkRequestAuthType(context.Background(), r, policy.ListBucketVersionsAction, "bucket", ""); got != want {
			t.Fatalf("%s version listing=%v want=%v", name, got, want)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, version := range []string{"", "old", "null"} {
			query := ""
			if version != "" {
				query = url.Values{"versionId": {version}}.Encode()
			}
			r := versionAuthorizationRequest(t, method, query, "v4", globalActiveCred, nil)
			if got := checkRequestAuthType(context.Background(), r, policy.GetObjectAction, "bucket", "key"); got != ErrNone {
				t.Fatalf("root %s version %q denied: %v", method, version, got)
			}
		}
	}
}

func TestConsoleVersionAuthorizationActionsAreIndependent(t *testing.T) {
	if iampolicy.NewActionSet(iampolicy.GetObjectVersionAction).Match(iampolicy.GetObjectAction) || iampolicy.NewActionSet(iampolicy.GetObjectAction).Match(iampolicy.GetObjectVersionAction) {
		t.Fatal("history and current-object permissions imply one another")
	}
	if !iampolicy.NewActionSet(iampolicy.Action("s3:GetObject*")).Match(iampolicy.GetObjectVersionAction) {
		t.Fatal("explicit wildcard no longer includes history")
	}
}

func TestConsoleVersionAuthorizationNullPaginationMarker(t *testing.T) {
	versions := FileInfoVersions{Versions: []FileInfo{{VersionID: "new"}, {VersionID: ""}}}
	if index := versions.findVersionIndex(nullVersionID); index != 1 {
		t.Fatalf("S3 null marker did not identify legacy null version: %d", index)
	}
}

func TestConsoleVersionAuthorizationConditionCannotBeSpoofed(t *testing.T) {
	user := selfCredentialsFixture(t)
	parsed, err := iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObjectVersion"],"Resource":["arn:aws:s3:::bucket/*"],"Condition":{"StringEquals":{"s3:versionid":"allowed"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	globalIAMSys.iamPolicyDocsMap["condition"] = *parsed
	globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy("condition")
	for _, signature := range []string{"v4", "presigned"} {
		for _, test := range []struct {
			query   string
			headers map[string]string
		}{
			{"versionId=forbidden&versionid=allowed", nil},
			{"versionId=forbidden&Versionid=allowed", nil},
			{"versionId=forbidden", map[string]string{"Versionid": "allowed"}},
		} {
			r := versionAuthorizationRequest(t, http.MethodGet, test.query, signature, user, test.headers)
			if got := checkRequestAuthType(context.Background(), r, policy.GetObjectAction, "bucket", "key"); got != ErrAccessDenied {
				t.Fatalf("spoofed %s version condition authorized: %v", signature, got)
			}
			values := getConditionValues(r, "", user.AccessKey, nil)
			if len(values["versionid"]) != 1 || values["versionid"][0] != "forbidden" || values["Versionid"] != nil {
				t.Fatal("caller controlled version condition")
			}
		}
	}
}

func TestConsoleVersionAuthorizationMatchesNormalizedLookup(t *testing.T) {
	user := selfCredentialsFixture(t)
	const version = "f34d9787-0892-4d0f-b450-f98371d5de64"
	policies := map[string]string{
		"current":   `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::bucket/*"]}]}`,
		"history":   `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObjectVersion"],"Resource":["arn:aws:s3:::bucket/*"]}]}`,
		"condition": `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObjectVersion"],"Resource":["arn:aws:s3:::bucket/*"],"Condition":{"StringEquals":{"s3:versionid":"` + version + `"}}}]}`,
	}
	for name, document := range policies {
		parsed, err := iampolicy.ParseConfig(strings.NewReader(document))
		if err != nil {
			t.Fatal(err)
		}
		globalIAMSys.iamPolicyDocsMap[name] = *parsed
	}
	for _, signature := range []string{"v4", "v2", "presigned"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, test := range []struct {
				policy, raw, lookup string
				want                APIErrorCode
			}{
				{"history", " \t ", "", ErrAccessDenied},
				{"history", "\u2003\n", "", ErrAccessDenied},
				{"history", "", "", ErrAccessDenied},
				{"current", " \t ", "", ErrNone},
				{"history", " null\t", "null", ErrNone},
				{"current", " null\t", "null", ErrAccessDenied},
				{"condition", " \t" + version + "\u2003", version, ErrNone},
				{"condition", " \t ", "", ErrAccessDenied},
			} {
				t.Run(signature+"/"+method+"/"+test.policy+"/"+url.QueryEscape(test.raw), func(t *testing.T) {
					globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy(test.policy)
					// Copy-source is irrelevant to a GET/HEAD lookup and cannot fill
					// the condition of a whitespace-only current-object reference.
					headers := map[string]string{"X-Amz-Copy-Source": "/bucket/source?versionId=" + version}
					r := versionAuthorizationRequest(t, method, url.Values{"versionId": {test.raw}}.Encode(), signature, user, headers)
					if got := checkRequestAuthType(context.Background(), r, policy.GetObjectAction, "bucket", "key"); got != test.want {
						t.Fatalf("authorization=%v want=%v", got, test.want)
					}
					opts, err := getOpts(context.Background(), r, "bucket", "key")
					if err != nil || opts.VersionID != test.lookup {
						t.Fatalf("lookup=%q err=%v want=%q", opts.VersionID, err, test.lookup)
					}
					values := getConditionValues(r, "", user.AccessKey, nil)["versionid"]
					if len(values) != 1 || values[0] != opts.VersionID {
						t.Fatalf("condition=%v does not match lookup=%q", values, opts.VersionID)
					}
				})
			}
		}
	}
	copyRequest := versionAuthorizationRequest(t, http.MethodPut, "", "v4", user, map[string]string{
		"X-Amz-Copy-Source": "/bucket/source?versionId=" + url.QueryEscape(" \t"+version+" "),
	})
	if values := getConditionValues(copyRequest, "", user.AccessKey, nil)["versionid"]; len(values) != 1 || values[0] != version {
		t.Fatalf("copy-source version condition changed: %v", values)
	}
}

func TestConsoleVersionAuthorizationCopySourceReferenceCannotBeSpoofed(t *testing.T) {
	user := selfCredentialsFixture(t)
	for _, test := range []struct {
		source, want string
	}{
		{"/source/key", ""},
		{"/source/key?versionId=%20%09", ""},
		{"/source/key?versionId=%20null%09", "null"},
		{"/source/key?versionId=forbidden&versionid=allowed&Versionid=allowed", "forbidden"},
		{"/source/key?versionId=forbidden&versionId=allowed", "forbidden"},
	} {
		r := versionAuthorizationRequest(t, http.MethodPut, "versionId=allowed&versionid=allowed&Versionid=allowed", "v4", user, map[string]string{
			"X-Amz-Copy-Source": test.source,
			"Versionid":         "allowed",
		})
		path, version := getCopySource(r)
		if path != "/source/key" || version != test.want {
			t.Fatalf("copy source parsed as %q / %q, want %q", path, version, test.want)
		}
		values := getConditionValues(withCopySourceVersionID(r, version), "", user.AccessKey, map[string]interface{}{
			"versionid": "allowed", "Versionid": "allowed",
		})
		if len(values["versionid"]) != 1 || values["versionid"][0] != test.want || values["Versionid"] != nil || values["versionId"] != nil {
			t.Fatalf("source version condition was replaced: %+v", values)
		}
	}
}

func TestConsoleVersionAuthorizationTagConditionOrigins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/bucket/key?ExistingObjectTag/environment=prod&Existingobjecttag/environment=prod&RequestObjectTag/environment=injected", nil)
	r.Header.Set("X-Amz-Tagging", "environment=request")
	r.Header.Set("ExistingObjectTag/environment", "prod")
	r.Header.Set("Existingobjecttag/environment", "prod")
	claims := map[string]interface{}{"ExistingObjectTag/environment": "prod", "Existingobjecttag/environment": "prod", "RequestObjectTag/environment": "injected"}
	for _, actual := range []string{"", "environment=dev", "environment=prod"} {
		values := getConditionValues(withObjectTags(r, actual), "", "", claims)
		want := strings.TrimPrefix(actual, "environment=")
		if actual == "" {
			if values["ExistingObjectTag/environment"] != nil {
				t.Fatal("empty stored tags acquired a caller supplied existing tag")
			}
		} else if len(values["ExistingObjectTag/environment"]) != 1 || values["ExistingObjectTag/environment"][0] != want {
			t.Fatalf("stored tags replaced: %+v", values)
		}
		if values["Existingobjecttag/environment"] != nil || len(values["RequestObjectTag/environment"]) != 1 || values["RequestObjectTag/environment"][0] != "request" {
			t.Fatalf("condition origins confused: %+v", values)
		}
	}
	if values := getConditionValues(r, "", "", claims); values["ExistingObjectTag/environment"] != nil || values["Existingobjecttag/environment"] != nil {
		t.Fatal("unloaded metadata acquired a caller supplied existing tag")
	}
	for _, parsed := range []string{"", "environment=xml"} {
		request := withRequestObjectTags(withObjectTags(r, "environment=stored"), parsed)
		values := getConditionValues(request, "", "", claims)
		if parsed == "" {
			if values["RequestObjectTag/environment"] != nil {
				t.Fatal("empty parsed request tags acquired caller supplied header/query/claim tags")
			}
		} else if len(values["RequestObjectTag/environment"]) != 1 || values["RequestObjectTag/environment"][0] != "xml" {
			t.Fatalf("parsed request tags replaced: %+v", values)
		}
		if values["ExistingObjectTag/environment"][0] != "stored" || request.Header.Get("X-Amz-Tagging") != "environment=request" {
			t.Fatal("parsed request tags modified stored tags or signed headers")
		}
	}
}
