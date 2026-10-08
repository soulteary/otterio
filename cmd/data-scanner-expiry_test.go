// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"testing"
	"time"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/bucket/versioning"
)

type expiryFreshObjectLayer struct {
	ObjectLayer
	fresh                ObjectInfo
	locks                *nsLockMap
	reads, deletes       int
	readOpts, deleteOpts ObjectOptions
	metadata             []byte
	deleteErr            error
	targetReadEntered    chan struct{}
}

type expiryObservedTargetLock struct {
	RWLocker
	entered chan struct{}
}

func (l expiryObservedTargetLock) GetRLock(ctx context.Context, timeout *dynamicTimeout) (context.Context, error) {
	l.entered <- struct{}{}
	return l.RWLocker.GetRLock(ctx, timeout)
}

func (o *expiryFreshObjectLayer) GetObjectNInfo(_ context.Context, bucket, key string, _ *HTTPRangeSpec, _ http.Header, _ LockType, _ ObjectOptions) (*GetObjectReader, error) {
	if bucket != otterioMetaBucket || key != pathJoin(bucketConfigPrefix, "expiry-bucket", bucketMetadataFile) {
		return nil, ObjectNotFound{Bucket: bucket, Object: key}
	}
	return NewGetObjectReaderFromReader(bytes.NewReader(o.metadata), ObjectInfo{}, ObjectOptions{})
}

func expiryTestMetadata(t *testing.T, lc *lifecycle.Lifecycle, status versioning.State) BucketMetadata {
	t.Helper()
	meta := newBucketMetadata("expiry-bucket")
	meta.lifecycleConfig, meta.versioningConfig.Status = lc, status
	var err error
	if lc != nil {
		encoded := *lc
		encoded.Rules = append([]lifecycle.Rule(nil), lc.Rules...)
		for i := range encoded.Rules {
			rule := &encoded.Rules[i]
			if !rule.Expiration.IsNull() {
				type expirationFields lifecycle.Expiration
				raw, marshalErr := xml.Marshal(struct {
					XMLName xml.Name `xml:"Expiration"`
					expirationFields
				}{expirationFields: expirationFields(rule.Expiration)})
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				if err = xml.Unmarshal(raw, &rule.Expiration); err != nil {
					t.Fatal(err)
				}
			}
		}
		meta.LifecycleConfigXML, err = xml.Marshal(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
	if status != "" {
		meta.VersioningConfigXML, err = xml.Marshal(meta.versioningConfig)
		if err != nil {
			t.Fatal(err)
		}
	}
	return meta
}

func expiryTestMockMetadata(t *testing.T, obj *expiryFreshObjectLayer, meta BucketMetadata) {
	t.Helper()
	data := make([]byte, 4)
	binary.LittleEndian.PutUint16(data[:2], bucketMetadataFormat)
	binary.LittleEndian.PutUint16(data[2:], bucketMetadataVersion)
	var err error
	obj.metadata, err = meta.MarshalMsg(data)
	if err != nil {
		t.Fatal(err)
	}
}

func (o *expiryFreshObjectLayer) NewNSLock(bucket string, names ...string) RWLocker {
	lk := o.locks.NewNSLock(nil, bucket, names...)
	if bucket == otterioMetaBucket && o.targetReadEntered != nil {
		return expiryObservedTargetLock{RWLocker: lk, entered: o.targetReadEntered}
	}
	return lk
}

func (o *expiryFreshObjectLayer) GetObjectInfo(_ context.Context, _, _ string, opts ObjectOptions) (ObjectInfo, error) {
	o.reads++
	o.readOpts = opts
	return o.fresh, nil
}

func (o *expiryFreshObjectLayer) DeleteObject(_ context.Context, _, _ string, opts ObjectOptions) (ObjectInfo, error) {
	o.deletes++
	o.deleteOpts = opts
	return o.fresh, o.deleteErr
}

func expiryTestDeletionIntent(t *testing.T, oi ObjectInfo) ObjectInfo {
	t.Helper()
	oi.TransitionStatus, oi.TransitionedObject = lifecycle.TransitionComplete, transitionStorageReference()
	oi.UserDefined = cloneMSS(oi.UserDefined)
	if oi.UserDefined == nil {
		oi.UserDefined = make(map[string]string)
	}
	data, err := json.Marshal(oi.TransitionedObject)
	if err != nil {
		t.Fatal(err)
	}
	oi.UserDefined[transitionDeleteIntentKey] = string(data)
	return oi
}

func TestExpiryPendingDeletionCompletesExactVersionAfterPolicyChange(t *testing.T) {
	for _, version := range []string{"old", nullVersionID, ""} {
		for _, policy := range []struct {
			name string
			lc   *lifecycle.Lifecycle
		}{
			{"removed", nil},
			{"disabled", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Disabled, Expiration: lifecycle.Expiration{Days: 1}}}}},
			{"delayed", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 60}}}}},
		} {
			t.Run(version+"-"+policy.name, func(t *testing.T) {
				oi := expiryTestObject()
				oi.VersionID, oi.UserDefined = version, map[string]string{xhttp.AmzObjectLockLegalHold: "ON"}
				oi = expiryTestDeletionIntent(t, oi)
				obj := &expiryFreshObjectLayer{fresh: oi, locks: newNSLock(false)}
				expiryTestGlobals(t, obj, policy.lc, versioning.Enabled)
				// Even a queued restored-copy expiry must finish a later permanent
				// delete intent rather than leave its absent remote authority behind.
				if applied, err := executeExpiry(context.Background(), obj, oi, true, false, &oi); !applied || err != nil {
					t.Fatalf("an already-started permanent delete was stranded by policy/retention: applied=%v error=%v", applied, err)
				}
				wantVersion := version
				if wantVersion == "" {
					wantVersion = nullVersionID
				}
				if obj.deletes != 1 || obj.readOpts.VersionID != wantVersion || obj.deleteOpts.VersionID != wantVersion ||
					!obj.deleteOpts.NoLock || obj.deleteOpts.Versioned || obj.deleteOpts.VersionSuspended ||
					obj.deleteOpts.TransitionStatus != "" || obj.deleteOpts.TransitionExpected == nil ||
					*obj.deleteOpts.TransitionExpected.TransitionedObject != *oi.TransitionedObject {
					t.Fatalf("pending permanent delete created a marker, selected latest or lost its source fence: %+v reads=%+v", obj.deleteOpts, obj.readOpts)
				}
			})
		}
	}
}

