// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/soulteary/otterio/cmd/config/dns"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

type conditionalCopyDNSStore struct{ dns.Store }

func TestConditionalCopyCapability(t *testing.T) {
	object, cache := newObjectLayerFn(), newCachedObjectLayerFn()
	gateway, distributed, federation := globalIsGateway, globalIsDistErasure, globalBucketFederation
	dnsConfig := globalDNSConfig
	t.Cleanup(func() {
		setObjectLayer(object)
		setCacheObjectLayer(cache)
		globalIsGateway, globalIsDistErasure, globalBucketFederation = gateway, distributed, federation
		globalDNSConfig = dnsConfig
	})
	for _, test := range []struct {
		name                             string
		object                           ObjectLayer
		cache                            CacheObjectLayer
		gateway, distributed, federation bool
		dnsConfigured                    bool
		want                             bool
	}{
		{name: "filesystem", object: &FSObjects{}, want: true},
		{name: "single-pool", object: &erasureServerPools{serverPools: []*erasureSets{{}}}, want: true},
		{name: "multiple-pools", object: &erasureServerPools{serverPools: []*erasureSets{{}, {}}}},
		{name: "distributed", object: &FSObjects{}, distributed: true},
		{name: "gateway", object: &FSObjects{}, gateway: true},
		{name: "federation", object: &FSObjects{}, federation: true},
		{name: "dns-configured", object: &FSObjects{}, dnsConfigured: true},
		{name: "read-cache", object: &FSObjects{}, cache: &cacheObjects{}},
		{name: "writeback-cache", object: &FSObjects{}, cache: &cacheObjects{commitWriteback: true}},
		{name: "uninitialized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setObjectLayer(test.object)
			setCacheObjectLayer(test.cache)
			globalIsGateway, globalIsDistErasure, globalBucketFederation = test.gateway, test.distributed, test.federation
			globalDNSConfig = nil
			if test.dnsConfigured {
				globalDNSConfig = &conditionalCopyDNSStore{}
			}
			if got := conditionalCopyWritesSupported(); got != test.want {
				t.Fatalf("capability=%v want %v", got, test.want)
			}
			response := httptest.NewRecorder()
			setCommonHeaders(response)
			if got := response.Header().Get(conditionalCopyWritesHeader) == "v1"; got != test.want {
				t.Fatalf("advertised=%v want %v", got, test.want)
			}
			if err := checkConditionalCopyOptions(ObjectOptions{RequireNewObject: true}); (err == nil) != test.want {
				t.Fatalf("object-layer support: %v", err)
			}
		})
	}
}

func TestConditionalCopyConditionValidation(t *testing.T) {
	object, cache := newObjectLayerFn(), newCachedObjectLayerFn()
	gateway, distributed, federation := globalIsGateway, globalIsDistErasure, globalBucketFederation
	dnsConfig := globalDNSConfig
	t.Cleanup(func() {
		setObjectLayer(object)
		setCacheObjectLayer(cache)
		globalIsGateway, globalIsDistErasure, globalBucketFederation = gateway, distributed, federation
		globalDNSConfig = dnsConfig
	})
	setObjectLayer(&FSObjects{})
	setCacheObjectLayer(nil)
	globalIsGateway, globalIsDistErasure, globalBucketFederation = false, false, false
	globalDNSConfig = nil
	for _, test := range []struct {
		name      string
		values    []string
		match     string
		version   bool
		imported  bool
		unsigned  bool
		anonymous bool
		code      APIErrorCode
	}{
		{name: "none"},
		{name: "create", values: []string{"*"}},
		{name: "match", match: `"opaque"`, code: ErrNotImplemented},
		{name: "mixed", values: []string{"*"}, match: `"opaque"`, code: ErrNotImplemented},
		{name: "empty", values: []string{""}, code: ErrNotImplemented},
		{name: "etag", values: []string{`"opaque"`}, code: ErrNotImplemented},
		{name: "list", values: []string{"*,*"}, code: ErrNotImplemented},
		{name: "duplicate", values: []string{"*", "*"}, code: ErrNotImplemented},
		{name: "destination-version", values: []string{"*"}, version: true, code: ErrNotImplemented},
		{name: "imported", values: []string{"*"}, imported: true, code: ErrNotImplemented},
		{name: "unsigned", values: []string{"*"}, unsigned: true, code: ErrUnsignedHeaders},
		{name: "anonymous", values: []string{"*"}, anonymous: true, code: ErrAccessDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := newTestSignedRequestV4(http.MethodPut, "/bucket/destination", 0, nil,
				"minioadmin", "minioadmin", map[string]string{xhttp.IfNoneMatch: "*"})
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Del(xhttp.IfNoneMatch)
			for _, value := range test.values {
				request.Header.Add(xhttp.IfNoneMatch, value)
			}
			if test.match != "" {
				request.Header.Set(xhttp.IfMatch, test.match)
			}
			if test.version {
				request.URL.RawQuery = "versionId="
			}
			if test.imported {
				request.Header.Set(xhttp.OtterIOSourceMTime, "2026-11-28T00:00:00Z")
			}
			if test.unsigned {
				request.Header.Set(xhttp.Authorization, strings.ReplaceAll(request.Header.Get(xhttp.Authorization), ";if-none-match", ""))
			}
			if test.anonymous {
				request.Header.Del(xhttp.Authorization)
			}
			var opts ObjectOptions
			if code := setObjectCopyWriteConditions(request, &opts); code != test.code {
				t.Fatalf("code=%v want %v", code, test.code)
			}
			if opts.RequireNewObject != (test.name == "create") {
				t.Fatalf("condition applied to invalid/unconditional request: %+v", opts)
			}
		})
	}
	for _, opts := range []ObjectOptions{
		{IfMatch: &ObjectIfMatchCondition{ETag: "opaque"}},
		{RequireNewObject: true, VersionID: "null"},
	} {
		if err := checkConditionalCopyOptions(opts); err == nil {
			t.Fatal("object layer accepted an unsupported destination condition")
		}
	}
}

