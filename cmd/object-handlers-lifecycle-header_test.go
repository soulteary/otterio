// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

type lifecycleHeaderObjectLayer struct {
	ObjectLayer
	fresh                ObjectInfo
	err                  error
	reads                int
	bucket, key, version string
	bounded              bool
}

func lifecycleResponseHeader(h http.Header, name string) string {
	for key, values := range h {
		if strings.EqualFold(key, name) {
			return strings.Join(values, ",")
		}
	}
	return ""
}

func (o *lifecycleHeaderObjectLayer) GetObjectInfo(ctx context.Context, bucket, key string, opts ObjectOptions) (ObjectInfo, error) {
	o.reads++
	o.bucket, o.key, o.version = bucket, key, opts.VersionID
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		o.bounded = remaining > 0 && remaining <= 5*time.Second
	}
	return o.fresh, o.err
}

func TestPutObjectExpiryHeadersRefreshExactVersionHistory(t *testing.T) {
	savedObject, savedMetadata, savedLifecycle, savedGateway := newObjectLayerFn(), globalBucketMetadataSys, globalLifecycleSys, globalIsGateway
	t.Cleanup(func() {
		setObjectLayer(savedObject)
		globalBucketMetadataSys, globalLifecycleSys, globalIsGateway = savedMetadata, savedLifecycle, savedGateway
	})
	globalIsGateway = false
	globalLifecycleSys = NewLifecycleSys()
	globalBucketMetadataSys = NewBucketMetadataSys()
	lc := lifecycle.Lifecycle{Rules: []lifecycle.Rule{{ID: "current-rule", Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 30}}, {ID: "old-version-rule", Status: lifecycle.Enabled, NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{NoncurrentDays: 7}}}}
	globalBucketMetadataSys.metadataMap["bucket"] = BucketMetadata{lifecycleConfig: &lc}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	written := ObjectInfo{Bucket: "bucket", Name: "item", VersionID: "committed-version", ModTime: now.Add(-30 * 24 * time.Hour), ETag: "committed-etag", Size: 23}
	for _, latest := range []bool{true, false} {
		fresh := written
		fresh.IsLatest, fresh.NumVersions = latest, 2
		if !latest {
			fresh.SuccessorModTime = now.Add(-10 * 24 * time.Hour)
		}
		obj := &lifecycleHeaderObjectLayer{fresh: fresh}
		setObjectLayer(obj)
		recorder := httptest.NewRecorder()
		setPutObjHeaders(recorder, written, false)
		id, base, days := "current-rule", written.ModTime, 30
		if !latest {
			id, base, days = "old-version-rule", fresh.SuccessorModTime, 7
		}
		want := fmt.Sprintf(`expiry-date="%s", rule-id="%s"`, lifecycle.ExpectedExpiryTime(base, days).Format(http.TimeFormat), id)
		if got := lifecycleResponseHeader(recorder.Header(), xhttp.AmzExpiration); got != want {
			t.Fatalf("committed version history selected wrong expiry: latest=%v got=%s want=%s", latest, got, want)
		}
		if obj.reads != 1 || obj.bucket != written.Bucket || obj.key != written.Name || obj.version != written.VersionID || !obj.bounded || lifecycleResponseHeader(recorder.Header(), xhttp.ETag) != `"committed-etag"` {
			t.Fatalf("response did not use bounded exact-version lookup or changed its written ETag: %+v", obj)
		}
	}
	for _, test := range []struct {
		name string
		err  error
		etag string
	}{
		{"deleted-before-reply", VersionNotFound{Bucket: "bucket", Object: "item", VersionID: written.VersionID}, written.ETag},
		{"replaced-null-version", nil, "replacement-etag"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fresh := written
			fresh.IsLatest, fresh.ETag = true, test.etag
			obj := &lifecycleHeaderObjectLayer{fresh: fresh, err: test.err}
			setObjectLayer(obj)
			recorder := httptest.NewRecorder()
			setPutObjHeaders(recorder, written, false)
			if lifecycleResponseHeader(recorder.Header(), xhttp.AmzExpiration) != "" || lifecycleResponseHeader(recorder.Header(), xhttp.ETag) != `"committed-etag"` || obj.reads != 1 {
				t.Fatal("failed/changed exact version generated an unreliable expiry header or changed committed response")
			}
		})
	}
	obj := &lifecycleHeaderObjectLayer{}
	setObjectLayer(obj)
	for _, known := range []ObjectInfo{
		{Bucket: written.Bucket, Name: written.Name, ModTime: written.ModTime},
		{Bucket: written.Bucket, Name: written.Name, VersionID: written.VersionID, IsLatest: true, ModTime: written.ModTime},
		{Bucket: written.Bucket, Name: written.Name, VersionID: written.VersionID, ModTime: written.ModTime, SuccessorModTime: now},
	} {
		setPutObjHeaders(httptest.NewRecorder(), known, false)
	}
	setPutObjHeaders(httptest.NewRecorder(), written, true)
	if obj.reads != 0 {
		t.Fatal("known history or delete responses performed an unnecessary lookup")
	}
}
