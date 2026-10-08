/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/soulteary/otterio-sdk/v7/pkg/tags"
	"github.com/soulteary/otterio/cmd/logger"
	bucketsse "github.com/soulteary/otterio/pkg/bucket/encryption"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	objectlock "github.com/soulteary/otterio/pkg/bucket/object/lock"
	"github.com/soulteary/otterio/pkg/bucket/policy"
	"github.com/soulteary/otterio/pkg/bucket/replication"
	"github.com/soulteary/otterio/pkg/bucket/versioning"
	"github.com/soulteary/otterio/pkg/event"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/soulteary/otterio/pkg/sync/errgroup"
)

// BucketMetadataSys captures all bucket metadata for a given cluster.
type BucketMetadataSys struct {
	sync.RWMutex
	metadataMap map[string]BucketMetadata
}

// Remove bucket metadata from memory.
func (sys *BucketMetadataSys) Remove(bucket string) {
	if globalIsGateway {
		return
	}
	sys.Lock()
	delete(sys.metadataMap, bucket)
	globalBucketMonitor.DeleteBucket(bucket)
	sys.Unlock()
}

// Set - sets a new metadata in-memory.
// Only a shallow copy is saved and fields with references
// cannot be modified without causing a race condition,
// so they should be replaced atomically and not appended to, etc.
// Data is not persisted to disk.
func (sys *BucketMetadataSys) Set(bucket string, meta BucketMetadata) {
	if globalIsGateway {
		return
	}

	if bucket != otterioMetaBucket {
		sys.Lock()
		sys.metadataMap[bucket] = meta
		sys.Unlock()
	}
}

// Update update bucket metadata for the specified config file.
// The configData data should not be modified after being sent here.
func (sys *BucketMetadataSys) Update(bucket string, configFile string, configData []byte) error {
	return sys.update(GlobalContext, bucket, configFile, configData, "", nil)
}

// UpdateWithContext also supports an optional revision checked inside the same
// transaction as persistence. An empty revision preserves legacy semantics.
func (sys *BucketMetadataSys) UpdateWithContext(ctx context.Context, bucket, configFile string, configData []byte, expected string) error {
	return sys.update(ctx, bucket, configFile, configData, expected, nil)
}