func TestConditionalCopyBackend(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx := context.Background()
			const bucket = "conditional-copy"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			source, err := obj.PutObject(ctx, bucket, "source", ifMatchBackendReader(t, "source bytes"), ObjectOptions{UserDefined: map[string]string{"x-amz-meta-description": "preserved"}})
			if err != nil {
				t.Fatal(err)
			}
			copyTo := func(key string, opts ObjectOptions) error {
				info := source.Clone()
				info.PutObjReader = ifMatchBackendReader(t, "source bytes")
				_, err := obj.CopyObject(ctx, bucket, "source", bucket, key, info, ObjectOptions{}, opts)
				return err
			}
			const writers = 8
			errs := make([]error, writers)
			var wg sync.WaitGroup
			for i := range writers {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					errs[i] = copyTo("concurrent", ObjectOptions{RequireNewObject: true})
				}(i)
			}
			wg.Wait()
			var successes int
			for _, err := range errs {
				if err == nil {
					successes++
				} else if !isErrPreconditionFailed(err) {
					t.Fatalf("unexpected concurrent copy error: %v", err)
				}
			}
			if successes != 1 {
				t.Fatalf("concurrent copies succeeded %d times, want 1", successes)
			}
			copied := ifMatchBackendContents(t, obj, bucket, "concurrent", "source bytes")
			if copied.UserDefined["x-amz-meta-description"] != "preserved" {
				t.Fatal("conditional copy lost source metadata")
			}
			if _, err := obj.PutObject(ctx, bucket, "empty", ifMatchBackendReader(t, ""), ObjectOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := copyTo("empty", ObjectOptions{RequireNewObject: true}); !isErrPreconditionFailed(err) {
				t.Fatalf("empty destination overwritten: %v", err)
			}
			ifMatchBackendContents(t, obj, bucket, "empty", "")
			for _, metadataOnly := range []bool{false, true} {
				info := source.Clone()
				info.metadataOnly = metadataOnly
				info.UserDefined = map[string]string{"x-amz-meta-description": "must not replace"}
				info.PutObjReader = ifMatchBackendReader(t, "must not replace")
				_, err := obj.CopyObject(ctx, bucket, "source", bucket, "source", info, ObjectOptions{}, ObjectOptions{RequireNewObject: true})
				if !isErrPreconditionFailed(err) {
					t.Fatalf("self-copy metadataOnly=%v: %v", metadataOnly, err)
				}
			}
			ifMatchBackendContents(t, obj, bucket, "source", "source bytes")
			lock := obj.NewNSLock(bucket, "caller-owned")
			lockedCtx, err := lock.GetLock(ctx, globalOperationTimeout)
			if err != nil {
				t.Fatal(err)
			}
			info := source.Clone()
			info.PutObjReader = ifMatchBackendReader(t, "source bytes")
			_, err = obj.CopyObject(lockedCtx, bucket, "source", bucket, "caller-owned", info, ObjectOptions{}, ObjectOptions{RequireNewObject: true, NoLock: true})
			lock.Unlock()
			if err != nil {
				t.Fatalf("copy reentered caller-owned lock: %v", err)
			}
			ifMatchBackendContents(t, obj, bucket, "caller-owned", "source bytes")
		})
	})
}

