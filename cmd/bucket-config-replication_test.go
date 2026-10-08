// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/soulteary/otterio/pkg/bucket/replication"
)

func TestBucketConfigReplicationVersioningTransaction(t *testing.T) {
	ExecObjectLayerTest(t, func(obj ObjectLayer, instance string, t TestErrHandler) {
		if instance != ErasureTestStr {
			return
		}
		savedObject, savedMetadata, savedNotify := newObjectLayerFn(), globalBucketMetadataSys, globalNotificationSys
		defer func() {
			setObjectLayer(savedObject)
			globalBucketMetadataSys, globalNotificationSys = savedMetadata, savedNotify
		}()
		setObjectLayer(obj)
		globalBucketMetadataSys, globalNotificationSys = NewBucketMetadataSys(), &NotificationSys{}
		ctx := context.Background()
		const bucket = "replication-transaction"
		if err := obj.MakeBucketWithLocation(ctx, bucket, BucketOptions{}); err != nil {
			t.Fatal(err)
		}
		enabled := []byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`)
		suspended := []byte(`<VersioningConfiguration><Status>Suspended</Status></VersioningConfiguration>`)
		replicationData := []byte(`<ReplicationConfiguration><Role>arn:aws:iam::AcctID:role/role-name</Role><Rule><Status>Enabled</Status><Priority>1</Priority><DeleteMarkerReplication><Status>Disabled</Status></DeleteMarkerReplication><DeleteReplication><Status>Disabled</Status></DeleteReplication><Prefix>logs/</Prefix><Destination><Bucket>arn:aws:s3:::destinationbucket</Bucket></Destination></Rule></ReplicationConfiguration>`)
		if config, err := replication.ParseConfig(bytes.NewReader(replicationData)); err != nil || config.Validate(bucket, false) != nil {
			t.Fatalf("invalid replication fixture: %v", err)
		}
		if err := globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, enabled); err != nil {
			t.Fatal(err)
		}
		config, err := globalBucketMetadataSys.GetVersioningConfig(bucket)
		if err != nil || !config.Enabled() {
			t.Fatal("replication's pre-body check did not observe Enabled")
		}
		// The replication handler has passed its versioning check, but is
		// delayed by its body/destination validation. A CAS suspension wins.
		allowCommit := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			<-allowCommit
			finished <- globalBucketMetadataSys.Update(bucket, bucketReplicationConfig, replicationData)
		}()
		if err := globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketVersioningConfig, suspended, bucketConfigRevision(bucketVersioningConfig, enabled)); err != nil {
			close(allowCommit)
			<-finished
			t.Fatal(err)
		}
		close(allowCommit)
		if err := <-finished; !errors.Is(err, errBucketConfigInvalidState) {
			t.Fatalf("replication committed after CAS suspension: %v", err)
		}
		meta, err := loadBucketMetadata(ctx, obj, bucket)
		if err != nil || !bytes.Equal(meta.VersioningConfigXML, suspended) || len(meta.ReplicationConfigXML) != 0 {
			t.Fatalf("rejected replication changed persistent state: %v", err)
		}
		if err := globalBucketMetadataSys.Update(bucket, bucketReplicationConfig, nil); err != nil {
			t.Fatalf("replication deletion was rejected while suspended: %v", err)
		}
		if err := globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketVersioningConfig, enabled, bucketConfigRevision(bucketVersioningConfig, suspended)); err != nil {
			t.Fatal(err)
		}
		if err := globalBucketMetadataSys.Update(bucket, bucketReplicationConfig, replicationData); err != nil {
			t.Fatalf("replication rejected with Enabled: %v", err)
		}
		if err := globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketVersioningConfig, suspended, bucketConfigRevision(bucketVersioningConfig, enabled)); !errors.Is(err, errBucketConfigInvalidState) {
			t.Fatalf("suspension bypassed persisted replication: %v", err)
		}
		meta, err = loadBucketMetadata(ctx, obj, bucket)
		if err != nil || !bytes.Equal(meta.VersioningConfigXML, enabled) || !bytes.Equal(meta.ReplicationConfigXML, replicationData) {
			t.Fatalf("rejected suspension changed configuration: %v", err)
		}
		if err := globalBucketMetadataSys.Update(bucket, bucketReplicationConfig, nil); err != nil {
			t.Fatal(err)
		}
	})
}