func (sys *BucketMetadataSys) update(ctx context.Context, bucket string, configFile string, configData []byte, expected string, commit *bucketConfigCommit) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return errServerNotInitialized
	}

	if expected != "" && !bucketConfigSupported(configFile) {
		return NotImplemented{}
	}

	if globalIsGateway {
		// This code is needed only for gateway implementations.
		switch configFile {
		case bucketSSEConfig:
			if globalGatewayName == NASBackendGateway {
				meta, err := loadBucketMetadataUnlocked(ctx, objAPI, bucket)
				if err != nil {
					return err
				}
				meta.EncryptionConfigXML = configData
				return meta.Save(ctx, objAPI)
			}
		case bucketLifecycleConfig:
			if globalGatewayName == NASBackendGateway {
				meta, err := loadBucketMetadataUnlocked(ctx, objAPI, bucket)
				if err != nil {
					return err
				}
				meta.LifecycleConfigXML = configData
				return meta.Save(ctx, objAPI)
			}
		case bucketTaggingConfig:
			if globalGatewayName == NASBackendGateway {
				meta, err := loadBucketMetadataUnlocked(ctx, objAPI, bucket)
				if err != nil {
					return err
				}
				meta.TaggingConfigXML = configData
				return meta.Save(ctx, objAPI)
			}
		case bucketNotificationConfig:
			if globalGatewayName == NASBackendGateway {
				meta, err := loadBucketMetadataUnlocked(ctx, objAPI, bucket)
				if err != nil {
					return err
				}
				meta.NotificationConfigXML = configData
				return meta.Save(ctx, objAPI)
			}
		case bucketPolicyConfig:
			if configData == nil {
				return objAPI.DeleteBucketPolicy(ctx, bucket)
			}
			config, err := policy.ParseConfig(bytes.NewReader(configData), bucket)
			if err != nil {
				return err
			}
			return objAPI.SetBucketPolicy(ctx, bucket, config)
		}
		return NotImplemented{}
	}

	if bucket == otterioMetaBucket {
		return errInvalidArgument
	}

	// Serialize the complete metadata read/modify/write, including updates to
	// different settings. This key differs from the metadata object's own lock.
	lock := objAPI.NewNSLock(otterioMetaBucket, bucketMetadataTransactionKey(bucket))
	ctx, err := lock.GetLock(ctx, newDynamicTimeout(10*time.Second, time.Second))
	if err != nil {
		return err
	}
	locked := true
	defer func() {
		if locked {
			lock.Unlock()
		}
	}()
	if _, err := objAPI.GetBucketInfo(ctx, bucket); err != nil {
		return err
	}

	meta, err := loadBucketMetadataUnlocked(ctx, objAPI, bucket)
	if err != nil {
		return err
	}

	if expected != "" && bucketConfigRevision(configFile, bucketConfigBytes(meta, configFile)) != expected {
		return PreConditionFailed{}
	}

	switch configFile {
	case bucketPolicyConfig:
		meta.PolicyConfigJSON = configData
	case bucketNotificationConfig:
		meta.NotificationConfigXML = configData
	case bucketLifecycleConfig:
		meta.LifecycleConfigXML = configData
	case bucketSSEConfig:
		meta.EncryptionConfigXML = configData
	case bucketTaggingConfig:
		meta.TaggingConfigXML = configData
	case bucketQuotaConfigFile:
		meta.QuotaConfigJSON = configData
	case objectLockConfig:
		if !globalIsErasure && !globalIsDistErasure && !bucketMetadataErasureBackend(objAPI) {
			return NotImplemented{}
		}
		meta.ObjectLockConfigXML = configData
	case bucketVersioningConfig:
		if !globalIsErasure && !globalIsDistErasure && !bucketMetadataErasureBackend(objAPI) {
			return NotImplemented{}
		}
		requested, err := versioning.ParseConfig(bytes.NewReader(configData))
		if err != nil {
			return err
		}
		// A peer's cache may lag a different setting. Recheck dependencies in
		// the same fresh transaction that will commit the versioning document.
		if requested.Suspended() && ((meta.objectLockConfig != nil && meta.objectLockConfig.ObjectLockEnabled == "Enabled") || meta.replicationConfig != nil) {
			return errBucketConfigInvalidState
		}
		meta.VersioningConfigXML = configData
	case bucketReplicationConfig:
		if !globalIsErasure && !globalIsDistErasure && !bucketMetadataErasureBackend(objAPI) {
			return NotImplemented{}
		}
		// Replication's handler checks versioning before reading the body and
		// contacting its destination. A concurrent suspension may have won
		// since that check, so validate the fresh state inside this transaction.
		if len(configData) != 0 && (meta.versioningConfig == nil || !meta.versioningConfig.Enabled()) {
			return errBucketConfigInvalidState
		}
		meta.ReplicationConfigXML = configData
	case bucketTargetsFile:
		meta.BucketTargetsConfigJSON, meta.BucketTargetsConfigMetaJSON, err = encryptBucketMetadata(meta.Name, configData, bucketTargetsCtx(meta.Name))
		if err != nil {
			return fmt.Errorf("Error encrypting bucket target metadata %w", err)
		}
	default:
		return fmt.Errorf("Unknown bucket %s metadata update requested %s", bucket, configFile)
	}

	if err := meta.Save(ctx, objAPI); err != nil {
		return err
	}

	if commit != nil {
		data := bucketConfigBytes(meta, configFile)
		commit.revision, commit.exists = bucketConfigRevision(configFile, data), len(data) != 0
	}
	sys.Set(bucket, meta)
	// A peer reload acquires this same distributed lock. Release it before
	// waiting for peer acknowledgments. Persistence/cache/ACK are captured.
	lock.Unlock()
	locked = false
	if globalNotificationSys != nil {
		globalNotificationSys.LoadBucketMetadata(ctx, bucket)
	}

	return nil
}