func TestExpiryPendingDeletionRetainsCorruptOrReplacedSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ObjectInfo)
	}{
		{"corrupt-json", func(oi *ObjectInfo) { oi.UserDefined[transitionDeleteIntentKey] = "{" }},
		{"different-locator", func(oi *ObjectInfo) {
			ref := *oi.TransitionedObject
			ref.Key += "changed"
			oi.TransitionedObject = &ref
		}},
		{"missing-locator", func(oi *ObjectInfo) { oi.TransitionedObject = nil }},
		{"same-time-replacement-etag", func(oi *ObjectInfo) { oi.ETag = "replacement" }},
		{"same-time-replacement-size", func(oi *ObjectInfo) { oi.Size++ }},
		{"different-source-version", func(oi *ObjectInfo) { oi.VersionID = "replacement-version" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queued := expiryTestDeletionIntent(t, expiryTestObject())
			fresh := queued
			fresh.UserDefined = cloneMSS(queued.UserDefined)
			tc.change(&fresh)
			obj := &expiryFreshObjectLayer{fresh: fresh, locks: newNSLock(false)}
			expiryTestGlobals(t, obj, nil, versioning.Enabled)
			if applied, err := executeExpiry(context.Background(), obj, queued, false, true, &queued); applied || err == nil || obj.deletes != 0 {
				t.Fatalf("corrupt/replaced source was deleted: applied=%v error=%v deletes=%d", applied, err, obj.deletes)
			}
		})
	}
}

