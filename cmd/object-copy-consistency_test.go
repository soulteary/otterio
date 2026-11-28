// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/otterio/cmd/crypto"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
)

func TestSelfCopySourceSnapshot(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx := context.Background()
			const bucket = "copy-source-snapshot"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			for _, metadataOnly := range []bool{true, false} {
				name := "metadata"
				if !metadataOnly {
					name = "full"
				}
				t.Run(name, func(t *testing.T) {
					old, err := obj.PutObject(ctx, bucket, name, ifMatchBackendReader(t, "old source"), ObjectOptions{})
					if err != nil {
						t.Fatal(err)
					}
					copyInfo := old.Clone()
					copyInfo.metadataOnly = metadataOnly
					copyInfo.UserDefined = map[string]string{"custom": "copied"}
					copyInfo.PutObjReader = ifMatchBackendReader(t, "old source")
					fresh, err := obj.PutObject(ctx, bucket, name, ifMatchBackendReader(t, "new source"), ObjectOptions{})
					if err != nil {
						t.Fatal(err)
					}
					// This is the precise stale-read/commit interleaving: the copy
					// carries A, while storage already contains B before it locks.
					if _, err = obj.CopyObject(ctx, bucket, name, bucket, name, copyInfo, ObjectOptions{}, ObjectOptions{}); !isErrPreconditionFailed(err) {
						t.Fatalf("stale self-copy: %v", err)
					}
					got := ifMatchBackendContents(t, obj, bucket, name, "new source")
					if got.ETag != fresh.ETag || got.UserDefined["custom"] == "copied" {
						t.Fatalf("stale copy changed current metadata: %+v", got)
					}
					copyInfo = fresh.Clone()
					copyInfo.metadataOnly = metadataOnly
					copyInfo.UserDefined = map[string]string{"custom": "fresh-copy"}
					copyInfo.PutObjReader = ifMatchBackendReader(t, "new source")
					if _, err = obj.CopyObject(ctx, bucket, name, bucket, name, copyInfo, ObjectOptions{}, ObjectOptions{}); err != nil {
						t.Fatalf("unchanged source copy: %v", err)
					}
					got = ifMatchBackendContents(t, obj, bucket, name, "new source")
					if got.ETag != fresh.ETag || got.UserDefined["custom"] != "fresh-copy" {
						t.Fatalf("fresh copy did not preserve data/ETag and replace metadata: %+v", got)
					}
				})
			}

			t.Run("raw-snapshot-and-caller-owned-lock", func(t *testing.T) {
				const key = "raw-snapshot"
				source, err := obj.PutObject(ctx, bucket, key, ifMatchBackendReader(t, "data"), ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				copyInfo := source.Clone()
				copyInfo.metadataOnly = true
				// Source readers can translate the ETag and size. The separate
				// raw snapshot must govern the storage comparison and validator.
				copyInfo.ETag = "client-visible-validator"
				copyInfo.Size = source.Size + 1
				copyInfo.UserDefined = map[string]string{"custom": "raw-snapshot"}
				lk := obj.NewNSLock(bucket, encodeDirObject(key))
				lockedCtx, err := lk.GetLock(ctx, globalOperationTimeout)
				if err != nil {
					t.Fatal(err)
				}
				_, err = obj.CopyObject(lockedCtx, bucket, key, bucket, key, copyInfo, ObjectOptions{}, ObjectOptions{NoLock: true, CopySourceExpected: &source})
				lk.Unlock()
				if err != nil {
					t.Fatalf("copy with a caller-owned lock and raw snapshot: %v", err)
				}
				got := ifMatchBackendContents(t, obj, bucket, key, "data")
				if got.ETag != source.ETag {
					t.Fatalf("copy replaced stored ETag with the reader's ETag: %s", got.ETag)
				}
			})
		})
	})
}

