// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
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

// Change stored tags after the handler's metadata read but before its mutation.
// The real backend must authorize the new metadata under its own write lock.
type consoleVersionTagMutationObjectLayer struct {
	ObjectLayer
	bucket, object string
	changed        bool
}

func (o *consoleVersionTagMutationObjectLayer) GetObjectInfo(ctx context.Context, bucket, object string, opts ObjectOptions) (ObjectInfo, error) {
	info, err := o.ObjectLayer.GetObjectInfo(ctx, bucket, object, opts)
	if err == nil && bucket == o.bucket && object == o.object && !o.changed {
		o.changed = true
		_, err = o.ObjectLayer.PutObjectTags(ctx, bucket, object, "environment=dev", ObjectOptions{VersionID: opts.VersionID})
	}
	return info, err
}

func TestConsoleVersionAuthorizationTaggingHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testConsoleVersionAuthorizationTaggingHTTPStorage, nil)
}

func testConsoleVersionAuthorizationTaggingHTTPStorage(obj ObjectLayer, instanceType, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
	ctx := context.Background()
	globalIAMSys.InitStore(obj)
	user := auth.Credentials{AccessKey: "version-tag-user", SecretKey: "version-tag-secret"}
	if err := globalIAMSys.CreateUser(user.AccessKey, madmin.UserInfo{SecretKey: user.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObjectTagging","s3:PutObjectTagging","s3:DeleteObjectTagging"],"Resource":["arn:aws:s3:::` + bucket + `/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`
	parsed, err := iampolicy.ParseConfig(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy("stored-tag-operations", *parsed); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet(user.AccessKey, "stored-tag-operations", false); err != nil {
		t.Fatal(err)
	}
	for _, signature := range []string{"v4", "v2", "presigned"} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			for _, state := range []string{"prod", "dev", "changed-before-write"} {
				if state == "changed-before-write" && method == http.MethodGet {
					continue
				}
				t.Run(instanceType+"/"+signature+"/"+method+"/"+state, func(t *testing.T) {
					object := fmt.Sprintf("tag-%s-%s-%s", signature, method, state)
					actual := state
					if state == "changed-before-write" {
						actual = "prod"
					}
					if _, err := obj.PutObject(ctx, bucket, object, mustGetPutObjReader(t, strings.NewReader("tag data"), 8, "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=" + actual}}); err != nil {
						t.Fatal(err)
					}
					var mutation *consoleVersionTagMutationObjectLayer
					if state == "changed-before-write" {
						mutation = &consoleVersionTagMutationObjectLayer{ObjectLayer: obj, bucket: bucket, object: object}
						setObjectLayer(mutation)
						defer setObjectLayer(obj)
					}
					query := url.Values{"tagging": {""}}
					headers := map[string]string{}
					if state != "prod" {
						headers[xhttp.AmzObjectTagging] = "environment=prod"
						query.Set("ExistingObjectTag/environment", "prod")
						query.Set("Existingobjecttag/environment", "prod")
					}
					body := ""
					if method == http.MethodPut {
						body = `<Tagging><TagSet><Tag><Key>environment</Key><Value>stage</Value></Tag></TagSet></Tagging>`
					}
					target := "http://otterio.test/" + bucket + "/" + object + "?" + query.Encode()
					var request *http.Request
					var err error
					switch signature {
					case "v2":
						request, err = newTestSignedRequestV2(method, target, int64(len(body)), bytes.NewReader([]byte(body)), user.AccessKey, user.SecretKey, headers)
					case "presigned":
						request, err = newTestRequest(method, target, int64(len(body)), bytes.NewReader([]byte(body)))
						if err == nil {
							for key, value := range headers {
								request.Header.Set(key, value)
							}
							request = signer.PreSignV4(*request, user.AccessKey, user.SecretKey, "", globalServerRegion, 60)
						}
					default:
						request, err = newTestSignedRequestV4(method, target, int64(len(body)), bytes.NewReader([]byte(body)), user.AccessKey, user.SecretKey, headers)
					}
					if err != nil {
						t.Fatal(err)
					}
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					status, want := http.StatusForbidden, "environment=dev"
					if state == "prod" {
						status, want = http.StatusOK, "environment=prod"
						if method == http.MethodPut {
							want = "environment=stage"
						} else if method == http.MethodDelete {
							status, want = http.StatusNoContent, ""
						}
					}
					if recorder.Code != status {
						t.Fatalf("want %d got %d %s", status, recorder.Code, recorder.Body.String())
					}
					if status == http.StatusForbidden {
						var response APIErrorResponse
						if xml.Unmarshal(recorder.Body.Bytes(), &response) != nil || response.Code != "AccessDenied" {
							t.Fatalf("want AccessDenied: %s", recorder.Body.String())
						}
					}
					if mutation != nil && !mutation.changed {
						t.Fatal("fixture did not change tags between metadata read and mutation")
					}
					stored, err := obj.GetObjectTags(ctx, bucket, object, ObjectOptions{})
					if err != nil || stored.String() != want {
						t.Fatalf("stored tags %v err=%v want=%s", stored, err, want)
					}
				})
			}
		}
	}
}

func TestConsoleVersionAuthorizationRequestTaggingHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testConsoleVersionAuthorizationRequestTaggingHTTPStorage, nil)
}

// PutObjectTagging writes XML tags. Its request-tag conditions must describe
// that exact set, independently of signed headers and the old stored tags.
func testConsoleVersionAuthorizationRequestTaggingHTTPStorage(obj ObjectLayer, instanceType, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
	ctx := context.Background()
	globalIAMSys.InitStore(obj)
	user := auth.Credentials{AccessKey: "xml-tag-user", SecretKey: "xml-tag-secret"}
	if err := globalIAMSys.CreateUser(user.AccessKey, madmin.UserInfo{SecretKey: user.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:PutObjectTagging"],"Resource":["arn:aws:s3:::` + bucket + `/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"stage","s3:RequestObjectTag/environment":"prod"}}}]}`
	parsed, err := iampolicy.ParseConfig(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy("parsed-request-tag-operations", *parsed); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet(user.AccessKey, "parsed-request-tag-operations", false); err != nil {
		t.Fatal(err)
	}
	for _, signature := range []string{"v4", "v2", "presigned"} {
		for _, test := range []struct {
			name, bodyTag, headerTag string
			injectQuery, mutate      bool
			status                   int
		}{
			{name: "prod-xml-no-header", bodyTag: "prod", status: http.StatusOK},
			{name: "prod-xml-dev-header", bodyTag: "prod", headerTag: "dev", status: http.StatusOK},
			{name: "dev-xml-forged-prod-header", bodyTag: "dev", headerTag: "prod", status: http.StatusForbidden},
			{name: "empty-xml-forged-prod-header", headerTag: "prod", status: http.StatusForbidden},
			{name: "prod-xml-injected-query", bodyTag: "prod", headerTag: "dev", injectQuery: true, status: http.StatusOK},
			{name: "prod-xml-stored-tag-race", bodyTag: "prod", headerTag: "dev", mutate: true, status: http.StatusForbidden},
		} {
			t.Run(instanceType+"/"+signature+"/"+test.name, func(t *testing.T) {
				object := "xml-tag-" + signature + "-" + test.name
				if _, err := obj.PutObject(ctx, bucket, object, mustGetPutObjReader(t, strings.NewReader("tag data"), 8, "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=stage"}}); err != nil {
					t.Fatal(err)
				}
				var mutation *consoleVersionTagMutationObjectLayer
				if test.mutate {
					mutation = &consoleVersionTagMutationObjectLayer{ObjectLayer: obj, bucket: bucket, object: object}
					setObjectLayer(mutation)
					defer setObjectLayer(obj)
				}
				query := url.Values{"tagging": {""}}
				if test.injectQuery {
					query.Set("RequestObjectTag/environment", "dev")
					query.Set("Requestobjecttag/environment", "dev")
					query.Set("ExistingObjectTag/environment", "prod")
				}
				headers := map[string]string{}
				if test.headerTag != "" {
					headers[xhttp.AmzObjectTagging] = "environment=" + test.headerTag
				}
				body := `<Tagging><TagSet></TagSet></Tagging>`
				if test.bodyTag != "" {
					body = `<Tagging><TagSet><Tag><Key>environment</Key><Value>` + test.bodyTag + `</Value></Tag></TagSet></Tagging>`
				}
				target := "http://otterio.test/" + bucket + "/" + object + "?" + query.Encode()
				var request *http.Request
				var err error
				switch signature {
				case "v2":
					request, err = newTestSignedRequestV2(http.MethodPut, target, int64(len(body)), strings.NewReader(body), user.AccessKey, user.SecretKey, headers)
				case "presigned":
					request, err = newTestRequest(http.MethodPut, target, int64(len(body)), strings.NewReader(body))
					if err == nil {
						for key, value := range headers {
							request.Header.Set(key, value)
						}
						request = signer.PreSignV4(*request, user.AccessKey, user.SecretKey, "", globalServerRegion, 60)
					}
				default:
					request, err = newTestSignedRequestV4(http.MethodPut, target, int64(len(body)), strings.NewReader(body), user.AccessKey, user.SecretKey, headers)
				}
				if err != nil {
					t.Fatal(err)
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if recorder.Code != test.status {
					t.Fatalf("want %d got %d %s", test.status, recorder.Code, recorder.Body.String())
				}
				want := "environment=prod"
				if test.status == http.StatusForbidden {
					var response APIErrorResponse
					if xml.Unmarshal(recorder.Body.Bytes(), &response) != nil || response.Code != "AccessDenied" {
						t.Fatalf("want AccessDenied: %s", recorder.Body.String())
					}
					want = "environment=stage"
					if test.mutate {
						want = "environment=dev"
					}
				}
				if mutation != nil && !mutation.changed {
					t.Fatal("fixture did not change tags between metadata read and mutation")
				}
				stored, err := obj.GetObjectTags(ctx, bucket, object, ObjectOptions{})
				if err != nil || stored.String() != want {
					t.Fatalf("stored tags %v err=%v want=%s", stored, err, want)
				}
			})
		}
	}
}
