// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/madmin"
)

type transitionIntentFaultDisk struct {
	StorageAPI
	key        string
	failIntent bool
}

func (d transitionIntentFaultDisk) UpdateMetadata(ctx context.Context, bucket, key string, fi FileInfo) error {
	if d.failIntent && key == d.key && fi.Metadata[transitionDeleteIntentKey] != "" {
		return errFaultyDisk
	}
	return d.StorageAPI.UpdateMetadata(ctx, bucket, key, fi)
}

func (d transitionIntentFaultDisk) DeleteVersion(ctx context.Context, bucket, key string, fi FileInfo, force bool) error {
	if !d.failIntent && key == d.key && fi.TransitionStatus == "" {
		return errFaultyDisk
	}
	return d.StorageAPI.DeleteVersion(ctx, bucket, key, fi, force)
}

func (d transitionIntentFaultDisk) DeleteVersions(ctx context.Context, bucket string, versions []FileInfo) []error {
	errs := make([]error, len(versions))
	for i, fi := range versions {
		errs[i] = d.DeleteVersion(ctx, bucket, fi.Name, fi, false)
	}
	return errs
}

type transitionIntentFaultLayer struct {
	ObjectLayer
	backend *erasureObjects
}

func (o transitionIntentFaultLayer) PutObjectMetadata(ctx context.Context, bucket, key string, opts ObjectOptions) (ObjectInfo, error) {
	return o.backend.PutObjectMetadata(ctx, bucket, key, opts)
}

func (o transitionIntentFaultLayer) DeleteObject(ctx context.Context, bucket, key string, opts ObjectOptions) (ObjectInfo, error) {
	return o.backend.DeleteObject(ctx, bucket, key, opts)
}

func (o transitionIntentFaultLayer) DeleteObjects(ctx context.Context, bucket string, objects []ObjectToDelete, opts ObjectOptions) ([]DeletedObject, []error) {
	names := make([]string, len(objects))
	for i, object := range objects {
		names[i] = encodeDirObject(object.ObjectName)
	}
	lock := o.NewNSLock(bucket, names...)
	ctx, err := lock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		errs := make([]error, len(objects))
		for i := range errs {
			errs[i] = err
		}
		return nil, errs
	}
	defer lock.Unlock()
	return o.backend.DeleteObjects(ctx, bucket, objects, opts)
}