// Get metadata for a bucket.
// If no metadata exists errConfigNotFound is returned and a new metadata is returned.
// Only a shallow copy is returned, so referenced data should not be modified,
// but can be replaced atomically.
//
// This function should only be used with
// - GetBucketInfo
// - ListBuckets
// For all other bucket specific metadata, use the relevant
// calls implemented specifically for each of those features.
func (sys *BucketMetadataSys) Get(bucket string) (BucketMetadata, error) {
	if globalIsGateway || bucket == otterioMetaBucket {
		return newBucketMetadata(bucket), errConfigNotFound
	}

	sys.RLock()
	defer sys.RUnlock()

	meta, ok := sys.metadataMap[bucket]
	if !ok {
		return newBucketMetadata(bucket), errConfigNotFound
	}

	return meta, nil
}

// GetVersioningConfig returns configured versioning config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetVersioningConfig(bucket string) (*versioning.Versioning, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	return meta.versioningConfig, nil
}

// GetTaggingConfig returns configured tagging config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetTaggingConfig(bucket string) (*tags.Tags, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketTaggingNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.taggingConfig == nil {
		return nil, BucketTaggingNotFound{Bucket: bucket}
	}
	return meta.taggingConfig, nil
}

// GetObjectLockConfig returns configured object lock config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetObjectLockConfig(bucket string) (*objectlock.Config, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketObjectLockConfigNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.objectLockConfig == nil {
		return nil, BucketObjectLockConfigNotFound{Bucket: bucket}
	}
	return meta.objectLockConfig, nil
}

// GetLifecycleConfig returns configured lifecycle config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetLifecycleConfig(bucket string) (*lifecycle.Lifecycle, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketLifecycleNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.lifecycleConfig == nil {
		return nil, BucketLifecycleNotFound{Bucket: bucket}
	}
	return meta.lifecycleConfig, nil
}

// GetNotificationConfig returns configured notification config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetNotificationConfig(bucket string) (*event.Config, error) {
	if globalIsGateway && globalGatewayName == NASBackendGateway {
		// Only needed in case of NAS gateway.
		objAPI := newObjectLayerFn()
		if objAPI == nil {
			return nil, errServerNotInitialized
		}
		meta, err := loadBucketMetadata(GlobalContext, objAPI, bucket)
		if err != nil {
			return nil, err
		}
		return meta.notificationConfig, nil
	}

	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	return meta.notificationConfig, nil
}

// GetSSEConfig returns configured SSE config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetSSEConfig(bucket string) (*bucketsse.BucketSSEConfig, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketSSEConfigNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.sseConfig == nil {
		return nil, BucketSSEConfigNotFound{Bucket: bucket}
	}
	return meta.sseConfig, nil
}

// GetPolicyConfig returns configured bucket policy
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetPolicyConfig(bucket string) (*policy.Policy, error) {
	if globalIsGateway {
		objAPI := newObjectLayerFn()
		if objAPI == nil {
			return nil, errServerNotInitialized
		}
		return objAPI.GetBucketPolicy(GlobalContext, bucket)
	}

	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketPolicyNotFound{Bucket: bucket}
		}
		return nil, err
	}
	if meta.policyConfig == nil {
		return nil, BucketPolicyNotFound{Bucket: bucket}
	}
	return meta.policyConfig, nil
}

// GetQuotaConfig returns configured bucket quota
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetQuotaConfig(bucket string) (*madmin.BucketQuota, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	return meta.quotaConfig, nil
}

// GetReplicationConfig returns configured bucket replication config
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetReplicationConfig(_ context.Context, bucket string) (*replication.Config, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		if errors.Is(err, errConfigNotFound) {
			return nil, BucketReplicationConfigNotFound{Bucket: bucket}
		}
		return nil, err
	}

	if meta.replicationConfig == nil {
		return nil, BucketReplicationConfigNotFound{Bucket: bucket}
	}
	return meta.replicationConfig, nil
}

// GetBucketTargetsConfig returns configured bucket targets for this bucket
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetBucketTargetsConfig(bucket string) (*madmin.BucketTargets, error) {
	meta, err := sys.GetConfig(bucket)
	if err != nil {
		return nil, err
	}
	if meta.bucketTargetConfig == nil {
		return nil, BucketRemoteTargetNotFound{Bucket: bucket}
	}
	return meta.bucketTargetConfig, nil
}

