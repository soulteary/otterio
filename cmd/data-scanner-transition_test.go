// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
)

func TestScannerLifecyclePendingAndRestoreWithoutRules(t *testing.T) {
	base := ObjectInfo{Bucket: "bucket", Name: "logs/item", ModTime: time.Now().Add(-30 * 24 * time.Hour), VersionID: "old", SuccessorModTime: time.Now().Add(-10 * 24 * time.Hour)}
	for _, tc := range []struct {
		name   string
		change func(*ObjectInfo)
		want   lifecycle.Action
	}{
		{"pending-noncurrent", func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionPending }, lifecycle.TransitionVersionAction},
		{"pending-current", func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionPending; o.IsLatest = true }, lifecycle.TransitionAction},
		{"pending-unversioned", func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionPending; o.VersionID = "" }, lifecycle.TransitionAction},
		{"pending-marker", func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionPending; o.DeleteMarker = true }, lifecycle.NoneAction},
		{"complete", func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionComplete }, lifecycle.NoneAction},
		{"restore-ongoing-without-config", func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionComplete; o.RestoreOngoing = true }, lifecycle.TransitionVersionAction},
		{"restored-noncurrent", func(o *ObjectInfo) {
			o.TransitionStatus = lifecycle.TransitionComplete
			o.RestoreExpires = time.Now().Add(-time.Hour)
		}, lifecycle.DeleteRestoredVersionAction},
		{"restored-unversioned", func(o *ObjectInfo) {
			o.TransitionStatus = lifecycle.TransitionComplete
			o.RestoreExpires = time.Now().Add(-time.Hour)
			o.VersionID = ""
		}, lifecycle.DeleteRestoredAction},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := base
			tc.change(&obj)
			if got := lifecycleActionForObject(lifecycle.Lifecycle{}, obj); got != tc.want {
				t.Fatalf("persisted state without active rules: got %v want %v", got, tc.want)
			}
		})
	}
	obj := base
	obj.TransitionStatus = lifecycle.TransitionPending
	for _, rule := range []lifecycle.Rule{
		{Status: lifecycle.Disabled, NoncurrentVersionTransition: lifecycle.NoncurrentVersionTransition{NoncurrentDays: 1, StorageClass: "changed-tier"}},
		{Status: lifecycle.Enabled, Filter: lifecycle.Filter{Tag: lifecycle.Tag{Key: "changed", Value: "scope"}}, NoncurrentVersionTransition: lifecycle.NoncurrentVersionTransition{NoncurrentDays: 1, StorageClass: "changed-tier"}},
	} {
		if got := lifecycleActionForObject(lifecycle.Lifecycle{Rules: []lifecycle.Rule{rule}}, obj); got != lifecycle.TransitionVersionAction {
			t.Fatalf("configuration/tag edit stranded a pending transition: %v", got)
		}
	}
	lc := lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{NoncurrentDays: 1}}}}
	if got := lifecycleActionForObject(lc, obj); got != lifecycle.DeleteVersionAction {
		t.Fatalf("pending retry suppressed due permanent expiration: %v", got)
	}
}

type scannerLifecycleFreshObjectLayer struct {
	ObjectLayer
	fresh       ObjectInfo
	err         error
	reads       int
	deletes     int
	bucket, key string
	versionID   string
	locks       *nsLockMap
}

func (o *scannerLifecycleFreshObjectLayer) NewNSLock(bucket string, objects ...string) RWLocker {
	return o.locks.NewNSLock(nil, bucket, objects...)
}

func (o *scannerLifecycleFreshObjectLayer) GetObjectInfo(_ context.Context, bucket, key string, opts ObjectOptions) (ObjectInfo, error) {
	o.reads++
	o.bucket, o.key, o.versionID = bucket, key, opts.VersionID
	return o.fresh, o.err
}

func (o *scannerLifecycleFreshObjectLayer) DeleteObject(context.Context, string, string, ObjectOptions) (ObjectInfo, error) {
	o.deletes++
	return ObjectInfo{}, errors.New("stale scan must not mutate metadata")
}

