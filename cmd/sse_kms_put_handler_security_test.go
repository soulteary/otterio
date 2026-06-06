// Copyright (c) 2026 OtterIO contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/soulteary/otterio/cmd/crypto"
	"github.com/soulteary/otterio/pkg/kms"
)

// SECURITY (SSE-KMS PUT-handler wiring, Bug B follow-up):
//
// These are L1 tests for enforceSSEKMSRequest, the single security gate
// every PUT / Copy / NewMultipart / PostPolicyForm handler calls before
// any KMS or object-layer side effect. The unit under test is the gate
// itself, not the surrounding handlers, because:
//
//   - Verifying the gate in isolation makes the property "KMS is not
//     invoked when the gate rejects" both directly observable and
//     trivially auditable.
//   - The gate is the single source of truth across all six PUT-style
//     entry points; pinning it once pins all of them.
//   - A handler-level harness would add an order-of-magnitude more
//     setup (object layer, auth, multipart store, ...) and obscure the
//     security property the gate exists to enforce.
//
// The companion L2 tests in cmd/crypto/sse-kms_e2e_test.go cover the
// crypto-layer round-trip with a real AEAD. Together the two layers
// pin the full PUT-then-GET path described in
// docs/security/upstream-cve-backlog.md row 32.

// recordingKMSForGate is a minimal kms.KMS that counts how many times
// GenerateKey / DecryptKey are invoked. The gate's hardest invariant
// (a reserved-key override MUST NOT call into the KMS) is enforced by
// asserting genCalls == 0 on the rejection cases.
type recordingKMSForGate struct {
	genCalls     int
	decryptCalls int
}

func (r *recordingKMSForGate) Stat() (kms.Status, error) {
	return kms.Status{Name: "recording-gate"}, nil
}
func (r *recordingKMSForGate) CreateKey(string) error { return errors.New("unused") }

func (r *recordingKMSForGate) GenerateKey(keyID string, _ kms.Context) (kms.DEK, error) {
	r.genCalls++
	return kms.DEK{KeyID: keyID, Plaintext: make([]byte, 32), Ciphertext: []byte("c")}, nil
}

func (r *recordingKMSForGate) DecryptKey(_ string, _ []byte, _ kms.Context) ([]byte, error) {
	r.decryptCalls++
	return make([]byte, 32), nil
}

// withTempGlobalKMS swaps the package-level GlobalKMS for the duration
// of a test and restores the original on cleanup. This isolates the
// gate's "KMS-not-configured" branch from any production global state.
func withTempGlobalKMS(t *testing.T, k kms.KMS) {
	t.Helper()
	old := GlobalKMS
	GlobalKMS = k
	t.Cleanup(func() { GlobalKMS = old })
}

// kmsRequest builds a PUT request carrying the supplied headers.
// Tests use this as a tiny fixture to keep call sites focused on the
// security property under test rather than HTTP plumbing.
func kmsRequest(t *testing.T, header http.Header) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "/bucket/object", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return req
}

// putHeader returns a PUT header map carrying the SSE-KMS algorithm
// header and the supplied raw KMS context (passed through verbatim,
// so callers can express malformed values). The wire format expected
// by crypto.S3KMS.ParseHTTP is the raw JSON document, so callers
// should pass things like `{"app":"billing"}` directly.
func putHeader(rawCtx string) http.Header {
	h := http.Header{}
	h.Set("X-Amz-Server-Side-Encryption", "aws:kms")
	if rawCtx != "" {
		h.Set("X-Amz-Server-Side-Encryption-Context", rawCtx)
	}
	return h
}

// putHeaderWithKeyID is putHeader plus an explicit KMS key ID, used
// for the success cases where we want to assert keyID round-trips
// through the gate.
func putHeaderWithKeyID(keyID, rawCtx string) http.Header {
	h := putHeader(rawCtx)
	if keyID != "" {
		h.Set("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id", keyID)
	}
	return h
}

// TestEnforceSSEKMSRequestPassthroughForNonSSEKMS pins that requests
// that do not carry SSE-KMS headers are returned untouched with
// ErrNone, so the gate can be called unconditionally from handlers
// that also serve plain PUT/COPY traffic.
func TestEnforceSSEKMSRequestPassthroughForNonSSEKMS(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
	}{
		{name: "no-sse-headers", header: http.Header{}},
		{name: "sse-s3", header: func() http.Header {
			h := http.Header{}
			h.Set("X-Amz-Server-Side-Encryption", "AES256")
			return h
		}()},
		{name: "sse-c", header: func() http.Header {
			h := http.Header{}
			h.Set("X-Amz-Server-Side-Encryption-Customer-Algorithm", "AES256")
			h.Set("X-Amz-Server-Side-Encryption-Customer-Key", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			h.Set("X-Amz-Server-Side-Encryption-Customer-Key-MD5", "ignored")
			return h
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rk := &recordingKMSForGate{}
			withTempGlobalKMS(t, rk)
			req := kmsRequest(t, tc.header)
			keyID, ctx, errCode := enforceSSEKMSRequest(req, "bkt", "obj")
			if errCode != ErrNone {
				t.Fatalf("errCode = %v, want ErrNone", errCode)
			}
			if keyID != "" {
				t.Fatalf("keyID = %q, want empty for non-SSE-KMS request", keyID)
			}
			if ctx != nil {
				t.Fatalf("ctx = %v, want nil for non-SSE-KMS request", ctx)
			}
			if rk.genCalls != 0 || rk.decryptCalls != 0 {
				t.Fatalf("KMS was invoked for non-SSE-KMS request: gen=%d decrypt=%d", rk.genCalls, rk.decryptCalls)
			}
		})
	}
}

