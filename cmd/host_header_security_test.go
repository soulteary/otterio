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
	"bufio"
	"net/http"
	"strings"
	"testing"
)

// TestNetHTTPRejectsMultipleHostHeaders pins down the protocol-layer guarantee
// that the net/http entrypoint never reaches the OtterIO middleware chain when
// a wire-level client smuggles more than one Host: header. RFC 9110 §7.2 is
// explicit ("more than one Host header field" MUST be answered with 400) and
// Go's http.ReadRequest enforces this with the literal error "too many Host
// headers". This is the second half of the safety invariant that complements
// TestFiberMultiValueHostHeaderSafe: the Fiber/fasthttp engine collapses
// duplicate Host headers to a single canonical value, while the net/http
// engine refuses the request outright. Either way the SigV4 canonicaliser
// (cmd/signature-v4-utils.go extractSignedHeaders, which signs r.Host) sees
// the same single Host value as the routing layer, so the "smuggle a second
// Host to forge a signature against vhost A while executing against vhost B"
// attack from upstream-cve-backlog.md §4 row 1 cannot be constructed.
//
// If a future Go version (or a vendored fork) silently relaxes this rule
// without us noticing, this test is the canary that fires.
func TestNetHTTPRejectsMultipleHostHeaders(t *testing.T) {
	t.Parallel()

	// Hand-crafted HTTP/1.1 request with two Host header lines on the wire.
	// We feed it through net/http's own request parser (the same code path
	// that http.Server uses for incoming connections) so the result is the
	// authoritative behavior, not a behavior we synthesized in a test
	// helper.
	raw := "GET /bucket/key HTTP/1.1\r\n" +
		"Host: legit.example\r\n" +
		"Host: evil.example\r\n" +
		"User-Agent: otterio-host-pinning\r\n" +
		"\r\n"
	_, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
	if err == nil {
		t.Fatalf("net/http accepted a request with two Host headers; the SigV4 vs routing host-disagreement attack from the upstream backlog (§4 row 1) is no longer guaranteed to be impossible at this entrypoint - audit cmd/signature-v4-utils.go and add an application-layer reject before merging")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "host") {
		t.Fatalf("net/http rejected the request but the error message no longer mentions Host (%q); this test would still pass for unrelated reasons - tighten the assertion before relying on it", err.Error())
	}
}

// TestNetHTTPSingleHostHeaderAccepted is the negative control for
// TestNetHTTPRejectsMultipleHostHeaders. A correctly formed HTTP/1.1 request
// with exactly one Host header MUST round-trip through http.ReadRequest, and
// r.Host MUST be populated with that single value. If this test ever starts
// failing it means we accidentally over-tightened the parser (or its fork) and
// the rejection test above is no longer testing what it claims to test.
func TestNetHTTPSingleHostHeaderAccepted(t *testing.T) {
	t.Parallel()

	raw := "GET /bucket/key HTTP/1.1\r\n" +
		"Host: legit.example:9000\r\n" +
		"User-Agent: otterio-host-pinning\r\n" +
		"\r\n"
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("net/http rejected a single-Host request: %v", err)
	}
	if req.Host != "legit.example:9000" {
		t.Fatalf("r.Host not populated from the canonical Host header: got %q want %q", req.Host, "legit.example:9000")
	}
}
