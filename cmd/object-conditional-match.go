// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/soulteary/otterio/cmd/crypto"
	xhttp "github.com/soulteary/otterio/cmd/http"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

// This is deliberately separate from conditionalWritesHeader: its v1 contract
// continues to mean create-only PUT and CompleteMultipartUpload.
const conditionalMatchWritesHeader = "X-Otterio-Conditional-Match"

// ObjectIfMatchCondition describes a condition on the current destination,
// never an explicitly selected historical version. Authorize runs under the
// destination write lock, before comparing or exposing current metadata.
// Internal object-layer callers are responsible for their own authorization.
type ObjectIfMatchCondition struct {
	ETag      string
	Any       bool
	Authorize func(ObjectInfo) error
}

func conditionalMatchWritesSupported() bool {
	// Distributed placement and mutation paths need their own acceptance.
	return !globalIsDistErasure && conditionalWritesSupported()
}

// parseObjectIfMatch supports one strong entity tag (quoted or the customary
// bare S3 ETag) or '*'. Lists and weak validators are not this v1 contract.
func parseObjectIfMatch(values []string) (*ObjectIfMatchCondition, APIErrorCode) {
	if len(values) == 0 {
		return nil, ErrNone
	}
	if len(values) != 1 {
		return nil, ErrInvalidRequest
	}
	value := strings.Trim(values[0], " \t")
	if value == "*" {
		return &ObjectIfMatchCondition{Any: true}, ErrNone
	}
	if value == "" {
		return nil, ErrInvalidRequest
	}
	if strings.HasPrefix(value, "W/") {
		return nil, ErrNotImplemented
	}
	if value[0] == '"' {
		end := strings.IndexByte(value[1:], '"')
		if end < 0 {
			return nil, ErrInvalidRequest
		}
		end++
		if tail := strings.Trim(value[end+1:], " \t"); tail != "" {
			if strings.HasPrefix(tail, ",") {
				return nil, ErrNotImplemented
			}
			return nil, ErrInvalidRequest
		}
		value = value[1:end]
	} else if strings.ContainsAny(value, ",*") {
		return nil, ErrNotImplemented
	}
	if value == "" {
		return nil, ErrInvalidRequest
	}
	for i := 0; i < len(value); i++ {
		// etagc: %x21 / %x23-7E / obs-text. ETags are opaque and
		// case-sensitive; do not unescape or normalize their contents.
		if value[i] < 0x21 || value[i] == '"' || value[i] == 0x7f {
			return nil, ErrInvalidRequest
		}
	}
	return &ObjectIfMatchCondition{ETag: value}, ErrNone
}

func objectWriteSignedHeaders(r *http.Request) ([]string, APIErrorCode) {
	switch getRequestAuthType(r) {
	case authTypeSigned, authTypeStreamingSigned:
		signed, code := parseSignV4(r.Header.Get(xhttp.Authorization), globalServerRegion, serviceS3)
		if code != ErrNone {
			return nil, code
		}
		return signed.SignedHeaders, ErrNone
	case authTypePresigned:
		signed, code := parsePreSignV4(r.URL.Query(), globalServerRegion, serviceS3)
		if code != ErrNone {
			return nil, code
		}
		return signed.SignedHeaders, ErrNone
	default:
		return nil, ErrNone
	}
}

// Called only after the handler has verified the request signature. Requiring
// the condition in SignedHeaders binds delegated/presigned uploads to it.
func requireSignedObjectIfMatch(r *http.Request) APIErrorCode {
	signedHeaders, code := objectWriteSignedHeaders(r)
	if code != ErrNone {
		return code
	}
	if signedHeaders == nil {
		return ErrAccessDenied
	}
	if !contains(signedHeaders, "if-match") {
		return ErrUnsignedHeaders
	}
	return ErrNone
}

func authorizeObjectIfMatch(ctx context.Context, r *http.Request, bucket, object string, current ObjectInfo) APIErrorCode {
	policyRequest := withObjectTags(r, current.UserTags)
	// This policy evaluator does not re-read the upload body and supports
	// streaming SigV4. Reusing checkRequestAuthType here would reject it.
	for _, action := range []iampolicy.Action{iampolicy.GetObjectAction, iampolicy.PutObjectAction} {
		if code := isPutActionAllowed(ctx, getRequestAuthType(r), bucket, object, policyRequest, action); code != ErrNone {
			return code
		}
	}
	return ErrNone
}

// The handler verifies the signature (or streaming seed) before calling this
// preliminary authorization. Validate the identity and session before reading
// current metadata, and authorize against origin tags before exposing lookup
// errors or inspecting an upload. The commit callback repeats authorization
// under the write lock because the current object can change after this read.
func authorizeObjectIfMatchBeforeWrite(ctx context.Context, r *http.Request, bucket, object string, get GetObjectInfoFn) APIErrorCode {
	// Validate the supported operation and condition before doing an origin
	// lookup, including when the active backend cannot promise this contract.
	var preliminaryOpts ObjectOptions
	if code := setObjectWriteConditions(ctx, r, bucket, object, &preliminaryOpts); code != ErrNone {
		return code
	}
	cred, _, code := getReqAccessKeyV4(r, globalServerRegion, serviceS3)
	if code != ErrNone {
		return code
	}
	if _, code = checkClaimsFromToken(r, cred); code != ErrNone {
		return code
	}
	current, err := get(ctx, bucket, object, ObjectOptions{})
	if err != nil || current.DeleteMarker {
		current = ObjectInfo{}
	}
	if code = authorizeObjectIfMatch(ctx, r, bucket, object, current); code != ErrNone {
		return code
	}
	if err != nil && !isErrObjectNotFound(toObjectErr(err, bucket, object)) {
		return toAPIErrorCode(ctx, err)
	}
	return ErrNone
}