func TestExpiryPendingDeletionLocalFailureRemainsQueuedWithoutRules(t *testing.T) {
	oi := expiryTestDeletionIntent(t, expiryTestObject())
	obj := &expiryFreshObjectLayer{fresh: oi, locks: newNSLock(false), deleteErr: errors.New("local write quorum failed")}
	expiryTestGlobals(t, obj, nil, versioning.Enabled)
	if applied, err := executeExpiry(context.Background(), obj, oi, false, true, &oi); applied || err == nil || obj.deletes != 1 {
		t.Fatalf("failed local commit was confirmed: applied=%v error=%v deletes=%d", applied, err, obj.deletes)
	}
	savedExpiry := globalExpiryState
	t.Cleanup(func() { globalExpiryState = savedExpiry })
	globalExpiryState = &expiryState{expiryCh: make(chan expiryTask, 1)}
	item := scannerItem{bucket: oi.Bucket, prefix: "logs", objectName: "item"}
	if applied, size := item.applyLifecycle(context.Background(), obj, actionMeta{oi: obj.fresh}); !applied || size != oi.Size || obj.deletes != 1 {
		t.Fatalf("failed permanent deletion lost its recovery work or accounting: applied=%v size=%d deletes=%d", applied, size, obj.deletes)
	}
	select {
	case queued := <-globalExpiryState.expiryCh:
		if !queued.versionExpiry || queued.objInfo.VersionID != oi.VersionID || queued.objInfo.UserDefined[transitionDeleteIntentKey] != oi.UserDefined[transitionDeleteIntentKey] {
			t.Fatalf("retry queue lost permanent deletion identity: %+v", queued)
		}
	default:
		t.Fatal("failed permanent deletion was not queued after rule removal")
	}
}

func expiryTestGlobals(t *testing.T, obj ObjectLayer, lc *lifecycle.Lifecycle, status versioning.State) {
	t.Helper()
	savedObject, savedMetadata, savedLifecycle, savedGateway := newObjectLayerFn(), globalBucketMetadataSys, globalLifecycleSys, globalIsGateway
	t.Cleanup(func() {
		setObjectLayer(savedObject)
		globalBucketMetadataSys, globalLifecycleSys, globalIsGateway = savedMetadata, savedLifecycle, savedGateway
	})
	setObjectLayer(obj)
	globalIsGateway = false
	globalLifecycleSys = NewLifecycleSys()
	globalBucketMetadataSys = NewBucketMetadataSys()
	meta := expiryTestMetadata(t, lc, status)
	globalBucketMetadataSys.Set(meta.Name, meta)
	if mock, ok := obj.(*expiryFreshObjectLayer); ok {
		expiryTestMockMetadata(t, mock, meta)
	}
}

func expiryTestObject() ObjectInfo {
	return ObjectInfo{Bucket: "expiry-bucket", Name: "logs/item", VersionID: "old", IsLatest: true,
		ModTime: time.Now().Add(-30 * 24 * time.Hour), ETag: "original", Size: 23, NumVersions: 1}
}

func TestExpiryQueuedActionRechecksRuleAndSource(t *testing.T) {
	for _, tiered := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			lc     *lifecycle.Lifecycle
			change func(*ObjectInfo)
		}{
			{"rule-removed", nil, func(*ObjectInfo) {}},
			{"deadline-delayed", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 60}}}}, func(*ObjectInfo) {}},
			{"tags-changed", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Filter: lifecycle.Filter{Tag: lifecycle.Tag{Key: "environment", Value: "prod"}}, Expiration: lifecycle.Expiration{Days: 1}}}}, func(o *ObjectInfo) { o.UserTags = "environment=dev" }},
			{"current-became-noncurrent", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{NoncurrentDays: 1}}}}, func(o *ObjectInfo) { o.IsLatest = false; o.SuccessorModTime = time.Now().Add(-10 * 24 * time.Hour) }},
			{"same-time-replacement", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 1}}}}, func(o *ObjectInfo) { o.ETag = "replacement"; o.Size++ }},
		} {
			name := tc.name
			if tiered {
				name += "-tiered"
			}
			t.Run(name, func(t *testing.T) {
				queued := expiryTestObject()
				queued.UserTags = "environment=prod"
				if tiered {
					queued.TransitionStatus = lifecycle.TransitionComplete
					queued.TransitionedObject = transitionStorageReference()
				}
				fresh := queued
				tc.change(&fresh)
				obj := &expiryFreshObjectLayer{fresh: fresh, locks: newNSLock(false)}
				expiryTestGlobals(t, obj, tc.lc, versioning.Enabled)
				if applyExpiryRule(context.Background(), obj, queued, false, false) || obj.deletes != 0 {
					t.Fatalf("old queued verdict deleted fresh object: reads=%d deletes=%d opts=%+v", obj.reads, obj.deletes, obj.deleteOpts)
				}
			})
		}
	}
}

