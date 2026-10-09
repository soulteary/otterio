// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

// A remote-only object's selected metadata is still authoritative for tag
// permissions when its persisted reference or remote target is unavailable.
func TestConsoleVersionAuthorizationTierErrorsHTTPStorage(t *testing.T) {
	if _, supported := reflect.TypeOf(ObjectOptions{}).FieldByName("TransitionedObject"); !supported {
		t.Skip("the 12 tier error authorization scenarios require the optional lifecycle storage profile")
	}
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
		if backend == FSTestStr {
			return // Remote-only lifecycle storage is an erasure backend capability.
		}
		ctx := context.Background()
		allowPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`
		denyPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"]},{"Effect":"Deny","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`
		user := consoleReviewUser(t, obj, "review-tier-allow", allowPolicy)
		consoleVersionCachePolicy(t, user, "review-tier-deny", denyPolicy)
		for _, corruptReference := range []bool{false, true} {
			object := "missing-tier-client"
			if corruptReference {
				object = "corrupt-tier-reference"
			}
			body := "remote-only object data"
			source, err := obj.PutObject(ctx, bucket, object, mustGetPutObjReader(t, strings.NewReader(body), int64(len(body)), "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}})
			if err != nil {
				t.Fatal(err)
			}
			// Keep this version-authorization test compilable without the
			// optional storage patch. JSON binds its fixture only when the
			// storage profile above exposes the optional ObjectOptions fields.
			transitionFixture, err := json.Marshal(struct {
				TransitionExpected ObjectInfo
				TransitionedObject json.RawMessage
			}{source, json.RawMessage(`{"arn":"arn:otterio:ilm::storage-test:archive","key":"unique/object","versionId":"remote-version","storageClass":"ARCHIVE"}`)})
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range []string{lifecycle.TransitionPending, lifecycle.TransitionComplete} {
				opts := ObjectOptions{VersionID: source.VersionID, TransitionStatus: status}
				if err = json.Unmarshal(transitionFixture, &opts); err != nil {
					t.Fatal(err)
				}
				if _, err = obj.DeleteObject(ctx, bucket, object, opts); err != nil {
					t.Fatal(err)
				}
			}
			if corruptReference {
				for _, disk := range obj.(*erasureServerPools).serverPools[0].sets[0].getDisks() {
					fi, readErr := disk.ReadVersion(ctx, bucket, object, source.VersionID, false)
					if readErr != nil {
						t.Fatal(readErr)
					}
					// ReadVersion exposes status separately; AddVersion persists it
					// from the metadata map when writing the damaged fixture.
					fi.Metadata["x-otterio-internal-transition-status"] = fi.TransitionStatus
					fi.Metadata["x-otterio-internal-transition-reference"] = "{broken"
					if err = disk.WriteMetadata(ctx, bucket, object, fi); err != nil {
						t.Fatal(err)
					}
				}
			}
			selected, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{})
			if err != nil || selected.UserTags != "environment=prod" || selected.TransitionStatus != lifecycle.TransitionComplete {
				t.Fatalf("remote-only metadata was not selected: %+v / %v", selected, err)
			}
			// Establish the existing backend error without a request authorization
			// callback. No remote target is configured, so no network is involved.
			reader, backendErr := obj.GetObjectNInfo(ctx, bucket, object, nil, http.Header{}, readLock, ObjectOptions{})
			if reader != nil {
				reader.Close()
			}
			if backendErr == nil {
				t.Fatal("remote-only fixture unexpectedly has a readable tier")
			}
			if corruptReference && backendErr.Error() != "corrupt persisted transition reference" {
				t.Fatalf("want corrupt reference error, got %v", backendErr)
			}
			if !corruptReference {
				if _, ok := backendErr.(BucketRemoteTargetNotFound); !ok {
					t.Fatalf("want missing tier client error, got %T: %v", backendErr, backendErr)
				}
			}
			wantError := toAPIError(ctx, backendErr)
			for _, signature := range []string{"v4", "v2", "presigned"} {
				for _, denied := range []bool{false, true} {
					policy, role := "review-tier-allow", "authorized"
					if denied {
						policy, role = "review-tier-deny", "denied"
					}
					t.Run(backend+"/"+object+"/"+signature+"/"+role, func(t *testing.T) {
						if err := globalIAMSys.PolicyDBSet(user.AccessKey, policy, false); err != nil {
							t.Fatal(err)
						}
						response := httptest.NewRecorder()
						router.ServeHTTP(response, consoleReviewSignedRequest(t, signature, http.MethodGet, "http://otterio.test/"+bucket+"/"+object, user, nil))
						if denied {
							assertConsoleVersionCacheDenied(t, http.MethodGet, response)
							if strings.Contains(response.Body.String(), "transition") || strings.Contains(response.Body.String(), "archive") {
								t.Fatalf("denied read disclosed tier failure: %s", response.Body.String())
							}
							return
						}
						var actual APIErrorResponse
						if err := xml.Unmarshal(response.Body.Bytes(), &actual); err != nil || response.Code != wantError.HTTPStatusCode || actual.Code != wantError.Code {
							t.Fatalf("authorized tier read changed backend error: want %d %s, got %d %s / %v", wantError.HTTPStatusCode, wantError.Code, response.Code, response.Body.String(), err)
						}
					})
				}
			}
		}
	}, nil)
}
