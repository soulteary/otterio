/*
 * MinIO Cloud Storage, (C) 2017 MinIO, Inc.
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
	"context"
	"errors"

	"github.com/soulteary/otterio/cmd/logger"

	"github.com/minio/minio-go/v7/pkg/tags"
	bucketsse "github.com/soulteary/otterio/pkg/bucket/encryption"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/bucket/policy"
	"github.com/soulteary/otterio/pkg/bucket/versioning"

	"github.com/soulteary/otterio/pkg/madmin"
)

// GatewayUnsupported list of unsupported call stubs for gateway.
type GatewayUnsupported struct{}

// BackendInfo returns the underlying backend information
func (a GatewayUnsupported) BackendInfo() madmin.BackendInfo {
	return madmin.BackendInfo{Type: madmin.Gateway}
}

// LocalStorageInfo returns the local disks information, mainly used
// in prometheus - for gateway this just a no-op
func (a GatewayUnsupported) LocalStorageInfo(ctx context.Context) (StorageInfo, []error) {
	logger.CriticalIf(ctx, errors.New("not implemented"))
	return StorageInfo{}, nil
}

// NSScanner - scanner is not implemented for gateway
func (a GatewayUnsupported) NSScanner(ctx context.Context, _ *bloomFilter, _ chan<- madmin.DataUsageInfo) error {
	logger.CriticalIf(ctx, errors.New("not implemented"))
	return NotImplemented{}
}

// PutObjectMetadata - not implemented for gateway.
func (a GatewayUnsupported) PutObjectMetadata(ctx context.Context, _, _ string, _ ObjectOptions) (ObjectInfo, error) {
	logger.CriticalIf(ctx, errors.New("not implemented"))
	return ObjectInfo{}, NotImplemented{}
}

// NewNSLock is a dummy stub for gateway.
func (a GatewayUnsupported) NewNSLock(_ string, _ ...string) RWLocker {
	logger.CriticalIf(context.Background(), errors.New("not implemented"))
	return nil
}

// SetDriveCounts no-op
func (a GatewayUnsupported) SetDriveCounts() []int {
	return nil
}

// ListMultipartUploads lists all multipart uploads.
func (a GatewayUnsupported) ListMultipartUploads(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ int) (lmi ListMultipartsInfo, err error) {
	return lmi, NotImplemented{}
}

// NewMultipartUpload upload object in multiple parts
func (a GatewayUnsupported) NewMultipartUpload(_ context.Context, _ string, _ string, _ ObjectOptions) (uploadID string, err error) {
	return "", NotImplemented{}
}

// CopyObjectPart copy part of object to uploadID for another object
func (a GatewayUnsupported) CopyObjectPart(_ context.Context, _, _, _, _, _ string, _ int, _, _ int64, _ ObjectInfo, _, _ ObjectOptions) (pi PartInfo, err error) {
	return pi, NotImplemented{}
}

// PutObjectPart puts a part of object in bucket
func (a GatewayUnsupported) PutObjectPart(ctx context.Context, _ string, _ string, _ string, _ int, _ *PutObjReader, _ ObjectOptions) (pi PartInfo, err error) {
	logger.LogIf(ctx, NotImplemented{})
	return pi, NotImplemented{}
}

// GetMultipartInfo returns metadata associated with the uploadId
func (a GatewayUnsupported) GetMultipartInfo(ctx context.Context, _ string, _ string, _ string, _ ObjectOptions) (MultipartInfo, error) {
	logger.LogIf(ctx, NotImplemented{})
	return MultipartInfo{}, NotImplemented{}
}

// ListObjectVersions returns all object parts for specified object in specified bucket
func (a GatewayUnsupported) ListObjectVersions(ctx context.Context, _, _, _, _, _ string, _ int) (ListObjectVersionsInfo, error) {
	logger.LogIf(ctx, NotImplemented{})
	return ListObjectVersionsInfo{}, NotImplemented{}
}

// ListObjectParts returns all object parts for specified object in specified bucket
func (a GatewayUnsupported) ListObjectParts(ctx context.Context, _ string, _ string, _ string, _ int, _ int, _ ObjectOptions) (lpi ListPartsInfo, err error) {
	logger.LogIf(ctx, NotImplemented{})
	return lpi, NotImplemented{}
}

// AbortMultipartUpload aborts a ongoing multipart upload
func (a GatewayUnsupported) AbortMultipartUpload(_ context.Context, _ string, _ string, _ string, _ ObjectOptions) error {
	return NotImplemented{}
}

// CompleteMultipartUpload completes ongoing multipart upload and finalizes object
func (a GatewayUnsupported) CompleteMultipartUpload(ctx context.Context, _ string, _ string, _ string, _ []CompletePart, _ ObjectOptions) (oi ObjectInfo, err error) {
	logger.LogIf(ctx, NotImplemented{})
	return oi, NotImplemented{}
}

// SetBucketPolicy sets policy on bucket
func (a GatewayUnsupported) SetBucketPolicy(ctx context.Context, _ string, _ *policy.Policy) error {
	logger.LogIf(ctx, NotImplemented{})
	return NotImplemented{}
}

// GetBucketPolicy will get policy on bucket
func (a GatewayUnsupported) GetBucketPolicy(_ context.Context, _ string) (bucketPolicy *policy.Policy, err error) {
	return nil, NotImplemented{}
}

// DeleteBucketPolicy deletes all policies on bucket
func (a GatewayUnsupported) DeleteBucketPolicy(_ context.Context, _ string) error {
	return NotImplemented{}
}

// SetBucketVersioning enables versioning on a bucket.
func (a GatewayUnsupported) SetBucketVersioning(ctx context.Context, _ string, _ *versioning.Versioning) error {
	logger.LogIf(ctx, NotImplemented{})
	return NotImplemented{}
}

// GetBucketVersioning retrieves versioning configuration of a bucket.
func (a GatewayUnsupported) GetBucketVersioning(ctx context.Context, _ string) (*versioning.Versioning, error) {
	logger.LogIf(ctx, NotImplemented{})
	return nil, NotImplemented{}
}

// SetBucketLifecycle enables lifecycle policies on a bucket.
func (a GatewayUnsupported) SetBucketLifecycle(ctx context.Context, _ string, _ *lifecycle.Lifecycle) error {
	logger.LogIf(ctx, NotImplemented{})
	return NotImplemented{}
}

// GetBucketLifecycle retrieves lifecycle configuration of a bucket.
func (a GatewayUnsupported) GetBucketLifecycle(_ context.Context, _ string) (*lifecycle.Lifecycle, error) {
	return nil, NotImplemented{}
}

// DeleteBucketLifecycle deletes all lifecycle policies on a bucket
func (a GatewayUnsupported) DeleteBucketLifecycle(_ context.Context, _ string) error {
	return NotImplemented{}
}

// GetBucketSSEConfig returns bucket encryption config on a bucket
func (a GatewayUnsupported) GetBucketSSEConfig(_ context.Context, _ string) (*bucketsse.BucketSSEConfig, error) {
	return nil, NotImplemented{}
}

// SetBucketSSEConfig sets bucket encryption config on a bucket
func (a GatewayUnsupported) SetBucketSSEConfig(_ context.Context, _ string, _ *bucketsse.BucketSSEConfig) error {
	return NotImplemented{}
}

// DeleteBucketSSEConfig deletes bucket encryption config on a bucket
func (a GatewayUnsupported) DeleteBucketSSEConfig(_ context.Context, _ string) error {
	return NotImplemented{}
}

// HealFormat - Not implemented stub
func (a GatewayUnsupported) HealFormat(_ context.Context, _ bool) (madmin.HealResultItem, error) {
	return madmin.HealResultItem{}, NotImplemented{}
}

// HealBucket - Not implemented stub
func (a GatewayUnsupported) HealBucket(_ context.Context, _ string, _ madmin.HealOpts) (madmin.HealResultItem, error) {
	return madmin.HealResultItem{}, NotImplemented{}
}

// HealObject - Not implemented stub
func (a GatewayUnsupported) HealObject(_ context.Context, _, _, _ string, _ madmin.HealOpts) (h madmin.HealResultItem, e error) {
	return h, NotImplemented{}
}

// ListObjectsV2 - Not implemented stub
func (a GatewayUnsupported) ListObjectsV2(_ context.Context, _, _, _, _ string, _ int, _ bool, _ string) (result ListObjectsV2Info, err error) {
	return result, NotImplemented{}
}

// Walk - Not implemented stub
func (a GatewayUnsupported) Walk(_ context.Context, _, _ string, _ chan<- ObjectInfo, _ ObjectOptions) error {
	return NotImplemented{}
}

// HealObjects - Not implemented stub
func (a GatewayUnsupported) HealObjects(_ context.Context, _, _ string, _ madmin.HealOpts, _ HealObjectFn) (e error) {
	return NotImplemented{}
}

// CopyObject copies a blob from source container to destination container.
func (a GatewayUnsupported) CopyObject(_ context.Context, _ string, _ string, _ string, _ string,
	_ ObjectInfo, _, _ ObjectOptions) (objInfo ObjectInfo, err error) {
	return objInfo, NotImplemented{}
}

// GetMetrics - no op
func (a GatewayUnsupported) GetMetrics(ctx context.Context) (*BackendMetrics, error) {
	logger.LogIf(ctx, NotImplemented{})
	return &BackendMetrics{}, NotImplemented{}
}

// PutObjectTags - not implemented.
func (a GatewayUnsupported) PutObjectTags(ctx context.Context, _, _ string, _ string, _ ObjectOptions) (ObjectInfo, error) {
	logger.LogIf(ctx, NotImplemented{})
	return ObjectInfo{}, NotImplemented{}
}

// GetObjectTags - not implemented.
func (a GatewayUnsupported) GetObjectTags(ctx context.Context, _, _ string, _ ObjectOptions) (*tags.Tags, error) {
	logger.LogIf(ctx, NotImplemented{})
	return nil, NotImplemented{}
}

// DeleteObjectTags - not implemented.
func (a GatewayUnsupported) DeleteObjectTags(ctx context.Context, _, _ string, _ ObjectOptions) (ObjectInfo, error) {
	logger.LogIf(ctx, NotImplemented{})
	return ObjectInfo{}, NotImplemented{}
}

// IsNotificationSupported returns whether bucket notification is applicable for this layer.
func (a GatewayUnsupported) IsNotificationSupported() bool {
	return false
}

// IsListenSupported returns whether listen bucket notification is applicable for this layer.
func (a GatewayUnsupported) IsListenSupported() bool {
	return false
}

// IsEncryptionSupported returns whether server side encryption is implemented for this layer.
func (a GatewayUnsupported) IsEncryptionSupported() bool {
	return false
}

// IsTaggingSupported returns whether object tagging is supported or not for this layer.
func (a GatewayUnsupported) IsTaggingSupported() bool {
	return false
}

// IsCompressionSupported returns whether compression is applicable for this layer.
func (a GatewayUnsupported) IsCompressionSupported() bool {
	return false
}

// Health - No Op.
func (a GatewayUnsupported) Health(_ context.Context, _ HealthOptions) HealthResult {
	return HealthResult{}
}

// ReadHealth - No Op.
func (a GatewayUnsupported) ReadHealth(_ context.Context) bool {
	return true
}