func TestExpiryCurrentVersionCreatesMarker(t *testing.T) {
	oi := expiryTestObject()
	oi.TransitionStatus, oi.TransitionedObject = lifecycle.TransitionComplete, transitionStorageReference()
	obj := &expiryFreshObjectLayer{fresh: oi, locks: newNSLock(false)}
	expiryTestGlobals(t, obj, &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 1}}}}, versioning.Enabled)
	if applied, err := executeExpiry(context.Background(), obj, oi, false, false, &oi); !applied || obj.deleteOpts.VersionID != "" || !obj.deleteOpts.Versioned {
		t.Fatalf("current version expiry became permanent deletion: %+v / %v", obj.deleteOpts, err)
	}
}

func TestExpiryIgnoresStaleCachedConfiguration(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable-rule-removed", true: "durable-config-unreadable"}[malformed], func(t *testing.T) {
			oi := expiryTestObject()
			obj := &expiryFreshObjectLayer{fresh: oi, locks: newNSLock(false)}
			lc := &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 1}}}}
			expiryTestGlobals(t, obj, lc, versioning.Enabled)
			expiryTestMockMetadata(t, obj, expiryTestMetadata(t, nil, versioning.Enabled))
			if malformed {
				obj.metadata = []byte("corrupt metadata")
			}
			if applyExpiryRule(context.Background(), obj, oi, false, false) || obj.deletes != 0 {
				t.Fatal("cached expiration authorized deletion despite changed/unreadable durable config")
			}
		})
	}
}

func TestExpiryFreshRetentionAndRestoreDeadline(t *testing.T) {
	for _, localRestore := range []bool{false, true} {
		t.Run(map[bool]string{false: "permanent-retention", true: "renewed-restore"}[localRestore], func(t *testing.T) {
			queued := expiryTestObject()
			queued.IsLatest, queued.SuccessorModTime = false, time.Now().Add(-10*24*time.Hour)
			queued.TransitionStatus, queued.TransitionedObject = lifecycle.TransitionComplete, transitionStorageReference()
			queued.RestoreExpires = time.Now().Add(-time.Hour)
			fresh := queued
			fresh.UserDefined = map[string]string{xhttp.AmzObjectLockLegalHold: "ON"}
			if localRestore {
				fresh.RestoreExpires = time.Now().Add(time.Hour)
			}
			obj := &expiryFreshObjectLayer{fresh: fresh, locks: newNSLock(false)}
			expiryTestGlobals(t, obj, &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{NoncurrentDays: 1}}}}, versioning.Enabled)
			if applyExpiryRule(context.Background(), obj, queued, localRestore, true) || obj.deletes != 0 {
				t.Fatalf("fresh retention/restore state was ignored: deletes=%d", obj.deletes)
			}
		})
	}
}