func TestAPIConditionalCopy(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, instance, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
		ctx := context.Background()
		source, err := obj.PutObject(ctx, bucket, "source", ifMatchBackendReader(t, "source bytes"), ObjectOptions{UserDefined: map[string]string{
			"x-amz-meta-description": "preserved", xhttp.AmzObjectTagging: "kind=source", "content-type": "text/plain",
		}})
		if err != nil {
			t.Fatal(err)
		}
		copyRequest := func(key string, headers map[string]string) *http.Request {
			t.Helper()
			request, err := newTestSignedRequestV4(http.MethodPut, getCopyObjectURL("", bucket, key), 0, nil,
				credentials.AccessKey, credentials.SecretKey, headers)
			if err != nil {
				t.Fatal(err)
			}
			return request
		}
		for _, test := range []struct {
			name, target, sourceCondition string
			status                        int
		}{
			{"create", "target", source.ETag, http.StatusOK},
			{"existing", "target", source.ETag, http.StatusPreconditionFailed},
			{"changed-source", "changed-source", "stale", http.StatusPreconditionFailed},
			{"self-copy", "source", source.ETag, http.StatusPreconditionFailed},
			{"source-only-independent", "source-only", source.ETag, http.StatusOK},
		} {
			t.Run(instance+"/"+test.name, func(t *testing.T) {
				headers := map[string]string{xhttp.AmzCopySource: "/" + bucket + "/source", xhttp.AmzCopySourceIfMatch: `"` + test.sourceCondition + `"`}
				if test.name != "source-only-independent" {
					headers[xhttp.IfNoneMatch] = "*"
				}
				if test.name == "self-copy" {
					headers[xhttp.AmzMetadataDirective] = "REPLACE"
					headers["X-Amz-Meta-Description"] = "must not replace"
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, copyRequest(test.target, headers))
				if response.Code != test.status {
					t.Fatalf("copy status=%d want %d: %s", response.Code, test.status, response.Body.String())
				}
				if conditionalMatchResponseHeader(response.Header(), conditionalCopyWritesHeader) != "v1" {
					t.Fatal("production response omitted conditional-copy capability")
				}
				if test.status == http.StatusOK {
					copied := ifMatchBackendContents(t, obj, bucket, test.target, "source bytes")
					if copied.UserDefined["x-amz-meta-description"] != "preserved" || copied.UserTags != "kind=source" || copied.ContentType != "text/plain" {
						t.Fatalf("copy changed source metadata: %+v", copied)
					}
				} else if test.name == "changed-source" {
					if _, err := obj.GetObjectInfo(ctx, bucket, test.target, ObjectOptions{}); !isErrObjectNotFound(err) {
						t.Fatalf("failed source condition created target: %v", err)
					}
				}
				ifMatchBackendContents(t, obj, bucket, "source", "source bytes")
			})
		}
		// Adding an unsigned destination condition must not create the object.
		request := copyRequest("unsigned", map[string]string{xhttp.AmzCopySource: "/" + bucket + "/source"})
		request.Header.Set(xhttp.IfNoneMatch, "*")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "<Code>AccessDenied</Code>") {
			t.Fatalf("unsigned conditional copy accepted: %d %s", response.Code, response.Body.String())
		}
		if _, err := obj.GetObjectInfo(ctx, bucket, "unsigned", ObjectOptions{}); !isErrObjectNotFound(err) {
			t.Fatalf("unsigned condition created target: %v", err)
		}
	}, nil)
}

