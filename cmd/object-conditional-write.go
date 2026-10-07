// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"net/http"
)

const conditionalWritesHeader = "X-Otterio-Conditional-Writes"

// V1 promises only atomic If-None-Match: * for PUT and multipart completion.
// Multi-pool placement and gateway implementations need a separate lock design.
func conditionalWritesSupported() bool {
	if globalIsGateway {
		return false
	}
	// An acknowledged write-back entry may not exist in origin storage yet;
	// its later unconditional flush could replace a conditional create.
	if cache := newCachedObjectLayerFn(); cache != nil {
		objects, ok := cache.(*cacheObjects)
		if !ok || objects.commitWriteback {
			return false
		}
	}
	switch obj := newObjectLayerFn().(type) {
	case *FSObjects:
		return true
	case *erasureServerPools:
		return obj.SinglePool()
	default:
		return false
	}
}

func conditionalWriteOptions(r *http.Request) (bool, APIErrorCode) {
	if len(r.Header.Values("If-Match")) != 0 {
		return false, ErrNotImplemented
	}
	values := r.Header.Values("If-None-Match")
	if len(values) == 0 {
		return false, ErrNone
	}
	if len(values) != 1 || values[0] != "*" || !conditionalWritesSupported() {
		return false, ErrNotImplemented
	}
	return true, ErrNone
}

// Called only with the destination namespace write lock already held.
func requireNewObject(ctx context.Context, bucket, object string, get func(context.Context, string, string) (ObjectInfo, error)) error {
	_, err := get(ctx, bucket, object)
	if err == nil {
		return PreConditionFailed{}
	}
	if isErrObjectNotFound(toObjectErr(err, bucket, object)) {
		return nil
	}
	return err
}

func requireNewErasureObject(ctx context.Context, bucket, object string, opts ObjectOptions, get GetObjectInfoFn) error {
	return requireNewObject(ctx, bucket, object, func(ctx context.Context, bucket, object string) (ObjectInfo, error) {
		return get(ctx, bucket, object, ObjectOptions{NoLock: true, Versioned: opts.Versioned, VersionSuspended: opts.VersionSuspended})
	})
}