// TestEnforceSSEKMSRequestRejectsBucketKeyOverride pins the load-bearing
// security property of the entire wiring: any client KMS context that
// attempts to override the server-bound bucket-binding key MUST be
// rejected with ErrKMSContextBindingConflict, AND the KMS MUST NOT be
// called. This is the unit-level proof that the wiring is faithful to
// the Bug B contract.
func TestEnforceSSEKMSRequestRejectsBucketKeyOverride(t *testing.T) {
	cases := []struct {
		name string
		ctx  string
	}{
		{name: "hostile-bucket-string", ctx: `{"bkt":"evil/path"}`},
		{name: "empty-bucket-string", ctx: `{"bkt":""}`},
		{name: "hostile-plus-extras", ctx: `{"bkt":"x","app":"billing"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rk := &recordingKMSForGate{}
			withTempGlobalKMS(t, rk)

			h := putHeaderWithKeyID("kid", tc.ctx)
			req := kmsRequest(t, h)
			_, _, errCode := enforceSSEKMSRequest(req, "bkt", "obj")
			if errCode != ErrKMSContextBindingConflict {
				t.Fatalf("errCode = %v, want ErrKMSContextBindingConflict", errCode)
			}
			if rk.genCalls != 0 {
				t.Fatalf("KMS GenerateKey was invoked despite reserved-key override; gen=%d", rk.genCalls)
			}
			if rk.decryptCalls != 0 {
				t.Fatalf("KMS DecryptKey was invoked despite reserved-key override; decrypt=%d", rk.decryptCalls)
			}
			// Verify the gate produced the expected HTTP status code
			// mapping (403 / AccessDenied) so a future re-shuffling
			// of the error-table cannot silently regress this surface.
			apiErr := errorCodes.ToAPIErr(errCode)
			if apiErr.HTTPStatusCode != http.StatusForbidden {
				t.Fatalf("HTTP status = %d, want 403", apiErr.HTTPStatusCode)
			}
			if apiErr.Code != "AccessDenied" {
				t.Fatalf("S3 code = %q, want AccessDenied", apiErr.Code)
			}
		})
	}
}

// TestEnforceSSEKMSRequestRejectsInvalidContextJSON pins that a
// malformed x-amz-server-side-encryption-context is rejected with
// ErrInvalidEncryptionParameters before any KMS or merge-binding
// step runs. Together with the bucket-key override test above this
// guarantees that no client-controlled byte makes it past the gate
// without a structural and semantic check.
func TestEnforceSSEKMSRequestRejectsInvalidContextJSON(t *testing.T) {
	cases := []struct {
		name string
		ctx  string
	}{
		{name: "garbage-bytes", ctx: "not-json-at-all"},
		{name: "json-array", ctx: `["a","b"]`},
		{name: "json-number", ctx: `42`},
		{name: "trailing-junk", ctx: `{"a":"b"}<<<garbage`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rk := &recordingKMSForGate{}
			withTempGlobalKMS(t, rk)

			h := putHeaderWithKeyID("kid", tc.ctx)
			req := kmsRequest(t, h)
			_, _, errCode := enforceSSEKMSRequest(req, "bkt", "obj")
			if errCode != ErrInvalidEncryptionParameters {
				t.Fatalf("errCode = %v, want ErrInvalidEncryptionParameters", errCode)
			}
			if rk.genCalls != 0 || rk.decryptCalls != 0 {
				t.Fatalf("KMS was invoked for invalid ctx: gen=%d decrypt=%d", rk.genCalls, rk.decryptCalls)
			}
		})
	}
}

// TestEnforceSSEKMSRequestRejectsKMSNotConfigured pins the orthogonal
// axis: a well-formed, non-conflicting ctx with GlobalKMS == nil
// rejects with ErrKMSNotConfigured. This test runs in isolation
// (no parallel sub-tests) because it mutates the package-level
// GlobalKMS and only restores it via cleanup.
func TestEnforceSSEKMSRequestRejectsKMSNotConfigured(t *testing.T) {
	cases := []struct {
		name string
		ctx  string
	}{
		{name: "no-ctx", ctx: ""},
		{name: "empty-ctx", ctx: `{}`},
		{name: "valid-non-bucket-ctx", ctx: `{"app":"billing"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTempGlobalKMS(t, nil)
			h := putHeaderWithKeyID("kid", tc.ctx)
			req := kmsRequest(t, h)
			_, _, errCode := enforceSSEKMSRequest(req, "bkt", "obj")
			if errCode != ErrKMSNotConfigured {
				t.Fatalf("errCode = %v, want ErrKMSNotConfigured", errCode)
			}
		})
	}
}

