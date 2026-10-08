// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

func TestErasurePutObjectOverwritesInconsistentLocalMetadata(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, roots, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer removeRoots(roots)
	defer obj.Shutdown(ctx)
	const bucket = "overwrite-local"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	disks := obj.(*erasureServerPools).serverPools[0].sets[0].getDisks()
	for _, target := range erasureOverwriteTargets() {
		for _, split := range []string{"modtime", "metadata"} {
			t.Run(target.name+"/"+split, func(t *testing.T) {
				key := target.name + "/" + split
				original := []byte("old inline object")
				if _, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(original), int64(len(original)), "", ""), target.opts); err != nil {
					t.Fatal(err)
				}
				metadata, readErrs := readAllFileInfo(ctx, disks, bucket, key, target.opts.VersionID, true)
				for i := range metadata {
					if readErrs[i] != nil {
						t.Fatal(readErrs[i])
					}
					if split == "modtime" {
						metadata[i].ModTime = metadata[i].ModTime.Add(time.Duration(i) * time.Second)
					} else {
						metadata[i].Metadata["split"] = fmt.Sprint(i)
					}
				}
				if _, err := writeUniqueFileInfo(ctx, disks, bucket, key, metadata, len(disks)); err != nil {
					t.Fatal(err)
				}
				if _, err := obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: target.opts.VersionID}); !errors.Is(err, errErasureReadQuorum) {
					t.Fatalf("split fixture must lack read quorum: %v", err)
				}

				replacement := []byte("replacement succeeds at write quorum")
				if _, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(replacement), int64(len(replacement)), "", ""), target.opts); err != nil {
					t.Fatalf("healthy writes required the old metadata's read quorum: %v", err)
				}
				reader, err := obj.GetObjectNInfo(ctx, bucket, key, nil, http.Header{}, readLock, ObjectOptions{VersionID: target.opts.VersionID})
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(reader)
				reader.Close()
				if err != nil || !bytes.Equal(got, replacement) {
					t.Fatalf("replacement was not readable: %q / %v", got, err)
				}
			})
		}
	}
}

func TestErasurePutObjectPreservesPartialTransitionMetadata(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, roots, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer removeRoots(roots)
	defer obj.Shutdown(ctx)
	const bucket = "overwrite-transition"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	disks := obj.(*erasureServerPools).serverPools[0].sets[0].getDisks()
	for _, target := range erasureOverwriteTargets() {
		for _, status := range []string{lifecycle.TransitionPending, lifecycle.TransitionComplete} {
			for _, references := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/%s/%d-disks", target.name, status, references), func(t *testing.T) {
					key := fmt.Sprintf("%s/%s/%d", target.name, status, references)
					original := []byte("retain the transition's recovery record")
					if _, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(original), int64(len(original)), "", ""), target.opts); err != nil {
						t.Fatal(err)
					}
					metadata, readErrs := readAllFileInfo(ctx, disks, bucket, key, target.opts.VersionID, true)
					for i := range metadata {
						if readErrs[i] != nil {
							t.Fatal(readErrs[i])
						}
						// No snapshot reaches read quorum, including the disks that
						// retain the only reference to the remote destination.
						metadata[i].Metadata["split"] = fmt.Sprint(i)
						if i < references {
							metadata[i].TransitionStatus = status
							metadata[i].Metadata[ReservedMetadataPrefixLower+"transition-status"] = status
							if err := setTransitionedObject(metadata[i].Metadata, transitionStorageReference()); err != nil {
								t.Fatal(err)
							}
						}
					}
					if _, err := writeUniqueFileInfo(ctx, disks, bucket, key, metadata, len(disks)); err != nil {
						t.Fatal(err)
					}
					if _, err := obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: target.opts.VersionID}); !errors.Is(err, errErasureReadQuorum) {
						t.Fatalf("partial transition fixture must lack read quorum: %v", err)
					}
					before := make([][]byte, len(disks))
					for i, disk := range disks {
						before[i], err = disk.ReadAll(ctx, bucket, pathJoin(key, xlStorageFormatFile))
						if err != nil {
							t.Fatal(err)
						}
					}

					replacement := []byte("must not overwrite the remote reference")
					_, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(replacement), int64(len(replacement)), "", ""), target.opts)
					var blocked NotImplemented
					if !errors.As(err, &blocked) {
						t.Fatalf("partial transition overwrite must be refused: %v", err)
					}
					for i, disk := range disks {
						after, err := disk.ReadAll(ctx, bucket, pathJoin(key, xlStorageFormatFile))
						if err != nil || !bytes.Equal(before[i], after) {
							t.Fatalf("refused overwrite changed disk %d metadata: %v", i, err)
						}
					}
				})
			}
		}
	}
}

func erasureOverwriteTargets() []struct {
	name string
	opts ObjectOptions
} {
	return []struct {
		name string
		opts ObjectOptions
	}{
		{name: "null"},
		{name: "suspended-null", opts: ObjectOptions{VersionSuspended: true}},
		{name: "named", opts: ObjectOptions{Versioned: true, VersionID: mustGetUUID()}},
	}
}