func TestTransitionDeletionIntentRealErasureRecovery(t *testing.T) {
	for _, mode := range []string{"expiry-named", "expiry-null", "bulk-named"} {
		t.Run(mode, func(t *testing.T) {
			obj, targets, bucket := targetTestSetup(t)
			ctx := context.Background()
			var deletes atomic.Int32
			var deleted atomic.Bool
			const key = "old"
			data := []byte("small inline authority before an irreversible delete")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodDelete:
					deletes.Add(1)
					deleted.Store(true)
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodGet && r.URL.Query().Has("versioning"):
					w.Write([]byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`))
				case r.Method == http.MethodHead:
					if strings.HasSuffix(r.URL.Path, "/remote-key") {
						if deleted.Load() {
							w.Header().Set("X-Minio-Error-Code", "NoSuchVersion")
							w.WriteHeader(http.StatusNotFound)
							return
						}
						w.Header().Set("Content-Length", strconv.Itoa(len(data)))
						w.Header().Set("X-Amz-Meta-Otterio-Transition-Id", "remote-key")
						w.Header().Set("X-Amz-Version-Id", "remote-version")
						w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
						w.Header().Set("ETag", `"remote-etag"`)
					}
					w.WriteHeader(http.StatusOK)
				default:
					w.WriteHeader(http.StatusNotImplemented)
				}
			}))
			t.Cleanup(server.Close)
			target := madmin.BucketTarget{Endpoint: strings.TrimPrefix(server.URL, "http://"), TargetBucket: "destination", Label: "Delete", Type: madmin.ILMService, Region: "us-east-1", Credentials: &auth.Credentials{AccessKey: globalActiveCred.AccessKey, SecretKey: globalActiveCred.SecretKey}}
			if err := targets.SetTarget(ctx, bucket, &target, false); err != nil {
				t.Fatal(err)
			}
			if mode != "expiry-null" {
				if err := globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, enabledBucketVersioningConfig); err != nil {
					t.Fatal(err)
				}
			}
			opts := ObjectOptions{Versioned: mode != "expiry-null", MTime: time.Now().Add(-30 * 24 * time.Hour)}
			oi, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), opts)
			if err != nil {
				t.Fatal(err)
			}
			ref := &TransitionedObject{ARN: target.Arn, Key: "remote-key", VersionID: "remote-version", StorageClass: "Delete"}
			if _, err = obj.DeleteObject(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID, TransitionStatus: lifecycle.TransitionPending, TransitionExpected: &oi, TransitionedObject: ref}); err != nil {
				t.Fatal(err)
			}
			// Failed intent persistence must keep the inline source intact and
			// must not dispatch even one irreversible remote DELETE.
			original := obj.(*erasureServerPools).serverPools[0].sets[0]
			faulty := *original
			wrap := func(failIntent bool) {
				disks := original.getDisks()
				wrapped := append([]StorageAPI(nil), disks...)
				for i := 0; i < 3; i++ {
					wrapped[i] = transitionIntentFaultDisk{StorageAPI: disks[i], key: key, failIntent: failIntent}
				}
				faulty.getDisks = func() []StorageAPI { return wrapped }
			}
			wrap(true)
			faultLayer := transitionIntentFaultLayer{ObjectLayer: obj, backend: &faulty}
			setObjectLayer(faultLayer)
			short, cancel := context.WithTimeout(ctx, 3*time.Second)
			_, err = faultLayer.DeleteObject(short, bucket, key, ObjectOptions{VersionID: oi.VersionID})
			cancel()
			if _, failedQuorum := err.(InsufficientWriteQuorum); !failedQuorum || deletes.Load() != 0 {
				t.Fatalf("intent quorum failed after remote mutation: err=%v deletes=%d", err, deletes.Load())
			}
			reader, err := obj.GetObjectNInfo(ctx, bucket, key, nil, http.Header{}, readLock, ObjectOptions{TransitionStatus: lifecycle.TransitionPending, VersionID: oi.VersionID})
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("failed intent quorum lost inline source: %q / %v", got, err)
			}
			setObjectLayer(obj)
			// Commit complete, demote the source, and queue an exact expiry.
			if _, err = obj.DeleteObject(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID, TransitionStatus: lifecycle.TransitionComplete, TransitionExpected: &oi, TransitionedObject: ref}); err != nil {
				t.Fatal(err)
			}
			if err = globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, enabledBucketVersioningConfig); err != nil {
				t.Fatal(err)
			}
			newData := []byte("newer version must survive")
			newer, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(newData), int64(len(newData)), "", ""), ObjectOptions{Versioned: true, MTime: time.Now().Add(-10 * 24 * time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			version := oi.VersionID
			if version == "" {
				version = nullVersionID
			}
			fresh, err := obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: version})
			if err != nil {
				t.Fatal(err)
			}
			if err = globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, []byte(`<LifecycleConfiguration><Rule><ID>expire-old</ID><Status>Enabled</Status><Filter><Prefix/></Filter><NoncurrentVersionExpiration><NoncurrentDays>1</NoncurrentDays></NoncurrentVersionExpiration></Rule></LifecycleConfiguration>`)); err != nil {
				t.Fatal(err)
			}
			wrap(false)
			setObjectLayer(faultLayer)
			short, cancel = context.WithTimeout(ctx, 3*time.Second)
			if mode == "bulk-named" {
				_, errs := faultLayer.DeleteObjects(short, bucket, []ObjectToDelete{{ObjectName: key, VersionID: version}}, ObjectOptions{Versioned: true})
				err = errs[0]
			} else {
				if applyExpiryRule(short, faultLayer, fresh, false, true) {
					t.Fatal("failed local deletion quorum reported success")
				}
				err = nil
			}
			cancel()
			if mode == "bulk-named" && err == nil {
				t.Fatal("failed bulk deletion quorum reported success")
			}
			if deletes.Load() != 1 || !deleted.Load() {
				t.Fatalf("real remote DELETE was not acknowledged exactly once: %d", deletes.Load())
			}
			setObjectLayer(obj)
			fresh, err = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: version})
			if err != nil {
				t.Fatal(err)
			}
			if pending, perr := transitionDeletionPending(fresh); !pending || perr != nil {
				t.Fatalf("failed local commit lost durable deletion intent: %+v / %v", fresh, perr)
			}
			// Metadata REPLACE on the same version must preserve the operation.
			copyInfo := fresh.Clone()
			copyInfo.metadataOnly = true
			copyInfo.UserDefined = map[string]string{"description": "replacement metadata"}
			copied, err := obj.CopyObject(ctx, bucket, key, bucket, key, copyInfo, ObjectOptions{VersionID: version}, ObjectOptions{Versioned: true, VersionID: version})
			if err != nil {
				t.Fatal(err)
			}
			if pending, perr := transitionDeletionPending(copied); !pending || perr != nil {
				t.Fatalf("metadata replacement dropped delete intent: %v", perr)
			}
			if err = globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, nil); err != nil {
				t.Fatal(err)
			}
			savedTargets := globalBucketTargetSys
			globalBucketTargetSys = NewBucketTargetSys()
			defer func() { globalBucketTargetSys = savedTargets }()
			fresh, err = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: version})
			if err != nil {
				t.Fatal(err)
			}
			savedExpiry := globalExpiryState
			globalExpiryState = &expiryState{expiryCh: make(chan expiryTask, 1)}
			item := scannerItem{bucket: bucket, objectName: key}
			applied, size := item.applyLifecycle(ctx, obj, actionMeta{oi: fresh})
			var task expiryTask
			select {
			case task = <-globalExpiryState.expiryCh:
			default:
				globalExpiryState = savedExpiry
				t.Fatal("restart scanner stranded durable deletion after rule removal")
			}
			globalExpiryState = savedExpiry
			if !applied || size != fresh.Size || !task.versionExpiry || task.objInfo.VersionID != fresh.VersionID {
				t.Fatalf("restart scanner queued wrong permanent identity/accounting: applied=%v size=%d task=%+v", applied, size, task)
			}
			if !applyExpiryRule(ctx, obj, task.objInfo, false, task.versionExpiry) {
				t.Fatal("durable deletion did not finish without its removed rule")
			}
			if _, err = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: version}); !isErrVersionNotFound(err) {
				t.Fatalf("exact deleted version survived recovery: %v", err)
			}
			if _, err = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: newer.VersionID}); err != nil {
				t.Fatalf("recovery deleted newer source version: %v", err)
			}
		})
	}
}
