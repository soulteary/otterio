// Copyright (c) 2025 OtterIO contributors
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

package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"path"
	"reflect"
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/soulteary/otterio/pkg/kms"
)

// Pinning tests for backlog row 32 (SSE-KMS context binding).
//
// These are L1 tests: they use a recording mockKMS so we can assert
// exactly what AEAD AAD UnsealObjectKey hands to the KMS, without
// relying on a particular KMS implementation. End-to-end cryptographic
// behavior is covered by L2 tests sitting alongside this file.

// recordingKMS is a kms.KMS that records the (keyID, ctx) pair given
// to GenerateKey/DecryptKey and is otherwise dumb. The Plaintext it
// returns is a fixed sentinel which is enough for UnsealObjectKey to
// proceed to the ObjectKey.Unseal call.
type recordingKMS struct {
	wantPlaintext []byte
	gotKeyID      string
	gotCtx        Context
	decryptErr    error
}

func (r *recordingKMS) Stat() (kms.Status, error) { return kms.Status{Name: "recording"}, nil }
func (r *recordingKMS) CreateKey(string) error    { return errors.New("unused") }

func (r *recordingKMS) GenerateKey(keyID string, ctx Context) (kms.DEK, error) {
	r.gotKeyID = keyID
	r.gotCtx = cloneCtx(ctx)
	return kms.DEK{
		KeyID:      keyID,
		Plaintext:  append([]byte(nil), r.wantPlaintext...),
		Ciphertext: []byte("recorded-ciphertext"),
	}, nil
}

func (r *recordingKMS) DecryptKey(keyID string, _ []byte, ctx Context) ([]byte, error) {
	r.gotKeyID = keyID
	r.gotCtx = cloneCtx(ctx)
	if r.decryptErr != nil {
		return nil, r.decryptErr
	}
	return append([]byte(nil), r.wantPlaintext...), nil
}

