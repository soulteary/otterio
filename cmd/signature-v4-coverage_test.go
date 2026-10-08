/*
 * OtterIO Cloud Storage, (C) 2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	xhttp "github.com/soulteary/otterio/cmd/http"
)

func TestSignedHeaderCoverage(t *testing.T) {
	for _, header := range []string{
		"x-amz-copy-source", "X-AMZ-COPY-SOURCE", "x-amz-copy-source-range",
		"x-amz-metadata-directive", "x-amz-meta-user", "x-amz-tagging",
		"x-amz-server-side-encryption", "x-amz-security-token",
		"x-amz-snowball-extract", "x-amz-object-lock-mode",
		"x-amz-future-extension", "x-otterio-source-etag",
	} {
		t.Run(header, func(t *testing.T) {
			for _, values := range [][]string{{"/private/secret"}, {""}, nil, {"first", "second"}} {
				r := httptest.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
				// Use raw map keys to check case-insensitive coverage as well as
				// the canonical keys produced by both HTTP servers.
				r.Header[header] = values
				if _, code := extractSignedHeaders([]string{"host"}, r); code != ErrUnsignedHeaders {
					t.Fatalf("unsigned %s=%v: got %v", header, values, code)
				}
			}
		})
	}

	for _, header := range []string{"User-Agent", "Content-Type", "Accept-Encoding", xhttp.AmzContentSha256} {
		r := httptest.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
		r.Header.Set(header, unsignedPayload)
		if _, code := extractSignedHeaders([]string{"host"}, r); code != ErrNone {
			t.Fatalf("compatible unsigned header %s: %v", header, code)
		}
	}

	r := httptest.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
	r.Header.Add("X-Amz-Meta-User", "first")
	r.Header.Add("X-Amz-Meta-User", "second")
	h, code := extractSignedHeaders([]string{"host", "x-amz-meta-user"}, r)
	if code != ErrNone || len(h.Values("X-Amz-Meta-User")) != 2 {
		t.Fatalf("signed multi-value metadata must be preserved: %v, %v", h, code)
	}
	if got := getAPIError(ErrUnsignedHeaders); got.Code != "AccessDenied" || got.HTTPStatusCode != http.StatusForbidden {
		t.Fatalf("unsigned headers must return 403 AccessDenied: %+v", got)
	}
}

func TestPresignedHeaderQueryCoverage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		query  url.Values
		header http.Header
		want   APIErrorCode
	}{
		{"matching hoisted header", url.Values{"x-amz-copy-source": {"/src/key"}}, http.Header{"X-Amz-Copy-Source": {"/src/key"}}, ErrNone},
		{"canonical query name", url.Values{"X-Amz-Copy-Source": {"/src/key"}}, http.Header{"X-Amz-Copy-Source": {"/src/key"}}, ErrNone},
		{"conflicting source", url.Values{"x-amz-copy-source": {"/src/key"}}, http.Header{"X-Amz-Copy-Source": {"/src/secret"}}, ErrInvalidRequest},
		{"conflicting case alias", url.Values{"x-amz-copy-source": {"/src/key"}, "X-Amz-Copy-Source": {"/src/secret"}}, http.Header{"X-Amz-Copy-Source": {"/src/key"}}, ErrInvalidRequest},
		{"ambiguous value list", url.Values{"x-amz-meta-user": {"a,b"}}, http.Header{"X-Amz-Meta-User": {"a", "b"}}, ErrInvalidRequest},
		{"conflicting payload marker", url.Values{xhttp.AmzContentSha256: {emptySHA256}}, http.Header{xhttp.AmzContentSha256: {unsignedPayload}}, ErrInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
			tc.query.Set(xhttp.AmzCredential, "key/20261004/us-east-1/s3/aws4_request")
			r.URL.RawQuery = tc.query.Encode()
			r.Header = tc.header
			if _, code := extractSignedHeaders([]string{"host"}, r); code != tc.want {
				t.Fatalf("got %v, want %v", code, tc.want)
			}
		})
	}
}

// Exercise valid signatures, rather than only checking an early failure on a
// malformed credential. All three verifier entry points must use the guard.
func TestSignatureV4RejectsInjectedHeaders(t *testing.T) {
	for _, mode := range []string{"presigned", "authorization", "streaming"} {
		t.Run(mode, func(t *testing.T) {
			var r *http.Request
			var err error
			if mode == "streaming" {
				r, err = newTestStreamingSignedRequest(http.MethodPut, "http://otterio.test/bucket/key", 4, 4,
					bytes.NewReader([]byte("data")), globalActiveCred.AccessKey, globalActiveCred.SecretKey)
			} else {
				r, err = http.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
				if err == nil {
					r.Header.Set(xhttp.AmzContentSha256, unsignedPayload)
					if mode == "presigned" {
						err = preSignV4(r, globalActiveCred.AccessKey, globalActiveCred.SecretKey, 900)
					} else {
						err = signRequestV4(r, globalActiveCred.AccessKey, globalActiveCred.SecretKey)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			verify := func(req *http.Request) APIErrorCode {
				if mode == "streaming" {
					_, _, _, _, code := calculateSeedSignature(req)
					return code
				}
				return reqSignatureV4Verify(req, globalServerRegion, serviceS3)
			}
			if code := verify(r); code != ErrNone {
				t.Fatalf("valid control: %v", code)
			}
			for _, header := range []string{xhttp.AmzCopySource, xhttp.AmzSnowballExtract, "X-Amz-Meta-User", xhttp.OtterIOSourceETag} {
				tampered := r.Clone(r.Context())
				tampered.Header.Set(header, "/private/secret")
				if code := verify(tampered); code != ErrUnsignedHeaders {
					t.Fatalf("injected %s: %v", header, code)
				}
			}
		})
	}
}

func TestPresignedHoistedHeaderSignature(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "http://otterio.test/dst/key?x-amz-copy-source=%2Fsrc%2Fkey", nil)
	if err := preSignV4(r, globalActiveCred.AccessKey, globalActiveCred.SecretKey, 900); err != nil {
		t.Fatal(err)
	}
	r.Header.Set(xhttp.AmzCopySource, "/src/key")
	if code := reqSignatureV4Verify(r, globalServerRegion, serviceS3); code != ErrNone {
		t.Fatalf("matching signed query/header: %v", code)
	}
	r.Header.Set(xhttp.AmzCopySource, "/src/secret")
	if code := reqSignatureV4Verify(r, globalServerRegion, serviceS3); code != ErrInvalidRequest {
		t.Fatalf("conflicting query/header: %v", code)
	}
	q := r.URL.Query()
	q.Set("x-amz-copy-source", "/src/secret")
	r.URL.RawQuery = q.Encode()
	if code := reqSignatureV4Verify(r, globalServerRegion, serviceS3); code != ErrSignatureDoesNotMatch {
		t.Fatalf("tampering with both query and header: %v", code)
	}
}

func TestObjectTagsDoNotMutateSignedHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://otterio.test/bucket/key", nil)
	r.Header.Set(xhttp.AmzObjectTagging, "dept=client")
	r.Header.Set(xhttp.AmzContentSha256, unsignedPayload)
	if err := signRequestV4(r, globalActiveCred.AccessKey, globalActiveCred.SecretKey); err != nil {
		t.Fatal(err)
	}
	for _, stored := range []string{"dept=stored", ""} {
		policyRequest := withObjectTags(r, stored)
		if code := reqSignatureV4Verify(policyRequest, globalServerRegion, serviceS3); code != ErrNone {
			t.Fatalf("server-loaded tags invalidated signature: %v", code)
		}
		if policyRequest.Header.Get(xhttp.AmzObjectTagging) != "dept=client" {
			t.Fatal("server-loaded tags mutated the signed request")
		}
		values := getConditionValues(policyRequest, "", "", nil)["ExistingObjectTag/dept"]
		if stored == "" && len(values) != 0 || stored != "" && strings.Join(values, "") != "stored" {
			t.Fatalf("stored tags must override request tags, including an empty set: %v", values)
		}
	}
}

func TestSignatureV4PayloadHeaderCompatibility(t *testing.T) {
	for _, mode := range []string{"presigned", "authorization"} {
		r := httptest.NewRequest(http.MethodPut, "http://otterio.test/bucket/key", nil)
		// The SDK binds UNSIGNED-PAYLOAD into the canonical request even
		// without a separate content-sha256 entry in SignedHeaders.
		if mode == "presigned" {
			r = signer.PreSignV4(*r, globalActiveCred.AccessKey, globalActiveCred.SecretKey, "", globalServerRegion, 900)
		} else {
			r = signer.SignV4(*r, globalActiveCred.AccessKey, globalActiveCred.SecretKey, "", globalServerRegion)
		}
		r.Header.Set(xhttp.AmzContentSha256, unsignedPayload)
		if code := reqSignatureV4Verify(r, globalServerRegion, serviceS3); code != ErrNone {
			t.Fatalf("%s: compatible payload header rejected: %v", mode, code)
		}
		r.Header.Set(xhttp.AmzContentSha256, emptySHA256)
		if code := reqSignatureV4Verify(r, globalServerRegion, serviceS3); code != ErrSignatureDoesNotMatch {
			t.Fatalf("%s: changing the payload hash must invalidate the signature: %v", mode, code)
		}
	}
}
