// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/madmin"
)

const lifecycleTargetRegistryFile = ".transition-targets.json"
const lifecycleTargetRegistryMaxSize = 4 << 20
const lifecycleTargetRegistryMaxJobs = 10000

type lifecycleTargetPin struct {
	SourceKey       string    `json:"sourceKey"`
	SourceVersionID string    `json:"sourceVersionID"`
	ModTime         time.Time `json:"modTime"`
	ETag            string    `json:"etag"`
	ARN             string    `json:"arn"`
	RemoteKey       string    `json:"remoteKey"`
}

type lifecycleTargetRegistry struct {
	Version    int                           `json:"version"`
	LegacyARNs []string                      `json:"legacyARNs,omitempty"`
	Jobs       map[string]lifecycleTargetPin `json:"jobs"`
}

func lifecycleRegistryKey(bucket string) string {
	return pathJoin(bucketConfigPrefix, bucket, lifecycleTargetRegistryFile)
}

// Callers own lifecycleTargetLock. The registry precedes each pending reference
// and permits target removal to check exact source versions without relying on
// a possibly stale listing cache. Legacy targets are conservatively retained.
func readLifecycleTargetRegistry(ctx context.Context, obj ObjectLayer, bucket string, targets *madmin.BucketTargets) (lifecycleTargetRegistry, bool, error) {
	registry := lifecycleTargetRegistry{Version: 1, Jobs: make(map[string]lifecycleTargetPin)}
	r, err := obj.GetObjectNInfo(ctx, otterioMetaBucket, lifecycleRegistryKey(bucket), nil, http.Header{}, readLock, ObjectOptions{})
	if err != nil {
		if !isErrObjectNotFound(err) {
			return registry, false, err
		}
		if targets != nil {
			for _, target := range targets.Targets {
				if target.Type == madmin.ILMService {
					registry.LegacyARNs = append(registry.LegacyARNs, target.Arn)
				}
			}
		}
		return registry, false, nil
	}
	defer r.Close()
	if r.ObjInfo.Size < 0 || r.ObjInfo.Size > lifecycleTargetRegistryMaxSize {
		return registry, false, errInvalidArgument
	}
	data, err := io.ReadAll(io.LimitReader(r, lifecycleTargetRegistryMaxSize+1))
	if err != nil {
		return registry, false, err
	}
	if len(data) > lifecycleTargetRegistryMaxSize || !validConfigurationJSONEncoding(data) {
		return registry, false, errInvalidArgument
	}
	unique := json.NewDecoder(bytes.NewReader(data))
	value, err := decodeUniqueBucketJSON(unique, 0)
	if err != nil {
		return registry, false, err
	}
	root, ok := value.(map[string]interface{})
	if !ok || !bucketJSONKeys(root, "version", "legacyARNs", "jobs") {
		return registry, false, errInvalidArgument
	}
	jobs, ok := root["jobs"].(map[string]interface{})
	if !ok {
		return registry, false, errInvalidArgument
	}
	for _, value := range jobs {
		pin, ok := value.(map[string]interface{})
		if !ok || !bucketJSONKeys(pin, "sourceKey", "sourceVersionID", "modTime", "etag", "arn", "remoteKey") {
			return registry, false, errInvalidArgument
		}
	}
	if _, err := unique.Token(); err != io.EOF {
		return registry, false, errInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return registry, false, err
	}
	if decoder.Decode(new(interface{})) != io.EOF || registry.Version != 1 || registry.Jobs == nil || len(registry.Jobs) > lifecycleTargetRegistryMaxJobs {
		return registry, false, errInvalidArgument
	}
	seen := make(map[string]bool)
	for _, arn := range registry.LegacyARNs {
		parsed, err := madmin.ParseARN(arn)
		if err != nil || parsed.Type != madmin.ILMService || seen[arn] {
			return registry, false, errInvalidArgument
		}
		seen[arn] = true
	}
	for identity, pin := range registry.Jobs {
		if pin.SourceKey == "" || pin.RemoteKey == "" || pin.ModTime.IsZero() || pin.ETag == "" {
			return registry, false, errInvalidArgument
		}
		arn, err := madmin.ParseARN(pin.ARN)
		if err != nil || arn.Type != madmin.ILMService {
			return registry, false, errInvalidArgument
		}
		raw, _ := json.Marshal(pin)
		digest := sha256.Sum256(raw)
		if identity != hex.EncodeToString(digest[:]) {
			return registry, false, errInvalidArgument
		}
	}
	return registry, true, nil
}

func saveLifecycleTargetRegistry(ctx context.Context, obj ObjectLayer, bucket string, registry lifecycleTargetRegistry) error {
	if len(registry.Jobs) > lifecycleTargetRegistryMaxJobs {
		return errInvalidArgument
	}
	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	if len(data) > lifecycleTargetRegistryMaxSize {
		return errInvalidArgument
	}
	return saveConfig(ctx, obj, lifecycleRegistryKey(bucket), data)
}