// TestEnforceSSEKMSRequestRejectionPrecedence pins the rejection
// ordering documented in cmd/sse-kms-request.go: a malformed ctx
// (ErrInvalidEncryptionParameters) takes precedence over a bucket
// override (ErrKMSContextBindingConflict), which takes precedence
// over GlobalKMS == nil (ErrKMSNotConfigured). Pinning the order
// matters because a future refactor that lets KMSNotConfigured win
// would let an attacker fingerprint the reserved-key set.
func TestEnforceSSEKMSRequestRejectionPrecedence(t *testing.T) {
	t.Run("malformed-beats-everything", func(t *testing.T) {
		withTempGlobalKMS(t, nil)
		h := putHeaderWithKeyID("kid", "not-json")
		req := kmsRequest(t, h)
		_, _, errCode := enforceSSEKMSRequest(req, "bkt", "obj")
		if errCode != ErrInvalidEncryptionParameters {
			t.Fatalf("errCode = %v, want ErrInvalidEncryptionParameters", errCode)
		}
	})

	t.Run("override-beats-no-kms", func(t *testing.T) {
		withTempGlobalKMS(t, nil)
		h := putHeaderWithKeyID("kid", `{"bkt":"evil"}`)
		req := kmsRequest(t, h)
		_, _, errCode := enforceSSEKMSRequest(req, "bkt", "obj")
		if errCode != ErrKMSContextBindingConflict {
			t.Fatalf("errCode = %v, want ErrKMSContextBindingConflict", errCode)
		}
	})
}

// TestEnforceSSEKMSRequestAcceptsValidContext pins the success path:
// a request with a well-formed, non-conflicting ctx and a configured
// KMS returns ErrNone with the parsed (keyID, ctx). The keyID and
// ctx round-trip byte-for-byte so the caller can thread them straight
// into EncryptRequestWithKMS / setEncryptionMetadata without re-parsing.
func TestEnforceSSEKMSRequestAcceptsValidContext(t *testing.T) {
	cases := []struct {
		name      string
		keyID     string
		ctxJSON   string
		wantKeyID string
		wantCtx   crypto.Context
	}{
		{
			name:      "no-ctx",
			keyID:     "alias/aws/s3",
			ctxJSON:   "",
			wantKeyID: "alias/aws/s3",
			wantCtx:   nil,
		},
		{
			name:      "empty-ctx",
			keyID:     "kid",
			ctxJSON:   `{}`,
			wantKeyID: "kid",
			wantCtx:   crypto.Context{},
		},
		{
			name:      "single-extra-key",
			keyID:     "kid",
			ctxJSON:   `{"app":"billing"}`,
			wantKeyID: "kid",
			wantCtx:   crypto.Context{"app": "billing"},
		},
		{
			name:      "multi-extra-keys",
			keyID:     "kid",
			ctxJSON:   `{"app":"billing","tenant":"acme"}`,
			wantKeyID: "kid",
			wantCtx:   crypto.Context{"app": "billing", "tenant": "acme"},
		},
		{
			name:      "matching-bucket-key-allowed",
			keyID:     "kid",
			ctxJSON:   `{"bkt":"bkt/obj"}`,
			wantKeyID: "kid",
			wantCtx:   crypto.Context{"bkt": "bkt/obj"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rk := &recordingKMSForGate{}
			withTempGlobalKMS(t, rk)

			h := putHeaderWithKeyID(tc.keyID, tc.ctxJSON)
			req := kmsRequest(t, h)

			keyID, gotCtx, errCode := enforceSSEKMSRequest(req, "bkt", "obj")
			if errCode != ErrNone {
				t.Fatalf("errCode = %v, want ErrNone", errCode)
			}
			if keyID != tc.wantKeyID {
				t.Fatalf("keyID = %q, want %q", keyID, tc.wantKeyID)
			}
			if !reflect.DeepEqual(gotCtx, tc.wantCtx) {
				t.Fatalf("ctx = %v, want %v", gotCtx, tc.wantCtx)
			}
			// Crucially: the gate itself MUST NOT call the KMS. The
			// KMS is only contacted by the encrypt-side helper
			// (EncryptRequestWithKMS / newEncryptMetadataKMS) AFTER
			// the caller acts on ErrNone.
			if rk.genCalls != 0 || rk.decryptCalls != 0 {
				t.Fatalf("gate invoked the KMS on the success path: gen=%d decrypt=%d", rk.genCalls, rk.decryptCalls)
			}
		})
	}
}
