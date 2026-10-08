// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"github.com/soulteary/otterio/pkg/hash"
	"github.com/soulteary/otterio/pkg/madmin"
)

func targetTestSetup(t *testing.T) (ObjectLayer, *BucketTargetSys, string) {
	t.Helper()
	savedObj, savedMeta, savedNotify, savedTargets := newObjectLayerFn(), globalBucketMetadataSys, globalNotificationSys, globalBucketTargetSys
	savedGateway := globalIsGateway
	savedVersioning := globalBucketVersioningSys
	ctx, cancel := context.WithCancel(context.Background())
	disks, err := getRandomDisks(4)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	obj, err := newErasureServerPools(ctx, mustGetPoolEndpoints(disks...))
	if err != nil {
		cancel()
		removeRoots(disks)
		t.Fatal(err)
	}
	sys := NewBucketTargetSys()
	t.Cleanup(func() {
		sys.Lock()
		for _, clients := range sys.bucketRemotes {
			for _, client := range clients {
				client.stopHealthCheck()
			}
		}
		sys.Unlock()
		obj.Shutdown(context.Background())
		cancel()
		removeRoots(disks)
		setObjectLayer(savedObj)
		globalBucketMetadataSys, globalNotificationSys, globalBucketTargetSys = savedMeta, savedNotify, savedTargets
		globalIsGateway = savedGateway
		globalBucketVersioningSys = savedVersioning
	})
	setObjectLayer(obj)
	globalIsGateway = false
	globalBucketMetadataSys, globalNotificationSys, globalBucketTargetSys = NewBucketMetadataSys(), nil, sys
	globalBucketVersioningSys = NewBucketVersioningSys()
	const bucket = "target-safety"
	if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	return obj, sys, bucket
}