func freshBucketTargets(ctx context.Context, obj ObjectLayer, bucket string) (BucketMetadata, *madmin.BucketTargets, error) {
	meta, err := loadBucketMetadata(ctx, obj, bucket)
	if err != nil {
		return meta, nil, err
	}
	targets, err := parseBucketTargetConfig(bucket, meta.BucketTargetsConfigJSON, meta.BucketTargetsConfigMetaJSON)
	if err != nil {
		return meta, nil, err
	}
	if targets == nil {
		targets = &madmin.BucketTargets{}
	}
	seen := make(map[string]bool)
	for i, target := range targets.Targets {
		arn, err := madmin.ParseARN(target.Arn)
		if err != nil || !target.Type.IsValid() || arn.Type != target.Type || arn.Bucket != target.TargetBucket || target.Credentials == nil || seen[target.Arn] {
			return meta, nil, errInvalidArgument
		}
		seen[target.Arn] = true
		target.SourceBucket = bucket
		targets.Targets[i] = cloneBucketTarget(target)
	}
	return meta, targets, nil
}

func pinLifecycleTarget(ctx context.Context, obj ObjectLayer, bucket string, oi ObjectInfo, ref *TransitionedObject) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if !ref.valid() || oi.Name == "" || oi.Bucket != bucket || oi.ModTime.IsZero() || oi.ETag == "" {
		return errInvalidArgument
	}
	_, targets, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil {
		return err
	}
	found := false
	for _, target := range targets.Targets {
		if target.Arn == ref.ARN && target.Type == madmin.ILMService {
			found = true
		}
	}
	if !found {
		return BucketRemoteTargetNotFound{Bucket: bucket}
	}
	registry, _, err := readLifecycleTargetRegistry(ctx, obj, bucket, targets)
	if err != nil {
		return err
	}
	pin := lifecycleTargetPin{SourceKey: oi.Name, SourceVersionID: oi.VersionID, ModTime: oi.ModTime, ETag: oi.ETag, ARN: ref.ARN, RemoteKey: ref.Key}
	identity, err := json.Marshal(pin)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(identity)
	identityKey := hex.EncodeToString(digest[:])
	registry.Jobs[identityKey] = pin
	data, err := json.Marshal(registry)
	if err != nil {
		return err
	}
	if len(registry.Jobs) >= lifecycleTargetRegistryMaxJobs*3/4 || len(data) >= lifecycleTargetRegistryMaxSize*3/4 {
		if err := reclaimLifecycleTargetPins(ctx, obj, bucket, &registry, oi.Name, identityKey); err != nil {
			return err
		}
	}
	return saveLifecycleTargetRegistry(ctx, obj, bucket, registry)
}

// Caller owns target lock and its current source object's write lock. Read
// that key without recursively taking its object lock, including other versions
// of the same key. Other keys use normal read locks and fresh exact versions.
func reclaimLifecycleTargetPins(ctx context.Context, obj ObjectLayer, bucket string, registry *lifecycleTargetRegistry, lockedKey, keepIdentity string) error {
	processed := 0
	for identity, pin := range registry.Jobs {
		if processed >= 1000 {
			break
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 2*time.Second {
			break
		}
		if identity == keepIdentity {
			continue
		}
		processed++
		version := pin.SourceVersionID
		if version == "" {
			version = nullVersionID
		}
		oi, err := obj.GetObjectInfo(ctx, bucket, pin.SourceKey, ObjectOptions{VersionID: version, NoLock: pin.SourceKey == lockedKey})
		if err != nil {
			if isErrObjectNotFound(err) || isErrVersionNotFound(err) {
				delete(registry.Jobs, identity)
				continue
			}
			return err // retain unknown identities, never erase evidence on I/O failure
		}
		if !oi.ModTime.Equal(pin.ModTime) || oi.ETag != pin.ETag {
			delete(registry.Jobs, identity)
			continue
		}
		if oi.TransitionStatus != "" || oi.TransitionedObject != nil || oi.UserDefined[transitionReferenceKey] != "" {
			continue
		}
		delete(registry.Jobs, identity)
	}
	return nil
}

// Matching jobs retain the physical destination. Missing/replaced source
// identities can be reclaimed under the same target lock that gates prepare.
func lifecycleTargetReferenced(ctx context.Context, obj ObjectLayer, bucket, arn string, registry *lifecycleTargetRegistry) (bool, error) {
	for _, legacy := range registry.LegacyARNs {
		if legacy == arn {
			return true, nil
		}
	}
	referenced := false
	for identity, pin := range registry.Jobs {
		if pin.ARN != arn {
			continue
		}
		version := pin.SourceVersionID
		if version == "" {
			version = nullVersionID
		}
		oi, err := obj.GetObjectInfo(ctx, bucket, pin.SourceKey, ObjectOptions{VersionID: version})
		if err != nil {
			if isErrObjectNotFound(err) || isErrVersionNotFound(err) {
				delete(registry.Jobs, identity)
				continue
			}
			return false, err
		}
		if !oi.ModTime.Equal(pin.ModTime) || oi.ETag != pin.ETag {
			delete(registry.Jobs, identity)
			continue
		}
		if oi.TransitionStatus == lifecycle.TransitionPending || oi.TransitionStatus == lifecycle.TransitionComplete {
			referenced = true
			continue
		}
		if oi.TransitionStatus != "" {
			return false, errInvalidArgument
		}
		delete(registry.Jobs, identity)
	}
	return referenced, nil
}

func lifecycleTargetMutationContext(ctx context.Context, obj ObjectLayer, bucket string) (context.Context, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	lock := lifecycleTargetLock(obj, bucket)
	locked, err := lock.GetLock(ctx, newDynamicTimeout(10*time.Second, time.Second))
	if err != nil {
		cancel()
		return ctx, func() {}, err
	}
	return locked, func() { lock.Unlock(); cancel() }, nil
}