func TestScannerLifecycleRechecksFreshObjectState(t *testing.T) {
	base := ObjectInfo{Bucket: "bucket", Name: "logs/item", ModTime: time.Now().Add(-30 * 24 * time.Hour), VersionID: "old", SuccessorModTime: time.Now().Add(-10 * 24 * time.Hour), Size: 23}
	tagged := lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Filter: lifecycle.Filter{Tag: lifecycle.Tag{Key: "environment", Value: "prod"}}, NoncurrentVersionTransition: lifecycle.NoncurrentVersionTransition{NoncurrentDays: 1, StorageClass: "archive"}}}}
	for _, tc := range []struct {
		name    string
		lc      *lifecycle.Lifecycle
		scanned func(*ObjectInfo)
		fresh   func(*ObjectInfo)
		err     error
		size    int64
	}{
		{"tag-changed", &tagged, func(o *ObjectInfo) { o.UserTags = "environment=prod" }, func(o *ObjectInfo) { o.UserTags = "environment=dev" }, nil, base.Size},
		{"pending-completed-without-config", nil, func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionPending }, func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionComplete }, nil, base.Size},
		{"restore-renewed-without-config", nil, func(o *ObjectInfo) {
			o.TransitionStatus = lifecycle.TransitionComplete
			o.RestoreExpires = time.Now().Add(-time.Hour)
		}, func(o *ObjectInfo) {
			o.TransitionStatus = lifecycle.TransitionComplete
			o.RestoreExpires = time.Now().Add(time.Hour)
		}, nil, base.Size},
		{"pending-object-removed", nil, func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionPending }, func(*ObjectInfo) {}, ObjectNotFound{Bucket: base.Bucket, Object: base.Name}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scanned, fresh := base, base
			tc.scanned(&scanned)
			tc.fresh(&fresh)
			obj := &scannerLifecycleFreshObjectLayer{fresh: fresh, err: tc.err}
			item := scannerItem{bucket: base.Bucket, prefix: "logs", objectName: "item", lifeCycle: tc.lc}
			if applied, size := item.applyLifecycle(context.Background(), obj, actionMeta{oi: scanned}); applied || size != tc.size {
				t.Fatalf("stale scan changed data accounting: applied=%v size=%d want=%d", applied, size, tc.size)
			}
			if obj.reads != 1 || obj.bucket != base.Bucket || obj.key != base.Name || obj.versionID != base.VersionID || obj.deletes != 0 {
				t.Fatalf("fresh exact-version check missing or stale mutation occurred: %+v", obj)
			}
		})
	}
}

func TestScannerLifecycleNoActionSkipsFreshLookup(t *testing.T) {
	obj := &scannerLifecycleFreshObjectLayer{}
	item := scannerItem{bucket: "bucket", objectName: "item"}
	oi := ObjectInfo{Name: "item", ModTime: time.Now(), Size: 23}
	if applied, size := item.applyLifecycle(context.Background(), obj, actionMeta{oi: oi}); applied || size != oi.Size || obj.reads != 0 {
		t.Fatalf("inactive scan performed work: applied=%v size=%d reads=%d", applied, size, obj.reads)
	}
}

func TestScannerLifecycleQueuesPersistedWorkWithoutRules(t *testing.T) {
	savedState := globalTransitionState
	t.Cleanup(func() { globalTransitionState = savedState })
	globalTransitionState = &transitionState{transitionCh: make(chan ObjectInfo, 2)}
	for _, status := range []string{lifecycle.TransitionPending, lifecycle.TransitionComplete} {
		oi := ObjectInfo{Bucket: "bucket", Name: "logs/item", ModTime: time.Now().Add(-30 * 24 * time.Hour), VersionID: "old", Size: 23,
			TransitionStatus: status, RestoreOngoing: status == lifecycle.TransitionComplete,
			TransitionedObject: &TransitionedObject{ARN: "arn:otterio:ilm::persisted-tier:archive", Key: "persisted-key", VersionID: "remote-version"}}
		obj := &scannerLifecycleFreshObjectLayer{fresh: oi, locks: newNSLock(false)}
		item := scannerItem{bucket: oi.Bucket, prefix: "logs", objectName: "item"}
		scanned := oi
		if status == lifecycle.TransitionComplete {
			// The old scan saw an expired cache, but the fresh exact version is
			// now being restored again. Resume it rather than deleting its data.
			scanned.RestoreOngoing = false
			scanned.RestoreExpires = time.Now().Add(-time.Hour)
		}
		if applied, size := item.applyLifecycle(context.Background(), obj, actionMeta{oi: scanned}); !applied || size != oi.Size {
			t.Fatalf("persisted %s operation stranded without rules: applied=%v size=%d", status, applied, size)
		}
		select {
		case queued := <-globalTransitionState.transitionCh:
			if queued.VersionID != oi.VersionID || queued.TransitionStatus != status || queued.TransitionedObject != oi.TransitionedObject || queued.RestoreOngoing != oi.RestoreOngoing {
				t.Fatalf("queue lost persisted identity or restore state: %+v", queued)
			}
		default:
			t.Fatalf("persisted %s operation did not reach worker queue", status)
		}
		wantReads := 1
		if status == lifecycle.TransitionPending {
			wantReads = 2 // Initial scanner refresh, then the locked prepare refresh.
		}
		if obj.reads != wantReads || obj.deletes != 0 {
			t.Fatalf("resume must refresh and retain locator without rewriting it: reads=%d deletes=%d", obj.reads, obj.deletes)
		}
	}
}

