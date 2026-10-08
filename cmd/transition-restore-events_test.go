// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/event"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/soulteary/otterio/pkg/pubsub"
)

type transitionRestoreEventLayer struct {
	ObjectLayer
	mu        sync.Mutex
	puts      int
	committed ObjectInfo
	putErr    error
}

func TestTransitionEventRedactionKeepsCallerMetadata(t *testing.T) {
	savedListen, savedNotify := globalHTTPListen, globalNotificationSys
	globalHTTPListen = pubsub.New()
	globalNotificationSys = &NotificationSys{bucketRulesMap: make(map[string]event.RulesMap)}
	t.Cleanup(func() { globalHTTPListen, globalNotificationSys = savedListen, savedNotify })
	events, done := make(chan interface{}, 8), make(chan struct{})
	globalHTTPListen.Subscribe(events, done, nil)
	defer close(done)
	for _, name := range []event.Name{event.ObjectCreatedPut, event.ObjectCreatedCopy, event.ObjectRemovedDelete, event.ObjectRestorePostInitiated, event.ObjectRestorePostCompleted} {
		t.Run(name.String(), func(t *testing.T) {
			metadata := map[string]string{
				"x-amz-meta-owner": "keep-public", xhttp.AmzServerSideEncryptionCustomerKey: "keep-caller-sensitive",
				transitionReferenceKey: "keep-destination", transitionDeleteIntentKey: "keep-delete-intent",
				transitionRestorePartsKey: "keep-part-layout", ReservedMetadataPrefixLower + "transition-status": lifecycle.TransitionComplete,
				strings.ToUpper(ReservedMetadataPrefixLower) + "CUSTOM-PRIVATE": "keep-private",
			}
			before := cloneMSS(metadata)
			oi := ObjectInfo{Bucket: "restore-events", Name: "item", VersionID: "version", ETag: "etag", Size: 17, UserDefined: metadata}
			sendEvent(eventArgs{EventName: name, BucketName: oi.Bucket, Object: oi})
			if !reflect.DeepEqual(metadata, before) {
				t.Fatalf("event redaction modified the caller's metadata: got=%v want=%v", metadata, before)
			}
			var received event.Event
			select {
			case value := <-events:
				received = value.(event.Event)
			default:
				t.Fatal("notification event was not published")
			}
			for key := range received.S3.Object.UserMetadata {
				if strings.HasPrefix(strings.ToLower(key), ReservedMetadataPrefixLower) || key == xhttp.AmzServerSideEncryptionCustomerKey {
					t.Fatalf("notification exposed private metadata %q", key)
				}
			}
			if name != event.ObjectRemovedDelete && received.S3.Object.UserMetadata["x-amz-meta-owner"] != "keep-public" {
				t.Fatal("redaction removed ordinary user metadata")
			}
		})
	}
}

func (o *transitionRestoreEventLayer) PutObject(ctx context.Context, bucket, object string, reader *PutObjReader, opts ObjectOptions) (ObjectInfo, error) {
	if o.putErr != nil {
		return ObjectInfo{}, o.putErr
	}
	oi, err := o.ObjectLayer.PutObject(ctx, bucket, object, reader, opts)
	if err == nil {
		o.mu.Lock()
		o.puts++
		o.committed = oi
		o.mu.Unlock()
	}
	return oi, err
}

