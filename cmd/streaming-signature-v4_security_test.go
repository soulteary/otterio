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
	"testing"

	xhttp "github.com/soulteary/otterio/cmd/http"
)

// newStreamingSeedRequest hand-rolls a request shaped like the one
// calculateSeedSignature is supposed to consume: aws-chunked encoding, the
// streaming sha256 marker, and an Authorization header whose SignedHeaders
// list is fully under our control. We don't bother computing a valid
// signature - the property under test (decoded-length must be in
// SignedHeaders) is checked before signature comparison, so the test only
// needs to drive the function as far as the new guard.
func newStreamingSeedRequest(t *testing.T, signedHeaders string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Encoding", "aws-chunked")
	req.Header.Set(xhttp.AmzContentSha256, streamingContentSHA256)
	req.Header.Set(xhttp.AmzDate, "20260101T000000Z")
	req.Header.Set("X-Amz-Decoded-Content-Length", "65536")
	auth := "AWS4-HMAC-SHA256 " +
		"Credential=AKIA/20260101/us-east-1/s3/aws4_request, " +
		"SignedHeaders=" + signedHeaders + ", " +
		"Signature=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	req.Header.Set(xhttp.Authorization, auth)
	return req
}

// TestSeedSignatureRequiresDecodedLengthSigned: an aws-chunked PUT whose
// SignedHeaders list omits `x-amz-decoded-content-length` MUST be rejected
// with ErrUnsignedHeaders before signature comparison. This is the new
// hardening for upstream-cve-backlog row 51's "x-amz-decoded-content-length
// not in SignedHeaders" sub-issue: leaving that header unsigned lets an
// attacker rewrite the decoded length post-signature, desynchronising the
// chunked reader from the upstream length checks.
func TestSeedSignatureRequiresDecodedLengthSigned(t *testing.T) {
	r := newStreamingSeedRequest(t, "host;x-amz-content-sha256;x-amz-date")
	_, _, _, _, errCode := calculateSeedSignature(r)
	if errCode != ErrUnsignedHeaders {
		t.Fatalf("expected ErrUnsignedHeaders when decoded-length is absent from SignedHeaders, got %v", errCode)
	}
}

// TestSeedSignatureAcceptsDecodedLengthSigned is the negative control: when
// the SignedHeaders list does include `x-amz-decoded-content-length` the new
// guard does NOT fire. Downstream steps (credential validation, signature
// comparison) will still fail because we don't have a real key, but they MUST
// fail with a different error code than the one we just added. This pins the
// guard against accidental over-firing.
func TestSeedSignatureAcceptsDecodedLengthSigned(t *testing.T) {
	r := newStreamingSeedRequest(t, "host;x-amz-content-sha256;x-amz-date;x-amz-decoded-content-length")
	_, _, _, _, errCode := calculateSeedSignature(r)
	if errCode == ErrUnsignedHeaders {
		t.Fatalf("decoded-length present in SignedHeaders: should NOT trigger ErrUnsignedHeaders, got %v", errCode)
	}
}

// TestSeedSignatureMixedCaseDecodedLength pins down the interaction with the
// new SignedHeaders-lowercase normalization. A non-conforming client that
// sends SignedHeaders=...;X-Amz-Decoded-Content-Length (mixed case) used to
// slip past contains(_, "x-amz-decoded-content-length") and trigger
// ErrUnsignedHeaders. After parseSignedHeader normalization that branch is
// reachable iff the header truly was signed.
func TestSeedSignatureMixedCaseDecodedLength(t *testing.T) {
	r := newStreamingSeedRequest(t, "Host;X-Amz-Content-Sha256;X-Amz-Date;X-Amz-Decoded-Content-Length")
	_, _, _, _, errCode := calculateSeedSignature(r)
	if errCode == ErrUnsignedHeaders {
		t.Fatalf("mixed-case decoded-length must be matched after lowercase normalization, got %v", errCode)
	}
}