func TestExpiryWaitsForRuleCommitBeforeSourceLock(t *testing.T) {
	oi := expiryTestObject()
	obj := &expiryFreshObjectLayer{fresh: oi, locks: newNSLock(false), targetReadEntered: make(chan struct{}, 1)}
	lc := &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 1}}}}
	expiryTestGlobals(t, obj, lc, versioning.Enabled)
	target := lifecycleTargetLock(obj, oi.Bucket)
	ctx, err := target.GetLock(context.Background(), globalOperationTimeout)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { done <- applyExpiryRule(ctx, obj, oi, false, false) }()
	select {
	case <-obj.targetReadEntered:
	case <-time.After(time.Second):
		target.Unlock()
		t.Fatal("expiry never attempted target read lock")
	}
	// While configuration owns the target lock, expiry must not own the
	// source lock. It must see the committed rule removal after waiting.
	source := obj.NewNSLock(oi.Bucket, encodeDirObject(oi.Name))
	short, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = source.GetLock(short, globalOperationTimeout); err != nil {
		target.Unlock()
		t.Fatalf("expiry took source before target: %v", err)
	}
	meta := expiryTestMetadata(t, nil, versioning.Enabled)
	expiryTestMockMetadata(t, obj, meta)
	globalBucketMetadataSys.Set(oi.Bucket, meta)
	source.Unlock()
	target.Unlock()
	if <-done || obj.deletes != 0 || obj.reads != 1 {
		t.Fatal("expired using the configuration from before the locked commit")
	}
}

func TestExpiryRestoredCopyUsesFreshDeadlineWithoutRules(t *testing.T) {
	for _, state := range []string{"expired", "renewed", "ongoing", "incomplete"} {
		t.Run(state, func(t *testing.T) {
			oi := expiryTestObject()
			oi.TransitionStatus, oi.TransitionedObject = lifecycle.TransitionComplete, transitionStorageReference()
			oi.RestoreExpires = time.Now().Add(-time.Hour)
			oi.UserDefined = map[string]string{xhttp.AmzObjectLockLegalHold: "ON"}
			fresh := oi
			switch state {
			case "renewed":
				fresh.RestoreExpires = time.Now().Add(time.Hour)
			case "ongoing":
				fresh.RestoreOngoing = true
			case "incomplete":
				fresh.TransitionStatus = lifecycle.TransitionPending
			}
			obj := &expiryFreshObjectLayer{fresh: fresh, locks: newNSLock(false)}
			expiryTestGlobals(t, obj, nil, versioning.Enabled)
			if got := applyExpiryRule(context.Background(), obj, oi, true, true); got != (state == "expired") {
				t.Fatalf("persisted restore deadline applied=%v state=%s", got, state)
			}
			if state == "expired" && (obj.deleteOpts.VersionID != oi.VersionID || obj.deleteOpts.TransitionStatus != lifecycle.TransitionComplete || obj.deleteOpts.TransitionExpected == nil || !obj.deleteOpts.NoLock) {
				t.Fatalf("local cache expiry changed remote authority: %+v", obj.deleteOpts)
			}
		})
	}
}

