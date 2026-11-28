// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/otterio/cmd/config/dns"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/replication"
)

type conditionalDeleteDNS struct{ dns.Store }

func TestConditionalDeleteCapabilities(t *testing.T) {
	object, cache := newObjectLayerFn(), newCachedObjectLayerFn()
	gateway, distributed, federation, federationEnabled := globalIsGateway, globalIsDistErasure, globalDNSConfig, globalBucketFederation
	t.Cleanup(func() {
		setObjectLayer(object)
		setCacheObjectLayer(cache)
		globalIsGateway, globalIsDistErasure, globalDNSConfig, globalBucketFederation = gateway, distributed, federation, federationEnabled
	})
	setObjectLayer(&FSObjects{})
	setCacheObjectLayer(nil)
	globalIsGateway, globalIsDistErasure, globalDNSConfig, globalBucketFederation = false, false, nil, false
	if !conditionalDeleteSupported() {
		t.Fatal("filesystem conditional deletion is missing")
	}
	response := httptest.NewRecorder()
	setCommonHeaders(response)
	if response.Header().Get(conditionalDeleteHeader) != "v1" || response.Header().Get(conditionalWritesHeader) != "v1" {
		t.Fatal("conditional delete must have its own advertised capability")
	}
	for _, cache := range []CacheObjectLayer{&cacheObjects{}, &cacheObjects{commitWriteback: true}} {
		setCacheObjectLayer(cache)
		if conditionalDeleteSupported() {
			t.Fatal("cached deletion must fail closed")
		}
	}
	setCacheObjectLayer(nil)
	globalDNSConfig = &conditionalDeleteDNS{}
	if conditionalDeleteSupported() {
		t.Fatal("federated deletion must fail closed")
	}
	globalDNSConfig = nil
	globalBucketFederation = true
	if conditionalDeleteSupported() {
		t.Fatal("enabled federation must fail closed before DNS initialization")
	}
	globalBucketFederation = false
	globalIsDistErasure = true
	if conditionalDeleteSupported() {
		t.Fatal("distributed deletion must fail closed")
	}
	globalIsDistErasure = false
	globalIsGateway = true
	if conditionalDeleteSupported() {
		t.Fatal("gateway deletion must fail closed")
	}
	globalIsGateway = false
	setObjectLayer(&erasureServerPools{serverPools: []*erasureSets{{}}})
	if !conditionalDeleteSupported() {
		t.Fatal("local single-pool conditional deletion is missing")
	}
	setObjectLayer(&erasureServerPools{serverPools: []*erasureSets{{}, {}}})
	if conditionalDeleteSupported() {
		t.Fatal("multiple pools must fail closed")
	}
	setObjectLayer(nil)
	if conditionalDeleteSupported() {
		t.Fatal("uninitialized storage must fail closed")
	}
}