func TestScannerLifecycleQueuesPendingDeletionDespitePolicyChanges(t *testing.T) {
	savedExpiry, savedTransition := globalExpiryState, globalTransitionState
	t.Cleanup(func() { globalExpiryState, globalTransitionState = savedExpiry, savedTransition })
	for _, version := range []string{"old", nullVersionID, ""} {
		for _, policy := range []struct {
			name string
			lc   *lifecycle.Lifecycle
		}{
			{"removed", nil},
			{"disabled", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Disabled, Expiration: lifecycle.Expiration{Days: 1}}}}},
			{"changed-target", &lifecycle.Lifecycle{Rules: []lifecycle.Rule{{Status: lifecycle.Enabled, Transition: lifecycle.Transition{Days: 1, StorageClass: "different-tier"}}}}},
			{"restored-cache-expired", &lifecycle.Lifecycle{}},
		} {
			t.Run(version+"-"+policy.name, func(t *testing.T) {
				globalExpiryState = &expiryState{expiryCh: make(chan expiryTask, 1)}
				globalTransitionState = &transitionState{transitionCh: make(chan ObjectInfo, 1)}
				oi := expiryTestObject()
				oi.VersionID = version
				oi = expiryTestDeletionIntent(t, oi)
				if policy.name == "restored-cache-expired" {
					oi.RestoreExpires = time.Now().Add(-time.Hour)
				}
				obj := &scannerLifecycleFreshObjectLayer{fresh: oi}
				item := scannerItem{bucket: oi.Bucket, prefix: "logs", objectName: "item", lifeCycle: policy.lc}
				if applied, size := item.applyLifecycle(context.Background(), obj, actionMeta{oi: oi}); !applied || size != oi.Size || obj.deletes != 0 {
					t.Fatalf("pending permanent deletion was stranded or applied from a scan: applied=%v size=%d deletes=%d", applied, size, obj.deletes)
				}
				wantVersion := version
				if wantVersion == "" {
					wantVersion = nullVersionID
				}
				if obj.reads != 1 || obj.versionID != wantVersion || len(globalTransitionState.transitionCh) != 0 {
					t.Fatalf("pending deletion selected a transition/latest-version instead of exact source: reads=%d version=%q", obj.reads, obj.versionID)
				}
				select {
				case queued := <-globalExpiryState.expiryCh:
					if !queued.versionExpiry || queued.objInfo.VersionID != oi.VersionID || queued.objInfo.UserDefined[transitionDeleteIntentKey] != oi.UserDefined[transitionDeleteIntentKey] {
						t.Fatalf("pending deletion lost permanent/exact identity in its queue: %+v", queued)
					}
				default:
					t.Fatal("pending deletion did not reach expiry queue")
				}
			})
		}
	}
}

func TestScannerLifecycleRetainsInvalidDeletionIntent(t *testing.T) {
	savedExpiry := globalExpiryState
	t.Cleanup(func() { globalExpiryState = savedExpiry })
	for _, tc := range []struct {
		name          string
		changeScanned bool
		change        func(*ObjectInfo)
	}{
		{"corrupt-scanned", true, func(oi *ObjectInfo) { oi.UserDefined[transitionDeleteIntentKey] = "{" }},
		{"corrupt-fresh", false, func(oi *ObjectInfo) { oi.UserDefined[transitionDeleteIntentKey] = "{" }},
		{"mismatched-fresh", false, func(oi *ObjectInfo) {
			ref := *oi.TransitionedObject
			ref.Key += "changed"
			oi.TransitionedObject = &ref
		}},
		{"missing-fresh-locator", false, func(oi *ObjectInfo) { oi.TransitionedObject = nil }},
		{"intent-cleared-after-scan", false, func(oi *ObjectInfo) { delete(oi.UserDefined, transitionDeleteIntentKey) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			globalExpiryState = &expiryState{expiryCh: make(chan expiryTask, 1)}
			scanned := expiryTestDeletionIntent(t, expiryTestObject())
			fresh := scanned
			fresh.UserDefined = cloneMSS(scanned.UserDefined)
			if tc.changeScanned {
				tc.change(&scanned)
			} else {
				tc.change(&fresh)
			}
			obj := &scannerLifecycleFreshObjectLayer{fresh: fresh}
			item := scannerItem{bucket: scanned.Bucket, prefix: "logs", objectName: "item"}
			if applied, size := item.applyLifecycle(context.Background(), obj, actionMeta{oi: scanned}); applied || size != scanned.Size || obj.deletes != 0 || len(globalExpiryState.expiryCh) != 0 {
				t.Fatalf("invalid intent was erased or replayed: applied=%v size=%d deletes=%d", applied, size, obj.deletes)
			}
			wantReads := 1
			if tc.changeScanned {
				wantReads = 0
			}
			if obj.reads != wantReads {
				t.Fatalf("invalid intent read count=%d want=%d", obj.reads, wantReads)
			}
		})
	}
}
