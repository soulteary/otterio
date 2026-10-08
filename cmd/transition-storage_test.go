package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/soulteary/otterio-kits/sio"
	"github.com/soulteary/otterio/cmd/config/storageclass"
	"github.com/soulteary/otterio/cmd/crypto"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/hash"
	"github.com/soulteary/otterio/pkg/madmin"
)

func transitionStorageFixture() FileInfo {
	fi := newFileInfo("object", 2, 2)
	fi.Volume, fi.Name = "bucket", "object"
	fi.VersionID, fi.DataDir = mustGetUUID(), mustGetUUID()
	fi.ModTime, fi.Size, fi.Erasure.Index = time.Unix(1700000000, 0).UTC(), 16, 1
	fi.Metadata = map[string]string{"etag": "source-etag", "content-type": "application/octet-stream"}
	fi.Parts = []ObjectPartInfo{{Number: 1, Size: 8, ActualSize: 8}, {Number: 2, Size: 8, ActualSize: 8}}
	return fi
}

func transitionStorageReference() *TransitionedObject {
	return &TransitionedObject{ARN: "arn:otterio:ilm::storage-test:archive", Key: "unique/object", VersionID: "remote-version", StorageClass: "ARCHIVE"}
}

func TestTransitionStorageMetadataRoundTrip(t *testing.T) {
	fi := transitionStorageFixture()
	var meta xlMetaV2
	if err := meta.AddVersion(fi); err != nil {
		t.Fatal(err)
	}
	ref := transitionStorageReference()
	delta := FileInfo{VersionID: fi.VersionID, TransitionStatus: lifecycle.TransitionPending, Metadata: make(map[string]string)}
	if err := setTransitionedObject(delta.Metadata, ref); err != nil {
		t.Fatal(err)
	}
	if _, _, err := meta.DeleteVersion(delta); err != nil {
		t.Fatal(err)
	}
	encoded, err := meta.AppendTo(nil)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded xlMetaV2
	if err = reloaded.Load(encoded); err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.ToFileInfo(fi.Volume, fi.Name, fi.VersionID)
	if err != nil || got.TransitionStatus != lifecycle.TransitionPending || !reflect.DeepEqual(parseTransitionedObject(got.Metadata), ref) {
		t.Fatalf("pending destination did not survive reload: %+v / %v", got, err)
	}
	// Adding a later version and changing rules never rewrites the old locator.
	newer := fi
	newer.VersionID, newer.DataDir, newer.ModTime = mustGetUUID(), mustGetUUID(), fi.ModTime.Add(time.Hour)
	if err = reloaded.AddVersion(newer); err != nil {
		t.Fatal(err)
	}
	delta.TransitionStatus = lifecycle.TransitionComplete
	ref.VersionID = "actual-remote-version"
	if err = setTransitionedObject(delta.Metadata, ref); err != nil {
		t.Fatal(err)
	}
	if _, _, err = reloaded.DeleteVersion(delta); err != nil {
		t.Fatal(err)
	}
	got, err = reloaded.ToFileInfo(fi.Volume, fi.Name, fi.VersionID)
	if err != nil || got.IsLatest || got.TransitionStatus != lifecycle.TransitionComplete || !reflect.DeepEqual(parseTransitionedObject(got.Metadata), ref) {
		t.Fatalf("noncurrent version lost its destination: %+v / %v", got, err)
	}
	clone := got.ToObjectInfo(fi.Volume, fi.Name).Clone()
	clone.TransitionedObject.Key = "changed"
	if parseTransitionedObject(got.Metadata).Key != ref.Key {
		t.Fatal("ObjectInfo.Clone shared its remote identity")
	}
}

