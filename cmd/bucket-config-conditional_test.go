// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/otterio/cmd/rest"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	xnet "github.com/soulteary/otterio/pkg/net"
)

type p3PeerRoundTripper func(*http.Request) (*http.Response, error)

func (f p3PeerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type p3LegacyDeleteFailure struct {
	ObjectLayer
	key     string
	failure error
}

func (o p3LegacyDeleteFailure) DeleteObject(ctx context.Context, bucket, object string, opts ObjectOptions) (ObjectInfo, error) {
	if bucket == otterioMetaBucket && object == o.key {
		return ObjectInfo{}, o.failure
	}
	return o.ObjectLayer.DeleteObject(ctx, bucket, object, opts)
}

func TestBucketConfigTransactions(t *testing.T) {
	ExecObjectLayerTest(t, testBucketConfigTransactions)
}

func testBucketConfigTransactions(obj ObjectLayer, instance string, t TestErrHandler) {
	savedObject, savedMetadata, savedNotify := newObjectLayerFn(), globalBucketMetadataSys, globalNotificationSys
	savedIAM, savedOPA := globalIAMSys, globalPolicyOPA
	savedTargets := globalBucketTargetSys
	defer func() {
		setObjectLayer(savedObject)
		globalBucketMetadataSys, globalNotificationSys = savedMetadata, savedNotify
		globalIAMSys, globalPolicyOPA = savedIAM, savedOPA
		globalBucketTargetSys = savedTargets
	}()
	setObjectLayer(obj)
	globalBucketMetadataSys, globalNotificationSys = NewBucketMetadataSys(), &NotificationSys{}
	globalIAMSys, globalPolicyOPA = NewIAMSys(), nil
	globalBucketTargetSys = NewBucketTargetSys()
	globalIAMSys.store = newIAMObjectStore(obj)
	close(globalIAMSys.configLoaded)
	ctx := context.Background()
	const bucket = "config-transactions"
	if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	policyData := func(id string) []byte {
		return []byte(fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Sid":%q,"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, id, bucket))
	}
	const lifecycleDoc = `<LifecycleConfiguration><Rule><ID>expire</ID><Filter><Prefix></Prefix></Filter><Status>Enabled</Status><Expiration><Days>2</Days></Expiration></Rule></LifecycleConfiguration>`
	_, initial, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketPolicyConfig)
	if err != nil {
		t.Fatal(err)
	}
	// All contenders read one revision. Exactly one may commit a different doc.
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketPolicyConfig, policyData(fmt.Sprint(i)), initial)
		}(i)
	}
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		} else if !isErrPreconditionFailed(err) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("%s: %d concurrent conditional updates succeeded", instance, success)
	}
	// Unconditional legacy updates to different fields share the same lock.
	for i := range 5 {
		p := policyData(fmt.Sprint("iteration-", i))
		var updateErrors [2]error
		wg.Add(2)
		go func() {
			defer wg.Done()
			updateErrors[0] = globalBucketMetadataSys.Update(bucket, bucketPolicyConfig, p)
		}()
		go func() {
			defer wg.Done()
			updateErrors[1] = globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, []byte(lifecycleDoc))
		}()
		wg.Wait()
		for _, err := range updateErrors {
			if err != nil {
				t.Fatal(err)
			}
		}
		meta, err := loadBucketMetadata(ctx, obj, bucket)
		if err != nil || !bytes.Equal(meta.PolicyConfigJSON, p) || string(meta.LifecycleConfigXML) != lifecycleDoc {
			t.Fatalf("%s: settings were lost during concurrent updates: %v", instance, err)
		}
	}
	request := func(method, query string, data []byte, headers map[string]string) *http.Request {
		r, err := newTestSignedRequestV4(method, "/"+bucket+"?"+query, int64(len(data)), bytes.NewReader(data), globalActiveCred.AccessKey, globalActiveCred.SecretKey, headers)
		if err != nil {
			t.Fatal(err)
		}
		return setURLVarsOnRequest(r, map[string]string{"bucket": bucket})
	}
	assertACK := func(rec *httptest.ResponseRecorder, file string, exists bool) {
		_, revision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, file)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Header().Get(bucketConfigCapabilityHeader) != "v1" || rec.Header().Get(bucketConfigRevisionHeader) != revision || rec.Header().Get(bucketConfigExistsHeader) != fmt.Sprint(exists) {
			t.Fatalf("%s commit ACK missing or stale: %v expected=%s", file, rec.Header(), revision)
		}
	}

	api := objectAPIHandlers{ObjectAPI: func() ObjectLayer { return obj }}
	// Deliberately stale memory does not influence the document or revision.
	meta := newBucketMetadata(bucket)
	meta.PolicyConfigJSON = policyData("stale")
	globalBucketMetadataSys.Set(bucket, meta)
	got := httptest.NewRecorder()
	api.GetBucketPolicyHandler(got, request(http.MethodGet, "policy", nil, nil))
	raw, revision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketPolicyConfig)
	if err != nil || got.Code != 200 || !bytes.Equal(got.Body.Bytes(), raw) || got.Header().Get(bucketConfigRevisionHeader) != revision || got.Header().Get(bucketConfigExistsHeader) != "true" {
		t.Fatalf("%s: fresh GET contract failed: %d %v", instance, got.Code, err)
	}
	// A header added after signing must not gain mutation semantics.
	unsigned := request(http.MethodPut, "policy", policyData("unsigned"), nil)
	unsigned.Header.Set(bucketConfigIfMatchHeader, revision)
	rec := httptest.NewRecorder()
	api.PutBucketPolicyHandler(rec, unsigned)
	if rec.Code != 400 && rec.Code != 403 {
		t.Fatalf("unsigned condition accepted: %d", rec.Code)
	}
	duplicate := request(http.MethodPut, "policy", policyData("duplicate"), map[string]string{bucketConfigIfMatchHeader: revision})
	duplicate.Header.Add(bucketConfigIfMatchHeader, revision)
	if err := signRequestV4(duplicate, globalActiveCred.AccessKey, globalActiveCred.SecretKey); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	api.PutBucketPolicyHandler(rec, duplicate)
	if rec.Code != 400 {
		t.Fatalf("duplicate condition accepted: %d", rec.Code)
	}
	conflict := request(http.MethodPut, "policy", policyData("old-revision"), map[string]string{bucketConfigIfMatchHeader: initial})
	rec = httptest.NewRecorder()
	api.PutBucketPolicyHandler(rec, conflict)
	if rec.Code != 412 {
		t.Fatalf("stale revision status=%d body=%s", rec.Code, rec.Body.String())
	}
	// A signed conditional DELETE checks revision, then missing GET still has it.
	rec = httptest.NewRecorder()
	api.DeleteBucketPolicyHandler(rec, request(http.MethodDelete, "policy", nil, map[string]string{bucketConfigIfMatchHeader: revision}))
	if rec.Code != 204 {
		t.Fatalf("conditional DELETE=%d %s", rec.Code, rec.Body.String())
	}
	assertACK(rec, bucketPolicyConfig, false)
	rec = httptest.NewRecorder()
	api.GetBucketPolicyHandler(rec, request(http.MethodGet, "policy", nil, nil))
	if rec.Code != 404 || rec.Header().Get(bucketConfigCapabilityHeader) != "v1" || rec.Header().Get(bucketConfigExistsHeader) != "false" || rec.Header().Get(bucketConfigRevisionHeader) != initial {
		t.Fatalf("missing config contract=%d headers=%v", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	api.PutBucketPolicyHandler(rec, request(http.MethodPut, "policy", policyData("created-through-http"), map[string]string{bucketConfigIfMatchHeader: initial}))
	if rec.Code != 204 {
		t.Fatalf("conditional policy HTTP create=%d %s", rec.Code, rec.Body.String())
	}
	assertACK(rec, bucketPolicyConfig, true)
	// Lifecycle uses the same wire contract for reads, updates and removal.
	raw, lcRevision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketLifecycleConfig)
	if err != nil || len(raw) == 0 {
		t.Fatal("lifecycle fixture missing")
	}
	rec = httptest.NewRecorder()
	api.GetBucketLifecycleHandler(rec, request(http.MethodGet, "lifecycle", nil, nil))
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), raw) || rec.Header().Get(bucketConfigRevisionHeader) != lcRevision {
		t.Fatal("lifecycle read/revision mismatch")
	}
	rec = httptest.NewRecorder()
	api.DeleteBucketLifecycleHandler(rec, request(http.MethodDelete, "lifecycle", nil, map[string]string{bucketConfigIfMatchHeader: lcRevision}))
	if rec.Code != 204 {
		t.Fatalf("conditional lifecycle delete=%d", rec.Code)
	}
	assertACK(rec, bucketLifecycleConfig, false)
	rec = httptest.NewRecorder()
	api.GetBucketLifecycleHandler(rec, request(http.MethodGet, "lifecycle", nil, nil))
	if rec.Code != 404 || rec.Header().Get(bucketConfigExistsHeader) != "false" || rec.Header().Get(bucketConfigRevisionHeader) != bucketConfigRevision(bucketLifecycleConfig, nil) {
		t.Fatal("missing lifecycle contract mismatch")
	}
	rec = httptest.NewRecorder()
	api.PutBucketLifecycleHandler(rec, request(http.MethodPut, "lifecycle", []byte(lifecycleDoc), map[string]string{bucketConfigIfMatchHeader: bucketConfigRevision(bucketLifecycleConfig, nil)}))
	if rec.Code != 200 {
		t.Fatalf("conditional lifecycle PUT=%d %s", rec.Code, rec.Body.String())
	}
	assertACK(rec, bucketLifecycleConfig, true)
	// Implemented tag-scoped expiration documents remain editable and survive
	// canonical GET/re-save, including spaces, plus, percent and Unicode.
	for _, action := range []string{
		`<Expiration><Days>2</Days></Expiration>`,
		`<Expiration><Date>2000-01-01T00:00:00Z</Date></Expiration>`,
		`<NoncurrentVersionExpiration><NoncurrentDays>2</NoncurrentDays></NoncurrentVersionExpiration>`,
	} {
		doc := `<LifecycleConfiguration><Rule><Filter><Tag><Key>environment name</Key><Value>prod+100%生产</Value></Tag></Filter><Status>Enabled</Status>` + action + `</Rule></LifecycleConfiguration>`
		_, revision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketLifecycleConfig)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		api.PutBucketLifecycleHandler(rec, request(http.MethodPut, "lifecycle", []byte(doc), map[string]string{bucketConfigIfMatchHeader: revision}))
		if rec.Code != 200 {
			t.Fatalf("implemented tag expiration rejected: %d %s", rec.Code, rec.Body.String())
		}
		assertACK(rec, bucketLifecycleConfig, true)
		canonical, revision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketLifecycleConfig)
		if err != nil || !bytes.Contains(canonical, []byte(`<Key>environment name</Key>`)) || !bytes.Contains(canonical, []byte(`<Value>prod+100%生产</Value>`)) {
			t.Fatalf("canonical lifecycle changed tags: %s %v", canonical, err)
		}
		rec = httptest.NewRecorder()
		api.PutBucketLifecycleHandler(rec, request(http.MethodPut, "lifecycle", canonical, map[string]string{bucketConfigIfMatchHeader: revision}))
		if rec.Code != 200 {
			t.Fatalf("canonical lifecycle cannot be saved: %d %s", rec.Code, rec.Body.String())
		}
		assertACK(rec, bucketLifecycleConfig, true)
	}
	// Unsupported fields must not be acknowledged after legacy decoders drop
	// them, and malformed envelopes must not change persistent state or cache.
	assertRejected := func(file, query string, handler http.HandlerFunc, data []byte, mutate func(*http.Request)) {
		before, revision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, file)
		if err != nil {
			t.Fatal(err)
		}
		r := request(http.MethodPut, query, data, map[string]string{bucketConfigIfMatchHeader: revision})
		if mutate != nil {
			mutate(r)
		}
		rec := httptest.NewRecorder()
		handler(rec, r)
		if rec.Code < 400 || rec.Code >= 500 || rec.Header().Get(bucketConfigRevisionHeader) != "" {
			t.Fatalf("%s unsafe edit accepted: status=%d body=%s", file, rec.Code, rec.Body.String())
		}
		after, afterRevision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, file)
		if err != nil || !bytes.Equal(before, after) || revision != afterRevision {
			t.Fatalf("%s rejected edit changed persistent configuration: %v", file, err)
		}
	}
	p := string(policyData("must-not-save"))
	for _, invalid := range []string{
		strings.Replace(p, `"Version":`, `"UnknownProtection":true,"Version":`, 1),
		strings.Replace(p, `"Effect":`, `"NotAction":["s3:DeleteObject"],"Effect":`, 1),
		strings.Replace(p, `"AWS":["*"]`, `"AWS":["*"],"Service":"other"`, 1),
		strings.Replace(p, `"Effect":"Allow"`, `"Effect":"Deny","Effect":"Allow"`, 1),
		strings.Replace(p, `"Version":`, `"ID":"one","Id":"two","Version":`, 1),
		strings.Replace(p, `"Effect":`, `"Condition":{"StringEquals":{}},"Effect":`, 1),
		p + strings.Repeat(" ", 8192) + `{}`,
		strings.Replace(p, `"must-not-save"`, `"orphan-\ud800"`, 1),
		strings.Replace(p, `"must-not-save"`, "\"raw-\xff\"", 1),
	} {
		assertRejected(bucketPolicyConfig, "policy", api.PutBucketPolicyHandler, []byte(invalid), nil)
	}
	for _, invalid := range []string{
		strings.Replace(lifecycleDoc, `<Expiration>`, `<AbortIncompleteMultipartUpload><DaysAfterInitiation>1</DaysAfterInitiation></AbortIncompleteMultipartUpload><Expiration>`, 1),
		strings.Replace(lifecycleDoc, `<Days>2</Days>`, `<Days>2</Days><ExpiredObjectAllVersions>true</ExpiredObjectAllVersions>`, 1),
		strings.Replace(lifecycleDoc, `<Days>2</Days>`, `<Days>2</Days><Days>3</Days>`, 1),
		strings.Replace(lifecycleDoc, `<Days>2</Days>`, `<Days><Unknown>2</Unknown></Days>`, 1),
		strings.Replace(lifecycleDoc, `<Days>`, `<Days unit="day">`, 1),
		strings.Replace(lifecycleDoc, `<Filter>`, `<Filter><ObjectSizeGreaterThan>1</ObjectSizeGreaterThan>`, 1),
		strings.Replace(lifecycleDoc, `<Prefix></Prefix>`, `<Tag><Key></Key><Value>prod</Value></Tag>`, 1),
		strings.Replace(lifecycleDoc, `<Prefix></Prefix>`, `<Tag><Value>prod</Value></Tag>`, 1),
		strings.Replace(lifecycleDoc, `<Prefix></Prefix>`, `<And/>`, 1),
		strings.Replace(lifecycleDoc, `<Prefix></Prefix>`, `<And/><Tag><Key>environment</Key><Value>prod</Value></Tag>`, 1),
		strings.Replace(lifecycleDoc, `<Prefix></Prefix>`, `<Prefix/><Tag><Key>environment</Key><Value>prod</Value></Tag>`, 1),
		strings.Replace(strings.Replace(lifecycleDoc, `<Prefix></Prefix>`, `<Tag><Key>environment</Key><Value>prod</Value></Tag>`, 1), `<Days>2</Days>`, `<ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker>`, 1),
		strings.Replace(lifecycleDoc, `<Expiration><Days>2</Days></Expiration>`, `<Transition><Days>0</Days><Date>2000-01-01T00:00:00Z</Date><StorageClass>archive</StorageClass></Transition>`, 1),
		strings.Replace(lifecycleDoc, `<Expiration><Days>2</Days></Expiration>`, `<NoncurrentVersionTransition><NoncurrentDays>2</NoncurrentDays><NewerNoncurrentVersions>3</NewerNoncurrentVersions><StorageClass>archive</StorageClass></NoncurrentVersionTransition>`, 1),
		`<!DOCTYPE LifecycleConfiguration>` + lifecycleDoc,
		lifecycleDoc + lifecycleDoc,
		lifecycleDoc + "trailing",
	} {
		assertRejected(bucketLifecycleConfig, "lifecycle", api.PutBucketLifecycleHandler, []byte(invalid), nil)
	}
	// Both conditional and legacy writes validate both transition targets;
	// a supported action with a missing destination is a native 404, not a
	// malformed-document 400. Neither path changes persistent state.
	for _, action := range []string{
		`<Transition><Days>2</Days><StorageClass>unconfigured-target</StorageClass></Transition>`,
		`<NoncurrentVersionTransition><NoncurrentDays>2</NoncurrentDays><StorageClass>unconfigured-target</StorageClass></NoncurrentVersionTransition>`,
	} {
		before, revision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketLifecycleConfig)
		if err != nil {
			t.Fatal(err)
		}
		doc := strings.Replace(lifecycleDoc, `<Expiration><Days>2</Days></Expiration>`, action, 1)
		for _, headers := range []map[string]string{nil, {bucketConfigIfMatchHeader: revision}} {
			rec := httptest.NewRecorder()
			api.PutBucketLifecycleHandler(rec, request(http.MethodPut, "lifecycle", []byte(doc), headers))
			if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "XOtterioAdminRemoteTargetNotFoundError") {
				t.Fatalf("transition did not return its native missing-target error: %d %s", rec.Code, rec.Body.String())
			}
		}
		after, afterRevision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketLifecycleConfig)
		if err != nil || !bytes.Equal(before, after) || revision != afterRevision {
			t.Fatalf("missing transition target changed lifecycle: %v", err)
		}
	}
	legacyNoncurrent := strings.Replace(lifecycleDoc, `<Expiration><Days>2</Days></Expiration>`, `<NoncurrentVersionTransition><NoncurrentDays>2</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition>`, 1)
	if err := globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, []byte(legacyNoncurrent)); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	api.GetBucketLifecycleHandler(rec, request(http.MethodGet, "lifecycle", nil, nil))
	if rec.Code != 200 || rec.Body.String() != legacyNoncurrent || rec.Header().Get(bucketConfigRevisionHeader) != bucketConfigRevision(bucketLifecycleConfig, []byte(legacyNoncurrent)) {
		t.Fatalf("legacy noncurrent transition was not preserved in GET: %d %s", rec.Code, rec.Body.String())
	}
	assertRejected(bucketLifecycleConfig, "lifecycle", api.PutBucketLifecycleHandler, []byte(legacyNoncurrent), nil)
	if err := globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, []byte(lifecycleDoc)); err != nil {
		t.Fatal(err)
	}
	// A decoder may stop before a long trailing body. Both checksum failures
	// and UNSIGNED-PAYLOAD must be rejected before any CAS save occurs.
	longPolicy := []byte(p + strings.Repeat(" ", 8192))
	assertRejected(bucketPolicyConfig, "policy", api.PutBucketPolicyHandler, longPolicy, func(r *http.Request) {
		tampered := append([]byte(nil), longPolicy...)
		tampered[len(tampered)-1] = '\t'
		r.Body = io.NopCloser(bytes.NewReader(tampered))
	})
	assertRejected(bucketPolicyConfig, "policy", api.PutBucketPolicyHandler, []byte(p), func(r *http.Request) {
		r.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
		if err := signRequestV4(r, globalActiveCred.AccessKey, globalActiveCred.SecretKey); err != nil {
			t.Fatal(err)
		}
	})
	_, currentPolicy, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketPolicyConfig)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	api.DeleteBucketPolicyHandler(rec, request(http.MethodDelete, "policy", []byte("nonempty"), map[string]string{bucketConfigIfMatchHeader: currentPolicy}))
	if rec.Code != 400 {
		t.Fatalf("conditional DELETE accepted ignored body: %d", rec.Code)
	}
	_, afterDelete, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketPolicyConfig)
	if err != nil || afterDelete != currentPolicy {
		t.Fatal("rejected DELETE changed policy")
	}
	// Explicit deny is checked before exposing the current document/revision.
	user := auth.Credentials{AccessKey: "config-denied", SecretKey: "config-denied-secret"}
	globalIAMSys.iamUsersMap[user.AccessKey] = user
	globalIAMSys.iamPolicyDocsMap["config-deny"] = *parseTestPolicy(t.(*testing.T), `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["s3:GetBucketPolicy","s3:PutBucketPolicy"],"Resource":["arn:aws:s3:::config-transactions"]}]}`)
	globalIAMSys.iamUserPolicyMap[user.AccessKey] = newMappedPolicy("config-deny")
	r, err := newTestSignedRequestV4(http.MethodGet, "/"+bucket+"?policy", 0, nil, user.AccessKey, user.SecretKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	r = setURLVarsOnRequest(r, map[string]string{"bucket": bucket})
	rec = httptest.NewRecorder()
	api.GetBucketPolicyHandler(rec, r)
	if rec.Code != 403 || rec.Header().Get(bucketConfigRevisionHeader) != "" {
		t.Fatalf("denied GET leaked revision: %d", rec.Code)
	}
	if instance == ErasureTestStr {
		v := httptest.NewRecorder()
		api.GetBucketVersioningHandler(v, request(http.MethodGet, "versioning", nil, nil))
		if v.Code != 200 || v.Header().Get(bucketConfigExistsHeader) != "false" || v.Header().Get(bucketConfigRevisionHeader) != bucketConfigRevision(bucketVersioningConfig, nil) {
			t.Fatalf("empty versioning contract: %d %v", v.Code, v.Header())
		}
		rec = httptest.NewRecorder()
		api.PutBucketVersioningHandler(rec, request(http.MethodPut, "versioning", []byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`), map[string]string{bucketConfigIfMatchHeader: bucketConfigRevision(bucketVersioningConfig, nil)}))
		if rec.Code != 200 {
			t.Fatalf("conditional versioning PUT=%d %s", rec.Code, rec.Body.String())
		}
		assertACK(rec, bucketVersioningConfig, true)
		for _, invalid := range []string{
			`<VersioningConfiguration><Status>Enabled</Status><MFADelete>Enabled</MFADelete></VersioningConfiguration>`,
			`<VersioningConfiguration><Status>Enabled</Status><Status>Suspended</Status></VersioningConfiguration>`,
			`<VersioningConfiguration><Status><Value>Enabled</Value></Status></VersioningConfiguration>`,
			`<VersioningConfiguration unknown="true"><Status>Enabled</Status></VersioningConfiguration>`,
			`<VersioningConfiguration xmlns="urn:unknown"><Status>Enabled</Status></VersioningConfiguration>`,
			`<!DOCTYPE VersioningConfiguration><VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`,
			`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration><Other/>`,
		} {
			assertRejected(bucketVersioningConfig, "versioning", api.PutBucketVersioningHandler, []byte(invalid), nil)
		}
		rec = httptest.NewRecorder()
		api.PutBucketVersioningHandler(rec, request(http.MethodPut, "versioning", []byte(`<VersioningConfiguration><Status>Suspended</Status></VersioningConfiguration>`), map[string]string{bucketConfigIfMatchHeader: bucketConfigRevision(bucketVersioningConfig, nil)}))
		if rec.Code != 412 {
			t.Fatalf("stale versioning PUT=%d %s", rec.Code, rec.Body.String())
		}
		if err := globalBucketMetadataSys.Update(bucket, objectLockConfig, enabledBucketObjectLockConfig); err != nil {
			t.Fatal(err)
		}
		_, currentV, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, bucketVersioningConfig)
		if err != nil {
			t.Fatal(err)
		}
		// A lagging cache must not allow suspension after object lock committed.
		globalBucketMetadataSys.Set(bucket, newBucketMetadata(bucket))
		rec = httptest.NewRecorder()
		api.PutBucketVersioningHandler(rec, request(http.MethodPut, "versioning", []byte(`<VersioningConfiguration><Status>Suspended</Status></VersioningConfiguration>`), map[string]string{bucketConfigIfMatchHeader: currentV}))
		if rec.Code != 409 {
			t.Fatalf("fresh transaction ignored object lock: %d %s", rec.Code, rec.Body.String())
		}
	}
	// A legacy migration from any ordinary load/cache entry point must wait
	// for the same transaction and must not later overwrite a newer CAS cache.
	for _, loader := range []string{"cache-miss", "background", "peer-load"} {
		legacy := policyData("legacy-" + loader)
		if err := saveConfig(ctx, obj, pathJoin(bucketConfigPrefix, bucket, bucketPolicyConfig), legacy); err != nil {
			t.Fatal(err)
		}
		globalBucketMetadataSys.Lock()
		delete(globalBucketMetadataSys.metadataMap, bucket)
		globalBucketMetadataSys.Unlock()
		transaction := obj.NewNSLock(otterioMetaBucket, bucketMetadataTransactionKey(bucket))
		if _, err := transaction.GetLock(ctx, newDynamicTimeout(time.Second, time.Second)); err != nil {
			t.Fatal(err)
		}
		loaded := make(chan error, 1)
		go func() {
			switch loader {
			case "cache-miss":
				_, err := globalBucketMetadataSys.GetConfig(bucket)
				loaded <- err
			case "background":
				globalBucketMetadataSys.concurrentLoad(ctx, []BucketInfo{{Name: bucket}}, obj)
				loaded <- nil
			default:
				_, err := globalBucketMetadataSys.loadConfig(ctx, obj, bucket)
				loaded <- err
			}
		}()
		select {
		case err := <-loaded:
			transaction.Unlock()
			t.Fatalf("%s load bypassed transaction lock: %v", loader, err)
		case <-time.After(30 * time.Millisecond):
		}
		changed := policyData("after-migration-" + loader)
		saved := make(chan error, 1)
		go func() {
			saved <- globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketPolicyConfig, changed, bucketConfigRevision(bucketPolicyConfig, legacy))
		}()
		transaction.Unlock()
		if err := <-loaded; err != nil {
			t.Fatal(err)
		}
		if err := <-saved; err != nil {
			t.Fatal(err)
		}
		meta, err := loadBucketMetadata(ctx, obj, bucket)
		if err != nil || !bytes.Equal(meta.PolicyConfigJSON, changed) {
			t.Fatalf("%s migration overwrote CAS: %v", loader, err)
		}
		cached, err := globalBucketMetadataSys.Get(bucket)
		if err != nil || !bytes.Equal(cached.PolicyConfigJSON, changed) {
			t.Fatalf("%s load published stale cache: %v", loader, err)
		}
	}
	// A leftover legacy file remains authoritative during the next load.
	// Failed cleanup must therefore prevent later RMW commits rather than
	// acknowledging an edit that a future migration can overwrite.
	legacyKey := pathJoin(bucketConfigPrefix, bucket, bucketPolicyConfig)
	legacy := policyData("cleanup-pending")
	if err := saveConfig(ctx, obj, legacyKey, legacy); err != nil {
		t.Fatal(err)
	}
	cleanupFailure := errors.New("fixture legacy cleanup failed")
	setObjectLayer(p3LegacyDeleteFailure{ObjectLayer: obj, key: legacyKey, failure: cleanupFailure})
	err = globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketPolicyConfig, policyData("must-not-commit-before-cleanup"), "")
	setObjectLayer(obj)
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("failed legacy cleanup allowed a later commit: %v", err)
	}
	meta = newBucketMetadata(bucket)
	if err := meta.Load(ctx, obj, bucket); err != nil || !bytes.Equal(meta.PolicyConfigJSON, legacy) {
		t.Fatalf("failed cleanup committed a later edit: %v", err)
	}
	if _, err := loadBucketMetadata(ctx, obj, bucket); err != nil {
		t.Fatal(err)
	}
	changed := policyData("cleanup-confirmed")
	if err := globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketPolicyConfig, changed, bucketConfigRevision(bucketPolicyConfig, legacy)); err != nil {
		t.Fatal(err)
	}
	meta, err = loadBucketMetadata(ctx, obj, bucket)
	if err != nil || !bytes.Equal(meta.PolicyConfigJSON, changed) {
		t.Fatalf("completed cleanup lost a conditional edit: %v", err)
	}

	// A remote peer shares the distributed namespace lock. Its reload must be
	// able to acquire that lock before this mutation waits for the peer's ACK.
	host, err := xnet.ParseHost("127.0.0.1:9000")
	if err != nil {
		t.Fatal(err)
	}
	peerReloaded := make(chan struct{}, 1)
	peerTransport := p3PeerRoundTripper(func(r *http.Request) (*http.Response, error) {
		lock := obj.NewNSLock(otterioMetaBucket, bucketMetadataTransactionKey(bucket))
		if _, err := lock.GetLock(r.Context(), newDynamicTimeout(100*time.Millisecond, time.Millisecond)); err != nil {
			return nil, err
		}
		lock.Unlock()
		select {
		case peerReloaded <- struct{}{}:
		default:
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), ContentLength: 0, Request: r}, nil
	})
	globalNotificationSys.peerClients = []*peerRESTClient{{host: host, restClient: rest.NewClient(&url.URL{Scheme: "http", Host: host.String(), Path: peerRESTPath}, peerTransport, func(string) string { return "fixture-token" })}}
	peerCtx, peerCancel := context.WithTimeout(ctx, time.Second)
	peerStart := time.Now()
	err = globalBucketMetadataSys.UpdateWithContext(peerCtx, bucket, bucketPolicyConfig, policyData("peer-reload-committed"), "")
	peerCancel()
	globalNotificationSys.peerClients = nil
	if err != nil || time.Since(peerStart) > 500*time.Millisecond {
		t.Fatalf("peer reload waited for held transaction: %v duration=%v", err, time.Since(peerStart))
	}
	select {
	case <-peerReloaded:
	default:
		t.Fatal("peer could not acquire shared transaction during reload")
	}
	// Cancellation interrupts waiting for the transaction lock without saving.
	lock := obj.NewNSLock(otterioMetaBucket, bucketMetadataTransactionKey(bucket))
	if _, err := lock.GetLock(ctx, newDynamicTimeout(time.Second, time.Second)); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	start := time.Now()
	err = globalBucketMetadataSys.UpdateWithContext(deadline, bucket, bucketPolicyConfig, policyData("must-not-save"), "")
	cancel()
	lock.Unlock()
	if err == nil || time.Since(start) > time.Second || (!errors.Is(err, context.DeadlineExceeded) && !func() bool { var timeout OperationTimedOut; return errors.As(err, &timeout) }()) {
		t.Fatalf("bounded transaction wait=%v duration=%v", err, time.Since(start))
	}
}

func TestBucketConfigCapabilityAndCondition(t *testing.T) {
	savedObj, savedGateway := newObjectLayerFn(), globalIsGateway
	t.Cleanup(func() { setObjectLayer(savedObj); globalIsGateway = savedGateway })
	globalIsGateway = false
	setObjectLayer(&FSObjects{})
	if !bucketConfigSupported(bucketPolicyConfig) || bucketConfigSupported(bucketVersioningConfig) {
		t.Fatal("incorrect FS capability")
	}
	globalIsGateway = true
	if bucketConfigSupported(bucketPolicyConfig) {
		t.Fatal("gateway advertised CAS")
	}
	globalIsGateway = false
	setObjectLayer(&erasureServerPools{serverPools: []*erasureSets{{}, {}}})
	if bucketConfigSupported(bucketPolicyConfig) {
		t.Fatal("multi-pool advertised CAS")
	}
	setObjectLayer(nil)
	if bucketConfigSupported(bucketPolicyConfig) {
		t.Fatal("unknown backend advertised CAS")
	}
}

func TestBucketConfigSupportedDocumentEnvelopes(t *testing.T) {
	// Standard XML declarations/namespaces, legacy lifecycle root names and
	// repeated rules/tags remain valid alongside the supported scalar fields.
	versioning := `<?xml version="1.0" encoding="UTF-8"?><VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><!-- state --><Status>Enabled</Status></VersioningConfiguration>`
	if !validConditionalBucketXML([]byte(versioning), bucketVersioningConfig) {
		t.Fatal("standard versioning envelope was rejected")
	}
	const rule = `<Rule><ID>supported</ID><Status>Enabled</Status><Filter><And><Prefix>logs/</Prefix><Tag><Key>first</Key><Value>one</Value></Tag><Tag><Key>second</Key><Value>two</Value></Tag></And></Filter><Expiration><Date>2030-01-01T00:00:00Z</Date></Expiration><Transition><Days>1</Days><StorageClass>arn:aws:s3:::archive</StorageClass></Transition><NoncurrentVersionExpiration><NoncurrentDays>7</NoncurrentDays></NoncurrentVersionExpiration><NoncurrentVersionTransition><NoncurrentDays>2</NoncurrentDays><StorageClass>other-archive</StorageClass></NoncurrentVersionTransition></Rule>`
	for _, root := range []string{"LifecycleConfiguration", "BucketLifecycleConfiguration"} {
		doc := `<` + root + ` xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` + rule + strings.Replace(rule, "supported", "other", 1) + `</` + root + `>`
		if !validConditionalBucketXML([]byte(doc), bucketLifecycleConfig) {
			t.Fatalf("supported lifecycle document rejected: %s", root)
		}
	}
	for _, filter := range []string{`<Filter/>`, `<Filter><Prefix/></Filter>`, `<Filter><Tag><Key>environment</Key><Value/></Tag></Filter>`} {
		doc := `<LifecycleConfiguration><Rule><ID>empty-matches</ID><Status>Enabled</Status>` + filter + `<Expiration><Days>1</Days></Expiration></Rule></LifecycleConfiguration>`
		if !validConditionalBucketXML([]byte(doc), bucketLifecycleConfig) {
			t.Fatalf("supported empty filter/prefix/tag value was rejected: %s", filter)
		}
	}
	policy := `{"Id":"standard-id","Version":"2012-10-17","Statement":[{"Sid":"read","Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::example/*","Condition":{"StringEquals":{"s3:prefix":["logs/"]}}}]}`
	if !validConditionalBucketPolicy([]byte(policy)) {
		t.Fatal("supported JSON policy envelope was rejected")
	}
}

func TestBucketConfigSupportedNoncurrentTransition(t *testing.T) {
	for _, status := range []string{"Enabled", "Disabled"} {
		for _, current := range []string{"", `<Transition><Days>2</Days><StorageClass>archive</StorageClass></Transition>`, `<Transition><Days>2</Days><StorageClass>other-archive</StorageClass></Transition>`} {
			doc := `<LifecycleConfiguration><Rule><Filter><Prefix>logs/</Prefix></Filter><Status>` + status + `</Status>` + current + `<NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>`
			config, err := lifecycle.ParseLifecycleConfig(strings.NewReader(doc))
			if err != nil || config.Validate() != nil {
				t.Fatalf("invalid native fixture: %v", err)
			}
			if !validConditionalBucketXML([]byte(doc), bucketLifecycleConfig) {
				t.Fatalf("CAS rejected a supported noncurrent transition: %s", doc)
			}
		}
	}
	const supported = `<LifecycleConfiguration><Rule><ID>old</ID><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status><NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>`
	for _, doc := range []string{
		strings.Replace(supported, `<NoncurrentDays>1</NoncurrentDays>`, `<NoncurrentDays>1</NoncurrentDays><NoncurrentDays>2</NoncurrentDays>`, 1),
		strings.Replace(supported, `<StorageClass>archive</StorageClass>`, `<StorageClass>archive</StorageClass><StorageClass>other</StorageClass>`, 1),
		strings.Replace(supported, `<NoncurrentDays>1</NoncurrentDays>`, `<NoncurrentDays>1</NoncurrentDays><NewerNoncurrentVersions>2</NewerNoncurrentVersions>`, 1),
		strings.Replace(supported, `<NoncurrentDays>1</NoncurrentDays>`, `<NoncurrentDays unit="day">1</NoncurrentDays>`, 1),
		strings.Replace(supported, `<StorageClass>archive</StorageClass>`, `<StorageClass><Unknown>archive</Unknown></StorageClass>`, 1),
	} {
		if validConditionalBucketXML([]byte(doc), bucketLifecycleConfig) {
			t.Fatalf("CAS accepted discarded or ambiguous noncurrent fields: %s", doc)
		}
	}
	for _, action := range []string{
		`<NoncurrentVersionTransition><NoncurrentDays>0</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition>`,
		`<NoncurrentVersionTransition><NoncurrentDays>-1</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition>`,
		`<NoncurrentVersionTransition><StorageClass>archive</StorageClass></NoncurrentVersionTransition>`,
		`<NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays></NoncurrentVersionTransition>`,
	} {
		doc := `<LifecycleConfiguration><Rule><ID>invalid</ID><Filter><Prefix>logs/</Prefix></Filter><Status>Enabled</Status>` + action + `</Rule></LifecycleConfiguration>`
		lc, err := lifecycle.ParseLifecycleConfig(strings.NewReader(doc))
		if err == nil && lc.Validate() == nil {
			t.Fatalf("native validation accepted an incomplete noncurrent transition: %s", doc)
		}
	}
}
