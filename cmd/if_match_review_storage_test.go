// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/hash"
)

func TestIfMatchReviewTagMutationHoldsCommitLock(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx := context.Background()
			const bucket = "if-match-review-tags"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"put-tags", "delete-tags"} {
				t.Run(operation, func(t *testing.T) {
					old, err := obj.PutObject(ctx, bucket, operation, ifMatchBackendReader(t, "old body"), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=prod"}})
					if err != nil {
						t.Fatal(err)
					}
					checked, release := make(chan struct{}), make(chan struct{})
					var releaseOnce sync.Once
					releaseWriter := func() { releaseOnce.Do(func() { close(release) }) }
					defer releaseWriter()
					writerDone := make(chan error, 1)
					reader := ifMatchBackendReader(t, "conditional body")
					go func() {
						_, err := obj.PutObject(ctx, bucket, operation, reader, ObjectOptions{IfMatch: &ObjectIfMatchCondition{
							ETag: old.ETag,
							Authorize: func(current ObjectInfo) error {
								if current.UserTags != "environment=prod" {
									return errAuthentication
								}
								close(checked)
								<-release
								return nil
							},
						}})
						writerDone <- err
					}()
					select {
					case <-checked:
					case err := <-writerDone:
						t.Fatalf("conditional writer did not authorize current tags: %v", err)
					case <-time.After(10 * time.Second):
						t.Fatal("conditional writer did not reach the commit check")
					}
					mutatorDone := make(chan error, 1)
					go func() {
						var err error
						if operation == "put-tags" {
							_, err = obj.PutObjectTags(ctx, bucket, operation, "environment=dev", ObjectOptions{})
						} else {
							_, err = obj.DeleteObjectTags(ctx, bucket, operation, ObjectOptions{})
						}
						mutatorDone <- err
					}()
					var escaped bool
					var mutatorErr error
					select {
					case mutatorErr = <-mutatorDone:
						escaped = true
					case <-time.After(100 * time.Millisecond):
					}
					releaseWriter()
					if err := <-writerDone; err != nil {
						t.Fatalf("conditional writer: %v", err)
					}
					if !escaped {
						mutatorErr = <-mutatorDone
					}
					if mutatorErr != nil {
						t.Fatalf("tag mutation: %v", mutatorErr)
					}
					if escaped {
						t.Fatal("tag mutation escaped the destination commit lock; the conditional write used stale tag authorization")
					}
				})
			}
			if instance == FSTestStr {
				t.Run("caller-owned-lock", func(t *testing.T) {
					const name = "caller-owned-lock"
					if _, err := obj.PutObject(ctx, bucket, name, ifMatchBackendReader(t, "body"), ObjectOptions{}); err != nil {
						t.Fatal(err)
					}
					lockCtx, cancel := context.WithTimeout(ctx, time.Second)
					defer cancel()
					lk := obj.NewNSLock(bucket, name)
					lockCtx, err := lk.GetLock(lockCtx, globalOperationTimeout)
					if err != nil {
						t.Fatal(err)
					}
					defer lk.Unlock()
					if _, err := obj.PutObjectTags(lockCtx, bucket, name, "environment=prod", ObjectOptions{NoLock: true}); err != nil {
						t.Fatalf("tag update reacquired its caller's lock: %v", err)
					}
					if _, err := obj.DeleteObjectTags(lockCtx, bucket, name, ObjectOptions{NoLock: true}); err != nil {
						t.Fatalf("tag deletion reacquired its caller's lock: %v", err)
					}
				})
			}
		})
	})
}

func TestSelfCopyReviewRejectsChangedMetadataSnapshot(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx := context.Background()
			const bucket = "self-copy-review-metadata"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			for _, mutation := range []string{"tags", "metadata"} {
				for _, metadataOnly := range []bool{true, false} {
					name := mutation + "-metadata-copy"
					if !metadataOnly {
						name = mutation + "-full-copy"
					}
					t.Run(name, func(t *testing.T) {
						const body = "source bytes"
						if _, err := obj.PutObject(ctx, bucket, name, ifMatchBackendReader(t, body), ObjectOptions{UserDefined: map[string]string{xhttp.AmzObjectTagging: "environment=prod", "description": "original"}}); err != nil {
							t.Fatal(err)
						}
						// The HTTP source authorization has observed environment=prod.
						snapshot, err := obj.GetObjectInfo(ctx, bucket, name, ObjectOptions{})
						if err != nil {
							t.Fatal(err)
						}
						copyInfo := snapshot.Clone()
						copyInfo.metadataOnly = metadataOnly
						copyInfo.PutObjReader = ifMatchBackendReader(t, body)
						copyInfo.UserDefined[xhttp.AmzObjectTagging] = snapshot.UserTags
						copyInfo.UserDefined["description"] = "stale authorized copy"
						wantTags, wantDescription := "environment=dev", "original"
						if mutation == "tags" {
							if _, err := obj.PutObjectTags(ctx, bucket, name, wantTags, ObjectOptions{}); err != nil {
								t.Fatal(err)
							}
						} else {
							wantTags, wantDescription = "environment=prod", "new metadata"
							update := snapshot.Clone()
							update.metadataOnly = true
							update.UserDefined[xhttp.AmzObjectTagging] = wantTags
							update.UserDefined["description"] = wantDescription
							if _, err := obj.CopyObject(ctx, bucket, name, bucket, name, update, ObjectOptions{}, ObjectOptions{CopySourceExpected: &snapshot}); err != nil {
								t.Fatalf("current metadata copy: %v", err)
							}
						}
						if _, err := obj.CopyObject(ctx, bucket, name, bucket, name, copyInfo, ObjectOptions{}, ObjectOptions{CopySourceExpected: &snapshot}); !isErrPreconditionFailed(err) {
							t.Fatalf("copy accepted a stale source snapshot after %s changed: %v", mutation, err)
						}
						current := ifMatchBackendContents(t, obj, bucket, name, body)
						if current.UserTags != wantTags || current.UserDefined["description"] != wantDescription {
							t.Fatalf("stale copy reverted current metadata: tags=%q description=%q", current.UserTags, current.UserDefined["description"])
						}
					})
				}
			}
		})
	})
}

