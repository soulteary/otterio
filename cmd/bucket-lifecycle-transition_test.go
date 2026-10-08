// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/bucket/policy"
	"github.com/soulteary/otterio/pkg/hash"
	"github.com/soulteary/otterio/pkg/madmin"
)

// The destination serves signed SDK requests through native S3 object handlers
// backed by a separate real erasure object layer. Bucket metadata is explicitly
// read from that backend because the test process has one source global layer.
type lifecycleDestinationFixture struct {
	ObjectLayer
	server               *httptest.Server
	mu                   sync.Mutex
	failPut, failDelete  bool
	allocateVersionID    bool
	blockPut, putEntered chan struct{}
	puts, gets, deletes  int
}

func (f *lifecycleDestinationFixture) PutObject(ctx context.Context, bucket, key string, r *PutObjReader, opts ObjectOptions) (ObjectInfo, error) {
	f.mu.Lock()
	allocate := f.allocateVersionID
	f.mu.Unlock()
	if allocate && opts.Versioned {
		opts.VersionID = ""
	}
	return f.ObjectLayer.PutObject(ctx, bucket, key, r, opts)
}

type lifecycleCommitFailDisk struct {
	StorageAPI
	bucket, key string
	fail        *atomic.Bool
}

func (f lifecycleCommitFailDisk) UpdateMetadata(ctx context.Context, bucket, key string, fi FileInfo) error {
	if f.fail.Load() && bucket == f.bucket && key == f.key && fi.TransitionStatus == lifecycle.TransitionComplete {
		return errors.New("injected source commit failure after remote ACK")
	}
	return f.StorageAPI.UpdateMetadata(ctx, bucket, key, fi)
}

func lifecycleFailCompletion(source ObjectLayer, bucket, key string) (*atomic.Bool, func()) {
	backend := source.(*erasureServerPools).serverPools[0]
	fail := &atomic.Bool{}
	fail.Store(true)
	backend.erasureDisksMu.Lock()
	original := make([][]StorageAPI, len(backend.erasureDisks))
	for set, disks := range backend.erasureDisks {
		original[set] = append([]StorageAPI(nil), disks...)
		for disk, storage := range disks {
			backend.erasureDisks[set][disk] = lifecycleCommitFailDisk{StorageAPI: storage, bucket: bucket, key: key, fail: fail}
		}
	}
	backend.erasureDisksMu.Unlock()
	return fail, func() {
		backend.erasureDisksMu.Lock()
		for set, disks := range original {
			copy(backend.erasureDisks[set], disks)
		}
		backend.erasureDisksMu.Unlock()
	}
}

type lifecycleObservedReadLayer struct {
	ObjectLayer
	key     string
	entered chan struct{}
}

func (o *lifecycleObservedReadLayer) GetObjectInfo(ctx context.Context, bucket, key string, opts ObjectOptions) (ObjectInfo, error) {
	if key == o.key {
		select {
		case o.entered <- struct{}{}:
		default:
		}
	}
	return o.ObjectLayer.GetObjectInfo(ctx, bucket, key, opts)
}