// Exercise production HTTP readers as well as the commit path: encrypted
// readers expose a different ETag and compressed readers expose a different
// size from the storage snapshot that must fence a self-copy.
func TestSelfCopyHTTPRepresentation(t *testing.T) {
	defer DetectTestLeak(t)()
	ExecObjectLayerAPITest(t, func(obj ObjectLayer, instance, bucket string, router http.Handler, credentials auth.Credentials, t *testing.T) {
		for _, mode := range []string{"compressed", "S3", "KMS"} {
			t.Run(instance+"/"+mode, func(t *testing.T) {
				globalCompressConfigMu.Lock()
				savedCompression := globalCompressConfig
				globalCompressConfig.Enabled = mode == "compressed"
				globalCompressConfig.AllowEncrypted = false
				globalCompressConfig.Extensions = nil
				globalCompressConfig.MimeTypes = nil
				globalCompressConfigMu.Unlock()
				defer func() {
					globalCompressConfigMu.Lock()
					globalCompressConfig = savedCompression
					globalCompressConfigMu.Unlock()
				}()
				oldKMS, oldTLS := GlobalKMS, globalIsTLS
				GlobalKMS, globalIsTLS = conditionalMatchTestKMS(t), true
				defer func() { GlobalKMS, globalIsTLS = oldKMS, oldTLS }()

				key := "self-copy-" + mode + ".txt"
				target := "http://otterio.test/" + bucket + "/" + key
				body := strings.Repeat("OtterIO self-copy must preserve these bytes.\n", 512)
				send := func(method, body string, headers http.Header) *httptest.ResponseRecorder {
					t.Helper()
					response := httptest.NewRecorder()
					router.ServeHTTP(response, conditionalMatchRequest(t, "v4", method, target, body, credentials, headers))
					if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<Error>") {
						t.Fatalf("%s: %d %s", method, response.Code, response.Body.String())
					}
					return response
				}
				writeHeaders := http.Header{}
				writeHeaders.Set(xhttp.ContentType, "text/plain")
				switch mode {
				case "S3":
					writeHeaders.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionAES)
				case "KMS":
					writeHeaders.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionKMS)
					writeHeaders.Set(xhttp.AmzServerSideEncryptionKmsID, conditionalMatchTestKeyID)
				}
				send(http.MethodPut, body, writeHeaders)
				before := send(http.MethodGet, "", nil)
				if before.Body.String() != body {
					t.Fatal("initial transformed-object GET did not round-trip")
				}
				raw, err := obj.GetObjectInfo(context.Background(), bucket, key, ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "compressed" {
					if !raw.IsCompressed() || raw.Size >= int64(len(body)) {
						t.Fatalf("fixture did not persist compressed bytes: %+v", raw)
					}
				} else if _, encrypted := crypto.IsEncrypted(raw.UserDefined); !encrypted {
					t.Fatal("fixture did not persist encrypted bytes")
				}

				copyHeaders := writeHeaders.Clone()
				copyHeaders.Set(xhttp.AmzCopySource, "/"+bucket+"/"+key)
				copyHeaders.Set(xhttp.AmzMetadataDirective, "REPLACE")
				copyHeaders.Set("X-Amz-Meta-Description", "metadata replaced")
				copied := send(http.MethodPut, "", copyHeaders)
				var copiedInfo CopyObjectResponse
				if err := xml.Unmarshal(copied.Body.Bytes(), &copiedInfo); err != nil || copiedInfo.ETag == "" {
					t.Fatalf("copy response: %s / %v", copied.Body.String(), err)
				}
				after := send(http.MethodGet, "", nil)
				if after.Body.String() != body || conditionalMatchResponseHeader(after.Header(), "X-Amz-Meta-Description") != "metadata replaced" {
					t.Fatalf("self-copy changed bytes or failed metadata replacement: length=%d headers=%v", after.Body.Len(), after.Header())
				}
				afterETag := conditionalMatchResponseHeader(after.Header(), xhttp.ETag)
				if afterETag == "" || canonicalizeETag(copiedInfo.ETag) != canonicalizeETag(afterETag) {
					t.Fatalf("copy ETag %s does not identify the stored response %s", copiedInfo.ETag, afterETag)
				}
				if mode == "compressed" {
					fresh, err := obj.GetObjectInfo(context.Background(), bucket, key, ObjectOptions{})
					if err != nil {
						t.Fatal(err)
					}
					for _, field := range []string{ReservedMetadataPrefix + "compression", ReservedMetadataPrefix + "actual-size"} {
						if fresh.UserDefined[field] != raw.UserDefined[field] {
							t.Fatalf("metadata-only copy changed %s: %q -> %q", field, raw.UserDefined[field], fresh.UserDefined[field])
						}
					}
					if fresh.Size != raw.Size || fresh.ETag != raw.ETag || afterETag != conditionalMatchResponseHeader(before.Header(), xhttp.ETag) {
						t.Fatal("metadata-only copy changed the compressed representation or validator")
					}
				}
			})
		}
	}, nil)
}