func TestTransitionStorageClassFromPersistedReference(t *testing.T) {
	ref := transitionStorageReference()
	encoded, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	withoutClass := *ref
	withoutClass.StorageClass = ""
	emptyClass, err := json.Marshal(withoutClass)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		status    string
		reference string
		deleted   bool
		want      string
	}{
		{name: "complete", status: lifecycle.TransitionComplete, reference: string(encoded), want: "ARCHIVE"},
		{name: "pending", status: lifecycle.TransitionPending, reference: string(encoded), want: "STANDARD"},
		{name: "ordinary", reference: string(encoded), want: "STANDARD"},
		{name: "legacy", status: lifecycle.TransitionComplete, want: "STANDARD"},
		{name: "invalid", status: lifecycle.TransitionComplete, reference: `{"arn":"invalid","key":"unique/object","storageClass":"ARCHIVE"}`, want: "STANDARD"},
		{name: "malformed", status: lifecycle.TransitionComplete, reference: `{"storageClass":"ARCHIVE"`, want: "STANDARD"},
		{name: "empty-class", status: lifecycle.TransitionComplete, reference: string(emptyClass), want: "STANDARD"},
		{name: "delete-marker", status: lifecycle.TransitionComplete, reference: string(encoded), deleted: true, want: "STANDARD"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fi := transitionStorageFixture()
			fi.Metadata[xhttp.AmzStorageClass] = "STANDARD"
			fi.Metadata[transitionReferenceKey] = test.reference
			fi.TransitionStatus, fi.Deleted = test.status, test.deleted
			if got := fi.ToObjectInfo(fi.Volume, fi.Name).StorageClass; got != test.want {
				t.Fatalf("storage class from persisted metadata = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTransitionStorageLegacyAndSharedData(t *testing.T) {
	fi := transitionStorageFixture()
	legacy := &xlMetaV1Object{Version: xlMetaVersion101, Format: xlMetaFormat, Stat: StatInfo{Size: fi.Size, ModTime: fi.ModTime}, Erasure: fi.Erasure, Meta: fi.Metadata, Parts: fi.Parts}
	var meta xlMetaV2
	if err := meta.AddLegacy(legacy); err != nil {
		t.Fatal(err)
	}
	delta := FileInfo{VersionID: nullVersionID, TransitionStatus: lifecycle.TransitionPending, Metadata: make(map[string]string)}
	if err := setTransitionedObject(delta.Metadata, transitionStorageReference()); err != nil {
		t.Fatal(err)
	}
	dir, _, err := meta.DeleteVersion(delta)
	if err != nil || dir != legacyDataDir {
		t.Fatalf("legacy pending failed: %q / %v", dir, err)
	}
	if parseTransitionedObject(meta.Versions[0].ObjectV1.Meta) == nil {
		t.Fatal("legacy destination was not saved")
	}
	meta = xlMetaV2{}
	if err = meta.AddVersion(fi); err != nil {
		t.Fatal(err)
	}
	other := fi
	other.VersionID, other.ModTime = mustGetUUID(), fi.ModTime.Add(time.Hour)
	if err = meta.AddVersion(other); err != nil {
		t.Fatal(err)
	}
	delta.VersionID, delta.TransitionStatus = fi.VersionID, lifecycle.TransitionComplete
	dir, _, err = meta.DeleteVersion(delta)
	if err != nil || dir != "" {
		t.Fatalf("transition removed data shared with a local version: %q / %v", dir, err)
	}
	delta.VersionID = other.VersionID
	dir, _, err = meta.DeleteVersion(delta)
	if err != nil || dir != fi.DataDir {
		t.Fatalf("last local reference did not release shared data: %q / %v", dir, err)
	}
}

func TestTransitionStorageQuorumAndDanglingProtection(t *testing.T) {
	old := transitionStorageFixture()
	pending := old
	pending.Metadata = cloneMSS(old.Metadata)
	pending.TransitionStatus = lifecycle.TransitionPending
	if err := setTransitionedObject(pending.Metadata, transitionStorageReference()); err != nil {
		t.Fatal(err)
	}
	got, err := pickValidFileInfo(context.Background(), []FileInfo{old, pending, pending, pending}, old.ModTime, old.DataDir, 3)
	if err != nil || got.TransitionStatus != lifecycle.TransitionPending {
		t.Fatalf("a stale first disk won over the destination quorum: %+v / %v", got, err)
	}
	if _, err = pickValidFileInfo(context.Background(), []FileInfo{old, old, pending, pending}, old.ModTime, old.DataDir, 3); err == nil {
		t.Fatal("conflicting destination snapshots were accepted")
	}
	if _, dangling := isObjectDangling([]FileInfo{old, pending, {}, {}}, []error{nil, nil, errFileNotFound, errFileNotFound}, []error{errFileNotFound, errFileNotFound, errFileNotFound, errFileNotFound}); dangling {
		t.Fatal("partial transition metadata was treated as disposable")
	}
	complete := pending
	complete.TransitionStatus = lifecycle.TransitionComplete
	if transitionHasLocalData(complete) {
		t.Fatal("completed remote object incorrectly needs local shards")
	}
	complete.Metadata[xhttp.AmzRestore] = fmt.Sprintf("ongoing-request=false, expiry-date=%s", UTCNow().Add(time.Hour).Format(http.TimeFormat))
	if !transitionHasLocalData(complete) {
		t.Fatal("an unexpired restored copy was ignored")
	}
}

func TestTransitionStorageInlineCleanupQuorum(t *testing.T) {
	base := newFileInfo("object", 6, 2)
	base.Volume, base.Name = "bucket", "object"
	base.ModTime, base.DataDir = time.Unix(1700000000, 0).UTC(), mustGetUUID()
	base.Size = 16
	base.Parts = []ObjectPartInfo{{Number: 1, Size: 16, ActualSize: 16}}
	base.Metadata = map[string]string{"etag": "same-source"}
	if err := setTransitionedObject(base.Metadata, transitionStorageReference()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		status     string
		restore    string
		conflict   bool
		wantQuorum bool
	}{
		{name: "remote-only", status: lifecycle.TransitionComplete, wantQuorum: true},
		{name: "expired-restore", status: lifecycle.TransitionComplete, restore: fmt.Sprintf("ongoing-request=false, expiry-date=%s", UTCNow().Add(-time.Hour).Format(http.TimeFormat)), wantQuorum: true},
		{name: "ongoing-restore", status: lifecycle.TransitionComplete, restore: "ongoing-request=true", wantQuorum: true},
		{name: "malformed-restore", status: lifecycle.TransitionComplete, restore: "ongoing-request=false, expiry-date=invalid", wantQuorum: true},
		{name: "pending", status: lifecycle.TransitionPending},
		{name: "ordinary"},
		{name: "valid-local-restore", status: lifecycle.TransitionComplete, restore: fmt.Sprintf("ongoing-request=false, expiry-date=%s", UTCNow().Add(time.Hour).Format(http.TimeFormat))},
		{name: "remote-metadata-conflict", status: lifecycle.TransitionComplete, conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := make([]FileInfo, 8)
			for i := range files {
				files[i] = base
				files[i].Erasure.Index = i + 1
				files[i].TransitionStatus = test.status
				files[i].Metadata = cloneMSS(base.Metadata)
				if test.restore != "" {
					files[i].Metadata[xhttp.AmzRestore] = test.restore
				}
				if i >= 3 {
					files[i].Data = []byte("remaining inline shard")
					if test.conflict {
						files[i].Metadata[xhttp.AmzObjectTagging] = "different=snapshot"
					}
				}
			}
			_, err := pickValidFileInfo(context.Background(), files, base.ModTime, base.DataDir, 6)
			if test.wantQuorum {
				if err != nil {
					t.Fatalf("completed remote metadata lost quorum during local cleanup: %v", err)
				}
			} else if !errors.Is(err, errErasureReadQuorum) {
				t.Fatalf("conflicting local data or metadata passed quorum: %v", err)
			}
		})
	}
}

type transitionCleanupFailDisk struct {
	StorageAPI
	bucket, key string
}

func (d transitionCleanupFailDisk) DeleteVersion(ctx context.Context, bucket, key string, fi FileInfo, force bool) error {
	if bucket == d.bucket && key == d.key && fi.TransitionStatus == lifecycle.TransitionComplete {
		return errFaultyDisk
	}
	return d.StorageAPI.DeleteVersion(ctx, bucket, key, fi, force)
}

func TestTransitionStoragePartialInlineCleanupRealErasure(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, roots, err := prepareErasure(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(ctx)
	defer removeRoots(roots)
	const bucket, key = "transition-inline-cleanup", "small"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("small inline source committed before partial cleanup")
	source, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{UserDefined: map[string]string{xhttp.AmzStorageClass: storageclass.RRS}})
	if err != nil {
		t.Fatal(err)
	}
	backend := obj.(*erasureServerPools).serverPools[0].sets[0]
	disks := backend.getDisks()
	for _, disk := range disks {
		fi, readErr := disk.ReadVersion(ctx, bucket, key, "", true)
		if readErr != nil || fi.Erasure.DataBlocks != 6 || fi.Erasure.ParityBlocks != 2 || len(fi.Data) == 0 {
			t.Fatalf("fixture must have eight inline disks and EC:2: data=%d blocks=%d/%d err=%v", len(fi.Data), fi.Erasure.DataBlocks, fi.Erasure.ParityBlocks, readErr)
		}
	}
	ref := transitionStorageReference()
	if _, err = obj.DeleteObject(ctx, bucket, key, ObjectOptions{TransitionExpected: &source, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: ref}); err != nil {
		t.Fatal(err)
	}
	faulty := *backend
	wrapped := append([]StorageAPI(nil), disks...)
	for i := 0; i < 5; i++ {
		wrapped[i] = transitionCleanupFailDisk{StorageAPI: disks[i], bucket: bucket, key: key}
	}
	faulty.getDisks = func() []StorageAPI { return wrapped }
	if _, err = faulty.DeleteObject(ctx, bucket, key, ObjectOptions{TransitionExpected: &source, TransitionStatus: lifecycle.TransitionComplete, TransitionedObject: ref}); err == nil {
		t.Fatal("five failed cleanup disks should report insufficient write quorum")
	}
	var cleaned, retained int
	for _, disk := range disks {
		fi, readErr := disk.ReadVersion(ctx, bucket, key, "", true)
		if readErr != nil || fi.TransitionStatus != lifecycle.TransitionComplete || !reflect.DeepEqual(parseTransitionedObject(fi.Metadata), ref) {
			t.Fatalf("cleanup failure lost the already committed destination: %+v / %v", fi, readErr)
		}
		if len(fi.Data) == 0 {
			cleaned++
		} else {
			retained++
		}
	}
	if cleaned != 3 || retained != 5 {
		t.Fatalf("expected 3 cleaned and 5 retained inline disks, got %d/%d", cleaned, retained)
	}
	if _, err = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{}); err != nil {
		t.Fatalf("HEAD lost completed destination metadata: %v", err)
	}
	// The GET path reads inline bytes before deciding to use the tier. Their
	// partial removal must not hide an otherwise unanimous remote reference.
	fi, _, _, err := backend.getObjectFileInfo(ctx, bucket, key, ObjectOptions{}, true)
	if err != nil || fi.TransitionStatus != lifecycle.TransitionComplete || !reflect.DeepEqual(parseTransitionedObject(fi.Metadata), ref) {
		t.Fatalf("GET metadata lost quorum after partial inline cleanup: %+v / %v", fi, err)
	}
}

func TestTransitionStorageMetadataSnapshotQuorum(t *testing.T) {
	old := transitionStorageFixture()
	old.Metadata[xhttp.AmzObjectTagging] = "destination=old"
	old.Metadata[crypto.MetaSealedKeySSEC] = "old-sealed-key"
	fresh := old
	fresh.Metadata = cloneMSS(old.Metadata)
	fresh.Metadata[xhttp.AmzObjectTagging] = "destination=new"
	fresh.Metadata[crypto.MetaSealedKeySSEC] = "new-sealed-key"
	got, err := pickValidFileInfo(context.Background(), []FileInfo{old, fresh, fresh, fresh}, old.ModTime, old.DataDir, 3)
	if err != nil || !reflect.DeepEqual(got.Metadata, fresh.Metadata) {
		t.Fatalf("a stale first disk displaced fresh tags/encryption metadata: %v / %v", got.Metadata, err)
	}
	if _, err = pickValidFileInfo(context.Background(), []FileInfo{old, old, fresh, fresh}, old.ModTime, old.DataDir, 3); err == nil {
		t.Fatal("mixed tag/encryption snapshots were accepted without quorum")
	}
	// Different insertion order must still represent the same snapshot.
	reordered := fresh
	reordered.Metadata = make(map[string]string)
	for _, key := range []string{crypto.MetaSealedKeySSEC, xhttp.AmzObjectTagging, "content-type", "etag"} {
		reordered.Metadata[key] = fresh.Metadata[key]
	}
	if _, err = pickValidFileInfo(context.Background(), []FileInfo{old, fresh, reordered, reordered}, old.ModTime, old.DataDir, 3); err != nil {
		t.Fatalf("metadata map insertion order changed quorum: %v", err)
	}
}

func TestTransitionStorageLogicalPartsKeepFileInfoWireFormat(t *testing.T) {
	fi := transitionStorageFixture()
	source := fi.ToObjectInfo(fi.Volume, fi.Name)
	fi.Parts = []ObjectPartInfo{{Number: 1, Size: fi.Size, ActualSize: fi.Size}}
	fi.TransitionStatus = lifecycle.TransitionComplete
	if err := setTransitionRestoreParts(fi.Metadata, &source); err != nil {
		t.Fatal(err)
	}
	packed, err := fi.MarshalMsg(nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded FileInfo
	rest, err := decoded.UnmarshalMsg(packed)
	if err != nil || len(rest) != 0 || len(decoded.Parts) != 1 || !reflect.DeepEqual(decoded.ToObjectInfo(fi.Volume, fi.Name).Parts, source.Parts) {
		t.Fatalf("logical boundaries did not survive existing FileInfo tuple: %+v / %v", decoded, err)
	}
	for _, invalid := range []string{`[]`, `[{"number":1,"size":-1,"actualSize":1}]`, `[{"number":1,"size":1,"actualSize":1},{"number":1,"size":1,"actualSize":1}]`, `[{"number":1,"size":9223372036854775807,"actualSize":1},{"number":2,"size":1,"actualSize":1}]`} {
		if parseTransitionRestoreParts(map[string]string{transitionRestorePartsKey: invalid}) != nil {
			t.Fatalf("invalid logical boundaries accepted: %s", invalid)
		}
	}
	metadata := cloneMSS(fi.Metadata)
	metadata[crypto.MetaMultipart] = ""
	metadata[ReservedMetadataPrefix+"compression"] = compressionAlgorithmV2
	metadata[transitionDeleteIntentKey] = `{"arn":"old","key":"old"}`
	clearTransitionMetadata(metadata)
	if metadata[transitionRestorePartsKey] != "" || metadata[transitionDeleteIntentKey] != "" || metadata["etag"] != source.ETag || metadata[ReservedMetadataPrefix+"compression"] != compressionAlgorithmV2 {
		t.Fatal("new copy lost unrelated object metadata")
	}
	if _, ok := metadata[crypto.MetaMultipart]; !ok {
		t.Fatal("new copy stripped encryption metadata")
	}
}

func TestTransitionStorageCanceledDiskCommitKeepsData(t *testing.T) {
	disk, root, err := newXLStorageTestSetup()
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	defer os.RemoveAll(root)
	ctx := context.Background()
	if err = disk.MakeVol(ctx, "bucket"); err != nil {
		t.Fatal(err)
	}
	fi := transitionStorageFixture()
	fi.TransitionStatus = lifecycle.TransitionPending
	fi.Metadata[ReservedMetadataPrefixLower+"transition-status"] = lifecycle.TransitionPending
	if err = setTransitionedObject(fi.Metadata, transitionStorageReference()); err != nil {
		t.Fatal(err)
	}
	if err = disk.WriteMetadata(ctx, fi.Volume, fi.Name, fi); err != nil {
		t.Fatal(err)
	}
	partPath := filepath.Join(root, fi.Volume, fi.Name, fi.DataDir, "part.1")
	if err = os.MkdirAll(filepath.Dir(partPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(partPath, []byte("local source"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := disk.ReadAll(ctx, fi.Volume, pathJoin(fi.Name, xlStorageFormatFile))
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	err = disk.DeleteVersion(canceled, fi.Volume, fi.Name, FileInfo{VersionID: fi.VersionID, TransitionStatus: lifecycle.TransitionComplete}, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled commit returned %v", err)
	}
	after, err := disk.ReadAll(ctx, fi.Volume, pathJoin(fi.Name, xlStorageFormatFile))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed commit changed metadata: %v", err)
	}
	if _, err = os.Stat(partPath); err != nil {
		t.Fatalf("failed commit removed local data: %v", err)
	}
}

func TestTransitionStorageFSRejectsTransitionWithoutDeleting(t *testing.T) {
	previousMetadata := globalBucketMetadataSys
	globalBucketMetadataSys = NewBucketMetadataSys()
	defer func() { globalBucketMetadataSys = previousMetadata }()
	obj := initFSObjects(t.TempDir(), t)
	ctx := context.Background()
	const bucket, object = "transition-fs", "source"
	if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("keep FS data")
	if _, err := obj.PutObject(ctx, bucket, object, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{lifecycle.TransitionPending, lifecycle.TransitionComplete} {
		if _, err := obj.DeleteObject(ctx, bucket, object, ObjectOptions{TransitionStatus: status, TransitionedObject: transitionStorageReference()}); err == nil {
			t.Fatalf("FS accepted %s transition", status)
		}
	}
	gr, err := obj.GetObjectNInfo(ctx, bucket, object, nil, http.Header{}, readLock, ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(gr)
	gr.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("FS transition removed source data: %q / %v", got, err)
	}
}

type transitionFailCommitDisk struct {
	StorageAPI
	deletes *atomic.Int32
}

func (d transitionFailCommitDisk) UpdateMetadata(ctx context.Context, volume, object string, fi FileInfo) error {
	if fi.TransitionStatus == lifecycle.TransitionComplete {
		return errFaultyDisk
	}
	return d.StorageAPI.UpdateMetadata(ctx, volume, object, fi)
}

func (d transitionFailCommitDisk) DeleteVersion(ctx context.Context, volume, object string, fi FileInfo, force bool) error {
	if fi.TransitionStatus == lifecycle.TransitionComplete {
		d.deletes.Add(1)
	}
	return d.StorageAPI.DeleteVersion(ctx, volume, object, fi, force)
}

func TestTransitionStorageOverwriteMetadataQuorum(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, roots, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(ctx)
	defer removeRoots(roots)
	const bucket = "transition-overwrite-quorum"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	disks := obj.(*erasureServerPools).serverPools[0].sets[0].getDisks()
	for _, test := range []struct {
		name   string
		status string
	}{
		{name: "ordinary"},
		{name: "pending", status: lifecycle.TransitionPending},
		{name: "complete", status: lifecycle.TransitionComplete},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := []byte("retain original inline source")
			source, err := obj.PutObject(ctx, bucket, test.name, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if test.status != "" {
				source, err = obj.PutObjectMetadata(ctx, bucket, test.name, ObjectOptions{TransitionStatus: test.status, TransitionedObject: transitionStorageReference()})
				if err != nil {
					t.Fatal(err)
				}
			}
			pristine, errs := readAllFileInfo(ctx, disks, bucket, test.name, "", true)
			pristineBytes := make([][]byte, len(disks))
			before := make([][]byte, len(disks))
			for i, disk := range disks {
				if errs[i] != nil {
					t.Fatal(errs[i])
				}
				pristineBytes[i], err = disk.ReadAll(ctx, bucket, pathJoin(test.name, xlStorageFormatFile))
				if err != nil {
					t.Fatal(err)
				}
				// Every disk can write, but no two agree on the old snapshot.
				// A successful write would discard an untrusted tier reference.
				conflict := pristine[i]
				conflict.Metadata = cloneMSS(conflict.Metadata)
				conflict.Metadata["snapshot"] = fmt.Sprintf("disk-%d", i)
				if err = disk.UpdateMetadata(ctx, bucket, test.name, conflict); err != nil {
					t.Fatal(err)
				}
				before[i], err = disk.ReadAll(ctx, bucket, pathJoin(test.name, xlStorageFormatFile))
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err = obj.GetObjectInfo(ctx, bucket, test.name, ObjectOptions{}); !errors.Is(err, errErasureReadQuorum) {
				t.Fatalf("conflicting fixture retained metadata quorum: %v", err)
			}
			replacement := []byte("replacement")
			_, err = obj.PutObject(ctx, bucket, test.name, mustGetPutObjReader(t, bytes.NewReader(replacement), int64(len(replacement)), "", ""), ObjectOptions{})
			var quorumErr InsufficientWriteQuorum
			if !errors.As(err, &quorumErr) || quorumErr.Bucket != bucket || quorumErr.Object != test.name {
				t.Fatalf("unsafe overwrite did not report the failed write: %v", err)
			}
			for i, disk := range disks {
				after, readErr := disk.ReadAll(ctx, bucket, pathJoin(test.name, xlStorageFormatFile))
				if readErr != nil || !bytes.Equal(before[i], after) {
					t.Fatalf("rejected overwrite changed disk %d metadata or inline data: %v", i, readErr)
				}
				if err = disk.WriteAll(ctx, bucket, pathJoin(test.name, xlStorageFormatFile), pristineBytes[i]); err != nil {
					t.Fatal(err)
				}
			}
			retained, err := obj.GetObjectInfo(ctx, bucket, test.name, ObjectOptions{})
			if err != nil || retained.ETag != source.ETag || retained.TransitionStatus != source.TransitionStatus || !reflect.DeepEqual(retained.TransitionedObject, source.TransitionedObject) {
				t.Fatalf("rejected overwrite lost the source identity or tier reference: %+v / %v", retained, err)
			}
		})
	}
}

func TestTransitionStorageErasureRestoreAndCAS(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, disks, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(ctx)
	defer removeRoots(disks)
	const bucket = "transition-storage"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	reader := func(raw []byte, actual int64) *PutObjReader {
		h, err := hash.NewReader(bytes.NewReader(raw), int64(len(raw)), "", "", actual)
		if err != nil {
			t.Fatal(err)
		}
		return NewPutObjReader(h)
	}
	t.Run("stale-and-no-lock", func(t *testing.T) {
		first, err := obj.PutObject(ctx, bucket, "cas", reader([]byte("first"), 5), ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := obj.PutObject(ctx, bucket, "cas", reader([]byte("newer"), 5), ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = obj.DeleteObject(ctx, bucket, "cas", ObjectOptions{TransitionExpected: &first, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: transitionStorageReference()}); !isErrPreconditionFailed(err) {
			t.Fatalf("stale source was transitioned: %v", err)
		}
		if _, err = obj.PutObjectMetadata(ctx, bucket, "cas", ObjectOptions{TransitionExpected: &first, TransitionedObject: transitionStorageReference()}); !isErrPreconditionFailed(err) {
			t.Fatalf("stale source metadata was updated: %v", err)
		}
		lock := obj.NewNSLock(bucket, "cas")
		lockedCtx, err := lock.GetLock(ctx, globalOperationTimeout)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Unlock()
		shortCtx, cancel := context.WithTimeout(lockedCtx, 2*time.Second)
		defer cancel()
		// A two-pool view exercises upper-layer NoLock handling as well.
		pools := obj.(*erasureServerPools)
		multi := &erasureServerPools{serverPools: []*erasureSets{pools.serverPools[0], pools.serverPools[0]}}
		if _, err = multi.GetObjectInfo(shortCtx, bucket, "cas", ObjectOptions{NoLock: true}); err != nil {
			t.Fatalf("GetObjectInfo reacquired an owned lock: %v", err)
		}
		if _, err = multi.DeleteObject(shortCtx, bucket, "cas", ObjectOptions{NoLock: true, TransitionExpected: &fresh, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: transitionStorageReference()}); err != nil {
			t.Fatalf("DeleteObject reacquired an owned lock: %v", err)
		}
		if _, err = multi.PutObjectMetadata(shortCtx, bucket, "cas", ObjectOptions{NoLock: true, TransitionExpected: &fresh, UserDefined: map[string]string{"custom": "preserved"}}); err != nil {
			t.Fatalf("PutObjectMetadata reacquired an owned lock: %v", err)
		}
	})
	t.Run("deleted-source-does-not-reappear", func(t *testing.T) {
		data := []byte("deleted source")
		source, err := obj.PutObject(ctx, bucket, "deleted", reader(data, int64(len(data))), ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = obj.DeleteObject(ctx, bucket, "deleted", ObjectOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err = obj.DeleteObject(ctx, bucket, "deleted", ObjectOptions{TransitionExpected: &source, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: transitionStorageReference()}); !isErrPreconditionFailed(err) {
			t.Fatalf("missing source did not fail its identity check: %v", err)
		}
		if _, err = obj.PutObjectMetadata(ctx, bucket, "deleted", ObjectOptions{TransitionExpected: &source, TransitionedObject: transitionStorageReference()}); !isErrPreconditionFailed(err) {
			t.Fatalf("missing source metadata did not fail its identity check: %v", err)
		}
		source.TransitionStatus = lifecycle.TransitionComplete
		if _, err = obj.PutObject(ctx, bucket, "deleted", reader(data, int64(len(data))), ObjectOptions{TransitionExpected: &source, TransitionRestore: &source, TransitionedObject: transitionStorageReference()}); !isErrPreconditionFailed(err) {
			t.Fatalf("restore resurrected a deleted source: %v", err)
		}
		if _, err = obj.GetObjectInfo(ctx, bucket, "deleted", ObjectOptions{}); !isErrObjectNotFound(err) {
			t.Fatalf("failed conditional operation recreated its source: %v", err)
		}
	})
	t.Run("failed-remote-delete-preserves-reference", func(t *testing.T) {
		bad, err := obj.PutObject(ctx, bucket, "bad-delete", reader([]byte("remote"), 6), ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = obj.DeleteObject(ctx, bucket, "bad-delete", ObjectOptions{TransitionExpected: &bad, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: transitionStorageReference()}); err != nil {
			t.Fatal(err)
		}
		if _, err = obj.PutObject(ctx, bucket, "good-delete", reader([]byte("local"), 5), ObjectOptions{}); err != nil {
			t.Fatal(err)
		}
		_, failures := obj.DeleteObjects(ctx, bucket, []ObjectToDelete{{ObjectName: "bad-delete"}, {ObjectName: "good-delete"}}, ObjectOptions{})
		if failures[0] == nil || failures[1] != nil {
			t.Fatalf("bulk did not isolate failed remote cleanup: %v", failures)
		}
		remaining, err := obj.GetObjectInfo(ctx, bucket, "bad-delete", ObjectOptions{})
		if err != nil || remaining.TransitionedObject == nil || remaining.ETag != bad.ETag {
			t.Fatalf("bulk discarded a failed object's recovery reference: %+v / %v", remaining, err)
		}
		if _, err = obj.DeleteObject(ctx, bucket, "bad-delete", ObjectOptions{}); err == nil {
			t.Fatal("single delete ignored failed remote cleanup")
		}
		if _, err = obj.PutObject(ctx, bucket, "bad-delete", reader([]byte("replacement"), 11), ObjectOptions{}); err == nil {
			t.Fatal("overwrite discarded a tiered object's recovery reference")
		}
	})
	t.Run("marker-keeps-remote-version", func(t *testing.T) {
		const versionedBucket = "transition-versioned"
		if err = obj.MakeBucketWithLocation(ctx, versionedBucket, BucketOptions{VersioningEnabled: true}); err != nil {
			t.Fatal(err)
		}
		source, err := obj.PutObject(ctx, versionedBucket, "retained", reader([]byte("old version"), 11), ObjectOptions{Versioned: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = obj.DeleteObject(ctx, versionedBucket, "retained", ObjectOptions{Versioned: true, VersionID: source.VersionID, TransitionExpected: &source, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: transitionStorageReference()}); err != nil {
			t.Fatal(err)
		}
		marker, err := obj.DeleteObject(ctx, versionedBucket, "retained", ObjectOptions{Versioned: true})
		if err != nil {
			t.Fatalf("source marker attempted remote cleanup: %v", err)
		}
		retained, err := obj.GetObjectInfo(ctx, versionedBucket, "retained", ObjectOptions{VersionID: source.VersionID})
		if err != nil || retained.IsLatest || retained.TransitionedObject == nil {
			t.Fatalf("marker lost the old version: %+v / %v", retained, err)
		}
		_, failures := obj.DeleteObjects(ctx, versionedBucket, []ObjectToDelete{{ObjectName: "retained"}}, ObjectOptions{Versioned: true})
		if failures[0] != nil {
			t.Fatalf("bulk source marker attempted remote cleanup: %v", failures[0])
		}
		pools := obj.(*erasureServerPools)
		multi := &erasureServerPools{serverPools: []*erasureSets{pools.serverPools[0], pools.serverPools[0]}}
		if _, err = multi.DeleteObject(ctx, versionedBucket, "retained", ObjectOptions{Versioned: true, VersionID: marker.VersionID}); err != nil {
			t.Fatalf("specific delete marker could not be located across pools: %v", err)
		}
		if _, err = multi.DeleteObject(ctx, versionedBucket, "retained", ObjectOptions{Versioned: true, VersionID: mustGetUUID()}); !isErrVersionNotFound(err) {
			t.Fatalf("missing specific version returned %v", err)
		}
	})
	t.Run("new-copy-version-owns-independent-data", func(t *testing.T) {
		const versionedBucket = "transition-copy"
		if err = obj.MakeBucketWithLocation(ctx, versionedBucket, BucketOptions{VersioningEnabled: true}); err != nil {
			t.Fatal(err)
		}
		data := []byte("independent copy")
		source, err := obj.PutObject(ctx, versionedBucket, "copy", reader(data, int64(len(data))), ObjectOptions{Versioned: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = obj.DeleteObject(ctx, versionedBucket, "copy", ObjectOptions{Versioned: true, VersionID: source.VersionID, TransitionExpected: &source, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: transitionStorageReference()}); err != nil {
			t.Fatal(err)
		}
		source, err = obj.GetObjectInfo(ctx, versionedBucket, "copy", ObjectOptions{VersionID: source.VersionID})
		if err != nil {
			t.Fatal(err)
		}
		intent, err := json.Marshal(source.TransitionedObject)
		if err != nil {
			t.Fatal(err)
		}
		source, err = obj.PutObjectMetadata(ctx, versionedBucket, "copy", ObjectOptions{VersionID: source.VersionID, TransitionExpected: &source, UserDefined: map[string]string{transitionDeleteIntentKey: string(intent)}})
		if err != nil {
			t.Fatal(err)
		}
		source.metadataOnly = true
		source.PutObjReader = reader(data, int64(len(data)))
		copied, err := obj.CopyObject(ctx, versionedBucket, "copy", versionedBucket, "copy", source, ObjectOptions{VersionID: source.VersionID}, ObjectOptions{Versioned: true})
		if err != nil {
			t.Fatal(err)
		}
		if copied.VersionID == source.VersionID || copied.TransitionStatus != "" || copied.TransitionedObject != nil || copied.UserDefined[transitionReferenceKey] != "" || copied.UserDefined[transitionDeleteIntentKey] != "" {
			t.Fatalf("new version shared the source's remote destination: %+v", copied)
		}
		if _, err = obj.DeleteObject(ctx, versionedBucket, "copy", ObjectOptions{Versioned: true, VersionID: copied.VersionID}); err != nil {
			t.Fatalf("independent version attempted source remote cleanup: %v", err)
		}
		retained, err := obj.GetObjectInfo(ctx, versionedBucket, "copy", ObjectOptions{VersionID: source.VersionID})
		if err != nil || retained.TransitionedObject == nil {
			t.Fatalf("deleting copied version discarded source reference: %+v / %v", retained, err)
		}
	})
	t.Run("failed-complete-keeps-local", func(t *testing.T) {
		current, err := obj.PutObject(ctx, bucket, "commit", reader([]byte("retain-source"), 13), ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = obj.DeleteObject(ctx, bucket, "commit", ObjectOptions{TransitionExpected: &current, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: transitionStorageReference()}); err != nil {
			t.Fatal(err)
		}
		original := obj.(*erasureServerPools).serverPools[0].sets[0]
		faulty := *original
		var deletes atomic.Int32
		wrapped := make([]StorageAPI, len(original.getDisks()))
		for i, disk := range original.getDisks() {
			wrapped[i] = transitionFailCommitDisk{StorageAPI: disk, deletes: &deletes}
		}
		faulty.getDisks = func() []StorageAPI { return wrapped }
		if _, err = faulty.DeleteObject(ctx, bucket, "commit", ObjectOptions{TransitionExpected: &current, TransitionStatus: lifecycle.TransitionComplete, TransitionedObject: transitionStorageReference()}); err == nil {
			t.Fatal("failed quorum was reported as complete")
		}
		if deletes.Load() != 0 {
			t.Fatal("local cleanup started before metadata quorum committed")
		}
		gr, err := obj.GetObjectNInfo(ctx, bucket, "commit", nil, http.Header{}, readLock, ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(gr)
		gr.Close()
		if err != nil || string(data) != "retain-source" {
			t.Fatalf("source lost after failed commit: %q / %v", data, err)
		}
	})
	for _, mode := range []string{"encrypted", "compressed"} {
		t.Run(mode+"-multipart", func(t *testing.T) {
			object := mode
			plainParts := [][]byte{bytes.Repeat([]byte("first-part-content"), (5<<20)/18+1), bytes.Repeat([]byte("second-part-content"), 1024)}
			metadata := make(map[string]string)
			headers := http.Header{}
			var objectKey crypto.ObjectKey
			if mode == "encrypted" {
				customerKey := bytes.Repeat([]byte{42}, 32)
				objectKey, err = newEncryptMetadata(customerKey, bucket, object, metadata, false)
				if err != nil {
					t.Fatal(err)
				}
				metadata[crypto.MetaMultipart] = ""
				headers.Set(xhttp.AmzServerSideEncryptionCustomerAlgorithm, "AES256")
				headers.Set(xhttp.AmzServerSideEncryptionCustomerKey, base64.StdEncoding.EncodeToString(customerKey))
				digest := md5.Sum(customerKey)
				headers.Set(xhttp.AmzServerSideEncryptionCustomerKeyMD5, base64.StdEncoding.EncodeToString(digest[:]))
			} else {
				metadata[ReservedMetadataPrefix+"compression"] = compressionAlgorithmV2
			}
			upload, err := obj.NewMultipartUpload(ctx, bucket, object, ObjectOptions{UserDefined: metadata})
			if err != nil {
				t.Fatal(err)
			}
			var raw bytes.Buffer
			completed := make([]CompletePart, len(plainParts))
			for i, plain := range plainParts {
				var data []byte
				if mode == "encrypted" {
					partKey := objectKey.DerivePartKey(uint32(i + 1))
					stream, err := sio.EncryptReader(bytes.NewReader(plain), sio.Config{Key: partKey[:], MinVersion: sio.Version20})
					if err != nil {
						t.Fatal(err)
					}
					data, err = io.ReadAll(stream)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					var compressed bytes.Buffer
					writer := s2.NewWriter(&compressed)
					if _, err = writer.Write(plain); err != nil {
						t.Fatal(err)
					}
					if err = writer.Close(); err != nil {
						t.Fatal(err)
					}
					data = compressed.Bytes()
				}
				raw.Write(data)
				part, err := obj.PutObjectPart(ctx, bucket, object, upload, i+1, reader(data, int64(len(plain))), ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				completed[i] = CompletePart{PartNumber: i + 1, ETag: part.ETag}
			}
			source, err := obj.CompleteMultipartUpload(ctx, bucket, object, upload, completed, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ref := transitionStorageReference()
			if _, err = obj.DeleteObject(ctx, bucket, object, ObjectOptions{TransitionExpected: &source, TransitionStatus: lifecycle.TransitionPending, TransitionedObject: ref}); err != nil {
				t.Fatal(err)
			}
			if _, err = obj.DeleteObject(ctx, bucket, object, ObjectOptions{TransitionExpected: &source, TransitionStatus: lifecycle.TransitionComplete, TransitionedObject: ref}); err != nil {
				t.Fatal(err)
			}
			fresh, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			// Healing a remote-only version must preserve its multipart reader
			// metadata even when one source disk has lost xl.meta entirely.
			original := obj.(*erasureServerPools).serverPools[0].sets[0]
			if err = original.getDisks()[0].DeleteVersion(ctx, bucket, object, FileInfo{}, false); err != nil {
				t.Fatal(err)
			}
			if _, err = obj.HealObject(ctx, bucket, object, "", madmin.HealOpts{Remove: true}); err != nil {
				t.Fatalf("remote-only metadata could not be healed: %v", err)
			}
			fresh, err = obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{})
			if err != nil || !reflect.DeepEqual(fresh.Parts, source.Parts) || fresh.TransitionedObject == nil {
				t.Fatalf("healing lost remote multipart metadata: %+v / %v", fresh, err)
			}
			lock := obj.NewNSLock(bucket, object)
			lockedCtx, err := lock.GetLock(ctx, globalOperationTimeout)
			if err != nil {
				t.Fatal(err)
			}
			restoredMeta := cloneMSS(fresh.UserDefined)
			restoredMeta["etag"] = fresh.ETag
			restoredMeta[xhttp.AmzRestore] = fmt.Sprintf("ongoing-request=false, expiry-date=%s", UTCNow().Add(time.Hour).Format(http.TimeFormat))
			restored, err := obj.PutObject(lockedCtx, bucket, object, reader(raw.Bytes(), int64(raw.Len())), ObjectOptions{NoLock: true, UserDefined: restoredMeta, TransitionExpected: &fresh, TransitionRestore: &fresh, TransitionedObject: fresh.TransitionedObject})
			lock.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if restored.TransitionStatus != lifecycle.TransitionComplete || !restored.ModTime.Equal(source.ModTime) || restored.ETag != source.ETag || !reflect.DeepEqual(restored.Parts, source.Parts) {
				t.Fatalf("restore changed logical multipart identity: %+v", restored)
			}
			head, err := obj.GetObjectInfo(ctx, bucket, object, ObjectOptions{})
			if err != nil || head.StorageClass != ref.StorageClass {
				t.Fatalf("restored HEAD lost its persisted storage class: %q / %v", head.StorageClass, err)
			}
			plain := bytes.Join(plainParts, nil)
			for _, rs := range []*HTTPRangeSpec{nil, {Start: int64(len(plainParts[0]) - 9), End: int64(len(plainParts[0]) + 12)}, {Start: int64(len(plainParts[0]) + 5), End: int64(len(plainParts[0]) + 30)}} {
				gr, err := obj.GetObjectNInfo(ctx, bucket, object, rs, headers, readLock, ObjectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if gr.ObjInfo.StorageClass != head.StorageClass {
					gr.Close()
					t.Fatalf("restored GET and HEAD disagree on storage class: GET=%q HEAD=%q", gr.ObjInfo.StorageClass, head.StorageClass)
				}
				got, err := io.ReadAll(gr)
				gr.Close()
				want := plain
				if rs != nil {
					want = plain[rs.Start : rs.End+1]
				}
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("restored %s range %+v differs: got=%d want=%d / %v", mode, rs, len(got), len(want), err)
				}
			}
			if _, err = obj.HealObject(ctx, bucket, object, "", madmin.HealOpts{Remove: true}); err != nil {
				t.Fatalf("restored data could not be healed: %v", err)
			}
		})
	}
}