func TestTransitionRestoreCompletionEvents(t *testing.T) {
	obj, targets, bucket := targetTestSetup(t)
	ctx := context.Background()
	data := []byte("restored body and its exact version")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("versioning") {
			w.Write([]byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`))
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"remote-etag"`)
		w.Write(data)
	}))
	t.Cleanup(server.Close)
	target := madmin.BucketTarget{Endpoint: strings.TrimPrefix(server.URL, "http://"), TargetBucket: "destination", Label: "Restore", Type: madmin.ILMService, Region: "us-east-1", Credentials: &auth.Credentials{AccessKey: globalActiveCred.AccessKey, SecretKey: globalActiveCred.SecretKey}}
	if err := targets.SetTarget(ctx, bucket, &target, false); err != nil {
		t.Fatal(err)
	}
	if err := globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, enabledBucketVersioningConfig); err != nil {
		t.Fatal(err)
	}
	savedListen := globalHTTPListen
	globalHTTPListen = pubsub.New()
	t.Cleanup(func() { globalHTTPListen = savedListen })
	globalNotificationSys = &NotificationSys{bucketRulesMap: make(map[string]event.RulesMap)}

	for _, mode := range []string{"scanner-recovery", "request-snapshot", "api-scanner-race", "put-failure"} {
		t.Run(mode, func(t *testing.T) {
			key := "restore/" + mode
			oi, err := obj.PutObject(ctx, bucket, key, mustGetPutObjReader(t, bytes.NewReader(data), int64(len(data)), "", ""), ObjectOptions{Versioned: true, MTime: time.Now().Add(-30 * 24 * time.Hour), UserDefined: map[string]string{"x-amz-meta-owner": "restore-owner", xhttp.AmzServerSideEncryptionCustomerKey: "test-sensitive-metadata"}})
			if err != nil {
				t.Fatal(err)
			}
			ref := &TransitionedObject{ARN: target.Arn, Key: "remote/" + mode, VersionID: "remote-version", StorageClass: target.Label}
			if _, err := obj.DeleteObject(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID, TransitionStatus: lifecycle.TransitionComplete, TransitionExpected: &oi, TransitionedObject: ref}); err != nil {
				t.Fatal(err)
			}
			complete, err := obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID})
			if err != nil {
				t.Fatal(err)
			}
			reserved, _, err := beginTransitionRestore(ctx, obj, bucket, key, complete, 1)
			if err != nil {
				t.Fatal(err)
			}
			events, done := make(chan interface{}, 8), make(chan struct{})
			globalHTTPListen.Subscribe(events, done, func(value interface{}) bool {
				e := value.(event.Event)
				return e.EventName == event.ObjectRestorePostCompleted && e.S3.Object.Key == key
			})
			defer close(done)
			layer := &transitionRestoreEventLayer{ObjectLayer: obj}
			snapshot := eventArgs{ReqParams: map[string]string{"region": "captured-region", "principalId": "original-user", "sourceIPAddress": "192.0.2.10"}, RespElements: map[string]string{"requestId": "original-request"}, Host: "192.0.2.10", UserAgent: "original-agent"}
			requestContext := withTransitionRestoreEvent(ctx, snapshot)
			// Reusing the original request or response must not change the
			// asynchronous completion event's authenticated identity.
			snapshot.ReqParams["principalId"] = "reused-user"
			snapshot.RespElements["requestId"] = "reused-request"
			expiry := lifecycle.ExpectedExpiryTime(time.Now(), 1)
			if mode == "put-failure" {
				layer.putErr = errors.New("injected restore persistence failure")
				if err := restoreTransitionedObject(requestContext, bucket, key, layer, reserved, &RestoreObjectRequest{Days: 1}, expiry); !errors.Is(err, layer.putErr) {
					t.Fatalf("restore failure was not retained: %v", err)
				}
				fresh, err := obj.GetObjectInfo(ctx, bucket, key, ObjectOptions{VersionID: oi.VersionID})
				if err != nil || !fresh.RestoreOngoing || layer.puts != 0 {
					t.Fatalf("failed restore lost its retry reservation: %+v / %v", fresh, err)
				}
			} else {
				switch mode {
				case "scanner-recovery":
					if err := transitionObject(ctx, layer, reserved); err != nil {
						t.Fatal(err)
					}
				case "request-snapshot":
					if err := restoreTransitionedObject(requestContext, bucket, key, layer, reserved, &RestoreObjectRequest{Days: 1}, expiry); err != nil {
						t.Fatal(err)
					}
				case "api-scanner-race":
					results := make(chan error, 2)
					go func() {
						results <- restoreTransitionedObject(requestContext, bucket, key, layer, reserved, &RestoreObjectRequest{Days: 1}, expiry)
					}()
					go func() { results <- transitionObject(ctx, layer, reserved) }()
					for range 2 {
						if err := <-results; err != nil {
							t.Fatal(err)
						}
					}
				}
				if layer.puts != 1 || layer.committed.RestoreOngoing || layer.committed.UserDefined[transitionReferenceKey] == "" || layer.committed.UserDefined[xhttp.AmzServerSideEncryptionCustomerKey] != "test-sensitive-metadata" {
					t.Fatalf("completion repeated a commit or notification mutated its metadata: puts=%d committed=%+v", layer.puts, layer.committed)
				}
				var received event.Event
				select {
				case value := <-events:
					received = value.(event.Event)
				default:
					t.Fatal("successful restore did not emit its completion event")
				}
				if received.S3.Object.VersionID != oi.VersionID || received.S3.Object.ETag != oi.ETag || received.S3.Object.Size != int64(len(data)) || received.S3.Object.UserMetadata["x-amz-meta-owner"] != "restore-owner" || received.S3.Object.UserMetadata[transitionReferenceKey] != "" || received.S3.Object.UserMetadata[xhttp.AmzServerSideEncryptionCustomerKey] != "" {
					t.Fatalf("event does not describe the actual committed version: %+v", received)
				}
				if mode == "request-snapshot" && (received.UserIdentity.PrincipalID != "original-user" || received.ResponseElements["x-amz-request-id"] != "original-request" || received.Source.UserAgent != "original-agent") {
					t.Fatalf("request reuse changed completion identity: %+v", received)
				}
				if mode == "scanner-recovery" && (received.AwsRegion != globalServerRegion || received.Source.Host != "Internal: [RESTORE]" || received.UserIdentity.PrincipalID != "") {
					t.Fatalf("recovered restore invented a request identity: %+v", received)
				}
				// A queued duplicate and an API retry may observe the completed
				// reservation; neither creates another commit or completion event.
				if err := transitionObject(ctx, layer, reserved); err != nil {
					t.Fatal(err)
				}
				if err := restoreTransitionedObject(requestContext, bucket, key, layer, reserved, &RestoreObjectRequest{Days: 1}, expiry); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case value := <-events:
				t.Fatalf("restore emitted an unexpected completion event: %+v", value)
			default:
			}
		})
	}
}
