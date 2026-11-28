// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"net/http"

	xhttp "github.com/soulteary/otterio/cmd/http"
)

// Conditional copy is a separate contract from create-only PUT/completion.
// It preserves source-copy preconditions and creates only a missing target.
const conditionalCopyWritesHeader = "X-Otterio-Conditional-Copy"

func conditionalCopyWritesSupported() bool {
	// Cached sources and deferred writes cannot participate in the same
	// origin-lock contract. Remote placement needs separate acceptance.
	return !globalIsDistErasure && !globalBucketFederation && globalDNSConfig == nil && newCachedObjectLayerFn() == nil && conditionalWritesSupported()
}

// Called after signature validation, before reading the source. Conditions
// belong to the destination; the existing copy-source headers remain separate.
func setObjectCopyWriteConditions(r *http.Request, opts *ObjectOptions) APIErrorCode {
	if len(r.Header.Values(xhttp.IfMatch)) != 0 {
		return ErrNotImplemented
	}
	values := r.Header.Values(xhttp.IfNoneMatch)
	if len(values) == 0 {
		return ErrNone
	}
	if len(values) != 1 || values[0] != "*" || !conditionalCopyWritesSupported() {
		return ErrNotImplemented
	}
	if opts.VersionID != "" || r.URL.Query().Has(xhttp.VersionID) || hasAnyOtterIOSourceHeader(r.Header) {
		return ErrNotImplemented
	}
	signedHeaders, code := objectWriteSignedHeaders(r)
	if code != ErrNone {
		return code
	}
	if signedHeaders == nil {
		return ErrAccessDenied
	}
	if !contains(signedHeaders, "if-none-match") {
		return ErrUnsignedHeaders
	}
	opts.RequireNewObject = true
	return ErrNone
}

// Object-layer callers must not silently lose a destination condition through
// a metadata-only copy or a backend that does not implement this contract.
func checkConditionalCopyOptions(opts ObjectOptions) error {
	if opts.IfMatch != nil || opts.RequireNewObject && (!conditionalCopyWritesSupported() || opts.VersionID != "") {
		return NotImplemented{}
	}
	return nil
}