func TestSelfCopyReviewRejectsStaleSSECKeyRotation(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx := context.Background()
			const bucket, name, body = "self-copy-review-rotation", "encrypted-source", "encrypted source bytes"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			oldKey, winnerKey, staleKey := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32)
			metadata := make(map[string]string)
			plain := ifMatchBackendReader(t, body)
			encReader, objectKey, err := newEncryptReader(plain.Reader, oldKey, bucket, name, metadata, false)
			if err != nil {
				t.Fatal(err)
			}
			sizeInfo := ObjectInfo{Size: int64(len(body))}
			encHash, err := hash.NewReader(encReader, sizeInfo.EncryptedSize(), "", "", int64(len(body)))
			if err != nil {
				t.Fatal(err)
			}
			data, err := plain.WithEncryption(encHash, &objectKey)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := obj.PutObject(ctx, bucket, name, data, ObjectOptions{UserDefined: metadata}); err != nil {
				t.Fatal(err)
			}
			snapshot, err := obj.GetObjectInfo(ctx, bucket, name, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			// Two rotations have prepared different seals from the same old key.
			rotation := func(key []byte) ObjectInfo {
				t.Helper()
				info := snapshot.Clone()
				info.metadataOnly, info.keyRotation = true, true
				if err := rotateKey(oldKey, key, bucket, name, info.UserDefined); err != nil {
					t.Fatal(err)
				}
				return info
			}
			winner, stale := rotation(winnerKey), rotation(staleKey)
			if _, err := obj.CopyObject(ctx, bucket, name, bucket, name, winner, ObjectOptions{}, ObjectOptions{CopySourceExpected: &snapshot}); err != nil {
				t.Fatalf("first key rotation: %v", err)
			}
			current, err := obj.GetObjectInfo(ctx, bucket, name, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if current.ETag != snapshot.ETag || current.Size != snapshot.Size || !current.ModTime.Equal(snapshot.ModTime) {
				t.Fatal("key rotation must preserve the data identity; this regression needs a metadata-only change")
			}
			if _, err := obj.CopyObject(ctx, bucket, name, bucket, name, stale, ObjectOptions{}, ObjectOptions{CopySourceExpected: &snapshot}); !isErrPreconditionFailed(err) {
				t.Fatalf("stale rotation replaced a newer customer key: %v", err)
			}
			current, err = obj.GetObjectInfo(ctx, bucket, name, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if key, err := decryptObjectInfo(winnerKey, bucket, name, current.UserDefined); err != nil || !bytes.Equal(key, objectKey[:]) {
				t.Fatalf("winning customer key no longer unseals the data key: %v", err)
			}
			if _, err := decryptObjectInfo(staleKey, bucket, name, current.UserDefined); err == nil {
				t.Fatal("stale customer key unexpectedly became authoritative")
			}
			digest := md5.Sum(winnerKey)
			headers := http.Header{}
			headers.Set(xhttp.AmzServerSideEncryptionCustomerAlgorithm, xhttp.AmzEncryptionAES)
			headers.Set(xhttp.AmzServerSideEncryptionCustomerKey, base64.StdEncoding.EncodeToString(winnerKey))
			headers.Set(xhttp.AmzServerSideEncryptionCustomerKeyMD5, base64.StdEncoding.EncodeToString(digest[:]))
			reader, err := obj.GetObjectNInfo(ctx, bucket, name, nil, headers, readLock, ObjectOptions{})
			if err != nil {
				t.Fatalf("read with winning key: %v", err)
			}
			got, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(got) != body {
				t.Fatalf("encrypted data did not round-trip with the winning key: %q / %v", got, err)
			}
		})
	})
}

func TestSelfCopyReviewUsesPersistedStorageClass(t *testing.T) {
	fi := transitionStorageFixture()
	fi.TransitionStatus = lifecycle.TransitionComplete
	ref := transitionStorageReference()
	ref.StorageClass = ""
	if err := setTransitionedObject(fi.Metadata, ref); err != nil {
		t.Fatal(err)
	}
	// GET and metadata-only copy obtain this raw representation from FileInfo.
	raw := fi.ToObjectInfo(fi.Volume, fi.Name)
	// getObjectInfo supplements legacy transitioned objects with a class from
	// the current lifecycle configuration. That derived label is not a change
	// to the stored source snapshot, in either direction of a self-copy check.
	lookup := raw.Clone()
	lookup.StorageClass = "ARCHIVE"
	if raw.StorageClass == lookup.StorageClass {
		t.Fatal("fixture did not produce different raw and derived storage classes")
	}
	if !sameCopySource(lookup, &raw, false) || !sameCopySource(raw, &lookup, false) {
		t.Fatal("derived lifecycle storage class changed an otherwise identical source snapshot")
	}

	persisted := lookup.Clone()
	persisted.UserDefined[xhttp.AmzStorageClass] = "REDUCED_REDUNDANCY"
	if sameCopySource(persisted, &raw, false) {
		t.Fatal("a changed persisted storage class escaped the source snapshot check")
	}
	persisted = lookup.Clone()
	persisted.TransitionedObject.StorageClass = "ANOTHER-TIER"
	if sameCopySource(persisted, &raw, false) {
		t.Fatal("a changed persisted transition reference escaped the source snapshot check")
	}
}