func TestAPIConditionalCopyAuthorization(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, instance, bucket string, router http.Handler, _ auth.Credentials, t *testing.T) {
		ctx := context.Background()
		source, err := obj.PutObject(ctx, bucket, "source", ifMatchBackendReader(t, "protected source"), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}})
		if err != nil {
			t.Fatal(err)
		}
		globalIAMSys.InitStore(obj)
		const user, password = "conditional-copy-user", "conditional-copy-secret"
		if err := globalIAMSys.CreateUser(user, madmin.UserInfo{SecretKey: password, Status: madmin.AccountEnabled}); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name     string
			source   string
			put, get bool
			tag      string
			want     int
		}{
			{name: "both", source: "source", put: true, get: true, want: http.StatusOK},
			{name: "put-only", source: "source", put: true, want: http.StatusForbidden},
			{name: "get-only", source: "source", get: true, want: http.StatusForbidden},
			{name: "matching-source-tag", source: "source", put: true, get: true, tag: "prod", want: http.StatusOK},
			{name: "wrong-source-tag", source: "source", put: true, get: true, tag: "dev", want: http.StatusForbidden},
			{name: "missing-source-without-get", source: "missing", put: true, want: http.StatusForbidden},
		} {
			t.Run(instance+"/"+test.name, func(t *testing.T) {
				var statements []string
				if test.put {
					statements = append(statements, `{"Effect":"Allow","Action":["s3:PutObject"],"Resource":["arn:aws:s3:::`+bucket+`/`+test.name+`"]}`)
				}
				if test.get {
					condition := ""
					if test.tag != "" {
						condition = `,"Condition":{"StringEquals":{"s3:ExistingObjectTag/environment":"` + test.tag + `"}}`
					}
					statements = append(statements, `{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::`+bucket+`/source"]`+condition+`}`)
				}
				policy, err := iampolicy.ParseConfig(strings.NewReader(`{"Version":"2012-10-17","Statement":[` + strings.Join(statements, ",") + `]}`))
				if err != nil {
					t.Fatal(err)
				}
				if err := globalIAMSys.SetPolicy("conditional-copy-policy", *policy); err != nil {
					t.Fatal(err)
				}
				if err := globalIAMSys.PolicyDBSet(user, "conditional-copy-policy", false); err != nil {
					t.Fatal(err)
				}
				request, err := newTestSignedRequestV4(http.MethodPut, "/"+bucket+"/"+test.name, 0, nil, user, password, map[string]string{
					xhttp.AmzCopySource: "/" + bucket + "/" + test.source, xhttp.AmzCopySourceIfMatch: `"` + source.ETag + `"`, xhttp.IfNoneMatch: "*",
				})
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != test.want {
					t.Fatalf("copy status=%d want %d: %s", response.Code, test.want, response.Body.String())
				}
				if test.want == http.StatusOK {
					ifMatchBackendContents(t, obj, bucket, test.name, "protected source")
				} else {
					if _, err := obj.GetObjectInfo(ctx, bucket, test.name, ObjectOptions{}); !isErrObjectNotFound(err) {
						t.Fatalf("unauthorized copy created target: %v", err)
					}
					conditionalMatchMetadataHeadersAbsent(t, response)
				}
			})
		}
	}, nil)
}

