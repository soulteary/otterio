// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
)

// Both permissions can depend on the current destination's stored tags. An
// early PUT authorization without that metadata must not reject a request that
// the commit-lock authorization would allow.
func TestIfMatchReviewAuthorizesStoredTagsForBothActions(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
		ctx := context.Background()
		user := consoleReviewUser(t, obj, "if-match-review-tags", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject"],"Resource":["arn:aws:s3:::`+bucket+`/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`)
		for _, operation := range []string{"put", "complete"} {
			for _, signature := range []string{"v4", "presigned", "streaming"} {
				if operation == "complete" && signature == "streaming" {
					continue
				}
				t.Run(backend+"/"+operation+"/"+signature, func(t *testing.T) {
					key := operation + "-" + signature
					original := "original content"
					info, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, strings.NewReader(original), int64(len(original)), "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}})
					if err != nil {
						t.Fatal(err)
					}
					method, body := http.MethodPut, "replacement content"
					target := "http://otterio.test/" + bucket + "/" + key
					if operation == "complete" {
						id, err := obj.NewMultipartUpload(ctx, bucket, key, ObjectOptions{})
						if err != nil {
							t.Fatal(err)
						}
						part, err := obj.PutObjectPart(ctx, bucket, key, id, 1, mustGetPutObjReader(t, strings.NewReader(body), int64(len(body)), "", ""), ObjectOptions{})
						if err != nil {
							t.Fatal(err)
						}
						method = http.MethodPost
						target = getCompleteMultipartUploadURL("http://otterio.test", bucket, key, id)
						body = fmt.Sprintf(`<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>"%s"</ETag></Part></CompleteMultipartUpload>`, part.ETag)
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, conditionalMatchRequest(t, signature, method, target, body, user, http.Header{xhttp.IfMatch: {info.ETag}}))
					if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<Error>") {
						t.Fatalf("stored tags satisfy both permissions but conditional %s failed: %d %s", operation, response.Code, response.Body.String())
					}
					conditionalMatchStored(t, obj, bucket, key, "replacement content")
				})
			}
		}
	}, nil)
}

type ifMatchReviewLookupLayer struct {
	ObjectLayer
	lookups int
}

func (o *ifMatchReviewLookupLayer) GetObjectInfo(ctx context.Context, bucket, object string, opts ObjectOptions) (ObjectInfo, error) {
	o.lookups++
	return o.ObjectLayer.GetObjectInfo(ctx, bucket, object, opts)
}

// Deferring tag-dependent policy evaluation must not also defer signature,
// credential, session-token or signed-condition validation until after a read.
func TestIfMatchReviewAuthenticatesBeforeMetadataLookup(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, _ http.Handler, credentials auth.Credentials, t *testing.T) {
		for _, operation := range []string{"put", "complete"} {
			for _, mode := range []string{"signature", "unknown-user", "invalid-token", "unsigned-condition", "anonymous", "v2", "unsupported-backend"} {
				t.Run(backend+"/"+operation+"/"+mode, func(t *testing.T) {
					wantStatus := http.StatusForbidden
					if mode == "unsupported-backend" {
						previous := globalIsDistErasure
						globalIsDistErasure = true
						t.Cleanup(func() { globalIsDistErasure = previous })
						wantStatus = http.StatusNotImplemented
					}
					layer := &ifMatchReviewLookupLayer{ObjectLayer: obj}
					api := objectAPIHandlers{
						ObjectAPI: func() ObjectLayer { return layer },
						CacheAPI:  func() CacheObjectLayer { return nil },
					}
					method, body, signature := http.MethodPut, "replacement", "v4"
					target := "http://otterio.test/" + bucket + "/object"
					if operation == "complete" {
						method = http.MethodPost
						target += "?uploadId=not-an-upload"
						body = `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>"part"</ETag></Part></CompleteMultipartUpload>`
					}
					headers := http.Header{xhttp.IfMatch: {"*"}}
					user := credentials
					switch mode {
					case "unknown-user":
						user = auth.Credentials{AccessKey: "not-an-existing-user", SecretKey: "not-an-existing-user-secret"}
					case "invalid-token":
						headers.Set(xhttp.AmzSecurityToken, "invalid-session-token")
					case "unsigned-condition":
						headers.Del(xhttp.IfMatch)
					case "v2":
						signature = "v2"
					}
					request := conditionalMatchRequest(t, signature, method, target, body, user, headers)
					switch mode {
					case "signature":
						request.Header.Set(xhttp.IfMatch, `"changed-after-signing"`)
					case "unsigned-condition":
						request.Header.Set(xhttp.IfMatch, "*")
					case "anonymous":
						request.Header.Del(xhttp.Authorization)
					}
					request = setURLVarsOnRequest(request, map[string]string{"bucket": bucket, "object": "object"})
					response := httptest.NewRecorder()
					if operation == "put" {
						api.PutObjectHandler(response, request)
					} else {
						api.CompleteMultipartUploadHandler(response, request)
					}
					if response.Code != wantStatus || layer.lookups != 0 {
						t.Fatalf("invalid request reached origin metadata: status=%d lookups=%d body=%s", response.Code, layer.lookups, response.Body.String())
					}
					conditionalMatchMetadataHeadersAbsent(t, response)
				})
			}
		}
	}, nil)
}
