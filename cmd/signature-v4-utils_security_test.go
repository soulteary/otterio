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
	"net/http"
	"strings"
	"testing"
)

// All of the helpers below build a minimal *http.Request rather than going
// through newTestStreamingRequest / signing flows. The skipContentSha256Cksum
// hardenings under test are agnostic to credential plumbing; they only look
// at request URL and the X-Amz-Content-Sha256 header value, so a hand-rolled
// request keeps the test surface tight and the failure modes easy to read.

func newSha256TestRequest(t *testing.T, hdrValue string, valueSet bool) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if valueSet {
		req.Header["X-Amz-Content-Sha256"] = []string{hdrValue}
	}
	return req
}

func newPresignedSha256TestRequest(t *testing.T, queryValue string, querySet bool) *http.Request {
	t.Helper()
	url := "http://otterio.test/bucket/key?X-Amz-Credential=key%2F20260101%2Fus-east-1%2Fs3%2Faws4_request"
	if querySet {
		url += "&X-Amz-Content-Sha256=" + queryValue
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

// TestSkipContentSha256EmptyValue pins the empty-as-missing rule introduced by
// upstream-cve-backlog.md row 51's hardening. A literally empty
// X-Amz-Content-Sha256 header used to slip past the legacy
// `!(ok && v[0] != unsignedPayload)` guard (because v[0] != UNSIGNED-PAYLOAD)
// and force the canonicaliser into a bogus comparison against the empty
// string. The new helper treats empty as "not provided" and reports
// "skip checksum", matching the behaviour for a fully-absent header.
func TestSkipContentSha256EmptyValue(t *testing.T) {
	r := newSha256TestRequest(t, "", true)
	if !skipContentSha256Cksum(r) {
		t.Fatalf("empty X-Amz-Content-Sha256 should skip payload checksum but did not")
	}
	if got := getContentSha256Cksum(r, serviceS3); got != emptySHA256 {
		t.Fatalf("empty X-Amz-Content-Sha256 should default to emptySHA256, got %q", got)
	}
}

// TestSkipContentSha256Unsigned: explicit UNSIGNED-PAYLOAD must skip.
func TestSkipContentSha256Unsigned(t *testing.T) {
	r := newSha256TestRequest(t, unsignedPayload, true)
	if !skipContentSha256Cksum(r) {
		t.Fatalf("UNSIGNED-PAYLOAD must skip payload checksum")
	}
	if got := getContentSha256Cksum(r, serviceS3); got != unsignedPayload {
		t.Fatalf("UNSIGNED-PAYLOAD should round-trip through getContentSha256Cksum; got %q", got)
	}
}

// TestSkipContentSha256ValidHash: a real hash must NOT skip.
func TestSkipContentSha256ValidHash(t *testing.T) {
	const hash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	r := newSha256TestRequest(t, hash, true)
	if skipContentSha256Cksum(r) {
		t.Fatalf("real sha256 hash must NOT cause skip; the upper layer needs to validate it")
	}
	if got := getContentSha256Cksum(r, serviceS3); got != hash {
		t.Fatalf("getContentSha256Cksum should round-trip the real hash; got %q want %q", got, hash)
	}
}

// TestSkipContentSha256NoPanicOnMissing: completely absent header must not
// panic and must skip. The legacy code panicked on `v[0]` when len(v)==0 was
// reachable (e.g. through fasthttp's adapter), so this test pins the new
// nil-safe branch.
func TestSkipContentSha256NoPanicOnMissing(t *testing.T) {
	r := newSha256TestRequest(t, "", false)
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("skipContentSha256Cksum panicked on missing header: %v", rec)
		}
	}()
	if !skipContentSha256Cksum(r) {
		t.Fatalf("missing X-Amz-Content-Sha256 must skip payload checksum")
	}
	if got := getContentSha256Cksum(r, serviceS3); got != emptySHA256 {
		t.Fatalf("missing header on signed request defaults to emptySHA256, got %q", got)
	}
}

// TestSkipContentSha256PresignedDefaultsUnsigned: presigned requests with no
// X-Amz-Content-Sha256 anywhere default to UNSIGNED-PAYLOAD per AWS spec.
func TestSkipContentSha256PresignedDefaultsUnsigned(t *testing.T) {
	r := newPresignedSha256TestRequest(t, "", false)
	if !skipContentSha256Cksum(r) {
		t.Fatalf("presigned request without sha256 should skip payload validation")
	}
	if got := getContentSha256Cksum(r, serviceS3); got != unsignedPayload {
		t.Fatalf("presigned default sha256 should be UNSIGNED-PAYLOAD, got %q", got)
	}
}

// TestSkipContentSha256PresignedQueryWins ensures the query parameter (when
// present) takes precedence over the header for presigned requests, matching
// the legacy precedence rule.
func TestSkipContentSha256PresignedQueryWins(t *testing.T) {
	const goodHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	r := newPresignedSha256TestRequest(t, goodHash, true)
	r.Header.Set("X-Amz-Content-Sha256", unsignedPayload)
	if got := getContentSha256Cksum(r, serviceS3); got != goodHash {
		t.Fatalf("presigned query should win over header: got %q want %q", got, goodHash)
	}
	if skipContentSha256Cksum(r) {
		t.Fatalf("presigned request with real query sha256 must NOT skip")
	}
}

// TestSignedHeadersLowercased pins the new lowercase normalisation in
// parseSignedHeader. SignedHeaders=Host;X-Amz-Content-Sha256 (mixed case) and
// SignedHeaders=host;x-amz-content-sha256 (canonical case) MUST yield the
// same parsed slice. This is what makes utils.contains(signedHeaders, "host")
// and the `case "host":` branch in extractSignedHeaders behave identically
// regardless of how the client capitalised its SignedHeaders list.
func TestSignedHeadersLowercased(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"SignedHeaders=Host;X-Amz-Content-Sha256;X-Amz-Date", []string{"host", "x-amz-content-sha256", "x-amz-date"}},
		{"SignedHeaders=host;x-amz-content-sha256;x-amz-date", []string{"host", "x-amz-content-sha256", "x-amz-date"}},
		{"SignedHeaders=HOST;X-AMZ-DATE", []string{"host", "x-amz-date"}},
	}
	for _, tc := range cases {
		got, err := parseSignedHeader(tc.in)
		if err != ErrNone {
			t.Fatalf("parseSignedHeader(%q) returned error code %v", tc.in, err)
		}
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("parseSignedHeader(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
