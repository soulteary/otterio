// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

func TestXLStorageInlineReplacementRealErasure(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, disks, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(ctx)
	defer removeRoots(disks)
	const bucket = "inline-replacement"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, named := range []bool{false, true} {
		name := "null"
		opts := ObjectOptions{}
		if named {
			name = "named"
			opts.Versioned, opts.VersionID = true, mustGetUUID()
		}
		t.Run(name, func(t *testing.T) {
			for step, payload := range [][]byte{
				[]byte(`{"schema":1,"jobs":{}}`),
				bytes.Repeat([]byte("large replacement."), 150000),
				[]byte("small again"),
				{},
				bytes.Repeat([]byte("different replacement."), 130000),
			} {
				if _, err = obj.PutObject(ctx, bucket, name, mustGetPutObjReader(t, bytes.NewReader(payload), int64(len(payload)), "", ""), opts); err != nil {
					t.Fatal(err)
				}
				reader, readErr := obj.GetObjectNInfo(ctx, bucket, name, nil, http.Header{}, readLock, ObjectOptions{VersionID: opts.VersionID})
				if readErr != nil {
					t.Fatalf("step %d open after inline change: %v", step, readErr)
				}
				got, readErr := io.ReadAll(reader)
				reader.Close()
				if readErr != nil || !bytes.Equal(got, payload) {
					t.Fatalf("step %d retained stale inline bytes: got=%d want=%d err=%v", step, len(got), len(payload), readErr)
				}
				if len(payload) > 1<<20 {
					for _, disk := range obj.(*erasureServerPools).serverPools[0].sets[0].getDisks() {
						fi, metadataErr := disk.ReadVersion(ctx, bucket, name, opts.VersionID, true)
						if metadataErr != nil || len(fi.Data) != 0 {
							t.Fatalf("step %d noninline object retained inline metadata: size=%d data=%d err=%v", step, fi.Size, len(fi.Data), metadataErr)
						}
					}
				}
			}
		})
	}
}

func TestXLStorageInlineMetadataPreservesData(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, disks, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(ctx)
	defer removeRoots(disks)
	const bucket, key = "inline-metadata", "small"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("inline source stays readable during pending metadata writes")
	if _, err = obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	ref := transitionStorageReference()
	intent, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, disk := range obj.(*erasureServerPools).serverPools[0].sets[0].getDisks() {
		fi, readErr := disk.ReadVersion(ctx, bucket, key, "", false)
		if readErr != nil || len(fi.Data) != 0 {
			t.Fatalf("metadata-only fixture unexpectedly contains data: %v", readErr)
		}
		fi.TransitionStatus = lifecycle.TransitionPending
		fi.Metadata[ReservedMetadataPrefixLower+"transition-status"] = lifecycle.TransitionPending
		fi.Metadata[transitionDeleteIntentKey] = string(intent)
		if err = setTransitionedObject(fi.Metadata, ref); err != nil {
			t.Fatal(err)
		}
		if err = disk.WriteMetadata(ctx, bucket, key, fi); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := obj.GetObjectNInfo(ctx, bucket, key, nil, http.Header{}, readLock, ObjectOptions{TransitionStatus: lifecycle.TransitionPending})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("metadata-only write discarded inline payload: got=%q err=%v", got, err)
	}
}