func TestAPIConditionalCopyVersions(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, instance, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
		if instance == FSTestStr {
			return // FS cannot enable bucket versioning.
		}
		ctx := context.Background()
		configure := func(status string) {
			t.Helper()
			if err := globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>`+status+`</Status></VersioningConfiguration>`)); err != nil {
				t.Fatal(err)
			}
		}
		configure("Enabled")
		old, err := obj.PutObject(ctx, bucket, "source", ifMatchBackendReader(t, "selected history"), ObjectOptions{Versioned: true})
		if err != nil || old.VersionID == "" {
			t.Fatalf("historical source: %+v / %v", old, err)
		}
		if _, err := obj.PutObject(ctx, bucket, "source", ifMatchBackendReader(t, "current source"), ObjectOptions{Versioned: true}); err != nil {
			t.Fatal(err)
		}
		copyTo := func(want int) ObjectInfo {
			t.Helper()
			request, err := newTestSignedRequestV4(http.MethodPut, "/"+bucket+"/target", 0, nil, credentials.AccessKey, credentials.SecretKey, map[string]string{
				xhttp.AmzCopySource: "/" + bucket + "/source?versionId=" + old.VersionID, xhttp.AmzCopySourceIfMatch: `"` + old.ETag + `"`, xhttp.IfNoneMatch: "*",
			})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != want {
				t.Fatalf("versioned copy status=%d want %d: %s", response.Code, want, response.Body.String())
			}
			return ifMatchBackendContents(t, obj, bucket, "target", "selected history")
		}
		first := copyTo(http.StatusOK)
		if first.VersionID == "" {
			t.Fatal("copy into a versioned bucket did not create a version")
		}
		copyTo(http.StatusPreconditionFailed)
		if _, err := obj.DeleteObject(ctx, bucket, "target", ObjectOptions{Versioned: true}); err != nil {
			t.Fatal(err)
		}
		second := copyTo(http.StatusOK)
		if second.VersionID == "" || second.VersionID == first.VersionID {
			t.Fatal("copy over a current delete marker did not create a new version")
		}
		configure("Suspended")
		if _, err := obj.DeleteObject(ctx, bucket, "target", ObjectOptions{VersionSuspended: true}); err != nil {
			t.Fatal(err)
		}
		suspended := copyTo(http.StatusOK)
		if suspended.VersionID != "" && suspended.VersionID != nullVersionID {
			t.Fatalf("suspended copy created a non-null version: %s", suspended.VersionID)
		}
		for _, version := range []string{first.VersionID, second.VersionID} {
			if _, err := obj.GetObjectInfo(ctx, bucket, "target", ObjectOptions{VersionID: version}); err != nil {
				t.Fatalf("conditional copy removed a previous version %s: %v", version, err)
			}
		}
	}, nil)
}

func TestAPIConditionalCopyEncryption(t *testing.T) {
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, instance, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
		oldKMS, oldTLS := GlobalKMS, globalIsTLS
		GlobalKMS, globalIsTLS = conditionalMatchTestKMS(t), true
		defer func() { GlobalKMS, globalIsTLS = oldKMS, oldTLS }()
		for _, mode := range []string{"S3", "KMS", "SSEC"} {
			t.Run(instance+"/"+mode, func(t *testing.T) {
				source, target := "encrypted-"+mode, "encrypted-copy-"+mode
				headers := map[string]string{xhttp.AmzServerSideEncryption: xhttp.AmzEncryptionAES, "X-Amz-Meta-Description": "preserved"}
				readHeaders := map[string]string{}
				if mode == "KMS" {
					headers[xhttp.AmzServerSideEncryption] = xhttp.AmzEncryptionKMS
					headers[xhttp.AmzServerSideEncryptionKmsID] = conditionalMatchTestKeyID
				}
				if mode == "SSEC" {
					delete(headers, xhttp.AmzServerSideEncryption)
					customer := bytes.Repeat([]byte{3}, 32)
					digest := md5.Sum(customer)
					readHeaders[xhttp.AmzServerSideEncryptionCustomerAlgorithm] = xhttp.AmzEncryptionAES
					readHeaders[xhttp.AmzServerSideEncryptionCustomerKey] = base64.StdEncoding.EncodeToString(customer)
					readHeaders[xhttp.AmzServerSideEncryptionCustomerKeyMD5] = base64.StdEncoding.EncodeToString(digest[:])
					for key, value := range readHeaders {
						headers[key] = value
					}
				}
				send := func(method, key, body string, headers map[string]string) *httptest.ResponseRecorder {
					t.Helper()
					request, err := newTestSignedRequestV4(method, "/"+bucket+"/"+key, int64(len(body)), strings.NewReader(body), credentials.AccessKey, credentials.SecretKey, headers)
					if err != nil {
						t.Fatal(err)
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					if response.Code != http.StatusOK {
						t.Fatalf("%s %s: %d %s", method, key, response.Code, response.Body.String())
					}
					return response
				}
				send(http.MethodPut, source, "encrypted source bytes", headers)
				before := send(http.MethodGet, source, "", readHeaders)
				headers[xhttp.IfNoneMatch] = "*"
				headers[xhttp.AmzCopySource] = "/" + bucket + "/" + source
				headers[xhttp.AmzCopySourceIfMatch] = conditionalMatchResponseHeader(before.Header(), xhttp.ETag)
				if mode == "SSEC" {
					headers[xhttp.AmzServerSideEncryptionCopyCustomerAlgorithm] = readHeaders[xhttp.AmzServerSideEncryptionCustomerAlgorithm]
					headers[xhttp.AmzServerSideEncryptionCopyCustomerKey] = readHeaders[xhttp.AmzServerSideEncryptionCustomerKey]
					headers[xhttp.AmzServerSideEncryptionCopyCustomerKeyMD5] = readHeaders[xhttp.AmzServerSideEncryptionCustomerKeyMD5]
				}
				copied := send(http.MethodPut, target, "", headers)
				var result CopyObjectResponse
				if err := xml.Unmarshal(copied.Body.Bytes(), &result); err != nil || result.ETag == "" {
					t.Fatalf("invalid copy acknowledgement: %s / %v", copied.Body.String(), err)
				}
				after := send(http.MethodGet, target, "", readHeaders)
				if after.Body.String() != "encrypted source bytes" || conditionalMatchResponseHeader(after.Header(), "X-Amz-Meta-Description") != "preserved" {
					t.Fatal("conditional copy changed decrypted bytes or metadata")
				}
				if canonicalizeETag(result.ETag) != canonicalizeETag(conditionalMatchResponseHeader(after.Header(), xhttp.ETag)) {
					t.Fatalf("copy response ETag does not identify encrypted target: %s", result.ETag)
				}
			})
		}
	}, nil)
}