func TestConditionalDeleteHTTP(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
		ctx := context.Background()
		put := func(key, body, tags string) ObjectInfo {
			t.Helper()
			info, err := obj.PutObject(ctx, bucket, key, ifMatchBackendReader(t, body), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: tags}})
			if err != nil {
				t.Fatal(err)
			}
			return info
		}
		for _, signature := range []string{"v4", "presigned"} {
			t.Run(backend+"/"+signature, func(t *testing.T) {
				key := "conditional-delete-" + signature
				info := put(key, "keep until a matching delete", "")
				target := "http://otterio.test/" + bucket + "/" + key
				for _, tc := range []struct {
					match string
					want  int
				}{{`"stale"`, http.StatusPreconditionFailed}, {`"` + info.ETag + `"`, http.StatusNoContent}, {info.ETag, http.StatusNotFound}} {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, conditionalMatchRequest(t, signature, http.MethodDelete, target, "", credentials, http.Header{xhttp.IfMatch: {tc.match}}))
					if response.Code != tc.want {
						t.Fatalf("delete condition %q: got %d, want %d: %s", tc.match, response.Code, tc.want, response.Body.String())
					}
					if tc.want == http.StatusPreconditionFailed {
						conditionalMatchStored(t, obj, bucket, key, "keep until a matching delete")
					}
					if tc.want >= 300 {
						conditionalMatchMetadataHeadersAbsent(t, response)
					}
					if conditionalMatchResponseHeader(response.Header(), conditionalDeleteHeader) != "v1" {
						t.Fatal("real route lost the conditional delete capability")
					}
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, conditionalMatchRequest(t, signature, http.MethodDelete, target, "", credentials, nil))
				if response.Code != http.StatusNoContent {
					t.Fatalf("ordinary DELETE of missing object changed: %d", response.Code)
				}
			})
		}
		t.Run(backend+"/reject-unsupported-conditions", func(t *testing.T) {
			info := put("delete-rejections", "preserve", "")
			for _, tc := range []struct {
				name, query string
				headers     http.Header
				want        int
			}{
				{"wildcard", "", http.Header{xhttp.IfMatch: {"*"}}, http.StatusNotImplemented},
				{"weak", "", http.Header{xhttp.IfMatch: {`W/"` + info.ETag + `"`}}, http.StatusNotImplemented},
				{"list", "", http.Header{xhttp.IfMatch: {`"` + info.ETag + `", "other"`}}, http.StatusNotImplemented},
				{"duplicate", "", http.Header{xhttp.IfMatch: {info.ETag, info.ETag}}, http.StatusBadRequest},
				{"conflicting", "", http.Header{xhttp.IfMatch: {info.ETag}, xhttp.IfNoneMatch: {"*"}}, http.StatusBadRequest},
				{"if-none-match", "", http.Header{xhttp.IfNoneMatch: {"*"}}, http.StatusNotImplemented},
				{"explicit-null", "?versionId=null", http.Header{xhttp.IfMatch: {info.ETag}}, http.StatusNotImplemented},
				{"explicit-empty", "?versionId=", http.Header{xhttp.IfMatch: {info.ETag}}, http.StatusNotImplemented},
				{"import", "", http.Header{xhttp.IfMatch: {info.ETag}, http.CanonicalHeaderKey(xhttp.OtterIOSourceDeleteMarker): {"true"}}, http.StatusNotImplemented},
				{"replica", "", http.Header{xhttp.IfMatch: {info.ETag}, xhttp.AmzBucketReplicationStatus: {"REPLICA"}}, http.StatusNotImplemented},
				{"bypass-retention", "", http.Header{xhttp.IfMatch: {info.ETag}, xhttp.AmzObjectLockBypassGovernance: {"true"}}, http.StatusNotImplemented},
				{"force-delete", "", http.Header{xhttp.IfMatch: {info.ETag}, http.CanonicalHeaderKey(xhttp.OtterIOForceDelete): {"true"}}, http.StatusNotImplemented},
			} {
				t.Run(tc.name, func(t *testing.T) {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, "http://otterio.test/"+bucket+"/delete-rejections"+tc.query, "", credentials, tc.headers))
					if response.Code != tc.want {
						t.Fatalf("got %d, want %d: %s", response.Code, tc.want, response.Body.String())
					}
					conditionalMatchStored(t, obj, bucket, "delete-rejections", "preserve")
				})
			}
		})
		t.Run(backend+"/both-actions-use-stored-tags", func(t *testing.T) {
			// No PutObject permission: deletion must never use the write CAS
			// authorization callback even though the ETag comparator is shared.
			user := consoleReviewUser(t, obj, "conditional-delete-tags", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::`+bucket+`/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`)
			for _, tag := range []string{"prod", "dev"} {
				key := "delete-tag-" + tag
				info := put(key, "source", "environment="+tag)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, "http://otterio.test/"+bucket+"/"+key, "", user, http.Header{xhttp.IfMatch: {info.ETag}, xhttp.AmzObjectTagging: {"environment=prod"}}))
				want := http.StatusForbidden
				if tag == "prod" {
					want = http.StatusNoContent
				}
				if response.Code != want {
					t.Fatalf("stored tag %s: %d want %d: %s", tag, response.Code, want, response.Body.String())
				}
				if tag == "dev" {
					conditionalMatchStored(t, obj, bucket, key, "source")
					conditionalMatchMetadataHeadersAbsent(t, response)
				}
			}
			for _, action := range []string{"s3:GetObject", "s3:DeleteObject"} {
				user := consoleReviewUser(t, obj, "delete-one-action-"+strings.ToLower(strings.TrimPrefix(action, "s3:")), `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["`+action+`"],"Resource":["arn:aws:s3:::`+bucket+`/*"]}]}`)
				info := put("requires-both-permissions", "keep", "")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, "http://otterio.test/"+bucket+"/requires-both-permissions", "", user, http.Header{xhttp.IfMatch: {info.ETag}}))
				if response.Code != http.StatusForbidden {
					t.Fatalf("only %s authorized deletion: %d", action, response.Code)
				}
				conditionalMatchStored(t, obj, bucket, "requires-both-permissions", "keep")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, "http://otterio.test/"+bucket+"/missing-forbidden", "", user, http.Header{xhttp.IfMatch: {"old"}}))
			if response.Code != http.StatusForbidden {
				t.Fatalf("missing source metadata leaked to tag-denied caller: %d", response.Code)
			}
		})
	}, nil)
}

func TestConditionalDeleteAuthenticatesBeforeLookup(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, _ http.Handler, credentials auth.Credentials, t *testing.T) {
		for _, mode := range []string{"signature", "unsigned-condition", "unknown-user", "invalid-token", "anonymous", "v2", "unsupported-backend"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				want := http.StatusForbidden
				if mode == "unsupported-backend" {
					saved := globalIsDistErasure
					globalIsDistErasure = true
					t.Cleanup(func() { globalIsDistErasure = saved })
					want = http.StatusNotImplemented
				}
				layer := &ifMatchReviewLookupLayer{ObjectLayer: obj}
				api := objectAPIHandlers{ObjectAPI: func() ObjectLayer { return layer }, CacheAPI: func() CacheObjectLayer { return nil }}
				headers := http.Header{xhttp.IfMatch: {`"old"`}}
				user, signature := credentials, "v4"
				switch mode {
				case "unsigned-condition":
					headers.Del(xhttp.IfMatch)
				case "unknown-user":
					user = auth.Credentials{AccessKey: "unknown-user", SecretKey: "unknown-secret-key"}
				case "invalid-token":
					headers.Set(xhttp.AmzSecurityToken, "invalid-session-token")
				case "v2":
					signature = "v2"
				}
				request := conditionalMatchRequest(t, signature, http.MethodDelete, "http://otterio.test/"+bucket+"/object", "", user, headers)
				switch mode {
				case "signature":
					request.Header.Set(xhttp.IfMatch, `"changed-after-signing"`)
				case "unsigned-condition":
					request.Header.Set(xhttp.IfMatch, `"old"`)
				case "anonymous":
					request.Header.Del(xhttp.Authorization)
				}
				request = setURLVarsOnRequest(request, map[string]string{"bucket": bucket, "object": "object"})
				response := httptest.NewRecorder()
				api.DeleteObjectHandler(response, request)
				if response.Code != want || layer.lookups != 0 {
					t.Fatalf("invalid delete read origin: status=%d lookups=%d: %s", response.Code, layer.lookups, response.Body.String())
				}
			})
		}
	}, nil)
}

func TestConditionalDeleteBackend(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, backend string, _ TestErrHandler) {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			const bucket = "conditional-delete-backend"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			put := func(key, body string, opts ObjectOptions) ObjectInfo {
				t.Helper()
				info, err := obj.PutObject(ctx, bucket, key, ifMatchBackendReader(t, body), opts)
				if err != nil {
					t.Fatal(err)
				}
				return info
			}
			t.Run("stale-source-is-preserved", func(t *testing.T) {
				old := put("source", "copied source", ObjectOptions{})
				fresh := put("source", "concurrent update", ObjectOptions{})
				if _, err := obj.DeleteObject(ctx, bucket, "source", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: old.ETag}}); !isErrPreconditionFailed(err) {
					t.Fatalf("stale source deletion: %v", err)
				}
				ifMatchBackendContents(t, obj, bucket, "source", "concurrent update")
				if _, err := obj.DeleteObject(ctx, bucket, "source", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: fresh.ETag}}); err != nil {
					t.Fatal(err)
				}
				if _, err := obj.DeleteObject(ctx, bucket, "source", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: fresh.ETag}}); !isErrObjectNotFound(err) {
					t.Fatalf("missing source acknowledged as conditionally deleted: %v", err)
				}
			})
			t.Run("authorization-rechecks-current-tags", func(t *testing.T) {
				old := put("tags", "unchanged data", ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}})
				if _, err := obj.PutObjectTags(ctx, bucket, "tags", "environment=dev", ObjectOptions{}); err != nil {
					t.Fatal(err)
				}
				called := false
				_, err := obj.DeleteObject(ctx, bucket, "tags", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: old.ETag, Authorize: func(actual ObjectInfo) error {
					called = true
					if actual.UserTags != "environment=dev" {
						t.Fatalf("stale authorization metadata: %q", actual.UserTags)
					}
					return errAuthentication
				}}})
				if err != errAuthentication || !called {
					t.Fatalf("delete bypassed locked authorization: called=%v err=%v", called, err)
				}
				ifMatchBackendContents(t, obj, bucket, "tags", "unchanged data")
			})
			t.Run("condition-and-delete-share-lock", func(t *testing.T) {
				old := put("locked", "old", ObjectOptions{})
				checked, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				deleted, updated := make(chan error, 1), make(chan error, 1)
				go func() {
					_, err := obj.DeleteObject(ctx, bucket, "locked", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: old.ETag, Authorize: func(ObjectInfo) error {
						close(checked)
						<-release
						return nil
					}}})
					deleted <- err
				}()
				select {
				case <-checked:
				case err := <-deleted:
					t.Fatalf("delete skipped condition: %v", err)
				case <-time.After(10 * time.Second):
					t.Fatal("delete did not reach locked condition")
				}
				reader := ifMatchBackendReader(t, "new after delete")
				go func() {
					_, err := obj.PutObject(ctx, bucket, "locked", reader, ObjectOptions{})
					updated <- err
				}()
				select {
				case err := <-updated:
					unblock()
					<-deleted
					t.Fatalf("update escaped the delete lock: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				unblock()
				if err := <-deleted; err != nil {
					t.Fatal(err)
				}
				if err := <-updated; err != nil {
					t.Fatal(err)
				}
				ifMatchBackendContents(t, obj, bucket, "locked", "new after delete")
			})
			if backend == ErasureTestStr {
				t.Run("versioned-delete-retains-history", func(t *testing.T) {
					const versioned = "conditional-delete-versioned"
					if err := obj.MakeBucketWithLocation(ctx, versioned, BucketOptions{VersioningEnabled: true}); err != nil {
						t.Fatal(err)
					}
					old, err := obj.PutObject(ctx, versioned, "source", ifMatchBackendReader(t, "retained history"), ObjectOptions{Versioned: true})
					if err != nil {
						t.Fatal(err)
					}
					marker, err := obj.DeleteObject(ctx, versioned, "source", ObjectOptions{Versioned: true, IfMatch: &ObjectIfMatchCondition{ETag: old.ETag}})
					if err != nil || !marker.DeleteMarker || marker.VersionID == "" {
						t.Fatalf("current conditional deletion did not create a marker: %+v %v", marker, err)
					}
					retained, err := obj.GetObjectInfo(ctx, versioned, "source", ObjectOptions{VersionID: old.VersionID})
					if err != nil || retained.ETag != old.ETag {
						t.Fatalf("conditional deletion purged history: %+v %v", retained, err)
					}
					if _, err := obj.DeleteObject(ctx, versioned, "source", ObjectOptions{Versioned: true, VersionID: old.VersionID, IfMatch: &ObjectIfMatchCondition{ETag: old.ETag}}); err == nil {
						t.Fatal("conditional deletion accepted an explicit historical version")
					}
				})
			}
		})
	})
}

func TestConditionalDeleteLockedScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts ObjectOptions
	}{
		{"wildcard", ObjectOptions{IfMatch: &ObjectIfMatchCondition{Any: true}}},
		{"create-only", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: "etag"}, RequireNewObject: true}},
		{"create-only-without-match", ObjectOptions{RequireNewObject: true}},
		{"explicit-version", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: "etag"}, VersionID: "null"}},
		{"import-marker", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: "etag"}, DeleteMarker: true}},
		{"replica", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: "etag"}, DeleteMarkerReplicationStatus: "REPLICA"}},
		{"transition", ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: "etag"}, TransitionStatus: "complete"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			err := checkObjectDeletePreconditions(context.Background(), "bucket", "object", tc.opts, func(context.Context, string, string, ObjectOptions) (ObjectInfo, error) {
				called = true
				return ObjectInfo{ETag: "etag"}, nil
			})
			if _, ok := err.(NotImplemented); !ok || called {
				t.Fatalf("unsupported delete inspected storage: %v called=%v", err, called)
			}
		})
	}
}

func TestConditionalDeleteProtectedObjects(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, backend, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
		ctx := context.Background()
		t.Run(backend+"/replication-configuration-refused", func(t *testing.T) {
			info, err := obj.PutObject(ctx, bucket, "replicated", ifMatchBackendReader(t, "preserve"), ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			meta, err := globalBucketMetadataSys.GetConfig(bucket)
			if err != nil {
				t.Fatal(err)
			}
			defer globalBucketMetadataSys.Set(bucket, meta)
			configured := meta
			configured.replicationConfig = &replication.Config{}
			globalBucketMetadataSys.Set(bucket, configured)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, "http://otterio.test/"+bucket+"/replicated", "", credentials, http.Header{xhttp.IfMatch: {info.ETag}}))
			if response.Code != http.StatusNotImplemented {
				t.Fatalf("replication configured deletion accepted: %d %s", response.Code, response.Body.String())
			}
			conditionalMatchStored(t, obj, bucket, "replicated", "preserve")
		})
		if obj.IsEncryptionSupported() {
			t.Run(backend+"/encrypted-head-validator", func(t *testing.T) {
				oldKMS := GlobalKMS
				GlobalKMS = conditionalMatchTestKMS(t)
				defer func() { GlobalKMS = oldKMS }()
				for _, mode := range []string{"S3", "KMS"} {
					target := "http://otterio.test/" + bucket + "/delete-encrypted-" + mode
					headers := http.Header{xhttp.AmzServerSideEncryption: {xhttp.AmzEncryptionAES}}
					if mode == "KMS" {
						headers.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionKMS)
						headers.Set(xhttp.AmzServerSideEncryptionKmsID, conditionalMatchTestKeyID)
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodPut, target, "encrypted original", credentials, headers))
					if response.Code != http.StatusOK {
						t.Fatalf("create %s: %d %s", mode, response.Code, response.Body.String())
					}
					head := httptest.NewRecorder()
					router.ServeHTTP(head, conditionalMatchRequest(t, "v4", http.MethodHead, target, "", credentials, nil))
					etag := conditionalMatchResponseHeader(head.Header(), xhttp.ETag)
					if head.Code != http.StatusOK || etag == "" {
						t.Fatalf("read %s validator: %d %s", mode, head.Code, head.Body.String())
					}
					response = httptest.NewRecorder()
					router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, target, "", credentials, http.Header{xhttp.IfMatch: {etag}}))
					if response.Code != http.StatusNoContent {
						t.Fatalf("delete %s with HEAD validator: %d %s", mode, response.Code, response.Body.String())
					}
				}
			})
		}
		if backend == ErasureTestStr {
			t.Run(backend+"/locked-history-retained", func(t *testing.T) {
				const locked = "conditional-delete-locked"
				if err := obj.MakeBucketWithLocation(ctx, locked, BucketOptions{LockEnabled: true, VersioningEnabled: true}); err != nil {
					t.Fatal(err)
				}
				if err := globalBucketMetadataSys.Update(locked, bucketVersioningConfig, []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)); err != nil {
					t.Fatal(err)
				}
				metadata := map[string]string{
					strings.ToLower(xhttp.AmzObjectLockMode):            "COMPLIANCE",
					strings.ToLower(xhttp.AmzObjectLockRetainUntilDate): UTCNow().Add(24 * time.Hour).Format(iso8601TimeFormat),
					strings.ToLower(xhttp.AmzObjectLockLegalHold):       "ON",
				}
				info, err := obj.PutObject(ctx, locked, "source", ifMatchBackendReader(t, "protected history"), ObjectOptions{Versioned: true, UserDefined: metadata})
				if err != nil {
					t.Fatal(err)
				}
				target := "http://otterio.test/" + locked + "/source"
				response := httptest.NewRecorder()
				router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, target, "", credentials, http.Header{xhttp.IfMatch: {info.ETag}}))
				if response.Code != http.StatusNoContent || conditionalMatchResponseHeader(response.Header(), xhttp.AmzDeleteMarker) != "true" {
					t.Fatalf("versioned delete did not create a marker: %d %v %s", response.Code, response.Header(), response.Body.String())
				}
				retained, err := obj.GetObjectInfo(ctx, locked, "source", ObjectOptions{VersionID: info.VersionID})
				if err != nil || retained.ETag != info.ETag {
					t.Fatalf("delete removed locked history: %+v %v", retained, err)
				}
				for key, value := range metadata {
					if retained.UserDefined[key] != value {
						t.Fatalf("delete altered protected %s", key)
					}
				}
				response = httptest.NewRecorder()
				router.ServeHTTP(response, conditionalMatchRequest(t, "v4", http.MethodDelete, target, "", credentials, http.Header{xhttp.IfMatch: {info.ETag}}))
				if response.Code != http.StatusNotFound {
					t.Fatalf("current delete marker satisfied a content condition: %d %s", response.Code, response.Body.String())
				}
			})
		}
	}, nil)
}
