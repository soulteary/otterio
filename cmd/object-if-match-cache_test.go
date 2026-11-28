// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/soulteary/otterio/cmd/config/cache"
)

func TestIfMatchCacheUsesOrigin(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			const bucket, key = "if-match-cache", "object"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			old, err := obj.PutObject(ctx, bucket, key, ifMatchBackendReader(t, "old"), ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			disk, err := newDiskCache(ctx, t.TempDir(), cache.Config{Quota: 100, WatermarkLow: 80, WatermarkHigh: 99})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = disk.Put(ctx, bucket, key, strings.NewReader("old"), 3, nil, ObjectOptions{UserDefined: map[string]string{"etag": old.ETag}}, false); err != nil {
				t.Fatal(err)
			}
			if !disk.Exists(ctx, bucket, key) {
				t.Fatal("test did not populate the stale cache entry")
			}
			fresh, err := obj.PutObject(ctx, bucket, key, ifMatchBackendReader(t, "fresh origin"), ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			objects := &cacheObjects{cache: []*diskCache{disk}, InnerPutObjectFn: obj.PutObject}
			if _, err = objects.PutObject(ctx, bucket, key, ifMatchBackendReader(t, "stale overwrite"), ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: old.ETag}}); !isErrPreconditionFailed(err) {
				t.Fatalf("cached validator overrode the origin's validator: %v", err)
			}
			ifMatchBackendContents(t, obj, bucket, key, "fresh origin")

			objects.commitWriteback = true
			if _, err = objects.PutObject(ctx, bucket, key, ifMatchBackendReader(t, "queued overwrite"), ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: fresh.ETag}}); err == nil {
				t.Fatal("write-back cache acknowledged a conditional write")
			} else if _, ok := err.(NotImplemented); !ok {
				t.Fatalf("write-back condition: got %T %v, want NotImplemented", err, err)
			}
			ifMatchBackendContents(t, obj, bucket, key, "fresh origin")

			objects.commitWriteback = false
			if _, err = objects.PutObject(ctx, bucket, key, ifMatchBackendReader(t, "conditional winner"), ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: fresh.ETag}}); err != nil {
				t.Fatalf("origin-matched conditional write: %v", err)
			}
			ifMatchBackendContents(t, obj, bucket, key, "conditional winner")
			if disk.Exists(ctx, bucket, key) {
				t.Fatal("successful conditional write retained stale cached contents")
			}
		})
	})
}
