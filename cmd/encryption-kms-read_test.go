// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"testing"

	"github.com/soulteary/otterio/cmd/crypto"
	xhttp "github.com/soulteary/otterio/cmd/http"
)

func TestSSEKMSReadUsesStoredKey(t *testing.T) {
	withTempGlobalKMS(t, conditionalMatchTestKMS(t))
	const bucket, object = "kms-read-bucket", "object"
	plain := bytes.Repeat([]byte("KMS reads recover the stored object key.\n"), 4096)
	metadata := make(map[string]string)
	encrypted, key, err := newEncryptReaderKMS(bytes.NewReader(plain), bucket, object, metadata, conditionalMatchTestKeyID, crypto.Context{"purpose": "read-regression"})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := io.ReadAll(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	digest := md5.Sum(plain)
	info := ObjectInfo{
		Bucket: bucket, Name: object, Size: int64(len(ciphertext)),
		ETag: hex.EncodeToString(key.SealETag(digest[:])), UserDefined: metadata,
	}
	for _, interval := range []struct {
		name       string
		start, end int64
	}{
		{"full", 0, int64(len(plain)) - 1},
		{"range-after-first-package", SSEDAREPackageBlockSize + 7, SSEDAREPackageBlockSize*2 + 19},
	} {
		t.Run(interval.name, func(t *testing.T) {
			var rs *HTTPRangeSpec
			if interval.name != "full" {
				rs = &HTTPRangeSpec{Start: interval.start, End: interval.end}
			}
			read, offset, length, err := NewGetObjectReader(rs, info, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := read(bytes.NewReader(ciphertext[offset:offset+length]), http.Header{}, nil)
			if err != nil {
				t.Fatalf("SSE-KMS read without customer headers: %v", err)
			}
			defer reader.Close()
			got, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(got, plain[interval.start:interval.end+1]) {
				t.Fatalf("KMS read changed the bytes: size=%d error=%v", len(got), err)
			}
			if reader.ObjInfo.ETag != hex.EncodeToString(digest[:]) {
				t.Fatalf("KMS reader exposed a storage ETag: %s", reader.ObjInfo.ETag)
			}
		})
	}
	// Selecting the KMS path must retain the object binding and the configured
	// KMS requirement, rather than accepting a supplied customer key or fallback.
	if _, err := DecryptRequestWithSequenceNumberR(bytes.NewReader(ciphertext), http.Header{}, bucket, object+"-other", 0, metadata); err == nil {
		t.Fatal("KMS ciphertext was accepted under another object's binding")
	}
	GlobalKMS = nil
	if _, err := DecryptRequestWithSequenceNumberR(bytes.NewReader(ciphertext), http.Header{}, bucket, object, 0, metadata); err != errKMSNotConfigured {
		t.Fatalf("missing KMS returned an unrelated customer-key error: %v", err)
	}
}

func TestSSEKMSReadRejectsIncompatibleHeaders(t *testing.T) {
	metadata := map[string]string{crypto.MetaSealedKeyKMS: ""}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, test := range []struct {
			name, header, value string
			want                error
		}{
			{"destination-SSE", xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionKMS, errInvalidEncryptionParameters},
			{"customer-key", xhttp.AmzServerSideEncryptionCustomerAlgorithm, xhttp.AmzEncryptionAES, errEncryptedObject},
			{"copy-customer-key", xhttp.AmzServerSideEncryptionCopyCustomerAlgorithm, xhttp.AmzEncryptionAES, errEncryptedObject},
		} {
			t.Run(method+"/"+test.name, func(t *testing.T) {
				request := &http.Request{Method: method, Header: http.Header{}}
				request.Header.Set(test.header, test.value)
				info := ObjectInfo{UserDefined: metadata}
				if _, err := DecryptObjectInfo(&info, request); err != test.want {
					t.Fatalf("incompatible KMS read headers: got %v, want %v", err, test.want)
				}
			})
		}
	}
	copyHeaders := http.Header{}
	copyHeaders.Set(xhttp.AmzServerSideEncryptionCopyCustomerAlgorithm, xhttp.AmzEncryptionAES)
	if _, err := ParseSSECopyCustomerRequest(copyHeaders, metadata); err != crypto.ErrIncompatibleEncryptionMethod {
		t.Fatalf("SSE-KMS copy accepted source customer-key headers: %v", err)
	}
}
