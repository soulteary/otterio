/*
 * Minio Cloud Storage, (C) 2019-2020 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package crypto implements server-side encryption helpers.
//
// SECURITY (SSE-KMS context binding):
//
//   - UnsealObjectKey ALWAYS rebuilds the binding context as
//     {bucket: path.Join(bucket, object)} and merges any persisted
//     client-supplied KMS context on top, REJECTING any attempt by the
//     persisted client context to override the bucket-binding reserved
//     key (returns errKMSContextBindingConflict).
//   - CreateMetadata persists ONLY the client-supplied KMS context (via
//     MetaContext); it never persists the server-bound binding context,
//     because the server reconstructs it deterministically from
//     (bucket, object) on every Unseal.
//   - ParseMetadata returns the deserialized client context unchanged;
//     callers MUST NOT trust the returned ctx as the AEAD AAD without
//     first running it through the binding-merge in UnsealObjectKey.
//
// Upstream backlog row 32 (SSE-KMS context binding) -- see
// docs/security/upstream-cve-backlog.md.
package crypto

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"path"
	"strings"

	jsoniter "github.com/json-iterator/go"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
)

type ssekms struct{}

var (
	// S3KMS represents AWS SSE-KMS. It provides functionality to
	// handle SSE-KMS requests.
	S3KMS = ssekms{}

	_ Type = S3KMS
)

// String returns the SSE domain as string. For SSE-KMS the
// domain is "SSE-KMS".
func (ssekms) String() string { return "SSE-KMS" }

// IsRequested returns true if the HTTP headers contains
// at least one SSE-KMS header.
func (ssekms) IsRequested(h http.Header) bool {
	if _, ok := h[xhttp.AmzServerSideEncryptionKmsID]; ok {
		return true
	}
	if _, ok := h[xhttp.AmzServerSideEncryptionKmsContext]; ok {
		return true
	}
	if _, ok := h[xhttp.AmzServerSideEncryption]; ok {
		return strings.ToUpper(h.Get(xhttp.AmzServerSideEncryption)) != xhttp.AmzEncryptionAES // Return only true if the SSE header is specified and does not contain the SSE-S3 value
	}
	return false
}

// ParseHTTP parses the SSE-KMS headers and returns the SSE-KMS key ID
// and the KMS context on success.
func (ssekms) ParseHTTP(h http.Header) (string, Context, error) {
	algorithm := h.Get(xhttp.AmzServerSideEncryption)
	if algorithm != xhttp.AmzEncryptionKMS {
		return "", nil, ErrInvalidEncryptionMethod
	}

	var ctx Context
	if context, ok := h[xhttp.AmzServerSideEncryptionKmsContext]; ok {
		var json = jsoniter.ConfigCompatibleWithStandardLibrary
		if err := json.Unmarshal([]byte(context[0]), &ctx); err != nil {
			return "", nil, err
		}
	}
	return h.Get(xhttp.AmzServerSideEncryptionKmsID), ctx, nil
}

// IsEncrypted returns true if the object metadata indicates
// that the object was uploaded using SSE-KMS.
func (ssekms) IsEncrypted(metadata map[string]string) bool {
	if _, ok := metadata[MetaSealedKeyKMS]; ok {
		return true
	}
	return false
}

// UnsealObjectKey extracts and decrypts the sealed object key from the
// metadata using the KMS and returns the decrypted object key.
//
// SECURITY: The KMS binding context is always reconstructed by the
// server from (bucket, object). Any persisted client-supplied KMS
// context (MetaContext) is merged in as additional AEAD AAD entries,
// but MAY NOT override the reserved bucket-binding key. Attempting to
// override that key (e.g. by tampering with the persisted metadata)
// returns errKMSContextBindingConflict.
func (s3 ssekms) UnsealObjectKey(kms KMS, metadata map[string]string, bucket, object string) (key ObjectKey, err error) {
	keyID, kmsKey, sealedKey, clientCtx, err := s3.ParseMetadata(metadata)
	if err != nil {
		return key, err
	}
	boundCtx, err := mergeBindingContext(bucket, object, clientCtx)
	if err != nil {
		return key, err
	}
	unsealKey, err := kms.DecryptKey(keyID, kmsKey, boundCtx)
	if err != nil {
		return key, err
	}
	err = key.Unseal(unsealKey[:], sealedKey, s3.String(), bucket, object)
	return key, err
}

// ObjectBindingContext returns the canonical SSE-KMS/SSE-S3 binding
// context for an (bucket, object) pair. It is the single source of
// truth for the server-bound AEAD AAD used by Generate/Decrypt calls
// against the KMS for object-level encryption.
//
// SECURITY: All cmd-package call sites that ask the KMS to wrap an
// object-encryption key for a specific (bucket, object) MUST build
// their context via this helper (or via mergeBindingContext) to keep
// PUT and GET paths symmetrical and to prevent silent drift between
// duplicated literal contexts.
func ObjectBindingContext(bucket, object string) Context {
	return objectBindingContext(bucket, object)
}

// objectBindingContext returns the canonical SSE-KMS binding context
// for an (bucket, object) pair. It is the single source of truth for
// the server-bound AEAD AAD used by SSE-KMS Generate/Decrypt calls.
func objectBindingContext(bucket, object string) Context {
	return Context{bucket: path.Join(bucket, object)}
}

// mergeBindingContext returns the binding context for (bucket, object)
// merged with the supplied client-provided KMS context. The bucket key
// is reserved: if clientCtx contains the reserved bucket key with a
// value different from the canonical binding value the function
// returns errKMSContextBindingConflict.
func mergeBindingContext(bucket, object string, clientCtx Context) (Context, error) {
	bound := objectBindingContext(bucket, object)
	for k, v := range clientCtx {
		if k == bucket {
			if v != bound[bucket] {
				return nil, errKMSContextBindingConflict
			}
			continue
		}
		bound[k] = v
	}
	return bound, nil
}

// MergeBindingContext is the exported wrapper around mergeBindingContext.
// It is the entry point for cmd-package PUT-handler glue (e.g.
// enforceSSEKMSRequest) that needs to validate a client-supplied SSE-KMS
// context against the server-bound bucket-binding key BEFORE calling
// into the KMS. On a reserved-key conflict it returns
// ErrKMSContextBindingConflict (the exported alias of the unexported
// sentinel) so callers can translate the failure into an HTTP-level
// AccessDenied without revealing which key was reserved.
func MergeBindingContext(bucket, object string, clientCtx Context) (Context, error) {
	return mergeBindingContext(bucket, object, clientCtx)
}

// CreateMetadata encodes the sealed object key into the metadata and
// returns the modified metadata. If the keyID and the kmsKey is not
// empty it encodes both into the metadata as well. When clientCtx is
// non-empty its JSON serialization is base64-encoded and stored under
// MetaContext so that future Unseal calls can replay the same AEAD
// AAD. It allocates a new metadata map if metadata is nil.
//
// SECURITY: clientCtx MUST be the raw client-supplied KMS context
// (e.g. parsed from x-amz-server-side-encryption-context). The caller
// MUST NOT pre-merge the server-bound binding context here; the
// binding context is reconstructed deterministically by
// UnsealObjectKey from (bucket, object) on each read.
func (ssekms) CreateMetadata(metadata map[string]string, keyID string, kmsKey []byte, sealedKey SealedKey, clientCtx Context) map[string]string {
	if sealedKey.Algorithm != SealAlgorithm {
		logger.CriticalIf(context.Background(), Errorf("The seal algorithm '%s' is invalid for SSE-S3", sealedKey.Algorithm))
	}

	// There are two possibilities:
	// - We use a KMS -> There must be non-empty key ID and a KMS data key.
	// - We use a K/V -> There must be no key ID and no KMS data key.
	// Otherwise, the caller has passed an invalid argument combination.
	if keyID == "" && len(kmsKey) != 0 {
		logger.CriticalIf(context.Background(), errors.New("The key ID must not be empty if a KMS data key is present"))
	}
	if keyID != "" && len(kmsKey) == 0 {
		logger.CriticalIf(context.Background(), errors.New("The KMS data key must not be empty if a key ID is present"))
	}

	if metadata == nil {
		metadata = make(map[string]string, 6)
	}

	metadata[MetaAlgorithm] = sealedKey.Algorithm
	metadata[MetaIV] = base64.StdEncoding.EncodeToString(sealedKey.IV[:])
	metadata[MetaSealedKeyKMS] = base64.StdEncoding.EncodeToString(sealedKey.Key[:])
	if len(kmsKey) > 0 && keyID != "" { // We use a KMS -> Store key ID and sealed KMS data key.
		metadata[MetaKeyID] = keyID
		metadata[MetaDataEncryptionKey] = base64.StdEncoding.EncodeToString(kmsKey)
	}
	if len(clientCtx) > 0 {
		// Use kms.Context.MarshalText directly so the persisted bytes
		// are the same canonical JSON object that the rest of the KMS
		// stack consumes. Going through jsoniter.Marshal would treat
		// Context as a TextMarshaler and double-encode it as a JSON
		// string, which then fails to unmarshal back into a Context
		// in ParseMetadata. The contract returned by MarshalText says
		// it never errors, but we tolerate the err signature anyway.
		if buf, err := clientCtx.MarshalText(); err == nil {
			metadata[MetaContext] = base64.StdEncoding.EncodeToString(buf)
		}
	}
	return metadata
}

// ParseMetadata extracts all SSE-KMS related values from the object
// metadata and checks whether they are well-formed. It returns the
// sealed object key on success. If the metadata contains both, a KMS
// master key ID and a sealed KMS data key it returns both. If the
// metadata does not contain neither a KMS master key ID nor a sealed
// KMS data key it returns an empty keyID and KMS data key. Otherwise,
// it returns an error.
//
// SECURITY: The returned ctx is the raw client-supplied KMS context
// as it was persisted on PUT. Callers MUST NOT use it directly as the
// AEAD AAD; instead they MUST run it through mergeBindingContext (or
// rely on UnsealObjectKey, which does so) so that the bucket-binding
// reserved key cannot be overridden by tampered metadata.
func (ssekms) ParseMetadata(metadata map[string]string) (keyID string, kmsKey []byte, sealedKey SealedKey, ctx Context, err error) {
	// Extract all required values from object metadata
	b64IV, ok := metadata[MetaIV]
	if !ok {
		return keyID, kmsKey, sealedKey, ctx, errMissingInternalIV
	}
	algorithm, ok := metadata[MetaAlgorithm]
	if !ok {
		return keyID, kmsKey, sealedKey, ctx, errMissingInternalSealAlgorithm
	}
	b64SealedKey, ok := metadata[MetaSealedKeyKMS]
	if !ok {
		return keyID, kmsKey, sealedKey, ctx, Errorf("The object metadata is missing the internal sealed key for SSE-S3")
	}

	// There are two possibilities:
	// - We use a KMS -> There must be a key ID and a KMS data key.
	// - We use a K/V -> There must be no key ID and no KMS data key.
	// Otherwise, the metadata is corrupted.
	keyID, idPresent := metadata[MetaKeyID]
	b64KMSSealedKey, kmsKeyPresent := metadata[MetaDataEncryptionKey]
	if !idPresent && kmsKeyPresent {
		return keyID, kmsKey, sealedKey, ctx, Errorf("The object metadata is missing the internal KMS key-ID for SSE-S3")
	}
	if idPresent && !kmsKeyPresent {
		return keyID, kmsKey, sealedKey, ctx, Errorf("The object metadata is missing the internal sealed KMS data key for SSE-S3")
	}

	// Check whether all extracted values are well-formed
	iv, err := base64.StdEncoding.DecodeString(b64IV)
	if err != nil || len(iv) != 32 {
		return keyID, kmsKey, sealedKey, ctx, errInvalidInternalIV
	}
	if algorithm != SealAlgorithm {
		return keyID, kmsKey, sealedKey, ctx, errInvalidInternalSealAlgorithm
	}
	encryptedKey, err := base64.StdEncoding.DecodeString(b64SealedKey)
	if err != nil || len(encryptedKey) != 64 {
		return keyID, kmsKey, sealedKey, ctx, Errorf("The internal sealed key for SSE-KMS is invalid")
	}
	if idPresent && kmsKeyPresent { // We are using a KMS -> parse the sealed KMS data key.
		kmsKey, err = base64.StdEncoding.DecodeString(b64KMSSealedKey)
		if err != nil {
			return keyID, kmsKey, sealedKey, ctx, Errorf("The internal sealed KMS data key for SSE-KMS is invalid")
		}
	}
	b64Ctx, ok := metadata[MetaContext]
	if ok {
		b, err := base64.StdEncoding.DecodeString(b64Ctx)
		if err != nil {
			return keyID, kmsKey, sealedKey, ctx, Errorf("The internal KMS context is not base64-encoded")
		}
		var json = jsoniter.ConfigCompatibleWithStandardLibrary
		ctx = Context{}
		if err = json.Unmarshal(b, &ctx); err != nil {
			return keyID, kmsKey, sealedKey, ctx, Errorf("The internal sealed KMS context is invalid")
		}
	}

	sealedKey.Algorithm = algorithm
	copy(sealedKey.IV[:], iv)
	copy(sealedKey.Key[:], encryptedKey)
	return keyID, kmsKey, sealedKey, ctx, nil
}
