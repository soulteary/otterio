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
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// buildFiberSecurityChain mirrors the production middleware order from
// fiber_router.go (globalFiberHandlers) restricted to the handlers that are
// relevant for the regression cases below: filterReservedMetadataFiber
// rejects X-Otterio-Internal-* header smuggling, addCustomHeadersFiber stamps
// x-amz-request-id, and the catch-all ok handler stands in for the real S3 /
// admin handler so each test can assert on outcome independently.
func buildFiberSecurityChain(t *testing.T) *fiber.App {
	t.Helper()
	app := newFiberApp()
	app.Use(filterReservedMetadataFiber)
	app.All("/*", func(c fiber.Ctx) error {
		c.Status(http.StatusOK)
		return nil
	})
	return app
}

// TestFiberRejectsWrappedReservedMetadata is the cross-cutting regression
// test for GHSA-3rh2-v3gr-35p9 at the Fiber middleware layer. The router
// must reject *both* the bare reserved-prefix form and its "X-Amz-Meta-"
// wrapped form before any handler runs, regardless of casing.
func TestFiberRejectsWrappedReservedMetadata(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		want   int
		denied bool
	}{
		{"plain-amz-meta-allowed", "X-Amz-Meta-Appid", http.StatusOK, false},
		{"bare-reserved-rejected", "X-Otterio-Internal-Server-Side-Encryption-Iv", http.StatusBadRequest, true},
		{"bare-reserved-rejected-lowercase", "x-otterio-internal-server-side-encryption-iv", http.StatusBadRequest, true},
		{"wrapped-reserved-rejected", "X-Amz-Meta-X-Otterio-Internal-Server-Side-Encryption-Iv", http.StatusBadRequest, true},
		{"wrapped-reserved-rejected-lowercase", "x-amz-meta-x-otterio-internal-server-side-encryption-iv", http.StatusBadRequest, true},
		{"legacy-minio-internal-rejected", "X-Minio-Internal-Server-Side-Encryption-Iv", http.StatusBadRequest, true},
		{"legacy-wrapped-minio-internal-rejected", "X-Amz-Meta-X-Minio-Internal-Server-Side-Encryption-Iv", http.StatusBadRequest, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := buildFiberSecurityChain(t)
			h := fiberHTTPTestHandler(app)

			req := httptest.NewRequest(http.MethodPut, "/mybucket/myobject", nil)
			req.Header.Set(tc.key, "junk")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if tc.denied {
				// ErrUnsupportedMetadata maps to 400 Bad Request; what we
				// really care about is that the request never reached the
				// downstream OK handler.
				if rec.Code == http.StatusOK {
					t.Fatalf("reserved header %q was forwarded to handler (got 200)", tc.key)
				}
			} else {
				if rec.Code != http.StatusOK {
					t.Fatalf("benign header %q was rejected: code=%d body=%s", tc.key, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

// TestFiberPathCanonicalizationStable verifies that the Fiber router
// preserves URL-encoded path segments and does *not* collapse "//" / ".."
// the way some HTTP servers do. This is a precondition for SigV4 to remain
// reproducible and for admin / STS handlers to match the route the operator
// intended; collapsing "/admin/..//" to "/admin/" historically produced
// signature-bypass and authorization-bypass classes of bugs in S3-compatible
// servers. Future router changes that flip UnescapePath / strip-slashes
// behavior will surface here.
func TestFiberPathCanonicalizationStable(t *testing.T) {
	app := newFiberApp()

	var seenPath string
	app.All("/*", func(c fiber.Ctx) error {
		// c.Path() returns the (potentially normalised) routing path; we
		// inspect the raw URI on the underlying fasthttp request to make
		// sure encoded segments survive untouched, which is what the SigV4
		// canonicaliser depends on downstream.
		seenPath = string(c.Request().URI().PathOriginal())
		c.Status(http.StatusOK)
		return nil
	})
	h := fiberHTTPTestHandler(app)

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"encoded-slash-preserved", "/bucket/a%2Fb", "/bucket/a%2Fb"},
		{"encoded-dot-preserved", "/bucket/a%2E%2Eb", "/bucket/a%2E%2Eb"},
		{"double-slash-preserved", "/bucket//key", "/bucket//key"},
		{"trailing-dot-segment-preserved", "/bucket/key/.", "/bucket/key/."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seenPath = ""
			req := httptest.NewRequest(http.MethodGet, tc.raw, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 (encoded path should not be filtered), got %d", rec.Code)
			}
			if seenPath != tc.want {
				t.Fatalf("path mutation detected: got %q, want %q", seenPath, tc.want)
			}
		})
	}
}

// TestFiberHostHeaderCasePreserved makes sure that headers used in the SigV4
// canonical-request build (Host, X-Amz-Date, X-Amz-Content-Sha256) reach the
// handler with their values intact. A naive Fiber upgrade that re-canonicalises
// header *values* (rather than just names) would silently break signature
// validation and historically allowed signature stripping in S3-compatible
// servers.
func TestFiberHostHeaderCasePreserved(t *testing.T) {
	app := newFiberApp()
	var got struct {
		host string
		date string
		sha  string
	}
	app.All("/*", func(c fiber.Ctx) error {
		got.host = string(c.Request().Header.Peek("Host"))
		got.date = string(c.Request().Header.Peek("X-Amz-Date"))
		got.sha = string(c.Request().Header.Peek("X-Amz-Content-Sha256"))
		c.Status(http.StatusOK)
		return nil
	})
	h := fiberHTTPTestHandler(app)

	req := httptest.NewRequest(http.MethodGet, "/bucket/key", nil)
	// Mixed-case host with explicit port - SigV4 hashes the literal value.
	req.Host = "MyBucket.s3.OTTERIO.local:9000"
	req.Header.Set("X-Amz-Date", "20251006T120000Z")
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if got.host != "MyBucket.s3.OTTERIO.local:9000" {
		t.Fatalf("Host header value mutated: got %q", got.host)
	}
	if got.date != "20251006T120000Z" {
		t.Fatalf("X-Amz-Date value mutated: got %q", got.date)
	}
	if got.sha != "UNSIGNED-PAYLOAD" {
		t.Fatalf("X-Amz-Content-Sha256 value mutated: got %q", got.sha)
	}
}

// TestFiberStreamingSignedPayloadHeadersPreserved exercises the SigV4
// streaming upload contract: the `Content-Encoding: aws-chunked`,
// `X-Amz-Content-Sha256: STREAMING-AWS4-HMAC-SHA256-PAYLOAD` and
// `X-Amz-Decoded-Content-Length` headers must survive the Fiber pipeline
// untouched, otherwise the streaming-signature-v4 verifier rebuilds the
// canonical request from a different header set than the client signed and
// rejects every legitimate upload (or, worse, accepts forged ones if the
// trimmer is too aggressive).
func TestFiberStreamingSignedPayloadHeadersPreserved(t *testing.T) {
	app := newFiberApp()
	var got struct {
		contentEnc    string
		amzSha        string
		decodedLength string
		amzDate       string
	}
	app.All("/*", func(c fiber.Ctx) error {
		got.contentEnc = string(c.Request().Header.Peek("Content-Encoding"))
		got.amzSha = string(c.Request().Header.Peek("X-Amz-Content-Sha256"))
		got.decodedLength = string(c.Request().Header.Peek("X-Amz-Decoded-Content-Length"))
		got.amzDate = string(c.Request().Header.Peek("X-Amz-Date"))
		c.Status(http.StatusOK)
		return nil
	})
	h := fiberHTTPTestHandler(app)

	req := httptest.NewRequest(http.MethodPut, "/bucket/key", nil)
	req.Header.Set("Content-Encoding", "aws-chunked,gzip")
	req.Header.Set("X-Amz-Content-Sha256", "STREAMING-AWS4-HMAC-SHA256-PAYLOAD")
	req.Header.Set("X-Amz-Decoded-Content-Length", "65536")
	req.Header.Set("X-Amz-Date", "20251006T120000Z")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIA/20251006/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=deadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if got.contentEnc != "aws-chunked,gzip" {
		t.Fatalf("Content-Encoding mutated: got %q", got.contentEnc)
	}
	if got.amzSha != "STREAMING-AWS4-HMAC-SHA256-PAYLOAD" {
		t.Fatalf("X-Amz-Content-Sha256 streaming marker mutated: got %q", got.amzSha)
	}
	if got.decodedLength != "65536" {
		t.Fatalf("X-Amz-Decoded-Content-Length mutated: got %q", got.decodedLength)
	}
	if got.amzDate != "20251006T120000Z" {
		t.Fatalf("X-Amz-Date mutated: got %q", got.amzDate)
	}
}

// TestFiberMultiValueHostHeaderSafe documents the Host-header multi-value
// behavior observed under fasthttp (the engine that powers Fiber). HTTP/1.1
// forbids more than one Host header but nothing prevents a malicious client
// from sending several on the wire. We pin the observed engine behavior so
// any future engine swap (or a fasthttp upgrade that flips the policy) fails
// loudly here: the SigV4 canonicaliser in OtterIO has to agree with the
// engine on *which* Host value is hashed, otherwise a smuggled second Host
// header would let an attacker forge a signature against one virtual host
// while the request executes against another.
//
// fasthttp keeps only one Host header in the canonical request and the
// header.Peek("Host") view returns that single canonical value. We assert
// that exactly one copy reaches the handler regardless of how many Host
// headers were sent, and that VisitAll never yields a duplicate Host.
func TestFiberMultiValueHostHeaderSafe(t *testing.T) {
	app := newFiberApp()
	var seenHost string
	var hostCount int
	app.All("/*", func(c fiber.Ctx) error {
		seenHost = string(c.Request().Header.Peek("Host"))
		c.Request().Header.VisitAll(func(k, _ []byte) {
			if string(k) == "Host" {
				hostCount++
			}
		})
		c.Status(http.StatusOK)
		return nil
	})
	h := fiberHTTPTestHandler(app)

	req := httptest.NewRequest(http.MethodGet, "/bucket/key", nil)
	req.Host = "primary.example:9000"
	req.Header["Host"] = []string{"primary.example:9000", "evil.example:9000"}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if hostCount != 1 {
		t.Fatalf("expected exactly one Host header at the handler, got %d copies (value=%q); SigV4 verification could diverge between engines", hostCount, seenHost)
	}
	if seenHost == "" {
		t.Fatalf("Host header dropped entirely")
	}
}

// TestFiberAdminPathDoesNotMatchRandomPrefix nails down that the admin
// router does not accept extraneous path segments before the API prefix.
// A naive Fiber app that mounts admin handlers under a wildcard prefix would
// allow `/anything/otterio/admin/v3/...` to reach the handler, defeating
// per-prefix authentication assumptions. We assert the negative case via the
// production registerAdminRouterFiber and ensure such requests are 404'd
// (or otherwise non-2xx) before any admin-handler code runs.
func TestFiberAdminPathDoesNotMatchRandomPrefix(t *testing.T) {
	app := newFiberApp()
	registerAdminRouterFiber(app, true, true)
	h := fiberHTTPTestHandler(app)

	cases := []string{
		"/decoy/otterio/admin/v3/list-users",
		"//otterio//admin//v3//list-users",
		"/otterio/admin/v3/../../etc/passwd",
		"/otterio/admin/v3/list-users/../../bypass",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code >= 200 && rec.Code < 300 {
				t.Fatalf("admin path %q unexpectedly reached a 2xx handler (status=%d body=%s)", p, rec.Code, rec.Body.String())
			}
		})
	}
}