func lifecycleNativeDestination(_ TestErrHandler, dest ObjectLayer) *lifecycleDestinationFixture {
	fixture := &lifecycleDestinationFixture{ObjectLayer: dest}
	api := objectAPIHandlers{ObjectAPI: func() ObjectLayer { return fixture }, CacheAPI: func() CacheObjectLayer { return nil }}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		bucket := parts[0]
		key := ""
		if len(parts) == 2 {
			key = parts[1]
		}
		r = setURLVarsOnRequest(r, map[string]string{"bucket": bucket, "object": key})
		if key == "" {
			if r.Method == http.MethodHead {
				api.HeadBucketHandler(w, r)
				return
			}
			if r.Method == http.MethodGet && r.URL.Query().Has("versioning") {
				if code := checkRequestAuthType(r.Context(), r, policy.GetBucketVersioningAction, bucket, ""); code != ErrNone {
					writeErrorResponse(r.Context(), w, errorCodes.ToAPIErr(code), r.URL, false)
					return
				}
				meta, err := loadBucketMetadata(r.Context(), dest, bucket)
				if err != nil {
					writeErrorResponse(r.Context(), w, toAPIError(r.Context(), err), r.URL, false)
					return
				}
				writeSuccessResponseXML(w, meta.VersioningConfigXML)
				return
			}
			if r.Method == http.MethodGet && r.URL.Query().Has("location") {
				writeSuccessResponseXML(w, []byte(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`))
				return
			}
		}
		fixture.mu.Lock()
		failed := false
		switch r.Method {
		case http.MethodPut:
			fixture.puts++
			failed = fixture.failPut
		case http.MethodGet:
			fixture.gets++
		case http.MethodDelete:
			fixture.deletes++
			failed = fixture.failDelete
		}
		gate, entered := fixture.blockPut, fixture.putEntered
		fixture.mu.Unlock()
		if r.Method == http.MethodPut && gate != nil {
			if entered != nil {
				select {
				case entered <- struct{}{}:
				default:
				}
			}
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		if failed {
			writeErrorResponse(r.Context(), w, errorCodes.ToAPIErr(ErrInternalError), r.URL, false)
			return
		}
		switch r.Method {
		case http.MethodPut:
			api.PutObjectHandler(w, r)
		case http.MethodGet:
			api.GetObjectHandler(w, r)
		case http.MethodHead:
			api.HeadObjectHandler(w, r)
		case http.MethodDelete:
			api.DeleteObjectHandler(w, r)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	return fixture
}

func lifecycleTestPut(t TestErrHandler, obj ObjectLayer, bucket, key string, data []byte, mtime time.Time) ObjectInfo {
	reader, err := hash.NewReader(bytes.NewReader(data), int64(len(data)), "", "", int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	oi, err := obj.PutObject(context.Background(), bucket, key, NewPutObjReader(reader), ObjectOptions{
		Versioned: true, MTime: mtime, UserDefined: map[string]string{
			"content-type": "text/plain", "content-encoding": "identity", "x-amz-meta-owner": "test-owner", xhttp.AmzObjectTagging: "team=%E4%B8%AD%E6%96%87+%2B",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return oi
}

func lifecycleTestRead(t TestErrHandler, obj ObjectLayer, bucket, key, version string, want []byte) {
	reader, err := obj.GetObjectNInfo(context.Background(), bucket, key, nil, http.Header{}, readLock, ObjectOptions{VersionID: version})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("tier read: got %q want %q / %v", data, want, err)
	}
}

func TestLifecycleTransitionRealErasurePipeline(t *testing.T) {
	ExecObjectLayerTest(t, func(source ObjectLayer, instance string, t TestErrHandler) {
		if instance != ErasureTestStr {
			return
		}
		ctx := context.Background()
		dest, disks, err := prepareErasure(ctx, 4)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { dest.Shutdown(ctx); removeRoots(disks) }()
		const bucket = "tier-source"
		const currentBucket = "tier-current"
		const oldBucket = "tier-noncurrent"
		if err := source.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
			t.Fatal(err)
		}
		enabled := []byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`)
		if err := globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, enabled); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{currentBucket, oldBucket} {
			if err := dest.MakeBucketWithLocation(ctx, name, BucketOptions{}); err != nil {
				t.Fatal(err)
			}
			meta := newBucketMetadata(name)
			meta.VersioningConfigXML = enabled
			if err := meta.Save(ctx, dest); err != nil {
				t.Fatal(err)
			}
			globalBucketMetadataSys.Set(name, meta)
		}
		remote := lifecycleNativeDestination(t, dest)
		defer remote.server.Close()
		endpoint := strings.TrimPrefix(remote.server.URL, "http://")
		current := madmin.BucketTarget{Endpoint: endpoint, TargetBucket: currentBucket, Type: madmin.ILMService, Label: "Current", Region: "us-east-1", Credentials: &auth.Credentials{AccessKey: globalActiveCred.AccessKey, SecretKey: globalActiveCred.SecretKey}}
		old := cloneBucketTarget(current)
		old.TargetBucket = oldBucket
		old.Label = "Old"
		if err := globalBucketTargetSys.SetTarget(ctx, bucket, &current, false); err != nil {
			t.Fatal(err)
		}
		if err := globalBucketTargetSys.SetTarget(ctx, bucket, &old, false); err != nil {
			t.Fatal(err)
		}

		defer globalBucketTargetSys.UpdateAllTargets(bucket, nil)
		lc := []byte(`<LifecycleConfiguration><Rule><ID>tiers</ID><Status>Enabled</Status><Filter><Prefix>data/</Prefix></Filter><Transition><Days>1</Days><StorageClass>Current</StorageClass></Transition><NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>Old</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>`)
		if err := globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, lc); err != nil {
			t.Fatal(err)
		}
		// One current version is moved to Current, then becomes noncurrent. Its
		// persisted reference must continue to read the original destination.
		original := []byte("original body on current tier")
		oi := lifecycleTestPut(t, source, bucket, "data/moved", original, time.Now().Add(-20*24*time.Hour).UTC())
		if err := transitionObject(ctx, source, oi); err != nil {
			t.Fatal("current transition", err)
		}
		complete, err := source.GetObjectInfo(ctx, bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID})
		if err != nil || complete.TransitionStatus != lifecycle.TransitionComplete || complete.TransitionedObject == nil || complete.TransitionedObject.ARN != current.Arn {
			t.Fatalf("current commit missing reference: %+v / %v", complete, err)
		}
		ref := *complete.TransitionedObject
		remoteInfo, err := dest.GetObjectInfo(ctx, currentBucket, ref.Key, ObjectOptions{VersionID: ref.VersionID})
		if err != nil || ref.VersionID == "" || remoteInfo.VersionID != ref.VersionID {
			t.Fatal("remote ACK version not persisted", err)
		}
		lifecycleTestPut(t, source, bucket, oi.Name, []byte("new latest"), time.Now().Add(-10*24*time.Hour).UTC())
		lifecycleTestRead(t, source, bucket, oi.Name, oi.VersionID, original)
		// Another old version starts transition only after becoming noncurrent and
		// must select Old even though its rule also has a current transition.
		oldData := []byte("body selected by noncurrent action")
		oldOI := lifecycleTestPut(t, source, bucket, "data/noncurrent", oldData, time.Now().Add(-30*24*time.Hour).UTC())
		lifecycleTestPut(t, source, bucket, oldOI.Name, []byte("new latest old key"), time.Now().Add(-10*24*time.Hour).UTC())
		oldOI, err = source.GetObjectInfo(ctx, bucket, oldOI.Name, ObjectOptions{VersionID: oldOI.VersionID})
		if err != nil || oldOI.IsLatest {
			t.Fatal("noncurrent fixture is latest", err)
		}
		if got := lifecycleActionForObject(lifecycle.Lifecycle{}, oldOI); got != lifecycle.NoneAction {
			t.Fatal("fixture unexpectedly has persisted work", got)
		}
		if err := transitionObject(ctx, source, oldOI); err != nil {
			t.Fatal("noncurrent transition", err)
		}
		oldComplete, err := source.GetObjectInfo(ctx, bucket, oldOI.Name, ObjectOptions{VersionID: oldOI.VersionID})
		if err != nil || oldComplete.TransitionedObject == nil || oldComplete.TransitionedObject.ARN != old.Arn {
			t.Fatal("noncurrent transition selected current tier", err)
		}
		lifecycleTestRead(t, source, bucket, oldOI.Name, oldOI.VersionID, oldData)

		// Empty/range reads use the same persisted remote identity.
		empty := lifecycleTestPut(t, source, bucket, "data/empty", nil, time.Now().Add(-20*24*time.Hour).UTC())
		if err := transitionObject(ctx, source, empty); err != nil {
			t.Fatal("empty transition", err)
		}
		lifecycleTestRead(t, source, bucket, empty.Name, empty.VersionID, nil)
		rs, err := parseRequestRangeSpec("bytes=2-6")
		if err != nil {
			t.Fatal(err)
		}
		ranged, err := source.GetObjectNInfo(ctx, bucket, oi.Name, rs, http.Header{}, readLock, ObjectOptions{VersionID: oi.VersionID})
		if err != nil {
			t.Fatal(err)
		}
		rangeData, err := io.ReadAll(ranged)
		ranged.Close()
		if err != nil || !bytes.Equal(rangeData, original[2:7]) {
			t.Fatal("tier range read", string(rangeData), err)
		}
		// A committed upload followed by a failed source-quorum completion remains
		// pending/local. Restart retry discovers the remote job instead of uploading.
		retryData := []byte("pending source preserved on uncertain completion")
		retryOI := lifecycleTestPut(t, source, bucket, "data/retry", retryData, time.Now().Add(-20*24*time.Hour).UTC())
		failComplete, restoreDisks := lifecycleFailCompletion(source, bucket, retryOI.Name)
		defer restoreDisks()
		if err := transitionObject(ctx, source, retryOI); err == nil {
			t.Fatal("injected completion failure succeeded")
		}
		pending, err := source.GetObjectInfo(ctx, bucket, retryOI.Name, ObjectOptions{VersionID: retryOI.VersionID})
		if err != nil || pending.TransitionStatus != lifecycle.TransitionPending || pending.TransitionedObject == nil {
			t.Fatal("uncertain completion lost durable pending source", err)
		}
		lifecycleTestRead(t, source, bucket, retryOI.Name, retryOI.VersionID, retryData)
		remote.mu.Lock()
		putsBefore := remote.puts
		remote.mu.Unlock()
		failComplete.Store(false)
		if err := transitionObject(ctx, source, pending); err != nil {
			t.Fatal("pending retry", err)
		}
		remote.mu.Lock()
		putsAfter := remote.puts
		remote.mu.Unlock()
		if putsBefore != putsAfter {
			t.Fatal("pending retry re-uploaded an already committed job")
		}
		lifecycleTestRead(t, source, bucket, retryOI.Name, retryOI.VersionID, retryData)
		// Third-party S3 targets can allocate their own remote version ID. Persist
		// the real acknowledgement and never select the latest remote object later.
		assignedData := []byte("remote allocated identity")
		assignedOI := lifecycleTestPut(t, source, bucket, "data/assigned", assignedData, time.Now().Add(-20*24*time.Hour).UTC())
		assignedPending, err := prepareTransition(ctx, source, assignedOI)
		if err != nil {
			t.Fatal(err)
		}
		planned := assignedPending.TransitionedObject.VersionID
		remote.mu.Lock()
		remote.allocateVersionID = true
		remote.mu.Unlock()
		if err := transitionObject(ctx, source, assignedPending); err != nil {
			t.Fatal("remote-assigned version", err)
		}
		remote.mu.Lock()
		remote.allocateVersionID = false
		remote.mu.Unlock()
		assignedComplete, err := source.GetObjectInfo(ctx, bucket, assignedOI.Name, ObjectOptions{VersionID: assignedOI.VersionID})
		if err != nil || assignedComplete.TransitionedObject == nil || assignedComplete.TransitionedObject.VersionID == "" || assignedComplete.TransitionedObject.VersionID == planned {
			t.Fatal("remote-assigned ACK not persisted", err)
		}
		lifecycleTestPut(t, dest, currentBucket, assignedComplete.TransitionedObject.Key, []byte("unrelated later remote version"), time.Now().UTC())
		lifecycleTestRead(t, source, bucket, assignedOI.Name, assignedOI.VersionID, assignedData)
		// A null version replacement can preserve ModTime. ETag/size must fence both
		// worker and stale deletion so the replacement cannot be uploaded/deleted.
		nullTime := time.Now().Add(-20 * 24 * time.Hour).UTC()
		nullPut := func(data string) ObjectInfo {
			hr, err := hash.NewReader(strings.NewReader(data), int64(len(data)), "", "", int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			result, err := source.PutObject(ctx, bucket, "data/null", NewPutObjReader(hr), ObjectOptions{MTime: nullTime})
			if err != nil {
				t.Fatalf("null PUT %q: %T %#v", data, err, err)
			}
			return result
		}
		nullOld := nullPut("null-before")
		nullPending := nullOld // snapshot precedes prepare; pending overwrites are rejected by storage
		nullNew := nullPut("null-after!")
		if nullNew.VersionID != "" && nullNew.VersionID != nullVersionID || !nullNew.ModTime.Equal(nullOld.ModTime) || nullNew.ETag == nullOld.ETag {
			t.Fatalf("invalid null replacement fixture: old=%+v new=%+v", nullOld, nullNew)
		}
		if err := transitionObject(ctx, source, nullPending); !isErrPreconditionFailed(err) {
			t.Fatal("stale null worker was not fenced", err)
		}
		if err := deleteTransitionedObject(ctx, source, bucket, nullOld.Name, lifecycleObjectOpts(nullPending), false, false, nullPending); !isErrPreconditionFailed(err) {
			t.Fatal("stale null delete was not fenced", err)
		}
		lifecycleTestRead(t, source, bucket, nullNew.Name, "", []byte("null-after!"))

		// Upload holds only the source write lock after durable prepare. Target
		// mutation can acquire its namespace lock, then waits on a fresh object read
		// and rejects the completed/pending reference without blocking completion.
		const busyBucket = "tier-busy"
		if err := dest.MakeBucketWithLocation(ctx, busyBucket, BucketOptions{}); err != nil {
			t.Fatal(err)
		}
		busyMeta := newBucketMetadata(busyBucket)
		busyMeta.VersioningConfigXML = enabled
		if err := busyMeta.Save(ctx, dest); err != nil {
			t.Fatal(err)
		}
		globalBucketMetadataSys.Set(busyBucket, busyMeta)
		busyTarget := cloneBucketTarget(current)
		busyTarget.TargetBucket = busyBucket
		busyTarget.Label = "Busy"
		busyTarget.Arn = ""
		if err := globalBucketTargetSys.SetTarget(ctx, bucket, &busyTarget, false); err != nil {
			t.Fatal(err)
		}
		busyRule := `<Rule><ID>busy</ID><Status>Enabled</Status><Filter><Prefix>blocked/</Prefix></Filter><Transition><Days>1</Days><StorageClass>Busy</StorageClass></Transition></Rule>`
		busyLC := bytes.Replace(lc, []byte(`</LifecycleConfiguration>`), []byte(busyRule+`</LifecycleConfiguration>`), 1)
		if err := globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, busyLC); err != nil {
			t.Fatal(err)
		}
		busyOI := lifecycleTestPut(t, source, bucket, "blocked/item", []byte("source write held during remote upload"), time.Now().Add(-20*24*time.Hour).UTC())
		gate, entered := make(chan struct{}), make(chan struct{}, 1)
		var releaseOnce sync.Once
		releaseUpload := func() { releaseOnce.Do(func() { close(gate) }) }
		defer releaseUpload()
		remote.mu.Lock()
		remote.blockPut = gate
		remote.putEntered = entered
		remote.mu.Unlock()
		workerResult := make(chan error, 1)
		go func() { workerResult <- transitionObject(ctx, source, busyOI) }()
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			releaseUpload()
			t.Fatal("worker did not reach upload")
		}
		if err := globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, nil); err != nil {
			releaseUpload()
			t.Fatal(err)
		}
		mutationRead := make(chan struct{}, 1)
		observed := &lifecycleObservedReadLayer{ObjectLayer: source, key: busyOI.Name, entered: mutationRead}
		setObjectLayer(observed)
		removalResult := make(chan error, 1)
		go func() { removalResult <- globalBucketTargetSys.RemoveTarget(ctx, bucket, busyTarget.Arn) }()
		select {
		case <-mutationRead:
		case err := <-removalResult:
			releaseUpload()
			setObjectLayer(source)
			t.Fatal("mutation skipped fresh source read", err)
		case <-time.After(10 * time.Second):
			releaseUpload()
			setObjectLayer(source)
			t.Fatal("mutation could not acquire target namespace during upload")
		}
		select {
		case err := <-removalResult:
			releaseUpload()
			setObjectLayer(source)
			t.Fatal("mutation bypassed source write lock", err)
		case <-time.After(50 * time.Millisecond):
		}
		releaseUpload()
		if err := <-workerResult; err != nil {
			setObjectLayer(source)
			t.Fatal("worker stalled behind target mutation", err)
		}
		if err := <-removalResult; err == nil {
			setObjectLayer(source)
			t.Fatal("mutation removed committed target")
		}
		setObjectLayer(source)
		remote.mu.Lock()
		remote.blockPut = nil
		remote.putEntered = nil
		remote.mu.Unlock()
		lifecycleTestRead(t, source, bucket, busyOI.Name, busyOI.VersionID, []byte("source write held during remote upload"))
		// Lifecycle removal and a new target cache simulate a restart. Durable
		// references alone must keep reads/restores/removal guards functional.
		if err := globalBucketMetadataSys.Update(bucket, bucketLifecycleConfig, nil); err != nil {
			t.Fatal(err)
		}
		savedSys := globalBucketTargetSys
		globalBucketTargetSys = NewBucketTargetSys()
		defer func() {
			globalBucketTargetSys.Lock()
			for _, clients := range globalBucketTargetSys.bucketRemotes {
				for _, client := range clients {
					client.stopHealthCheck()
				}
			}
			globalBucketTargetSys.Unlock()
			globalBucketTargetSys = savedSys
		}()
		lifecycleTestRead(t, source, bucket, oi.Name, oi.VersionID, original)
		lifecycleTestRead(t, source, bucket, oldOI.Name, oldOI.VersionID, oldData)
		if err := globalBucketTargetSys.RemoveTarget(ctx, bucket, current.Arn); err == nil {
			t.Fatal("completed destination removed after rule deletion")
		}
		if err := globalBucketTargetSys.RemoveTarget(ctx, bucket, old.Arn); err == nil {
			t.Fatal("noncurrent destination removed after rule deletion")
		}

		api := objectAPIHandlers{ObjectAPI: func() ObjectLayer { return source }, CacheAPI: func() CacheObjectLayer { return nil }}
		goodBody := []byte(`<RestoreRequest><Days>1</Days></RestoreRequest>`)
		badBody := []byte(`<RestoreRequest><Days>2</Days></RestoreRequest>`)
		badRestore, err := newTestSignedRequestV4(http.MethodPost, "http://source.invalid/"+bucket+"/"+oi.Name+"?restore&versionId="+oi.VersionID, int64(len(goodBody)), bytes.NewReader(goodBody), globalActiveCred.AccessKey, globalActiveCred.SecretKey, nil)
		if err != nil {
			t.Fatal(err)
		}
		badRestore.Body = io.NopCloser(bytes.NewReader(badBody))
		badRestore = setURLVarsOnRequest(badRestore, map[string]string{"bucket": bucket, "object": oi.Name})
		rec := httptest.NewRecorder()
		api.PostRestoreObjectHandler(rec, badRestore)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad signed restore digest got %d: %s", rec.Code, rec.Body.String())
		}
		unchanged, err := source.GetObjectInfo(ctx, bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID})
		if err != nil || unchanged.RestoreOngoing || !unchanged.RestoreExpires.IsZero() {
			t.Fatal("bad digest created restore reservation", err)
		}
		expiry := time.Now().Add(24 * time.Hour).UTC()
		if err := restoreTransitionedObject(ctx, bucket, oi.Name, source, complete, &RestoreObjectRequest{Days: 1}, expiry); err != nil {
			t.Fatal("restore", err)
		}
		restored, err := source.GetObjectInfo(ctx, bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID})
		if err != nil || restored.VersionID != oi.VersionID || restored.ETag != oi.ETag || !restored.ModTime.Equal(oi.ModTime) || restored.UserDefined["x-amz-meta-owner"] != "test-owner" || restored.UserTags != oi.UserTags || restored.TransitionedObject == nil || *restored.TransitionedObject != ref {
			t.Fatalf("restore changed identity or metadata: %+v / %v", restored, err)
		}
		remote.mu.Lock()
		getsBefore := remote.gets
		remote.mu.Unlock()
		lifecycleTestRead(t, source, bucket, oi.Name, oi.VersionID, original)
		remote.mu.Lock()
		getsAfter := remote.gets
		remote.mu.Unlock()
		if getsAfter != getsBefore {
			t.Fatal("restored body still read from remote")
		}

		// Expiring a temporary restored copy drops only local data. The original
		// remote version and durable reference survive and remain readable.
		meta := make(map[string]string)
		for k, v := range restored.UserDefined {
			meta[k] = v
		}
		restoreKey := xhttp.AmzRestore
		for k := range meta {
			if strings.EqualFold(k, xhttp.AmzRestore) {
				restoreKey = k
			}
		}
		removeRestoreHeader(meta)
		meta[restoreKey] = "ongoing-request=false, expiry-date=" + time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
		expired, err := source.PutObjectMetadata(ctx, bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID, MTime: oi.ModTime, UserDefined: meta, TransitionExpected: &restored})
		if err != nil {
			t.Fatal("expire restored metadata", err)
		}
		if err := deleteTransitionedObject(ctx, source, bucket, oi.Name, lifecycleObjectOpts(expired), true, false, expired); err != nil {
			t.Fatalf("expire local restored copy: %v expired=%+v", err, expired)
		}
		retained, err := source.GetObjectInfo(ctx, bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID})
		if err != nil || retained.TransitionedObject == nil || *retained.TransitionedObject != ref {
			t.Fatal("temporary-copy expiration removed remote identity", err)
		}
		if _, err := dest.GetObjectInfo(ctx, currentBucket, ref.Key, ObjectOptions{VersionID: ref.VersionID}); err != nil {
			t.Fatal("temporary expiration deleted remote", err)
		}
		remote.mu.Lock()
		getsBefore = remote.gets
		remote.mu.Unlock()
		lifecycleTestRead(t, source, bucket, oi.Name, oi.VersionID, original)
		remote.mu.Lock()
		getsAfter = remote.gets
		remote.mu.Unlock()
		if getsAfter <= getsBefore {
			t.Fatal("expired restored copy did not return to remote reads")
		}
		// A failed remote deletion cannot discard the source reference; retry
		// targets the exact remote version and only then releases the registry job.
		remote.mu.Lock()
		remote.failDelete = true
		remote.mu.Unlock()
		if _, err := source.DeleteObject(ctx, bucket, oldOI.Name, ObjectOptions{VersionID: oldOI.VersionID, Versioned: true}); err == nil {
			t.Fatal("remote delete failure discarded source")
		}
		lifecycleTestRead(t, source, bucket, oldOI.Name, oldOI.VersionID, oldData)
		remote.mu.Lock()
		remote.failDelete = false
		remote.mu.Unlock()
		if _, err := source.DeleteObject(ctx, bucket, oldOI.Name, ObjectOptions{VersionID: oldOI.VersionID, Versioned: true}); err != nil {
			t.Fatal("remote delete retry", err)
		}
		if _, err := dest.GetObjectInfo(ctx, oldBucket, oldComplete.TransitionedObject.Key, ObjectOptions{VersionID: oldComplete.TransitionedObject.VersionID}); !isErrVersionNotFound(err) && !isErrObjectNotFound(err) {
			t.Fatal("remote version survived source delete", err)
		}
		if err := globalBucketTargetSys.RemoveTarget(ctx, bucket, old.Arn); err != nil {
			t.Fatal("deleted noncurrent job still pins target", err)
		}
	})
}
