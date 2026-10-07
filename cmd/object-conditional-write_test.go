// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/soulteary/otterio/pkg/hash"
)

func TestConditionalCreateDestinationLock(t *testing.T) {
	ExecObjectLayerTest(t, testConditionalCreateDestinationLock)
}

func TestConditionalWriteCapabilitiesFailClosed(t *testing.T) {
	savedObject, savedCache, savedGateway := newObjectLayerFn(), newCachedObjectLayerFn(), globalIsGateway
	t.Cleanup(func() {
		setObjectLayer(savedObject)
		setCacheObjectLayer(savedCache)
		globalIsGateway = savedGateway
	})
	setObjectLayer(&FSObjects{})
	setCacheObjectLayer(nil)
	globalIsGateway = false
	if !conditionalWritesSupported() {
		t.Fatal("filesystem capability is missing")
	}
	setCacheObjectLayer(&cacheObjects{commitWriteback: true})
	if conditionalWritesSupported() {
		t.Fatal("write-back cache must not promise atomic origin checks")
	}
	r := httptest.NewRequest(http.MethodPut, "/", nil)
	r.Header.Set("If-None-Match", "*")
	if _, code := conditionalWriteOptions(r); code != ErrNotImplemented {
		t.Fatal("write-back conditional request must be rejected")
	}
	setCacheObjectLayer(&cacheObjects{})
	if !conditionalWritesSupported() {
		t.Fatal("write-through capability is missing")
	}
	setCacheObjectLayer(nil)
	globalIsGateway = true
	if conditionalWritesSupported() {
		t.Fatal("gateway capability must be rejected")
	}
	globalIsGateway = false
	setObjectLayer(&erasureServerPools{serverPools: []*erasureSets{{}, {}}})
	if conditionalWritesSupported() {
		t.Fatal("multi-pool placement must not promise atomic existence checks")
	}
	setObjectLayer(nil)
	if conditionalWritesSupported() {
		t.Fatal("uninitialized storage must not advertise a capability")
	}
}

func testConditionalCreateDestinationLock(obj ObjectLayer, instance string, t TestErrHandler) {
	ctx := context.Background()
	const bucket = "conditional-create"
	if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	reader := func(data []byte) *PutObjReader {
		r, err := hash.NewReader(bytes.NewReader(data), int64(len(data)), "", "", int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		return NewPutObjReader(r)
	}
	checkContents := func(key string, want []byte) {
		r, err := obj.GetObjectNInfo(ctx, bucket, key, nil, http.Header{}, readLock, ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		r.Close()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: %q content changed: %q / %v", instance, key, got, err)
		}
	}
	// All contenders must serialize their existence check with final commit.
	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = obj.PutObject(ctx, bucket, "race", reader([]byte("winner")), ObjectOptions{RequireNewObject: true})
		}(i)
	}
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if !isErrPreconditionFailed(err) {
			t.Fatalf("%s: unexpected contender error: %v", instance, err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%s: %d conditional writes succeeded, want 1", instance, succeeded)
	}
	checkContents("race", []byte("winner"))
	// A zero-byte current object also counts as existing.
	if _, err := obj.PutObject(ctx, bucket, "empty", reader(nil), ObjectOptions{RequireNewObject: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := obj.PutObject(ctx, bucket, "empty", reader([]byte("replacement")), ObjectOptions{RequireNewObject: true}); !isErrPreconditionFailed(err) {
		t.Fatalf("%s: empty-object replacement: %v", instance, err)
	}
	checkContents("empty", nil)
	// A key created after the parts were uploaded must survive completion.
	for _, key := range []string{"multipart-conflict", "multipart-created"} {
		id, err := obj.NewMultipartUpload(ctx, bucket, key, ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		part, err := obj.PutObjectPart(ctx, bucket, key, id, 1, reader([]byte("multipart")), ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if key == "multipart-conflict" {
			if _, err := obj.PutObject(ctx, bucket, key, reader([]byte("new-current")), ObjectOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		_, err = obj.CompleteMultipartUpload(ctx, bucket, key, id, []CompletePart{{PartNumber: 1, ETag: part.ETag}}, ObjectOptions{RequireNewObject: true})
		if key == "multipart-conflict" {
			if !isErrPreconditionFailed(err) {
				t.Fatalf("%s: multipart conflict: %v", instance, err)
			}
			checkContents(key, []byte("new-current"))
			if err := obj.AbortMultipartUpload(ctx, bucket, key, id, ObjectOptions{}); err != nil {
				t.Fatal(err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			checkContents(key, []byte("multipart"))
		}
	}
	if !conditionalWritesSupported() {
		t.Fatalf("%s: supported backend not advertised", instance)
	}
	r := httptest.NewRequest(http.MethodPut, "/", nil)
	r.Header.Set("If-None-Match", "*")
	if required, code := conditionalWriteOptions(r); !required || code != ErrNone {
		t.Fatalf("%s: conditional options: required=%v code=%v", instance, required, code)
	}
	r.Header.Add("If-None-Match", "*")
	if _, code := conditionalWriteOptions(r); code != ErrNotImplemented {
		t.Fatalf("%s: duplicate conditions not rejected", instance)
	}
	if toAPIErrorCode(ctx, PreConditionFailed{}) != ErrPreconditionFailed {
		t.Fatal("precondition errors must map to HTTP 412")
	}
}
