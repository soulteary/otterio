// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	"github.com/soulteary/otterio/cmd/config/cache"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

type consoleVersionCacheFixture struct {
	layer         *cacheObjects
	disk          *diskCache
	metadataReads atomic.Int64
	bodyReads     atomic.Int64
}

func newConsoleVersionCacheFixture(ctx context.Context, t *testing.T, obj ObjectLayer) *consoleVersionCacheFixture {
	t.Helper()
	disk, err := newDiskCache(ctx, t.TempDir(), cache.Config{Quota: 100, WatermarkLow: 100, WatermarkHigh: 100})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &consoleVersionCacheFixture{disk: disk}
	fixture.layer = &cacheObjects{
		cache: []*diskCache{disk}, cacheStats: newCacheStats(),
		// The tests explicitly populate cache. Avoid background fills when
		// asserting an identity mismatch and inspecting the backend read count.
		after: 100,
		InnerGetObjectNInfoFn: func(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, lock LockType, opts ObjectOptions) (*GetObjectReader, error) {
			fixture.bodyReads.Add(1)
			return obj.GetObjectNInfo(ctx, bucket, object, rs, h, lock, opts)
		},
		InnerGetObjectInfoFn: func(ctx context.Context, bucket, object string, opts ObjectOptions) (ObjectInfo, error) {
			fixture.metadataReads.Add(1)
			return obj.GetObjectInfo(ctx, bucket, object, opts)
		},
	}
	return fixture
}

func (f *consoleVersionCacheFixture) seed(ctx context.Context, t *testing.T, bucket, object, body string, info ObjectInfo) {
	t.Helper()
	if _, err := f.disk.Put(ctx, bucket, object, strings.NewReader(body), int64(len(body)), nil, ObjectOptions{UserDefined: getMetadata(info)}, false); err != nil {
		t.Fatal(err)
	}
}

func consoleVersionCacheRequest(t *testing.T, signature, method, target string, credentials auth.Credentials, headers map[string]string) *http.Request {
	t.Helper()
	var request *http.Request
	var err error
	switch signature {
	case "v2":
		request, err = newTestSignedRequestV2(method, target, 0, strings.NewReader(""), credentials.AccessKey, credentials.SecretKey, headers)
	case "presigned":
		request, err = newTestRequest(method, target, 0, strings.NewReader(""))
		if err == nil {
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			request = signer.PreSignV4(*request, credentials.AccessKey, credentials.SecretKey, "", globalServerRegion, 60)
		}
	default:
		request, err = newTestSignedRequestV4(method, target, 0, strings.NewReader(""), credentials.AccessKey, credentials.SecretKey, headers)
	}
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func consoleVersionCachePolicy(t *testing.T, user auth.Credentials, name, document string) {
	t.Helper()
	parsed, err := iampolicy.ParseConfig(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.SetPolicy(name, *parsed); err != nil {
		t.Fatal(err)
	}
	if err = globalIAMSys.PolicyDBSet(user.AccessKey, name, false); err != nil {
		t.Fatal(err)
	}
}

func consoleVersionCacheHeaderValue(response *httptest.ResponseRecorder, name string) string {
	// The Fiber test bridge preserves the production spelling of header keys.
	// HTTP header names are case insensitive, including lower-case S3 headers.
	for key, values := range response.Header() {
		if strings.EqualFold(key, name) {
			return strings.Join(values, ",")
		}
	}
	return ""
}

func assertConsoleVersionCacheDenied(t *testing.T, method string, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", response.Code, response.Body.String())
	}
	if consoleVersionCacheHeaderValue(response, xhttp.ETag) != "" || consoleVersionCacheHeaderValue(response, xhttp.AmzVersionID) != "" || consoleVersionCacheHeaderValue(response, xhttp.LastModified) != "" {
		t.Fatalf("denied read disclosed object headers: %v", response.Header())
	}
	if method == http.MethodGet {
		var apiError APIErrorResponse
		if err := xml.Unmarshal(response.Body.Bytes(), &apiError); err != nil || apiError.Code != "AccessDenied" {
			t.Fatalf("want AccessDenied, got %s", response.Body.String())
		}
	} else if response.Body.Len() != 0 {
		t.Fatalf("HEAD denial has a body: %s", response.Body.String())
	}
}

// Cached data must retain ordinary current-object hits, while all permissions
// and response metadata use the current backend snapshot. Tags can change
// without changing ETag, size or modification time.
func TestConsoleVersionAuthorizationCacheTagsHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testConsoleVersionAuthorizationCacheTagsHTTPStorage, nil)
}