func targetTestDestination(t *testing.T, label string) madmin.BucketTarget {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet && r.URL.Query().Has("versioning") {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return madmin.BucketTarget{Endpoint: strings.TrimPrefix(server.URL, "http://"), TargetBucket: "destination", Label: label, Type: madmin.ILMService, Region: "us-east-1", Credentials: &auth.Credentials{AccessKey: globalActiveCred.AccessKey, SecretKey: globalActiveCred.SecretKey}}
}

func TestBucketTargetMultipleDestinationsAndImmutableClients(t *testing.T) {
	obj, sys, bucket := targetTestSetup(t)
	ctx := context.Background()
	first, second := targetTestDestination(t, "Cold"), targetTestDestination(t, "Deep")
	first.API = "s3v4"
	first.Path = "auto"
	if err := sys.SetTarget(ctx, bucket, &first, false); err != nil {
		t.Fatal(err)
	}
	if err := sys.SetTarget(ctx, bucket, &second, false); err != nil {
		t.Fatal(err)
	}
	if first.Arn == second.Arn {
		t.Fatal("different endpoints share an ARN")
	}
	_, targets, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil || len(targets.Targets) != 2 {
		t.Fatalf("second ILM target replaced first: %v %+v", err, targets)
	}
	if got := sys.GetRemoteTargetWithLabel(ctx, bucket, "cOlD"); got == nil || got.Arn != first.Arn {
		t.Fatal("case-insensitive label lookup failed")
	}
	duplicate := targetTestDestination(t, "COLD")
	if err := sys.SetTarget(ctx, bucket, &duplicate, false); err == nil {
		t.Fatal("case variant label collision accepted")
	}

	public := sys.ListTargets(ctx, bucket, "")
	var edit madmin.BucketTarget
	for _, entry := range public {
		if entry.Arn == first.Arn {
			edit = entry
		}
	}
	if edit.API != first.API || edit.Path != first.Path || edit.Credentials.SecretKey != "" || edit.Credentials.SessionToken != "" {
		t.Fatal("public clone changed API or exposed credentials")
	}
	edit.BandwidthLimit = 1024
	edit.Label = "cOLD" // label casing is equivalent for matching rules
	if err := sys.SetTarget(ctx, bucket, &edit, true); err != nil {
		t.Fatal("metadata-only edit failed", err)
	}
	_, afterEdit, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil || afterEdit.Targets[0].Credentials.SecretKey != first.Credentials.SecretKey || afterEdit.Targets[0].API != first.API || afterEdit.Targets[0].BandwidthLimit != 1024 {
		t.Fatal("metadata-only edit lost secret or API", err)
	}
	badEdit := publicBucketTarget(edit)
	badEdit.Credentials.AccessKey = "different-access-key"
	if err := sys.SetTarget(ctx, bucket, &badEdit, true); err == nil {
		t.Fatal("metadata-only edit accepted changed access key without secret")
	}
	oldClient := sys.GetRemoteTargetClientForBucket(ctx, bucket, first.Arn)
	if oldClient == nil {
		t.Fatal("missing old client")
	}
	oldSecret := oldClient.config.Credentials.SecretKey
	first.Credentials.SecretKey = "rotated-secret"
	if err := sys.SetTarget(ctx, bucket, &first, true); err != nil {
		t.Fatal(err)
	}
	newClient := sys.GetRemoteTargetClientForBucket(ctx, bucket, first.Arn)
	if newClient == nil || newClient == oldClient || oldClient.config.Credentials.SecretKey != oldSecret || newClient.config.Credentials.SecretKey != first.Credentials.SecretKey {
		t.Fatal("credential rotation mutated an in-flight client")
	}
	listed, _ := sys.ListBucketTargets(ctx, bucket)
	listed.Targets[0].Credentials.SecretKey = "caller-mutation"
	listed.Targets[0].Label = "caller-mutation"
	_, persisted, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil || !strings.EqualFold(persisted.Targets[0].Label, "Cold") || sys.GetRemoteTargetClientForBucket(ctx, bucket, first.Arn).config.Credentials.SecretKey != first.Credentials.SecretKey {
		t.Fatal("list caller changed target state")
	}
	if err := sys.RemoveTarget(ctx, bucket, second.Arn); err != nil {
		t.Fatal(err)
	}
	if sys.GetRemoteTargetClientForBucket(ctx, bucket, second.Arn) != nil {
		t.Fatal("removed destination still routable")
	}
	first.SourceBucket = "another-source"
	if generateARN(&first) == generateARN(&madmin.BucketTarget{SourceBucket: bucket, Endpoint: first.Endpoint, TargetBucket: first.TargetBucket, Type: first.Type, Region: first.Region, Label: first.Label}) {
		t.Fatal("source buckets share generated ARNs")
	}
}

type targetFailSaveDisk struct {
	StorageAPI
	fail *atomic.Bool
}

func (d targetFailSaveDisk) RenameData(ctx context.Context, sv, sp string, fi FileInfo, dv, dp string) error {
	if d.fail.Load() && dv == otterioMetaBucket && strings.HasSuffix(dp, "/"+bucketMetadataFile) {
		return errors.New("injected metadata commit failure")
	}
	return d.StorageAPI.RenameData(ctx, sv, sp, fi, dv, dp)
}

func TestBucketTargetPersistenceBeforePublication(t *testing.T) {
	obj, sys, bucket := targetTestSetup(t)
	backend := obj.(*erasureServerPools).serverPools[0]
	original := backend.GetDisks(0)()
	var fail atomic.Bool
	fail.Store(true)
	backend.erasureDisksMu.Lock()
	for i, disk := range original {
		backend.erasureDisks[0][i] = targetFailSaveDisk{StorageAPI: disk, fail: &fail}
	}
	backend.erasureDisksMu.Unlock()
	defer func() {
		backend.erasureDisksMu.Lock()
		copy(backend.erasureDisks[0], original)
		backend.erasureDisksMu.Unlock()
	}()
	target := targetTestDestination(t, "Cold")
	if err := sys.SetTarget(context.Background(), bucket, &target, false); err == nil {
		t.Fatal("injected metadata save failure succeeded")
	}
	if _, err := sys.ListBucketTargets(context.Background(), bucket); err == nil {
		t.Fatal("uncommitted target published")
	}
	_, targets, err := freshBucketTargets(context.Background(), obj, bucket)
	if err != nil || len(targets.Targets) != 0 {
		t.Fatal("failed update changed targets", err)
	}
	fail.Store(false)
	if err := sys.SetTarget(context.Background(), bucket, &target, false); err != nil {
		t.Fatal(err)
	}
	before := cloneBucketTarget(target)
	target.Endpoint = targetTestDestination(t, "Other").Endpoint
	fail.Store(true)
	if err := sys.SetTarget(context.Background(), bucket, &target, true); err == nil {
		t.Fatal("failed endpoint update succeeded")
	}
	got := sys.GetRemoteTargetClientForBucket(context.Background(), bucket, before.Arn)
	if got == nil || got.config.Endpoint != before.Endpoint {
		t.Fatal("failed update published changed destination")
	}
}

func TestBucketTargetDurableReferencesAndLegacyProtection(t *testing.T) {
	obj, sys, bucket := targetTestSetup(t)
	ctx := context.Background()
	target := targetTestDestination(t, "Cold")
	if err := sys.SetTarget(ctx, bucket, &target, false); err != nil {
		t.Fatal(err)
	}
	ref := &TransitionedObject{ARN: target.Arn, Key: "tier/job", VersionID: mustGetUUID(), StorageClass: target.Label}
	hr, err := hash.NewReader(strings.NewReader("body"), 4, "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	oi, err := obj.PutObject(ctx, bucket, "data/key", NewPutObjReader(hr), ObjectOptions{Versioned: true, MTime: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := pinLifecycleTarget(ctx, obj, bucket, oi, ref); err != nil {
		t.Fatal(err)
	}

	for _, status := range []string{lifecycle.TransitionPending, lifecycle.TransitionComplete} {
		if _, err := obj.PutObjectMetadata(ctx, bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID, TransitionStatus: status, TransitionedObject: ref}); err != nil {
			t.Fatal(err)
		}
		if err := sys.RemoveTarget(ctx, bucket, target.Arn); err == nil {
			t.Fatal("referenced target removed after lifecycle rule deletion")
		}

		changed := cloneBucketTarget(target)
		changed.Endpoint = targetTestDestination(t, "Other").Endpoint
		if err := sys.SetTarget(ctx, bucket, &changed, true); err == nil {
			t.Fatal("referenced physical endpoint changed")
		}
		target.Credentials.SecretKey = "safe-credential-rotation"
		if err := sys.SetTarget(ctx, bucket, &target, true); err != nil {
			t.Fatal("credential rotation blocked", err)
		}
	}
	if _, err := obj.DeleteObject(ctx, bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID, Versioned: true}); err != nil {
		t.Fatal(err)
	}
	if err := sys.RemoveTarget(ctx, bucket, target.Arn); err != nil {
		t.Fatal("deleted source version did not release target", err)
	}
	// A pre-upgrade ILM target has no durable registry. Absence of a listing
	// result cannot prove no transitioned versions exist, so retain its address.
	legacy := targetTestDestination(t, "Legacy")
	legacy.SourceBucket = bucket
	legacy.Arn = generateARN(&legacy)
	data, _ := json.Marshal(&madmin.BucketTargets{Targets: []madmin.BucketTarget{legacy}})
	setObjectLayer(obj)
	if err := globalBucketMetadataSys.Update(bucket, bucketTargetsFile, data); err != nil {
		t.Fatal(err)
	}
	if _, err := obj.DeleteObject(ctx, otterioMetaBucket, lifecycleRegistryKey(bucket), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := sys.RemoveTarget(ctx, bucket, legacy.Arn); err == nil {
		t.Fatal("legacy target removed without proving object history")
	}
	legacy.Credentials.SecretKey = "legacy-rotation"
	if err := sys.SetTarget(ctx, bucket, &legacy, true); err != nil {
		t.Fatal("legacy credential rotation blocked", err)
	}
}

