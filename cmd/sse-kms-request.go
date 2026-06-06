// Otterio Cloud Storage, (C) 2026 Otterio, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// SECURITY (SSE-KMS PUT-handler wiring, Bug B follow-up):
//
// enforceSSEKMSRequest is the single entry point every SSE-KMS PUT,
// CopyObject, PutObjectPart, NewMultipartUpload, CompleteMultipartUpload
// and PostPolicyForm handler MUST call before doing any work that could
// reach the KMS or persist object metadata. Its purpose is to translate
// the upstream early-reject (historically: ErrNotImplemented for any
// SSE-KMS-requested PUT-style operation) into a full security-checked
// dispatch:
//
//  1. Non SSE-KMS requests are passed through untouched.
//  2. A malformed x-amz-server-side-encryption-context (or missing
//     algorithm header) is rejected with ErrInvalidEncryptionParameters
//     before any KMS call.
//  3. A client-supplied context that attempts to override the
//     server-bound reserved bucket key is rejected with
//     ErrKMSContextBindingConflict (HTTP 403, S3 code AccessDenied).
//     The KMS is GUARANTEED not to be called in this branch -- that
//     property is what makes the wire-level threat model around Bug B
//     enforceable at the request boundary.
//  4. If GlobalKMS is unset the request is rejected with
//     ErrKMSNotConfigured before any object-layer side effect.
//
// Once this function returns ErrNone the caller may safely thread the
// returned (keyID, clientCtx) into EncryptRequestWithKMS or persist
// them into encMetadata for a later multipart finalization. The bound
// AAD context is reconstructed deterministically by mergeBindingContext
// (and ObjectBindingContext) inside the crypto package, so the caller
// never has to handle the bucket-binding key directly.

package cmd

import (
	"net/http"

	"github.com/soulteary/otterio/cmd/crypto"
)

// enforceSSEKMSRequest validates the SSE-KMS request headers on the
// request boundary and returns the parsed (keyID, clientCtx) on
// success.
//
// The returned APIErrorCode is the unique source of truth for SSE-KMS
// preflight failures; callers SHOULD return immediately when it is not
// ErrNone, before any object-layer or KMS-layer side effect.
//
// The function returns ErrNone (and zero-valued keyID/clientCtx) if
// the request is not an SSE-KMS request, so the caller can call this
// unconditionally in handlers that also serve plain PUT/COPY traffic.
func enforceSSEKMSRequest(r *http.Request, bucket, object string) (keyID string, clientCtx crypto.Context, apiErr APIErrorCode) {
	if !crypto.S3KMS.IsRequested(r.Header) {
		return "", nil, ErrNone
	}

	keyID, clientCtx, err := crypto.S3KMS.ParseHTTP(r.Header)
	if err != nil {
		return "", nil, ErrInvalidEncryptionParameters
	}

	if _, err := crypto.MergeBindingContext(bucket, object, clientCtx); err != nil {
		// Any merge error here is a reserved-key override attempt; the
		// crypto package never returns merge errors for any other reason.
		// We deliberately return BEFORE looking at GlobalKMS so that an
		// attacker cannot use a server-misconfiguration response to
		// fingerprint the reserved-key set.
		return "", nil, ErrKMSContextBindingConflict
	}

	if GlobalKMS == nil {
		return "", nil, ErrKMSNotConfigured
	}

	return keyID, clientCtx, ErrNone
}
