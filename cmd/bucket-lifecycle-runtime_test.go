// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/hash"
)

const runtimeRestoreDocument = `<RestoreRequest xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Days>2</Days></RestoreRequest>`

type restoreBodyFailureReader struct{ err error }

func (r restoreBodyFailureReader) Read([]byte) (int, error) { return 0, r.err }

func TestParseRestoreRequestAuthenticatesWholeBody(t *testing.T) {
	failedDigest, err := hash.NewReader(strings.NewReader(runtimeRestoreDocument), int64(len(runtimeRestoreDocument)), "", strings.Repeat("0", 64), int64(len(runtimeRestoreDocument)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseRestoreRequest(failedDigest); err == nil {
		t.Fatal("XML root completed before signed SHA256 failure was consumed")
	}
	transportErr := errors.New("body was truncated after XML root")
	if _, err := parseRestoreRequest(io.MultiReader(strings.NewReader(runtimeRestoreDocument), restoreBodyFailureReader{transportErr})); !errors.Is(err, transportErr) {
		t.Fatalf("body EOF error was lost: %v", err)
	}
	for _, doc := range []string{
		runtimeRestoreDocument + runtimeRestoreDocument,
		runtimeRestoreDocument + `trailing`,
		runtimeRestoreDocument + `<?instruction stop?>`,
		`<!DOCTYPE RestoreRequest>` + runtimeRestoreDocument,
		strings.Replace(runtimeRestoreDocument, `<Days>2</Days>`, `<Days>1</Days><Days>2</Days>`, 1),
		strings.Replace(runtimeRestoreDocument, `<Days>2</Days>`, `<Days>2</Days><Type>SELECT</Type><Type/>`, 1),
		strings.ReplaceAll(runtimeRestoreDocument, "RestoreRequest", "UnexpectedRequest"),
		runtimeRestoreDocument + strings.Repeat(" ", maxRestoreObjectRequestSize),
	} {
		if _, err := parseRestoreRequest(strings.NewReader(doc)); err == nil {
			t.Fatalf("invalid or oversized restore envelope accepted: %.150s", doc)
		}
	}
	for _, doc := range []string{runtimeRestoreDocument, `<?xml version="1.0"?>` + runtimeRestoreDocument + ` <!-- complete --> `, runtimeRestoreDocument + strings.Repeat(" ", maxRestoreObjectRequestSize-len(runtimeRestoreDocument))} {
		req, err := parseRestoreRequest(strings.NewReader(doc))
		if err != nil || req.Days != 2 || req.validate(context.Background(), nil) != nil {
			t.Fatalf("supported bounded restore envelope rejected: %v %#v", err, req)
		}
	}
}

func TestRestoreRequestRejectsUnknownTypeAndNegativeDays(t *testing.T) {
	for _, req := range []RestoreObjectRequest{
		{Type: "ARCHIVE", Days: 1}, {Type: "select", Days: 1}, {Days: -1}, {Days: 0}, {Type: SelectRestoreRequest, Days: -1},
	} {
		if err := req.validate(context.Background(), nil); err == nil {
			t.Fatalf("unsupported restore request accepted: %#v", req)
		}
	}
}

func TestPrepareTransitionRejectsStaleSourceBeforeRemoteCalls(t *testing.T) {
	base := ObjectInfo{Bucket: "bucket", Name: "item", VersionID: "old", ModTime: time.Now().Add(-30 * 24 * time.Hour), ETag: "original", Size: 23, TransitionStatus: lifecycle.TransitionPending,
		TransitionedObject: &TransitionedObject{ARN: "arn:otterio:ilm::persisted-tier:archive", Key: "unique-key", VersionID: "remote-version"}}
	for _, tc := range []struct {
		name   string
		change func(*ObjectInfo)
	}{
		{"replaced-modtime", func(o *ObjectInfo) { o.ModTime = o.ModTime.Add(time.Second) }},
		{"replaced-etag", func(o *ObjectInfo) { o.ETag = "replacement" }},
		{"replaced-size", func(o *ObjectInfo) { o.Size++ }},
		{"delete-marker", func(o *ObjectInfo) { o.DeleteMarker = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := base
			tc.change(&fresh)
			obj := &scannerLifecycleFreshObjectLayer{fresh: fresh, locks: newNSLock(false)}
			_, err := prepareTransition(context.Background(), obj, base)
			if _, ok := err.(PreConditionFailed); !ok || obj.reads != 1 || obj.deletes != 0 {
				t.Fatalf("stale queued source reached transition side effects: err=%v reads=%d deletes=%d", err, obj.reads, obj.deletes)
			}
		})
	}
}

func TestBeginRestoreUsesFreshExactVersionAndRejectsDuplicateWork(t *testing.T) {
	base := ObjectInfo{Bucket: "bucket", Name: "item", VersionID: "old", ModTime: time.Now().Add(-30 * 24 * time.Hour), ETag: "original", Size: 23, TransitionStatus: lifecycle.TransitionComplete}
	for _, tc := range []struct {
		name   string
		change func(*ObjectInfo)
		want   error
	}{
		{"already-pending", func(o *ObjectInfo) { o.RestoreOngoing = true }, errTransitionRestoreInProgress},
		{"transition-incomplete", func(o *ObjectInfo) { o.TransitionStatus = lifecycle.TransitionPending }, PreConditionFailed{}},
		{"source-replaced", func(o *ObjectInfo) { o.ETag = "replacement" }, PreConditionFailed{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := base
			tc.change(&fresh)
			obj := &scannerLifecycleFreshObjectLayer{fresh: fresh, locks: newNSLock(false)}
			_, _, err := beginTransitionRestore(context.Background(), obj, base.Bucket, base.Name, base, 2)
			if !errors.Is(err, tc.want) || obj.versionID != base.VersionID || obj.reads != 1 || obj.deletes != 0 {
				t.Fatalf("restore did not fence fresh source identity: err=%v reads=%d version=%q", err, obj.reads, obj.versionID)
			}
		})
	}
}

func TestRestoredVersionCleanupDoesNotRequirePermanentDeletionAccess(t *testing.T) {
	oi := ObjectInfo{Bucket: "bucket", Name: "item", VersionID: "old", ModTime: time.Now().Add(-30 * 24 * time.Hour), TransitionStatus: lifecycle.TransitionComplete, RestoreExpires: time.Now().Add(-time.Hour),
		UserDefined: map[string]string{xhttp.AmzObjectLockLegalHold: "ON", xhttp.AmzObjectLockMode: "COMPLIANCE", xhttp.AmzObjectLockRetainUntilDate: time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)}}
	if !enforceRetentionForDeletion(context.Background(), oi) {
		t.Fatal("fixture did not protect permanent source deletion")
	}
	if got := evalActionFromLifecycle(context.Background(), lifecycle.Lifecycle{}, oi, false); got != lifecycle.DeleteRestoredVersionAction {
		t.Fatalf("retention of the remote authority blocked local restored-cache cleanup: %v", got)
	}
}

func TestTransitionIdentityMetadataCannotBeShadowed(t *testing.T) {
	oi := ObjectInfo{VersionID: "source-version", ModTime: time.Now(), ETag: "source-etag", UserDefined: map[string]string{
		"x-amz-meta-otterio-transition-id": "user-value", "x-amz-meta-description": "preserve-me",
	}, TransitionedObject: &TransitionedObject{ARN: "arn:otterio:ilm::persisted-tier:archive", Key: "unique-owned-key", VersionID: "destination-version"}}
	opts, err := putTransitionOpts(oi)
	if err != nil {
		t.Fatal(err)
	}
	for range 64 {
		hdr := opts.Header()
		if got := hdr.Get("X-Amz-Meta-Otterio-Transition-Id"); got != oi.TransitionedObject.Key {
			t.Fatalf("source user metadata replaced the remote ownership marker: got %q want %q", got, oi.TransitionedObject.Key)
		}
		if hdr.Get("X-Amz-Meta-Description") != "preserve-me" || opts.Internal.SourceVersionID != oi.TransitionedObject.VersionID {
			t.Fatal("ordinary metadata or exact remote version changed")
		}
	}
}

func TestTransitionExpiryRejectsReplacedNullVersion(t *testing.T) {
	expected := ObjectInfo{Bucket: "bucket", Name: "item", VersionID: "null", ModTime: time.Now().Add(-30 * 24 * time.Hour), ETag: "old", Size: 23, TransitionStatus: lifecycle.TransitionComplete,
		TransitionedObject: &TransitionedObject{ARN: "arn:otterio:ilm::persisted-tier:archive", Key: "old-key", VersionID: "old-remote-version"}}
	for _, sameModTime := range []bool{false, true} {
		fresh := expected
		fresh.ETag, fresh.Size = "replacement", 24
		if !sameModTime {
			fresh.ModTime = time.Now()
		}
		fresh.TransitionedObject = &TransitionedObject{ARN: expected.TransitionedObject.ARN, Key: "replacement-key", VersionID: "replacement-remote-version"}
		obj := &scannerLifecycleFreshObjectLayer{fresh: fresh, locks: newNSLock(false)}
		err := deleteTransitionedObject(context.Background(), obj, expected.Bucket, expected.Name, lifecycleObjectOpts(expected), false, false, expected)
		if _, ok := err.(PreConditionFailed); !ok || obj.deletes != 0 {
			t.Fatalf("old expiration verdict reached replacement null-version deletion: sameModTime=%v err=%v deletes=%d", sameModTime, err, obj.deletes)
		}
	}
}

func TestLifecycleQueuesConcurrentShutdownKeepChannelsOpen(t *testing.T) {
	saved := GlobalContext
	t.Cleanup(func() { GlobalContext = saved })
	ctx, cancel := context.WithCancel(context.Background())
	GlobalContext = ctx
	cancel()
	transitions := &transitionState{transitionCh: make(chan ObjectInfo, 1)}
	expirations := &expiryState{expiryCh: make(chan expiryTask, 1)}
	var wg sync.WaitGroup
	panics := make(chan any, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if value := recover(); value != nil {
					panics <- value
				}
			}()
			for range 100 {
				transitions.queueTransitionTask(ObjectInfo{Name: "pending"})
				expirations.queueExpiryTask(ObjectInfo{Name: "expired"}, true)
			}
		}()
	}
	wg.Wait()
	close(panics)
	for value := range panics {
		t.Errorf("concurrent queue submission after shutdown panicked: %v", value)
	}
	select {
	case _, ok := <-transitions.transitionCh:
		if !ok {
			t.Fatal("a producer closed the shared transition channel")
		}
	default:
	}
	select {
	case _, ok := <-expirations.expiryCh:
		if !ok {
			t.Fatal("a producer closed the shared expiry channel")
		}
	default:
	}
	GlobalContext = context.Background()
	transitions.queueTransitionTask(ObjectInfo{Name: "resumed"})
	expirations.queueExpiryTask(ObjectInfo{Name: "resumed"}, true)
	if (<-transitions.transitionCh).Name != "resumed" || !(<-expirations.expiryCh).versionExpiry {
		t.Fatal("shutdown corrupted the shared queues")
	}
}