func TestBucketTargetRegistryCorruptionAndBounds(t *testing.T) {
	obj, _, bucket := targetTestSetup(t)
	ctx := context.Background()
	for _, raw := range []string{
		`{"version":1,"jobs":{},"jobs":{}}`,
		`{"version":1,"Version":1,"jobs":{}}`,
		`{"version":1,"jobs":{"forged":{"sourceKey":"key","sourceVersionID":"","modTime":"2026-01-01T00:00:00Z","etag":"e","arn":"x","remoteKey":"r"}}}`,
		`{"version":2,"jobs":{}}`,
	} {
		if err := saveConfig(ctx, obj, lifecycleRegistryKey(bucket), []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readLifecycleTargetRegistry(ctx, obj, bucket, nil); err == nil {
			t.Fatal("corrupt registry accepted", raw)
		}
	}
	registry := lifecycleTargetRegistry{Version: 1, Jobs: make(map[string]lifecycleTargetPin)}
	for i := 0; i <= lifecycleTargetRegistryMaxJobs; i++ {
		registry.Jobs[strconv.Itoa(i)] = lifecycleTargetPin{}
	}
	if err := saveLifecycleTargetRegistry(ctx, obj, bucket, registry); err == nil {
		t.Fatal("unbounded registry accepted")
	}
	oversized := bytes.Repeat([]byte("x"), lifecycleTargetRegistryMaxSize+1)
	reader, err := hash.NewReader(bytes.NewReader(oversized), int64(len(oversized)), "", "", int64(len(oversized)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := obj.PutObject(ctx, otterioMetaBucket, lifecycleRegistryKey(bucket), NewPutObjReader(reader), ObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readLifecycleTargetRegistry(ctx, obj, bucket, nil); err == nil {
		t.Fatal("oversized registry accepted")
	}
}

func TestBucketTargetLegacyARNCollisionUsesSourceBucket(t *testing.T) {
	obj, sys, bucket := targetTestSetup(t)
	const other = "other-target-source"
	ctx := context.Background()
	if err := obj.MakeBucketWithLocation(ctx, other, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	first, second := targetTestDestination(t, "Cold"), targetTestDestination(t, "Cold")
	first.SourceBucket = bucket
	first.Arn = generateARN(&first)
	second.SourceBucket = other
	second.Arn = first.Arn // historical hash ignored endpoint/source
	for _, entry := range []struct {
		bucket string
		target madmin.BucketTarget
	}{{bucket, first}, {other, second}} {
		targets := &madmin.BucketTargets{Targets: []madmin.BucketTarget{entry.target}}
		data, _ := json.Marshal(targets)
		if err := globalBucketMetadataSys.Update(entry.bucket, bucketTargetsFile, data); err != nil {
			t.Fatal(err)
		}
		sys.UpdateAllTargets(entry.bucket, targets)
	}
	one, two := sys.GetRemoteTargetClientForBucket(ctx, bucket, first.Arn), sys.GetRemoteTargetClientForBucket(ctx, other, second.Arn)
	if one == nil || two == nil || one == two || one.config.Endpoint != first.Endpoint || two.config.Endpoint != second.Endpoint {
		t.Fatal("legacy ARN collision crossed source buckets")
	}
	// Delayed peer publication must not win over the durable physical address.
	sys.UpdateAllTargets(bucket, &madmin.BucketTargets{Targets: []madmin.BucketTarget{second}})
	if got := sys.GetRemoteTargetClientForBucket(ctx, bucket, first.Arn); got == nil || got.config.Endpoint != first.Endpoint {
		t.Fatal("stale cache publication routed to another destination")
	}
	sys.UpdateAllTargets(bucket, nil)
	if got := sys.GetRemoteTargetClientForBucket(ctx, other, second.Arn); got == nil || got.config.Endpoint != second.Endpoint {
		t.Fatal("cache removal removed another source's client")
	}
}

type targetGCReadLayer struct {
	ObjectLayer
	heldKey                  string
	reads                    int
	ownNoLock, otherReadLock bool
	failure                  error
}

func (o *targetGCReadLayer) GetObjectInfo(_ context.Context, bucket, key string, opts ObjectOptions) (ObjectInfo, error) {
	o.reads++
	if key == o.heldKey {
		if !opts.NoLock {
			return ObjectInfo{}, errors.New("recursive object lock")
		}
		o.ownNoLock = true
	} else {
		if opts.NoLock {
			return ObjectInfo{}, errors.New("other object read is unlocked")
		}
		o.otherReadLock = true
	}
	if o.failure != nil {
		return ObjectInfo{}, o.failure
	}
	return ObjectInfo{}, VersionNotFound{Bucket: bucket, Object: key, VersionID: opts.VersionID}
}

func targetRegistryTestIdentity(pin lifecycleTargetPin) string {
	raw, _ := json.Marshal(pin)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestBucketTargetRegistryReclaimsDeletedHistory(t *testing.T) {
	obj, sys, bucket := targetTestSetup(t)
	ctx := context.Background()
	target := targetTestDestination(t, "Cold")
	if err := sys.SetTarget(ctx, bucket, &target, false); err != nil {
		t.Fatal(err)
	}
	oi := ObjectInfo{Bucket: bucket, Name: "held-key", VersionID: mustGetUUID(), ETag: "current-etag", ModTime: time.Now().UTC()}
	ref := &TransitionedObject{ARN: target.Arn, Key: "new-job", VersionID: mustGetUUID()}
	registry := lifecycleTargetRegistry{Version: 1, Jobs: make(map[string]lifecycleTargetPin)}
	for i := 0; i < lifecycleTargetRegistryMaxJobs*3/4; i++ {
		pin := lifecycleTargetPin{SourceKey: "deleted/" + strconv.Itoa(i), SourceVersionID: mustGetUUID(), ModTime: oi.ModTime, ETag: "old", ARN: target.Arn, RemoteKey: "old/" + strconv.Itoa(i)}
		registry.Jobs[targetRegistryTestIdentity(pin)] = pin
	}
	if err := saveLifecycleTargetRegistry(ctx, obj, bucket, registry); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readLifecycleTargetRegistry(ctx, obj, bucket, nil); err != nil {
		r, rerr := obj.GetObjectNInfo(ctx, otterioMetaBucket, lifecycleRegistryKey(bucket), nil, http.Header{}, readLock, ObjectOptions{})
		if rerr != nil {
			t.Fatal("raw registry open", rerr)
		}
		raw, rerr := io.ReadAll(r)
		r.Close()
		fi, ferr := obj.(*erasureServerPools).serverPools[0].GetDisks(0)()[0].ReadVersion(ctx, otterioMetaBucket, lifecycleRegistryKey(bucket), "", true)
		t.Fatalf("historical index: err=%T %#v rawlen=%d size=%d rawerr=%v fi=%+v ferr=%v", err, err, len(raw), r.ObjInfo.Size, rerr, fi, ferr)
	}
	wrapped := &targetGCReadLayer{ObjectLayer: obj, heldKey: oi.Name}
	lock := obj.NewNSLock(bucket, oi.Name)
	locked, err := lock.GetLock(ctx, newDynamicTimeout(time.Second, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := pinLifecycleTarget(locked, wrapped, bucket, oi, ref); err != nil {
		lock.Unlock()
		t.Fatal("full historical index blocked a new job", err)
	}
	lock.Unlock()
	after, _, err := readLifecycleTargetRegistry(ctx, obj, bucket, nil)
	if err != nil || len(after.Jobs) >= len(registry.Jobs) || len(after.Jobs) == 0 {
		t.Fatal("historical jobs were not reclaimed", err, len(after.Jobs))
	}
	current := lifecycleTargetPin{SourceKey: oi.Name, SourceVersionID: oi.VersionID, ModTime: oi.ModTime, ETag: oi.ETag, ARN: ref.ARN, RemoteKey: ref.Key}
	if _, ok := after.Jobs[targetRegistryTestIdentity(current)]; !ok {
		t.Fatal("GC removed the job registered before its pending write")
	}
	// Versions share one source key lock. An old version of the current key
	// must not recursively acquire it; another key must still take a read lock.
	old := current
	old.SourceVersionID = mustGetUUID()
	old.RemoteKey = "old-held-job"
	other := old
	other.SourceKey = "another-key"
	other.RemoteKey = "other-job"
	small := lifecycleTargetRegistry{Version: 1, Jobs: map[string]lifecycleTargetPin{targetRegistryTestIdentity(old): old, targetRegistryTestIdentity(other): other}}
	if err := reclaimLifecycleTargetPins(ctx, wrapped, bucket, &small, oi.Name, ""); err != nil {
		t.Fatal(err)
	}
	if !wrapped.ownNoLock || !wrapped.otherReadLock || len(small.Jobs) != 0 {
		t.Fatal("GC did not respect source key lock ownership")
	}
	// An unavailable source is uncertainty, never proof that its pin is stale.
	wrapped.failure = errors.New("injected read quorum failure")
	unknown := lifecycleTargetRegistry{Version: 1, Jobs: map[string]lifecycleTargetPin{targetRegistryTestIdentity(other): other}}
	if err := reclaimLifecycleTargetPins(ctx, wrapped, bucket, &unknown, oi.Name, ""); err == nil || len(unknown.Jobs) != 1 {
		t.Fatal("GC removed a job on uncertain source read")
	}
}

func TestBucketTargetLifecycleCapabilityFilesystem(t *testing.T) {
	saved := newObjectLayerFn()
	fs, dir, err := prepareFS(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { setObjectLayer(saved); fs.Shutdown(context.Background()); removeRoots([]string{dir}) }()
	setObjectLayer(fs)
	if lifecycleTransitionSupported(fs) {
		t.Fatal("filesystem advertised durable tier support")
	}
	if !globalIsErasure {
		req, err := newTestSignedRequestV4(http.MethodGet, "http://source.invalid/otterio/admin/v3/list-remote-targets?bucket=filesystem", 0, nil, globalActiveCred.AccessKey, globalActiveCred.SecretKey, nil)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		(adminAPIHandlers{}).ListRemoteTargetsHandler(recorder, req)
		if recorder.Header().Get("X-Otterio-Lifecycle-Transition") != "" {
			t.Fatal("filesystem capability header disclosed")
		}
	}
}

func TestBucketTargetUnsupportedTopologyAndCanceledMutation(t *testing.T) {
	obj, sys, bucket := targetTestSetup(t)
	target := targetTestDestination(t, "Cold")
	for _, unsupported := range []ObjectLayer{&FSObjects{}, &struct{ ObjectLayer }{ObjectLayer: obj}, &erasureServerPools{serverPools: []*erasureSets{{}, {}}}} {
		setObjectLayer(unsupported)
		if err := sys.SetTarget(context.Background(), bucket, &target, false); err == nil {
			t.Fatal("unsupported ILM topology accepted")
		}
	}
	setObjectLayer(obj)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sys.SetTarget(ctx, bucket, &target, false); err == nil {
		t.Fatal("canceled target mutation succeeded")
	}
	if _, err := sys.ListBucketTargets(context.Background(), bucket); err == nil {
		t.Fatal("rejected target published")
	}
	_, targets, err := freshBucketTargets(context.Background(), obj, bucket)
	if err != nil || len(targets.Targets) != 0 {
		t.Fatal("rejected topology/cancellation changed target config", err)
	}
	if _, _, err := readLifecycleTargetRegistry(context.Background(), obj, bucket, nil); err != nil {
		t.Fatal(err)
	}
}