func testConsoleVersionAuthorizationCacheTagsHTTPStorage(obj ObjectLayer, backend, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previousCache := newCachedObjectLayerFn()
	defer setCacheObjectLayer(previousCache)
	globalIAMSys.InitStore(obj)
	user := auth.Credentials{AccessKey: "version-cache-user", SecretKey: "version-cache-secret"}
	if err := globalIAMSys.CreateUser(user.AccessKey, madmin.UserInfo{SecretKey: user.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	allowProd := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`
	denyProd := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"]},{"Effect":"Deny","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::` + bucket + `/*"],"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"prod"}}}]}`
	consoleVersionCachePolicy(t, user, "cache-allow-prod", allowProd)
	consoleVersionCachePolicy(t, user, "cache-deny-prod", denyProd)
	fixture := newConsoleVersionCacheFixture(ctx, t, obj)
	setCacheObjectLayer(fixture.layer)
	for _, signature := range []string{"v4", "v2", "presigned"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, test := range []struct {
				name, cachedTag, actualTag, requestTag, policy string
				allowed                                        bool
			}{
				{"valid-prod", "prod", "prod", "", "cache-allow-prod", true},
				{"valid-prod-request-dev", "prod", "prod", "dev", "cache-allow-prod", true},
				{"stored-prod-explicit-deny", "prod", "prod", "", "cache-deny-prod", false},
				{"tag-changed-to-dev", "prod", "dev", "prod", "cache-allow-prod", false},
				{"tag-changed-to-prod", "dev", "prod", "", "cache-allow-prod", true},
				{"tag-deleted-forged-prod", "prod", "", "prod", "cache-allow-prod", false},
			} {
				t.Run(backend+"/"+signature+"/"+method+"/"+test.name, func(t *testing.T) {
					if err := globalIAMSys.PolicyDBSet(user.AccessKey, test.policy, false); err != nil {
						t.Fatal(err)
					}
					object := fmt.Sprintf("cache-%s-%s-%s", signature, method, test.name)
					body := "cached object data " + object
					info, err := obj.PutObject(ctx, bucket, object, mustGetPutObjReader(t, strings.NewReader(body), int64(len(body)), "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=" + test.cachedTag, "cache-control": "max-age=3600"}})
					if err != nil {
						t.Fatal(err)
					}
					fixture.seed(ctx, t, bucket, object, body, info)
					if test.actualTag == "" {
						_, err = obj.DeleteObjectTags(ctx, bucket, object, ObjectOptions{})
					} else {
						_, err = obj.PutObjectTags(ctx, bucket, object, "environment="+test.actualTag, ObjectOptions{})
					}
					if err != nil {
						t.Fatal(err)
					}
					actual, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{})
					if err != nil || actual.ETag != info.ETag || actual.Size != info.Size || !actual.ModTime.Equal(info.ModTime) {
						t.Fatalf("tag-only fixture changed data identity: before=%+v after=%+v err=%v", info, actual, err)
					}
					headers := map[string]string{}
					if test.requestTag != "" {
						headers[xhttp.AmzObjectTagging] = "environment=" + test.requestTag
					}
					beforeHits, beforeMetadata, beforeBodies := fixture.layer.cacheStats.getHits(), fixture.metadataReads.Load(), fixture.bodyReads.Load()
					response := httptest.NewRecorder()
					router.ServeHTTP(response, consoleVersionCacheRequest(t, signature, method, "http://otterio.test/"+bucket+"/"+object, user, headers))
					if !test.allowed {
						assertConsoleVersionCacheDenied(t, method, response)
					} else {
						if response.Code != http.StatusOK || (method == http.MethodGet && response.Body.String() != body) {
							t.Fatalf("want authorized data, got %d: %s", response.Code, response.Body.String())
						}
						if method == http.MethodGet && fixture.layer.cacheStats.getHits() != beforeHits+1 {
							t.Fatal("ordinary current-object GET did not retain a data cache hit")
						}
					}
					if fixture.metadataReads.Load() != beforeMetadata+1 || fixture.bodyReads.Load() != beforeBodies {
						t.Fatalf("want one authoritative metadata read and no backend data read; metadata=%d body=%d", fixture.metadataReads.Load()-beforeMetadata, fixture.bodyReads.Load()-beforeBodies)
					}
				})
			}
		}
	}
}

