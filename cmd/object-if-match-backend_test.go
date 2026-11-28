// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/hash"
)

func ifMatchBackendReader(t testing.TB, body string) *PutObjReader {
	t.Helper()
	r, err := hash.NewReader(bytes.NewBufferString(body), int64(len(body)), "", "", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	return NewPutObjReader(r)
}

func ifMatchBackendContents(t testing.TB, obj ObjectLayer, bucket, key, want string) ObjectInfo {
	t.Helper()
	r, err := obj.GetObjectNInfo(context.Background(), bucket, key, nil, http.Header{}, readLock, ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(got) != want {
		t.Fatalf("%s: got %q / %v, want %q", key, got, err, want)
	}
	return r.ObjInfo
}

func TestIfMatchBackendWrites(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx := context.Background()
			const bucket = "if-match-backend"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			put := func(t *testing.T, key, body string, opts ObjectOptions) ObjectInfo {
				t.Helper()
				oi, err := obj.PutObject(ctx, bucket, key, ifMatchBackendReader(t, body), opts)
				if err != nil {
					t.Fatal(err)
				}
				return oi
			}
			match := func(etag string) ObjectOptions {
				return ObjectOptions{IfMatch: &ObjectIfMatchCondition{ETag: etag}}
			}

			t.Run("replace-and-stale-validator", func(t *testing.T) {
				old := put(t, "replace", "old", ObjectOptions{})
				fresh := put(t, "replace", "new", match(old.ETag))
				if fresh.ETag == old.ETag {
					t.Fatal("different object bodies retained the same ETag")
				}
				if _, err := obj.PutObject(ctx, bucket, "replace", ifMatchBackendReader(t, "stale"), match(old.ETag)); !isErrPreconditionFailed(err) {
					t.Fatalf("stale validator: %v", err)
				}
				ifMatchBackendContents(t, obj, bucket, "replace", "new")
				if _, err := obj.PutObject(ctx, bucket, "absent", ifMatchBackendReader(t, "unexpected"), match(old.ETag)); !isErrObjectNotFound(err) {
					t.Fatalf("absent target: %v", err)
				}
				if _, err := obj.GetObjectInfo(ctx, bucket, "absent", ObjectOptions{}); !isErrObjectNotFound(err) {
					t.Fatalf("failed conditional PUT created a target: %v", err)
				}
			})

			t.Run("empty-object-and-wildcard", func(t *testing.T) {
				empty := put(t, "empty", "", ObjectOptions{})
				put(t, "empty", "populated", match(empty.ETag))
				put(t, "empty", "wildcard", ObjectOptions{IfMatch: &ObjectIfMatchCondition{Any: true}})
				if _, err := obj.PutObject(ctx, bucket, "wildcard-absent", ifMatchBackendReader(t, "unexpected"), ObjectOptions{IfMatch: &ObjectIfMatchCondition{Any: true}}); !isErrObjectNotFound(err) {
					t.Fatalf("wildcard created absent target: %v", err)
				}
				ifMatchBackendContents(t, obj, bucket, "empty", "wildcard")
			})

			t.Run("concurrent-distinct-writers", func(t *testing.T) {
				old := put(t, "race", "original", ObjectOptions{})
				const writers = 8
				readers := make([]*PutObjReader, writers)
				for i := range readers {
					readers[i] = ifMatchBackendReader(t, fmt.Sprintf("writer-%d", i))
				}
				errs := make([]error, writers)
				var wg sync.WaitGroup
				start := make(chan struct{})
				for i := range writers {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						<-start
						_, errs[i] = obj.PutObject(ctx, bucket, "race", readers[i], match(old.ETag))
					}(i)
				}
				close(start)
				wg.Wait()
				winner := -1
				for i, err := range errs {
					if err == nil {
						if winner != -1 {
							t.Fatalf("writers %d and %d both replaced the same old ETag", winner, i)
						}
						winner = i
					} else if !isErrPreconditionFailed(err) {
						t.Fatalf("writer %d: %v", i, err)
					}
				}
				if winner == -1 {
					t.Fatal("no conditional writer succeeded")
				}
				ifMatchBackendContents(t, obj, bucket, "race", fmt.Sprintf("writer-%d", winner))
			})

			t.Run("multipart-rechecks-target-and-can-retry", func(t *testing.T) {
				const key = "multipart"
				old := put(t, key, "original", ObjectOptions{})
				id, err := obj.NewMultipartUpload(ctx, bucket, key, ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				part, err := obj.PutObjectPart(ctx, bucket, key, id, 1, ifMatchBackendReader(t, "multipart body"), ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				unused, err := obj.PutObjectPart(ctx, bucket, key, id, 2, ifMatchBackendReader(t, "unused part"), ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				parts := []CompletePart{{PartNumber: 1, ETag: part.ETag}}
				fresh := put(t, key, "intervening write", ObjectOptions{})
				if _, err = obj.CompleteMultipartUpload(ctx, bucket, key, id, parts, match(old.ETag)); !isErrPreconditionFailed(err) {
					t.Fatalf("stale multipart completion: %v", err)
				}
				ifMatchBackendContents(t, obj, bucket, key, "intervening write")
				listed, err := obj.ListObjectParts(ctx, bucket, key, id, 0, 100, ObjectOptions{})
				if err != nil {
					t.Fatalf("failed condition destroyed the upload: %v", err)
				}
				if len(listed.Parts) != 2 || listed.Parts[0].ETag != part.ETag || listed.Parts[1].ETag != unused.ETag {
					t.Fatalf("failed condition changed uploaded parts: %+v", listed.Parts)
				}
				if _, err = obj.CompleteMultipartUpload(ctx, bucket, key, id, parts, match(fresh.ETag)); err != nil {
					t.Fatalf("retry with current ETag: %v", err)
				}
				ifMatchBackendContents(t, obj, bucket, key, "multipart body")
			})

			t.Run("multipart-competes-with-conditional-put", func(t *testing.T) {
				const key = "multipart-race"
				old := put(t, key, "original", ObjectOptions{})
				id, err := obj.NewMultipartUpload(ctx, bucket, key, ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				part, err := obj.PutObjectPart(ctx, bucket, key, id, 1, ifMatchBackendReader(t, "multipart winner"), ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				parts := []CompletePart{{PartNumber: 1, ETag: part.ETag}}
				putReader := ifMatchBackendReader(t, "put winner")
				start := make(chan struct{})
				putDone, completeDone := make(chan error, 1), make(chan error, 1)
				go func() {
					<-start
					_, err := obj.PutObject(ctx, bucket, key, putReader, match(old.ETag))
					putDone <- err
				}()
				go func() {
					<-start
					_, err := obj.CompleteMultipartUpload(ctx, bucket, key, id, parts, match(old.ETag))
					completeDone <- err
				}()
				close(start)
				putErr, completeErr := <-putDone, <-completeDone
				switch {
				case putErr == nil && isErrPreconditionFailed(completeErr):
					ifMatchBackendContents(t, obj, bucket, key, "put winner")
					if err := obj.AbortMultipartUpload(ctx, bucket, key, id, ObjectOptions{}); err != nil {
						t.Fatalf("losing multipart upload is no longer recoverable: %v", err)
					}
				case completeErr == nil && isErrPreconditionFailed(putErr):
					ifMatchBackendContents(t, obj, bucket, key, "multipart winner")
				default:
					t.Fatalf("conditional PUT/completion did not serialize: put=%v complete=%v", putErr, completeErr)
				}
			})

			t.Run("multipart-duplicate-completion-preserves-winner", func(t *testing.T) {
				if instance != ErasureTestStr {
					t.Skip("erasure completion consumes shared upload metadata before rename")
				}
				const key = "duplicate-multipart"
				const body = "completed exactly once"
				put(t, key, "existing object", ObjectOptions{})
				id, err := obj.NewMultipartUpload(ctx, bucket, key, ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				part, err := obj.PutObjectPart(ctx, bucket, key, id, 1, ifMatchBackendReader(t, body), ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				parts := []CompletePart{{PartNumber: 1, ETag: part.ETag}}
				type completion struct {
					info ObjectInfo
					err  error
				}
				checked, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
				defer releaseFirst()
				firstDone := make(chan completion, 1)
				go func() {
					info, err := obj.CompleteMultipartUpload(ctx, bucket, key, id, parts, ObjectOptions{IfMatch: &ObjectIfMatchCondition{
						Any: true,
						Authorize: func(ObjectInfo) error {
							close(checked)
							<-release
							return nil
						},
					}})
					firstDone <- completion{info: info, err: err}
				}()
				select {
				case <-checked:
				case result := <-firstDone:
					t.Fatalf("first completion did not reach its locked check: %v", result.err)
				case <-time.After(10 * time.Second):
					t.Fatal("first completion did not reach its locked check")
				}
				secondDone := make(chan completion, 1)
				started := make(chan struct{})
				secondChecked := make(chan struct{}, 1)
				go func() {
					close(started)
					info, err := obj.CompleteMultipartUpload(ctx, bucket, key, id, parts, ObjectOptions{IfMatch: &ObjectIfMatchCondition{
						Any: true,
						Authorize: func(ObjectInfo) error {
							secondChecked <- struct{}{}
							return nil
						},
					}})
					secondDone <- completion{info: info, err: err}
				}()
				<-started
				select {
				case result := <-secondDone:
					releaseFirst()
					<-firstDone
					t.Fatalf("duplicate completion escaped the upload transaction: %v", result.err)
				case <-time.After(100 * time.Millisecond):
				}
				releaseFirst()
				first, second := <-firstDone, <-secondDone
				if first.err != nil {
					t.Fatalf("first completion: %v", first.err)
				}
				if _, ok := second.err.(InvalidUploadID); !ok {
					t.Fatalf("consumed upload must fail with NoSuchUpload, got %T: %v", second.err, second.err)
				}
				select {
				case <-secondChecked:
					t.Fatal("duplicate completion reused the consumed upload metadata")
				default:
				}
				stored := ifMatchBackendContents(t, obj, bucket, key, body)
				if want := getCompleteMultipartMD5(parts); first.info.ETag != want || stored.ETag != want {
					t.Fatalf("duplicate completion damaged the winning validator: first=%s stored=%s want=%s", first.info.ETag, stored.ETag, want)
				}
			})

			t.Run("deleted-target", func(t *testing.T) {
				old := put(t, "deleted", "original", ObjectOptions{})
				if _, err := obj.DeleteObject(ctx, bucket, "deleted", ObjectOptions{}); err != nil {
					t.Fatal(err)
				}
				if _, err := obj.PutObject(ctx, bucket, "deleted", ifMatchBackendReader(t, "unexpected"), match(old.ETag)); !isErrObjectNotFound(err) {
					t.Fatalf("conditional PUT resurrected a deleted target: %v", err)
				}
			})

			if _, ok := obj.(*erasureServerPools); ok {
				t.Run("versioned-current-and-delete-marker", func(t *testing.T) {
					const versionedBucket = "if-match-versions"
					if err := obj.MakeBucketWithLocation(ctx, versionedBucket, BucketOptions{VersioningEnabled: true}); err != nil {
						t.Fatal(err)
					}
					old, err := obj.PutObject(ctx, versionedBucket, "key", ifMatchBackendReader(t, "retained version"), ObjectOptions{Versioned: true})
					if err != nil {
						t.Fatal(err)
					}
					marker, err := obj.DeleteObject(ctx, versionedBucket, "key", ObjectOptions{Versioned: true})
					if err != nil {
						t.Fatal(err)
					}
					opts := match(old.ETag)
					opts.Versioned = true
					if _, err = obj.PutObject(ctx, versionedBucket, "key", ifMatchBackendReader(t, "unexpected"), opts); !isErrObjectNotFound(err) {
						t.Fatalf("matched a hidden historical version: %v", err)
					}
					if _, err = obj.DeleteObject(ctx, versionedBucket, "key", ObjectOptions{Versioned: true, VersionID: marker.VersionID}); err != nil {
						t.Fatal(err)
					}
					if _, err = obj.PutObject(ctx, versionedBucket, "key", ifMatchBackendReader(t, "new current"), opts); err != nil {
						t.Fatal(err)
					}
					ifMatchBackendContents(t, obj, versionedBucket, "key", "new current")
					if _, err = obj.GetObjectInfo(ctx, versionedBucket, "key", ObjectOptions{VersionID: old.VersionID}); err != nil {
						t.Fatalf("conditional replacement removed the old version: %v", err)
					}
				})
			}
		})
	})
}

func TestIfMatchBackendHoldsCommitLock(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, _ TestErrHandler) {
		t.Run(instance, func(t *testing.T) {
			ctx := context.Background()
			const bucket = "if-match-commit-lock"
			if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"put", "delete", "bulk-delete", "metadata-self-copy", "full-self-copy"} {
				t.Run(operation, func(t *testing.T) {
					old, err := obj.PutObject(ctx, bucket, operation, ifMatchBackendReader(t, "old body"), ObjectOptions{})
					if err != nil {
						t.Fatal(err)
					}
					if operation == "bulk-delete" {
						if _, err := obj.PutObject(ctx, bucket, "bulk-delete-other", ifMatchBackendReader(t, "other object"), ObjectOptions{}); err != nil {
							t.Fatal(err)
						}
					}
					copyInfo := old.Clone()
					copyInfo.metadataOnly = operation == "metadata-self-copy"
					copyInfo.PutObjReader = ifMatchBackendReader(t, "old body")
					ordinaryReader := ifMatchBackendReader(t, "ordinary write")
					conditionalReader := ifMatchBackendReader(t, "conditional write")
					checked, release := make(chan struct{}), make(chan struct{})
					conditionalDone := make(chan error, 1)
					go func() {
						_, err := obj.PutObject(ctx, bucket, operation, conditionalReader, ObjectOptions{IfMatch: &ObjectIfMatchCondition{
							ETag: old.ETag,
							Authorize: func(ObjectInfo) error {
								close(checked)
								<-release
								return nil
							},
						}})
						conditionalDone <- err
					}()
					select {
					case <-checked:
					case err := <-conditionalDone:
						t.Fatalf("conditional writer did not reach its locked check: %v", err)
					case <-time.After(10 * time.Second):
						close(release)
						t.Fatal("conditional writer did not reach its locked check")
					}
					competingDone := make(chan error, 1)
					started := make(chan struct{})
					go func() {
						close(started)
						var err error
						switch operation {
						case "put":
							_, err = obj.PutObject(ctx, bucket, operation, ordinaryReader, ObjectOptions{})
						case "delete":
							_, err = obj.DeleteObject(ctx, bucket, operation, ObjectOptions{})
						case "bulk-delete":
							_, errs := obj.DeleteObjects(ctx, bucket, []ObjectToDelete{{ObjectName: operation}, {ObjectName: "bulk-delete-other"}}, ObjectOptions{})
							err = errs[0]
							if err == nil {
								err = errs[1]
							}
						default:
							_, err = obj.CopyObject(ctx, bucket, operation, bucket, operation, copyInfo, ObjectOptions{}, ObjectOptions{})
						}
						competingDone <- err
					}()
					<-started
					var early bool
					var competingErr error
					select {
					case competingErr = <-competingDone:
						early = true
					case <-time.After(100 * time.Millisecond):
					}
					close(release)
					if err := <-conditionalDone; err != nil {
						t.Fatalf("conditional write: %v", err)
					}
					if !early {
						competingErr = <-competingDone
					}
					if early {
						t.Fatalf("%s escaped the conditional commit lock: %v", operation, competingErr)
					}
					switch operation {
					case "metadata-self-copy", "full-self-copy":
						if !isErrPreconditionFailed(competingErr) {
							t.Fatalf("copy accepted the source snapshot replaced by CAS: %v", competingErr)
						}
						ifMatchBackendContents(t, obj, bucket, operation, "conditional write")
					case "put":
						if competingErr != nil {
							t.Fatal(competingErr)
						}
						ifMatchBackendContents(t, obj, bucket, operation, "ordinary write")
					default:
						if competingErr != nil {
							t.Fatal(competingErr)
						}
						if _, err := obj.GetObjectInfo(ctx, bucket, operation, ObjectOptions{}); !isErrObjectNotFound(err) {
							t.Fatalf("delete did not follow conditional commit: %v", err)
						}
					}
				})
			}
		})
	})
}