func TestExpiryRealErasureMarkerAndSuspendedNull(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, disks, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(ctx)
	defer removeRoots(disks)
	lc := &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 1}}}}
	expiryTestGlobals(t, obj, lc, versioning.Enabled)
	const bucket = "expiry-bucket"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{VersioningEnabled: true}); err != nil {
		t.Fatal(err)
	}
	setConfig := func(status versioning.State) {
		meta := expiryTestMetadata(t, lc, status)
		if err = meta.Save(ctx, obj); err != nil {
			t.Fatal(err)
		}
		globalBucketMetadataSys.Set(bucket, meta)
	}
	setConfig(versioning.Enabled)
	putTiered := func(key string, opts ObjectOptions) ObjectInfo {
		data := []byte("retained remote authority")
		opts.MTime = time.Now().Add(-30 * 24 * time.Hour)
		oi, perr := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), opts)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, status := range []string{lifecycle.TransitionPending, lifecycle.TransitionComplete} {
			if _, perr = obj.DeleteObject(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID, TransitionStatus: status, TransitionExpected: &oi, TransitionedObject: transitionStorageReference()}); perr != nil {
				t.Fatal(perr)
			}
		}
		oi, perr = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID})
		if perr != nil {
			t.Fatal(perr)
		}
		return oi
	}
	current := putTiered("current", ObjectOptions{Versioned: true})
	for _, changed := range []*lifecycle.Lifecycle{
		nil,
		{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 60}}}},
		{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Filter: lifecycle.Filter{Tag: lifecycle.Tag{Key: "environment", Value: "prod"}}, Expiration: lifecycle.Expiration{Days: 1}}}},
	} {
		// Simulate a durable rule edit whose peer cache notification was lost.
		meta := expiryTestMetadata(t, changed, versioning.Enabled)
		if err = meta.Save(ctx, obj); err != nil {
			t.Fatal(err)
		}
		if applyExpiryRule(ctx, obj, current, false, false) {
			t.Fatal("real backend used cached rule after durable rule removal/edit")
		}
		if _, err = obj.GetObjectInfo(ctx, bucket, current.Name, ObjectOptions{VersionID: current.VersionID}); err != nil {
			t.Fatal("durable rule edit lost remote source authority", err)
		}
	}
	setConfig(versioning.Enabled)
	if applied, err := executeExpiry(ctx, obj, current, false, false, &current); !applied {
		t.Fatal("current version expiry attempted permanent tier cleanup", err)
	}
	marker, merr := obj.GetObjectInfo(ctx, bucket, current.Name, ObjectOptions{})
	if !isErrObjectNotFound(merr) || !marker.DeleteMarker {
		t.Fatalf("current expiry did not create a marker: %+v / %v", marker, merr)
	}
	retained, rerr := obj.GetObjectInfo(ctx, bucket, current.Name, ObjectOptions{VersionID: current.VersionID})
	if rerr != nil || retained.IsLatest || retained.TransitionedObject == nil {
		t.Fatalf("current expiry discarded its version/ref: %+v / %v", retained, rerr)
	}
	setConfig(versioning.Suspended)
	for _, operation := range []string{"single", "bulk", "expiry"} {
		t.Run("suspended-null-"+operation, func(t *testing.T) {
			oi := putTiered(operation, ObjectOptions{VersionSuspended: true})
			switch operation {
			case "single":
				_, err = obj.DeleteObject(ctx, bucket, oi.Name, ObjectOptions{VersionSuspended: true})
			case "bulk":
				_, errs := obj.DeleteObjects(ctx, bucket, []ObjectToDelete{{ObjectName: oi.Name}}, ObjectOptions{VersionSuspended: true})
				err = errs[0]
			case "expiry":
				if applyExpiryRule(ctx, obj, oi, false, false) {
					t.Fatal("expiry dropped null source without cleaning its tier")
				}
				err = errors.New("expected conservative tier cleanup failure")
			}
			if err == nil {
				t.Fatal("null marker replacement ignored failed tier cleanup")
			}
			fresh, ferr := obj.GetObjectInfo(ctx, bucket, oi.Name, ObjectOptions{VersionID: nullVersionID})
			if ferr != nil || fresh.DeleteMarker || fresh.TransitionedObject == nil || fresh.ETag != oi.ETag {
				t.Fatalf("failed tier cleanup lost null recovery authority: %+v / %v", fresh, ferr)
			}
		})
	}
	named := putTiered("suspended-named", ObjectOptions{Versioned: true})
	if !applyExpiryRule(ctx, obj, named, false, false) {
		t.Fatal("suspended named version expiry tried to clean its retained tier")
	}
	marker, merr = obj.GetObjectInfo(ctx, bucket, named.Name, ObjectOptions{})
	if !isErrObjectNotFound(merr) || !marker.DeleteMarker || marker.VersionID != nullVersionID {
		t.Fatalf("suspended expiry did not create null marker: %+v / %v", marker, merr)
	}
	if _, rerr = obj.GetObjectInfo(ctx, bucket, named.Name, ObjectOptions{VersionID: named.VersionID}); rerr != nil {
		t.Fatalf("suspended marker purged named source: %v", rerr)
	}
}