func TestConsoleVersionAuthorizationCacheVersionHTTPStorage(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testConsoleVersionAuthorizationCacheVersionHTTPStorage, nil)
}

func testConsoleVersionAuthorizationCacheVersionHTTPStorage(obj ObjectLayer, backend, bucket string, router http.Handler, root auth.Credentials, t *testing.T) {
	if backend == FSTestStr {
		return // FS cannot enable versioning.
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previousCache := newCachedObjectLayerFn()
	defer setCacheObjectLayer(previousCache)
	globalIAMSys.InitStore(obj)
	user := auth.Credentials{AccessKey: "cache-history-user", SecretKey: "cache-history-secret"}
	if err := globalIAMSys.CreateUser(user.AccessKey, madmin.UserInfo{SecretKey: user.SecretKey, Status: madmin.AccountEnabled}); err != nil {
		t.Fatal(err)
	}
	const object, nullData, oldData, currentData = "cached-versions", "null bytes", "old bytes", "current confidential bytes"
	put := func(body string, versioned bool) ObjectInfo {
		t.Helper()
		info, err := obj.PutObject(ctx, bucket, object, mustGetPutObjReader(t, strings.NewReader(body), int64(len(body)), "", ""), ObjectOptions{Versioned: versioned, UserDefined: map[string]string{"cache-control": "max-age=3600"}})
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	put(nullData, false)
	if err := globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)); err != nil {
		t.Fatal(err)
	}
	old, current := put(oldData, true), put(currentData, true)
	if old.VersionID == "" || current.VersionID == "" || old.VersionID == current.VersionID {
		t.Fatal("fixture did not create distinct stored versions")
	}
	document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObjectVersion"],"Resource":["arn:aws:s3:::` + bucket + `/*"],"Condition":{"StringEquals":{"s3:versionid":["` + old.VersionID + `","null"]}}}]}`
	consoleVersionCachePolicy(t, user, "cache-only-old-history", document)
	fixture := newConsoleVersionCacheFixture(ctx, t, obj)
	fixture.seed(ctx, t, bucket, object, currentData, current)
	setCacheObjectLayer(fixture.layer)
	for _, signature := range []string{"v4", "v2", "presigned"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, test := range []struct {
				name, version, body, responseVersion string
				owner                                bool
			}{
				{"old-authorized", old.VersionID, oldData, old.VersionID, false},
				{"null-authorized", "null", nullData, "null", false},
				{"old-padded-authorized", " \t" + old.VersionID + "\u2003", oldData, old.VersionID, false},
				{"history-current-denied", "", "", "", false},
				{"history-blank-denied", " \t\u2003", "", "", false},
				{"history-other-version-denied", current.VersionID, "", "", false},
				{"owner-current", "", currentData, current.VersionID, true},
				{"owner-current-version", current.VersionID, currentData, current.VersionID, true},
			} {
				t.Run(backend+"/"+signature+"/"+method+"/"+test.name, func(t *testing.T) {
					credentials := user
					if test.owner {
						credentials = root
					}
					query := url.Values{}
					if test.version != "" {
						query.Set(xhttp.VersionID, test.version)
					}
					beforeBodies := fixture.bodyReads.Load()
					response := httptest.NewRecorder()
					router.ServeHTTP(response, consoleVersionCacheRequest(t, signature, method, "http://otterio.test/"+bucket+"/"+object+"?"+query.Encode(), credentials, nil))
					if test.body == "" {
						assertConsoleVersionCacheDenied(t, method, response)
					} else if response.Code != http.StatusOK || consoleVersionCacheHeaderValue(response, xhttp.AmzVersionID) != test.responseVersion || (method == http.MethodGet && response.Body.String() != test.body) || response.Header().Get(xhttp.ContentLength) != fmt.Sprint(len(test.body)) {
						t.Fatalf("wrong selected version/data: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
					}
					if fixture.layer.cacheStats.getHits() != 0 || (method == http.MethodGet && fixture.bodyReads.Load() != beforeBodies+1) {
						t.Fatal("versioned read did not bypass the name-only data cache")
					}
				})
			}
		}
	}
}

func TestConsoleVersionAuthorizationCacheMetadataFailureAndIdentity(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, testConsoleVersionAuthorizationCacheMetadataFailureAndIdentity, nil)
}

func testConsoleVersionAuthorizationCacheMetadataFailureAndIdentity(obj ObjectLayer, backend, bucket string, router http.Handler, root auth.Credentials, t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previousCache := newCachedObjectLayerFn()
	defer setCacheObjectLayer(previousCache)
	fixture := newConsoleVersionCacheFixture(ctx, t, obj)
	setCacheObjectLayer(fixture.layer)
	const object, body = "cache-identity", "identical bytes with different write timestamps"
	put := func(mtime time.Time) ObjectInfo {
		t.Helper()
		info, err := obj.PutObject(ctx, bucket, object, mustGetPutObjReader(t, strings.NewReader(body), int64(len(body)), "", ""), ObjectOptions{MTime: mtime, UserDefined: map[string]string{"cache-control": "max-age=3600"}})
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	before := put(time.Time{})
	fixture.seed(ctx, t, bucket, object, body, before)
	after := put(before.ModTime.Add(time.Nanosecond))
	if before.ETag != after.ETag || before.Size != after.Size || before.ModTime.Equal(after.ModTime) {
		t.Fatal("fixture requires same ETag/size and a distinct subsecond backend write timestamp")
	}
	t.Run(backend+"/subsecond-identity-change", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, consoleVersionCacheRequest(t, "v4", http.MethodGet, "http://otterio.test/"+bucket+"/"+object, root, nil))
		if response.Code != http.StatusOK || response.Body.String() != body || fixture.bodyReads.Load() != 1 || fixture.layer.cacheStats.getHits() != 0 {
			t.Fatalf("different actual write reused the cached entry: status=%d backendReads=%d hits=%d", response.Code, fixture.bodyReads.Load(), fixture.layer.cacheStats.getHits())
		}
	})
	fixture.seed(ctx, t, bucket, object, body, after)
	fixture.layer.InnerGetObjectInfoFn = func(context.Context, string, string, ObjectOptions) (ObjectInfo, error) {
		return ObjectInfo{}, BackendDown{}
	}
	fixture.layer.InnerGetObjectNInfoFn = func(context.Context, string, string, *HTTPRangeSpec, http.Header, LockType, ObjectOptions) (*GetObjectReader, error) {
		return nil, BackendDown{}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(backend+"/backend-down/"+method, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, consoleVersionCacheRequest(t, "v4", method, "http://otterio.test/"+bucket+"/"+object, root, nil))
			if response.Code == http.StatusOK || strings.Contains(response.Body.String(), body) || response.Header().Get(xhttp.ETag) != "" || fixture.layer.cacheStats.getHits() != 0 {
				t.Fatalf("backend failure fell back to cache: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}
