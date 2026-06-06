/*
 * OtterIO Cloud Storage, (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cmd

import (
	"context"
	"errors"
	"net/http"
	"testing"

	xhttp "github.com/soulteary/otterio/cmd/http"
)

// withAnonymousPolicySys arms globalPolicySys / globalBucketMetadataSys for
// the duration of one test so that isPutActionAllowed's anonymous branch can
// run end-to-end without panicking on a nil PolicySys. Because globalObjectAPI
// is left as nil, BucketMetadataSys.GetConfig short-circuits with
// errServerNotInitialized, PolicySys.IsAllowed falls back to args.IsOwner
// (false), and the caller is denied - which is exactly the situation an
// unauthenticated client carrying X-Otterio-Source-* would see in a freshly
// booted server. We restore the previous globals on cleanup so other tests
// in the package are unaffected.
func withAnonymousPolicySys(t *testing.T) {
	t.Helper()
	prevPolicy := globalPolicySys
	prevMeta := globalBucketMetadataSys
	globalPolicySys = NewPolicySys()
	globalBucketMetadataSys = NewBucketMetadataSys()
	t.Cleanup(func() {
		globalPolicySys = prevPolicy
		globalBucketMetadataSys = prevMeta
	})
}

// TestHasAnyOtterIOSourceHeader pins the cheap pre-check used to short-circuit
// the IAM evaluation for ordinary S3 traffic. Every header in
// otterioSourceHeaderKeys must trip the helper, an unrelated header must not,
// and a nil http.Header must be safe (the helper is called from getOpts /
// putOpts / delOpts which can in principle be invoked with a request that
// has no headers populated, e.g. inside copy paths).
func TestHasAnyOtterIOSourceHeader(t *testing.T) {
	cases := []struct {
		name string
		hdr  http.Header
		want bool
	}{
		{name: "nil header", hdr: nil, want: false},
		{name: "empty header", hdr: http.Header{}, want: false},
		{
			name: "unrelated header only",
			hdr:  http.Header{"X-Amz-Date": []string{"20260606T000000Z"}},
			want: false,
		},
		{
			name: "MTime tripping",
			hdr:  http.Header{xhttp.OtterIOSourceMTime: []string{"2025-01-02T03:04:05Z"}},
			want: true,
		},
		{
			name: "ETag tripping",
			hdr:  http.Header{xhttp.OtterIOSourceETag: []string{"deadbeef"}},
			want: true,
		},
		{
			name: "DeleteMarker tripping",
			hdr:  http.Header{xhttp.OtterIOSourceDeleteMarker: []string{"true"}},
			want: true,
		},
		{
			name: "DeleteMarkerDelete tripping",
			hdr:  http.Header{xhttp.OtterIOSourceDeleteMarkerDelete: []string{"true"}},
			want: true,
		},
		{
			name: "ReplicationRequest tripping",
			hdr:  http.Header{xhttp.OtterIOSourceReplicationRequest: []string{""}},
			want: true,
		},
		{
			name: "MTime present but empty value still trips (defence against suppression of audit signal via empty value)",
			hdr:  http.Header{xhttp.OtterIOSourceMTime: []string{""}},
			want: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := hasAnyOtterIOSourceHeader(tc.hdr); got != tc.want {
				t.Fatalf("hasAnyOtterIOSourceHeader(%v) = %v want %v", tc.hdr, got, tc.want)
			}
		})
	}
}

// TestEnforceSourceHeaderIAMNoHeaderSkips pins the zero-overhead invariant
// promised by the SECURITY comment at the top of cmd/object-api-options.go:
// a request that carries none of the IAM-gated source headers must never
// reach the IAM evaluator, so it must return nil even when globalPolicySys
// and globalIAMSys are completely unset. This is what guarantees that the
// new gate cannot regress the latency of normal S3 traffic.
func TestEnforceSourceHeaderIAMNoHeaderSkips(t *testing.T) {
	r, err := http.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := enforceSourceHeaderIAM(context.Background(), r, "bucket", "key"); err != nil {
		t.Fatalf("enforceSourceHeaderIAM with no source header should be nil, got %v", err)
	}
}

// TestEnforceSourceHeaderIAMAnonymousRejected pins the core security invariant
// for backlog row 4.1: an anonymous caller carrying any X-Otterio-Source-*
// header must be rejected with errAccessDeniedReplicationHeader. Because the
// test does not arm a bucket policy, isPutActionAllowed's anonymous branch
// falls through to the deny path, which is exactly the situation a low-trust
// public endpoint sees on a freshly booted server. If the gate ever regresses
// to "fail open" this test fires.
func TestEnforceSourceHeaderIAMAnonymousRejected(t *testing.T) {
	withAnonymousPolicySys(t)

	r, err := http.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	r.Header.Set(xhttp.OtterIOSourceMTime, "2024-01-02T03:04:05Z")

	gotErr := enforceSourceHeaderIAM(context.Background(), r, "bucket", "key")
	if !errors.Is(gotErr, errAccessDeniedReplicationHeader) {
		t.Fatalf("anonymous source header must be rejected with errAccessDeniedReplicationHeader, got %v", gotErr)
	}
}

// TestErrAccessDeniedReplicationHeaderTranslatesTo403 pins the wire-level
// contract: the sentinel error produced by enforceSourceHeaderIAM must be
// translated by toAPIErrorCode into ErrAccessDeniedReplicationHeader, and
// the registered errorCodeMap entry must surface as HTTP 403 with the
// generic S3 code "AccessDenied" (so callers cannot fingerprint the
// replication-specific permission state, while audit logs still get the
// distinct internal code).
func TestErrAccessDeniedReplicationHeaderTranslatesTo403(t *testing.T) {
	got := toAPIErrorCode(context.Background(), errAccessDeniedReplicationHeader)
	if got != ErrAccessDeniedReplicationHeader {
		t.Fatalf("toAPIErrorCode(errAccessDeniedReplicationHeader) = %v want ErrAccessDeniedReplicationHeader", got)
	}
	apiErr := errorCodes.ToAPIErr(ErrAccessDeniedReplicationHeader)
	if apiErr.HTTPStatusCode != http.StatusForbidden {
		t.Fatalf("ErrAccessDeniedReplicationHeader HTTP status = %d want 403", apiErr.HTTPStatusCode)
	}
	if apiErr.Code != "AccessDenied" {
		t.Fatalf("ErrAccessDeniedReplicationHeader S3 code = %q want %q (so callers cannot fingerprint replication-specific permission state)", apiErr.Code, "AccessDenied")
	}
}

// TestPutOptsRejectsAnonymousSourceHeader is the end-to-end version of
// TestEnforceSourceHeaderIAMAnonymousRejected: it drives a real putOpts call
// to confirm the gate is wired in at the entrypoint that production handlers
// actually use. We confirm both the sentinel identity (so other layers can
// errors.Is on it) and the wire translation (so the client genuinely sees a
// 403 / AccessDenied).
func TestPutOptsRejectsAnonymousSourceHeader(t *testing.T) {
	withAnonymousPolicySys(t)

	r, err := http.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	r.Header.Set(xhttp.OtterIOSourceETag, "deadbeefcafebabe")

	_, gotErr := putOpts(context.Background(), r, "bucket", "key", nil)
	if !errors.Is(gotErr, errAccessDeniedReplicationHeader) {
		t.Fatalf("putOpts must reject anonymous X-Otterio-Source-* with errAccessDeniedReplicationHeader, got %v", gotErr)
	}
	if got := toAPIErrorCode(context.Background(), gotErr); got != ErrAccessDeniedReplicationHeader {
		t.Fatalf("putOpts rejection must translate to ErrAccessDeniedReplicationHeader at the handler boundary, got %v", got)
	}
}