func setObjectWriteConditions(ctx context.Context, r *http.Request, bucket, object string, opts *ObjectOptions) APIErrorCode {
	values := r.Header.Values(xhttp.IfMatch)
	if len(values) == 0 {
		var code APIErrorCode
		opts.RequireNewObject, code = conditionalWriteOptions(r)
		return code
	}
	if len(r.Header.Values(xhttp.IfNoneMatch)) != 0 {
		return ErrInvalidRequest
	}
	condition, code := parseObjectIfMatch(values)
	if code != ErrNone {
		return code
	}
	if !conditionalMatchWritesSupported() {
		return ErrNotImplemented
	}
	// Replication/import writes can select historical versions or replace
	// identity metadata; those are not current-object conditional writes.
	if opts.VersionID != "" || r.URL.Query().Get(xhttp.VersionID) != "" || hasAnyOtterIOSourceHeader(r.Header) {
		return ErrNotImplemented
	}
	if code = requireSignedObjectIfMatch(r); code != ErrNone {
		return code
	}
	condition.Authorize = func(current ObjectInfo) error {
		if authorizeObjectIfMatch(ctx, r, bucket, object, current) != ErrNone {
			return errAuthentication
		}
		return nil
	}
	opts.IfMatch = condition
	return ErrNone
}

// Called only with the destination namespace write lock held. The same lock
// must remain held until commit, and all competing mutations must participate.
func checkObjectWritePreconditions(ctx context.Context, bucket, object string, opts ObjectOptions, get GetObjectInfoFn) error {
	if opts.IfMatch == nil {
		if !opts.RequireNewObject {
			return nil
		}
		return requireNewErasureObject(ctx, bucket, object, opts, get)
	}
	if opts.RequireNewObject || opts.VersionID != "" {
		return NotImplemented{Message: "If-Match cannot be combined with create-only or explicit-version writes"}
	}
	current, err := get(ctx, bucket, object, ObjectOptions{
		NoLock: true, Versioned: opts.Versioned, VersionSuspended: opts.VersionSuspended,
	})
	missing := isErrObjectNotFound(toObjectErr(err, bucket, object))
	if err != nil && !missing {
		return err
	}
	if current.DeleteMarker {
		missing = true
	}
	if opts.IfMatch.Authorize != nil {
		authoritative := current
		if missing {
			authoritative = ObjectInfo{}
		}
		if err := opts.IfMatch.Authorize(authoritative); err != nil {
			return err
		}
	}
	if missing {
		return ObjectNotFound{Bucket: bucket, Object: object}
	}
	// FS can synthesize metadata for files created outside the object API.
	// Its placeholder validator and directory entries are not authoritative
	// object snapshots, including for wildcard conditions and tag policies.
	if current.IsDir || current.ETag == "" || current.ETag == defaultEtag {
		return NotImplemented{Message: "If-Match requires authoritative current object metadata"}
	}
	// Historical compressed PUT responses and GET/HEAD validators are not
	// consistent. Do not guess which validator the client observed.
	if current.IsCompressed() {
		return NotImplemented{Message: "If-Match on a compressed current object is not supported"}
	}
	if opts.IfMatch.Any {
		return nil
	}
	visible, err := objectIfMatchETag(bucket, object, current)
	if err != nil {
		return err
	}
	if visible != opts.IfMatch.ETag {
		return PreConditionFailed{}
	}
	return nil
}

// Resolve the validator exposed by native GET/HEAD without silently falling
// back to an encrypted storage ETag when key unsealing fails. Destination SSE
// headers describe the new object and must not select the current object's key.
func objectIfMatchETag(bucket, object string, current ObjectInfo) (string, error) {
	if current.ETag == "" {
		return "", errObjectTampered
	}
	_, encrypted := crypto.IsEncrypted(current.UserDefined)
	if !encrypted || len(current.ETag) == 32 || crypto.IsMultiPart(current.UserDefined) {
		return current.ETag, nil
	}
	if crypto.SSEC.IsEncrypted(current.UserDefined) {
		// SSE-C exposes an opaque suffix, including on HEAD. No customer
		// decryption key is needed to compare this already-authorized tag.
		if len(current.ETag) < 32 {
			return "", errObjectTampered
		}
		if _, err := hex.DecodeString(current.ETag); err != nil {
			return "", errObjectTampered
		}
		return current.ETag[len(current.ETag)-32:], nil
	}
	keyBytes, err := decryptObjectInfo(nil, bucket, object, current.UserDefined)
	if err != nil {
		return "", err
	}
	var key crypto.ObjectKey
	if len(keyBytes) != len(key) {
		return "", errObjectTampered
	}
	copy(key[:], keyBytes)
	return DecryptETag(key, current)
}
