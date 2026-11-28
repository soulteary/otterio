// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"net/http"

	xhttp "github.com/soulteary/otterio/cmd/http"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

// This capability is separate from conditional PUT and multipart completion.
// It promises comparison and current-object deletion under the same write lock.
const conditionalDeleteHeader = "X-Otterio-Conditional-Delete"

func conditionalDeleteSupported() bool {
	return !globalIsDistErasure && !globalBucketFederation && globalDNSConfig == nil &&
		newCachedObjectLayerFn() == nil && conditionalWritesSupported()
}

func authorizeObjectIfMatchDelete(ctx context.Context, r *http.Request, bucket, object string, current ObjectInfo) APIErrorCode {
	policyRequest := withObjectTags(r, current.UserTags)
	for _, action := range []iampolicy.Action{iampolicy.GetObjectAction, iampolicy.DeleteObjectAction} {
		if code := isPutActionAllowed(ctx, getRequestAuthType(r), bucket, object, policyRequest, action); code != ErrNone {
			return code
		}
	}
	return ErrNone
}

// Authenticate before any current-object lookup. Policy evaluation follows the
// lookup because both permissions can depend on stored tags. The callback is
// evaluated again under the backend's final delete lock.
func prepareObjectIfMatchDelete(ctx context.Context, r *http.Request, bucket, object string, get GetObjectInfoFn) (*ObjectIfMatchCondition, APIErrorCode) {
	switch getRequestAuthType(r) {
	case authTypeSigned, authTypePresigned:
	default:
		return nil, ErrAccessDenied
	}
	if code := reqSignatureV4Verify(r, globalServerRegion, serviceS3); code != ErrNone {
		return nil, code
	}
	cred, _, code := getReqAccessKeyV4(r, globalServerRegion, serviceS3)
	if code != ErrNone {
		return nil, code
	}
	if _, code = checkClaimsFromToken(r, cred); code != ErrNone {
		return nil, code
	}
	if len(r.Header.Values(xhttp.IfNoneMatch)) != 0 {
		return nil, ErrInvalidRequest
	}
	condition, code := parseObjectIfMatch(r.Header.Values(xhttp.IfMatch))
	if code != ErrNone {
		return nil, code
	}
	if condition == nil || condition.Any || !conditionalDeleteSupported() {
		return nil, ErrNotImplemented
	}
	// Do not turn a current-key deletion into a historical-version purge or
	// replication/import operation. In particular, presence of versionId=null
	// is an explicit version selection too.
	if r.URL.Query().Has(xhttp.VersionID) || hasAnyOtterIOSourceHeader(r.Header) ||
		len(r.Header.Values(xhttp.AmzBucketReplicationStatus)) != 0 ||
		len(r.Header.Values(xhttp.AmzObjectLockBypassGovernance)) != 0 ||
		len(r.Header.Values(xhttp.OtterIOForceDelete)) != 0 {
		return nil, ErrNotImplemented
	}
	if code = requireSignedObjectIfMatch(r); code != ErrNone {
		return nil, code
	}
	condition.Authorize = func(current ObjectInfo) error {
		if authorizeObjectIfMatchDelete(ctx, r, bucket, object, current) != ErrNone {
			return errAuthentication
		}
		return nil
	}
	current, err := get(ctx, bucket, object, ObjectOptions{})
	if err != nil || current.DeleteMarker {
		current = ObjectInfo{}
	}
	if code = authorizeObjectIfMatchDelete(ctx, r, bucket, object, current); code != ErrNone {
		return nil, code
	}
	if err != nil && !isErrObjectNotFound(toObjectErr(err, bucket, object)) {
		return nil, toAPIErrorCode(ctx, err)
	}
	return condition, ErrNone
}

// Called with the namespace write lock held until deletion commits. Internal
// callers are responsible for authorization, as with conditional object PUT.
func checkObjectDeletePreconditions(ctx context.Context, bucket, object string, opts ObjectOptions, get GetObjectInfoFn) error {
	if opts.RequireNewObject {
		return NotImplemented{Message: "If-None-Match is not supported for DELETE"}
	}
	if opts.IfMatch == nil {
		return nil
	}
	if !conditionalDeleteSupported() || opts.IfMatch.Any || opts.VersionID != "" || opts.DeleteMarker ||
		opts.DeleteMarkerReplicationStatus != "" || !opts.VersionPurgeStatus.Empty() ||
		opts.TransitionStatus != "" || opts.TransitionedObject != nil || opts.TransitionExpected != nil {
		return NotImplemented{Message: "Conditional DELETE supports a strong ETag on the current object only"}
	}
	return checkObjectWritePreconditions(ctx, bucket, object, opts, get)
}
