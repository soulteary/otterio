// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/madmin"
)

type overwriteReadErrorDisk struct {
	StorageAPI
	bucket, key string
	readFailed  atomic.Bool
	renamed     atomic.Bool
}

func (d *overwriteReadErrorDisk) ReadVersion(ctx context.Context, bucket, key, versionID string, readData bool) (FileInfo, error) {
	if bucket == d.bucket && key == d.key && d.readFailed.CompareAndSwap(false, true) {
		return FileInfo{}, errFaultyDisk
	}
	return d.StorageAPI.ReadVersion(ctx, bucket, key, versionID, readData)
}

func (d *overwriteReadErrorDisk) RenameData(ctx context.Context, srcVolume, srcPath string, fi FileInfo, dstVolume, dstPath string) error {
	if dstVolume == d.bucket && dstPath == d.key {
		d.renamed.Store(true)
	}
	return d.StorageAPI.RenameData(ctx, srcVolume, srcPath, fi, dstVolume, dstPath)
}

func TestErasurePutObjectReadErrorPreservesTransitionReference(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, roots, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer removeRoots(roots)
	defer obj.Shutdown(ctx)
	const bucket = "overwrite-read-error"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	backend := obj.(*erasureServerPools).serverPools[0].sets[0]
	disks := backend.getDisks()
	for _, target := range []struct {
		key  string
		opts ObjectOptions
	}{
		{key: "null"},
		{key: "suspended-null", opts: ObjectOptions{VersionSuspended: true}},
		{key: "named", opts: ObjectOptions{Versioned: true, VersionID: mustGetUUID()}},
	} {
		t.Run(target.key, func(t *testing.T) {
			original := []byte("source with one durable remote recovery record")
			if _, err := backend.PutObject(ctx, bucket, target.key, mustGetPutObjReader(t, bytes.NewReader(original), int64(len(original)), "", ""), target.opts); err != nil {
				t.Fatal(err)
			}
			metadata, readErrs := readAllFileInfo(ctx, disks, bucket, target.key, target.opts.VersionID, true)
			for i := range metadata {
				if readErrs[i] != nil {
					t.Fatal(readErrs[i])
				}
				metadata[i].Metadata["split"] = fmt.Sprint(i)
			}
			metadata[0].TransitionStatus = lifecycle.TransitionPending
			metadata[0].Metadata[ReservedMetadataPrefixLower+"transition-status"] = lifecycle.TransitionPending
			if err := setTransitionedObject(metadata[0].Metadata, transitionStorageReference()); err != nil {
				t.Fatal(err)
			}
			if _, err := writeUniqueFileInfo(ctx, disks, bucket, target.key, metadata, len(disks)); err != nil {
				t.Fatal(err)
			}
			if _, err := backend.getObjectInfo(ctx, bucket, target.key, ObjectOptions{VersionID: target.opts.VersionID}); !errors.Is(err, errErasureReadQuorum) {
				t.Fatalf("fixture must lack an old metadata read quorum: %v", err)
			}
			before, err := disks[0].ReadAll(ctx, bucket, pathJoin(target.key, xlStorageFormatFile))
			if err != nil {
				t.Fatal(err)
			}
			transient := &overwriteReadErrorDisk{StorageAPI: disks[0], bucket: bucket, key: target.key}
			wrapped := append([]StorageAPI(nil), disks...)
			wrapped[0] = transient
			faulty := *backend
			faulty.getDisks = func() []StorageAPI { return wrapped }
			// Exercise healing explicitly after checking the upload's disk state.
			faulty.mrfOpCh = nil
			replacement := []byte("replacement on the three ordinary disks")
			if _, err := faulty.PutObject(ctx, bucket, target.key, mustGetPutObjReader(t, bytes.NewReader(replacement), int64(len(replacement)), "", ""), target.opts); err != nil {
				t.Fatalf("three ordinary disks must still satisfy write quorum: %v", err)
			}
			if !transient.readFailed.Load() || !transient.renamed.Load() {
				t.Fatal("fixture must fail the safety read and then attempt a recovered rename")
			}
			assertReferenceUnchanged := func() {
				t.Helper()
				after, readErr := disks[0].ReadAll(ctx, bucket, pathJoin(target.key, xlStorageFormatFile))
				if readErr != nil || !bytes.Equal(before, after) {
					t.Fatalf("the only transition recovery metadata was overwritten: %v", readErr)
				}
			}
			assertReferenceUnchanged()
			if _, err := backend.HealObject(ctx, bucket, target.key, target.opts.VersionID, madmin.HealOpts{}); err == nil {
				t.Fatal("healing must refuse to discard the retained remote recovery record")
			}
			assertReferenceUnchanged()
		})
	}
}

func TestXLStorageHealsPendingTransitionMetadata(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, roots, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer removeRoots(roots)
	defer obj.Shutdown(ctx)
	const bucket, key = "heal-pending-transition", "source"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	backend := obj.(*erasureServerPools).serverPools[0].sets[0]
	disks := backend.getDisks()
	original := bytes.Repeat([]byte("pending transition source."), 1<<16)
	if _, err = backend.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(original), int64(len(original)), "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	metadata, readErrs := readAllFileInfo(ctx, disks, bucket, key, "", false)
	for i := range metadata {
		if readErrs[i] != nil {
			t.Fatal(readErrs[i])
		}
		metadata[i].TransitionStatus = lifecycle.TransitionPending
		metadata[i].Metadata[ReservedMetadataPrefixLower+"transition-status"] = lifecycle.TransitionPending
		if err := setTransitionedObject(metadata[i].Metadata, transitionStorageReference()); err != nil {
			t.Fatal(err)
		}
		metadata[i].Metadata[transitionDeleteIntentKey] = metadata[i].Metadata[transitionReferenceKey]
	}
	if _, err := writeUniqueFileInfo(ctx, disks, bucket, key, metadata, len(disks)); err != nil {
		t.Fatal(err)
	}
	if err := disks[0].Delete(ctx, bucket, pathJoin(key, metadata[0].DataDir, "part.1"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.HealObject(ctx, bucket, key, "", madmin.HealOpts{ScanMode: madmin.HealNormalScan}); err != nil {
		t.Fatalf("same-source healing must preserve the transition and repair its shard: %v", err)
	}
	for i, disk := range disks {
		fi, readErr := disk.ReadVersion(ctx, bucket, key, "", false)
		if readErr != nil || fi.TransitionStatus != lifecycle.TransitionPending ||
			fi.Metadata[transitionReferenceKey] != metadata[i].Metadata[transitionReferenceKey] ||
			fi.Metadata[transitionDeleteIntentKey] != metadata[i].Metadata[transitionDeleteIntentKey] {
			t.Fatalf("healing lost disk %d's transition state, destination or intent: %+v / %v", i, fi, readErr)
		}
	}
	var got bytes.Buffer
	if err := backend.getObject(ctx, bucket, key, 0, int64(len(original)), &got, ObjectOptions{}); err != nil || !bytes.Equal(got.Bytes(), original) {
		t.Fatalf("healing changed the source bytes: %d / %v", got.Len(), err)
	}
}