func TestExpiryRealErasureExactVersionAndMarker(t *testing.T) {
	skipIfWindowsErasureExec(t)
	ctx := context.Background()
	obj, disks, err := prepareErasure(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Shutdown(ctx)
	defer removeRoots(disks)
	lc := &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{NoncurrentDays: 1}}}}
	expiryTestGlobals(t, obj, lc, versioning.Enabled)
	const bucket, key = "expiry-bucket", "exact"
	if err = obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{VersioningEnabled: true}); err != nil {
		t.Fatal(err)
	}
	meta := expiryTestMetadata(t, lc, versioning.Enabled)
	if err = meta.Save(ctx, obj); err != nil {
		t.Fatal(err)
	}
	globalBucketMetadataSys.Set(bucket, meta)
	data := []byte("old version")
	oi, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{Versioned: true, MTime: time.Now().Add(-30 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := obj.DeleteObject(ctx, bucket, key, ObjectOptions{Versioned: true, MTime: time.Now().Add(-10 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	old, err := obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID})
	if err != nil {
		t.Fatal(err)
	}
	if !applyExpiryRule(ctx, obj, old, false, true) {
		t.Fatal("due exact noncurrent version was not removed")
	}
	if _, err = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID}); !isErrVersionNotFound(err) {
		t.Fatalf("exact old version remains: %v", err)
	}
	remaining, err := obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: marker.VersionID})
	if _, allowed := err.(MethodNotAllowed); !allowed || !remaining.DeleteMarker || remaining.NumVersions != 1 {
		t.Fatalf("expected one remaining marker: %+v / %v", remaining, err)
	}
	if !applyExpiryRule(ctx, obj, remaining, false, true) {
		t.Fatal("fresh expired sole marker was not removed")
	}
	if _, err = obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{}); !isErrObjectNotFound(err) {
		t.Fatalf("expired sole marker remains: %v", err)
	}

	// An old unversioned queue entry cannot delete an overwritten object even
	// when a caller preserves the original modification time on the overwrite.
	meta = expiryTestMetadata(t, &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Days: 1}}}}, "")
	if err = meta.Save(ctx, obj); err != nil {
		t.Fatal(err)
	}
	globalBucketMetadataSys.Set(bucket, meta)
	old, err = obj.PutObject(ctx, bucket, "replaced", mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{MTime: time.Now().Add(-30 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	newData := []byte("newer replacement")
	replacement, err := obj.PutObject(ctx, bucket, "replaced", mustGetPutObjReader(t, bytes.NewReader(newData), int64(len(newData)), "", ""), ObjectOptions{MTime: old.ModTime})
	if err != nil {
		t.Fatal(err)
	}
	if applyExpiryRule(ctx, obj, old, false, false) {
		t.Fatal("old queue deleted overwritten null object")
	}
	fresh, err := obj.GetObjectInfo(ctx, bucket, "replaced", ObjectOptions{})
	if err != nil || fresh.ETag != replacement.ETag {
		t.Fatalf("replacement was not retained: %+v / %v", fresh, err)
	}
}

func TestExpiryFSUsesOwnedLock(t *testing.T) {
	obj := initFSObjects(t.TempDir(), t)
	ctx := context.Background()
	lc := &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Expiration: lifecycle.Expiration{Date: lifecycle.ExpirationDate{Time: time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)}}}}}
	expiryTestGlobals(t, obj, lc, "")
	const bucket = "expiry-bucket"
	if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
		t.Fatal(err)
	}
	meta := expiryTestMetadata(t, lc, "")
	if err := meta.Save(ctx, obj); err != nil {
		t.Fatal(err)
	}
	globalBucketMetadataSys.Set(bucket, meta)
	data := []byte("expired FS object")
	oi, err := obj.PutObject(ctx, bucket, "expired", mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{MTime: time.Now().Add(-30 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if applied, expireErr := executeExpiry(short, obj, oi, false, false, &oi); !applied {
		t.Fatalf("FS expiry reacquired its owned source lock or did not delete: %v", expireErr)
	}
	if _, err = obj.GetObjectNInfo(ctx, bucket, oi.Name, nil, http.Header{}, readLock, ObjectOptions{}); !isErrObjectNotFound(err) {
		t.Fatalf("FS expired object still readable: %v", err)
	}
}