func cloneCtx(in Context) Context {
	if in == nil {
		return nil
	}
	out := make(Context, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// helper: build a minimal-but-valid SSE-KMS metadata blob. It seals a
// fresh ObjectKey so that UnsealObjectKey reaches the AAD-recording
// step before bailing out on the dummy KMS plaintext.
func sealedKMSMetadata(t *testing.T, bucket, object string, persistedClientCtx Context) (metadata map[string]string, plaintext []byte) {
	t.Helper()

	// Generate a 32-byte external key. The recordingKMS will hand
	// the same bytes back from DecryptKey so that ObjectKey.Unseal
	// succeeds.
	plaintext = make([]byte, 32)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatalf("rand: %v", err)
	}

	objKey := GenerateKey(plaintext, rand.Reader)
	iv := GenerateIV(rand.Reader)
	sealed := objKey.Seal(plaintext, iv, S3KMS.String(), bucket, object)

	metadata = S3KMS.CreateMetadata(nil, "test-key-id", []byte("ciphertext-bytes"), sealed, persistedClientCtx)
	return metadata, plaintext
}

// TestSSEKMSUnsealObjectKeyForcesBucketBinding pins Bug A: regardless
// of what client ctx the persisted MetaContext carried, the boundCtx
// reaching the KMS MUST contain bucket=path.Join(bucket,object).
func TestSSEKMSUnsealObjectKeyForcesBucketBinding(t *testing.T) {
	const bucket, object = "bkt", "obj"

	cases := []struct {
		name         string
		clientCtx    Context
		extraEntries []string // expected extra keys to be present in the recorded ctx
	}{
		{name: "nil-client-ctx", clientCtx: nil},
		{name: "empty-client-ctx", clientCtx: Context{}},
		{
			name:         "non-conflicting-client-ctx",
			clientCtx:    Context{"app-tag": "billing", "tenant": "acme"},
			extraEntries: []string{"app-tag", "tenant"},
		},
		{
			// Persisted bucket key matching the canonical binding
			// is allowed and must appear unchanged in the recorded
			// ctx.
			name:      "client-ctx-bucket-matches-canonical",
			clientCtx: Context{bucket: path.Join(bucket, object)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metadata, plaintext := sealedKMSMetadata(t, bucket, object, tc.clientCtx)
			rk := &recordingKMS{wantPlaintext: plaintext}

			if _, err := S3KMS.UnsealObjectKey(rk, metadata, bucket, object); err != nil {
				t.Fatalf("UnsealObjectKey returned unexpected error: %v", err)
			}

			if rk.gotCtx == nil {
				t.Fatalf("recordingKMS did not receive a context")
			}
			gotBound, ok := rk.gotCtx[bucket]
			if !ok {
				t.Fatalf("recorded ctx missing the bucket-binding key %q: %#v", bucket, rk.gotCtx)
			}
			wantBound := path.Join(bucket, object)
			if gotBound != wantBound {
				t.Fatalf("recorded bucket-binding value = %q, want %q", gotBound, wantBound)
			}
			for _, k := range tc.extraEntries {
				if v, ok := rk.gotCtx[k]; !ok || v != tc.clientCtx[k] {
					t.Fatalf("expected extra ctx key %q=%q in recorded ctx, got %q (present=%v)", k, tc.clientCtx[k], v, ok)
				}
			}
		})
	}
}

// TestSSEKMSUnsealObjectKeyRejectsBucketKeyOverride pins Bug A: a
// persisted client ctx that tries to point the bucket-binding key at
// a different (bucket, object) MUST cause UnsealObjectKey to return
// errKMSContextBindingConflict and MUST NOT call into the KMS.
func TestSSEKMSUnsealObjectKeyRejectsBucketKeyOverride(t *testing.T) {
	const bucket, object = "bkt", "obj"

	hostile := Context{bucket: "evil/path"}
	metadata, plaintext := sealedKMSMetadata(t, bucket, object, hostile)
	rk := &recordingKMS{wantPlaintext: plaintext}

	_, err := S3KMS.UnsealObjectKey(rk, metadata, bucket, object)
	if !errors.Is(err, errKMSContextBindingConflict) {
		t.Fatalf("UnsealObjectKey err = %v, want errKMSContextBindingConflict", err)
	}
	if rk.gotCtx != nil {
		t.Fatalf("KMS was invoked despite binding conflict; recorded ctx = %#v", rk.gotCtx)
	}
}

// TestSSEKMSCreateMetadataPersistsClientContext pins Bug B: when the
// caller provides a non-empty clientCtx, CreateMetadata MUST write a
// base64-JSON serialization to MetaContext that round-trips through
// ParseMetadata.
func TestSSEKMSCreateMetadataPersistsClientContext(t *testing.T) {
	clientCtx := Context{"app": "billing", "tenant": "acme"}

	objKey := GenerateKey(make([]byte, 32), rand.Reader)
	sealed := objKey.Seal(make([]byte, 32), GenerateIV(rand.Reader), S3KMS.String(), "bkt", "obj")
	md := S3KMS.CreateMetadata(nil, "kid", []byte("kk"), sealed, clientCtx)

	encoded, ok := md[MetaContext]
	if !ok {
		t.Fatalf("metadata missing %s", MetaContext)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("MetaContext is not base64: %v", err)
	}
	var got Context
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("MetaContext payload is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(got, clientCtx) {
		t.Fatalf("persisted client ctx = %v, want %v", got, clientCtx)
	}

	// And ParseMetadata must agree.
	_, _, _, parsedCtx, err := S3KMS.ParseMetadata(md)
	if err != nil {
		t.Fatalf("ParseMetadata: %v", err)
	}
	if !reflect.DeepEqual(parsedCtx, clientCtx) {
		t.Fatalf("ParseMetadata ctx = %v, want %v", parsedCtx, clientCtx)
	}
}

// TestSSEKMSCreateMetadataEmptyClientContext pins the inverse: an
// empty / nil clientCtx MUST NOT cause MetaContext to appear in the
// metadata, so that a later ParseMetadata returns a nil Context and
// UnsealObjectKey takes the no-client-ctx branch.
func TestSSEKMSCreateMetadataEmptyClientContext(t *testing.T) {
	objKey := GenerateKey(make([]byte, 32), rand.Reader)
	sealed := objKey.Seal(make([]byte, 32), GenerateIV(rand.Reader), S3KMS.String(), "bkt", "obj")

	for _, ctx := range []Context{nil, {}} {
		md := S3KMS.CreateMetadata(nil, "kid", []byte("kk"), sealed, ctx)
		if v, ok := md[MetaContext]; ok {
			t.Fatalf("MetaContext set for empty ctx %v: %q", ctx, v)
		}
	}
}

// TestMergeBindingContextRejectsBucketOverride pins the helper itself:
// any clientCtx whose bucket key disagrees with the canonical binding
// is rejected with errKMSContextBindingConflict.
func TestMergeBindingContextRejectsBucketOverride(t *testing.T) {
	const bucket, object = "bkt", "obj"
	bound := path.Join(bucket, object)

	cases := []struct {
		name      string
		clientCtx Context
		wantErr   error
	}{
		{name: "nil", clientCtx: nil, wantErr: nil},
		{name: "empty", clientCtx: Context{}, wantErr: nil},
		{name: "matching-bucket", clientCtx: Context{bucket: bound}, wantErr: nil},
		{name: "extra-keys", clientCtx: Context{"foo": "bar"}, wantErr: nil},
		{name: "hostile-bucket", clientCtx: Context{bucket: "evil/path"}, wantErr: errKMSContextBindingConflict},
		{name: "empty-bucket-value", clientCtx: Context{bucket: ""}, wantErr: errKMSContextBindingConflict},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := mergeBindingContext(bucket, object, tc.clientCtx)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if got := ctx[bucket]; got != bound {
				t.Fatalf("bound value = %q, want %q", got, bound)
			}
		})
	}
}

// TestObjectBindingContextStable pins the canonical shape of the
// binding context. Any change here is a load-bearing change to all
// SSE-KMS / SSE-S3 GenerateKey / DecryptKey call sites and MUST be
// done deliberately.
func TestObjectBindingContextStable(t *testing.T) {
	got := ObjectBindingContext("bkt", "obj")
	want := Context{"bkt": path.Join("bkt", "obj")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ObjectBindingContext = %v, want %v", got, want)
	}
}