// GetBucketTarget returns the target for the bucket and arn.
func (sys *BucketMetadataSys) GetBucketTarget(bucket string, arn string) (madmin.BucketTarget, error) {
	targets, err := sys.GetBucketTargetsConfig(bucket)
	if err != nil {
		return madmin.BucketTarget{}, err
	}
	for _, t := range targets.Targets {
		if t.Arn == arn {
			return t, nil
		}
	}
	return madmin.BucketTarget{}, errConfigNotFound
}

// GetConfig returns a specific configuration from the bucket metadata.
// The returned object may not be modified.
func (sys *BucketMetadataSys) GetConfig(bucket string) (BucketMetadata, error) {
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return newBucketMetadata(bucket), errServerNotInitialized
	}

	if globalIsGateway {
		return newBucketMetadata(bucket), NotImplemented{}
	}

	if bucket == otterioMetaBucket {
		return newBucketMetadata(bucket), errInvalidArgument
	}

	sys.RLock()
	meta, ok := sys.metadataMap[bucket]
	sys.RUnlock()
	if ok {
		return meta, nil
	}
	return sys.loadConfig(GlobalContext, objAPI, bucket)
}

// Loading can write migrations. Publish the resulting cache before releasing
// the transaction, so an older load cannot overwrite a newer committed cache.
func (sys *BucketMetadataSys) loadConfig(ctx context.Context, obj ObjectLayer, bucket string) (BucketMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if !globalIsGateway && bucketMetadataTransactionSupported(obj) {
		lock := obj.NewNSLock(otterioMetaBucket, bucketMetadataTransactionKey(bucket))
		locked, err := lock.GetLock(ctx, newDynamicTimeout(10*time.Second, time.Second))
		if err != nil {
			return newBucketMetadata(bucket), err
		}
		ctx = locked
		defer lock.Unlock()
		if _, err := obj.GetBucketInfo(ctx, bucket); err != nil {
			return newBucketMetadata(bucket), err
		}
	}
	meta, err := loadBucketMetadataUnlocked(ctx, obj, bucket)
	if err == nil {
		sys.Set(bucket, meta)
	}
	return meta, err
}

// Init - initializes bucket metadata system for all buckets.
func (sys *BucketMetadataSys) Init(ctx context.Context, buckets []BucketInfo, objAPI ObjectLayer) error {
	if objAPI == nil {
		return errServerNotInitialized
	}

	// In gateway mode, we don't need to load the policies
	// from the backend.
	if globalIsGateway {
		return nil
	}

	// Load bucket metadata sys in background
	go sys.load(ctx, buckets, objAPI)
	return nil
}

// concurrently load bucket metadata to speed up loading bucket metadata.
func (sys *BucketMetadataSys) concurrentLoad(ctx context.Context, buckets []BucketInfo, objAPI ObjectLayer) {
	g := errgroup.WithNErrs(len(buckets))
	for index := range buckets {
		index := index
		g.Go(func() error {
			_, _ = objAPI.HealBucket(ctx, buckets[index].Name, madmin.HealOpts{
				// Ensure heal opts for bucket metadata be deep healed all the time.
				ScanMode: madmin.HealDeepScan,
			})
			_, err := sys.loadConfig(ctx, objAPI, buckets[index].Name)
			if err != nil {
				return err
			}
			return nil
		}, index)
	}
	for _, err := range g.Wait() {
		if err != nil {
			logger.LogIf(ctx, err)
		}
	}
}

// Loads bucket metadata for all buckets into BucketMetadataSys.
func (sys *BucketMetadataSys) load(ctx context.Context, buckets []BucketInfo, objAPI ObjectLayer) {
	count := 100 // load 100 bucket metadata at a time.
	for {
		if len(buckets) < count {
			sys.concurrentLoad(ctx, buckets, objAPI)
			return
		}
		sys.concurrentLoad(ctx, buckets[:count], objAPI)
		buckets = buckets[count:]
	}
}

// Reset the state of the BucketMetadataSys.
func (sys *BucketMetadataSys) Reset() {
	sys.Lock()
	for k := range sys.metadataMap {
		delete(sys.metadataMap, k)
	}
	sys.Unlock()
}

// NewBucketMetadataSys - creates new policy system.
func NewBucketMetadataSys() *BucketMetadataSys {
	return &BucketMetadataSys{
		metadataMap: make(map[string]BucketMetadata),
	}
}
