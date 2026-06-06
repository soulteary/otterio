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
	"path"
	"reflect"
	"testing"

	"github.com/soulteary/otterio/cmd/crypto"
	"github.com/soulteary/otterio/pkg/kms"
)

// Pinning tests for backlog row 32 (SSE-KMS context binding) at the
// cmd-package boundary. They are intentionally narrow:
//
//   - cmd/encryption-v1.go and cmd/disk-cache-{utils,backend}.go must
//     all build their object-bound KMS context through the canonical
//     crypto.ObjectBindingContext helper. Any literal Context{bucket:
//     path.Join(bucket, object)} construction in this package is a
//     regression of Bug C.
//
//   - cmd/bucket-metadata*.go must build bucket-level metadata KMS
//     context through the local bucketTargetsCtx helper. Any literal
//     kms.Context{bucket: pathJoin("bucket-targets", "bucket-targets.json")}
//     elsewhere is a regression of Bug D's sealing-by-convention.
//
// SSE-KMS PUT wiring is intentionally out of scope here (tracked
// separately as Bug B follow-up). Real AEAD round-trip behavior is
// pinned in cmd/crypto/sse-kms_e2e_test.go.

// TestObjectBindingContextStableInCmd is a sanity check that the
// helper imported from cmd/crypto returns the exact map shape the cmd
// package depends on. The L1/L2 tests already pin the same property
// inside the crypto package; this test guards against an accidental
// re-export change that would only surface when cmd/* compiles.
func TestObjectBindingContextStableInCmd(t *testing.T) {
	got := crypto.ObjectBindingContext("alpha", "x/y")
	want := kms.Context{"alpha": path.Join("alpha", "x/y")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("crypto.ObjectBindingContext = %v, want %v", got, want)
	}
}

// TestBucketTargetsCtxStable pins the canonical shape of the bucket
// targets metadata context. Every encrypt/decrypt site in the cmd
// package MUST go through bucketTargetsCtx so that the AAD bytes are
// byte-equal at seal- and unseal-time. If the shape ever changes, all
// previously stored bucket-targets payloads become unreadable, so any
// edit here is a load-bearing migration and should be done deliberately.
func TestBucketTargetsCtxStable(t *testing.T) {
	got := bucketTargetsCtx("my-bucket")
	want := kms.Context{
		"my-bucket":       "my-bucket",
		bucketTargetsFile: bucketTargetsFile,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bucketTargetsCtx = %v, want %v", got, want)
	}
}
