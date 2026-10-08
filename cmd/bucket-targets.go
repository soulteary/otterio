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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	otterio "github.com/soulteary/otterio-sdk/v7"
	otteriogo "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio/cmd/crypto"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/versioning"
	"github.com/soulteary/otterio/pkg/madmin"
)

const (
	defaultHealthCheckDuration = 100 * time.Second
)

// BucketTargetSys represents bucket targets subsystem
type BucketTargetSys struct {
	sync.RWMutex
	arnRemotesMap map[string]*TargetClient
	targetsMap    map[string][]madmin.BucketTarget
	bucketRemotes map[string]map[string]*TargetClient
}

// ListTargets lists bucket targets across tenant or for individual bucket, and returns
// results filtered by arnType
func (sys *BucketTargetSys) ListTargets(ctx context.Context, bucket, arnType string) (targets []madmin.BucketTarget) {
	if bucket != "" {
		if ts, err := sys.ListBucketTargets(ctx, bucket); err == nil {
			for _, t := range ts.Targets {
				if string(t.Type) == arnType || arnType == "" {
					targets = append(targets, publicBucketTarget(t))
				}
			}
		}
		return targets
	}
	sys.RLock()
	defer sys.RUnlock()
	for _, tgts := range sys.targetsMap {
		for _, t := range tgts {
			if string(t.Type) == arnType || arnType == "" {
				targets = append(targets, publicBucketTarget(t))
			}
		}
	}
	return
}

// ListBucketTargets - gets list of bucket targets for this bucket.
func (sys *BucketTargetSys) ListBucketTargets(_ context.Context, bucket string) (*madmin.BucketTargets, error) {
	sys.RLock()
	defer sys.RUnlock()

	tgts, ok := sys.targetsMap[bucket]
	if ok {
		result := &madmin.BucketTargets{Targets: make([]madmin.BucketTarget, len(tgts))}
		for i, target := range tgts {
			result.Targets[i] = cloneBucketTarget(target)
		}
		return result, nil
	}
	return nil, BucketRemoteTargetNotFound{Bucket: bucket}
}

// SetTarget validates and persists a target before publishing its client.
func (sys *BucketTargetSys) SetTarget(ctx context.Context, bucket string, tgt *madmin.BucketTarget, update bool) error {
	if globalIsGateway {
		if tgt != nil && tgt.Type == madmin.ILMService {
			return NotImplemented{}
		}
		return nil
	}
	if tgt == nil || !tgt.Type.IsValid() || tgt.Credentials == nil {
		return BucketRemoteArnTypeInvalid{Bucket: bucket}
	}
	obj := newObjectLayerFn()
	if obj == nil {
		return errServerNotInitialized
	}
	if tgt.Type == madmin.ILMService && !lifecycleTransitionSupported(obj) {
		return NotImplemented{Message: "Lifecycle transition requires native single-pool local erasure storage"}
	}
	ctx, release, err := lifecycleTargetMutationContext(ctx, obj, bucket)
	if err != nil {
		return err
	}
	defer release()
	meta, targets, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil {
		return err
	}
	candidate := cloneBucketTarget(*tgt)
	candidate.SourceBucket = bucket
	if candidate.Type == madmin.ILMService && strings.TrimSpace(candidate.Label) == "" {
		return errInvalidArgument
	}
	if !update {
		candidate.Arn = remoteARNFromTargets(bucket, &candidate, targets)
	}
	parsedARN, arnErr := madmin.ParseARN(candidate.Arn)
	if arnErr != nil || parsedARN.Type != candidate.Type || parsedARN.Bucket != candidate.TargetBucket {
		return BucketRemoteArnInvalid{Bucket: bucket}
	}
	if candidate.Arn == "" {
		return BucketRemoteArnInvalid{Bucket: bucket}
	}
	index := -1
	for i, target := range targets.Targets {
		if target.Arn == candidate.Arn {
			if index != -1 {
				return BucketRemoteArnInvalid{Bucket: bucket}
			}
			index = i
		}
		if strings.EqualFold(target.Label, candidate.Label) && target.Arn != candidate.Arn {
			return BucketRemoteLabelInUse{Bucket: target.TargetBucket}
		}
	}
	if update && index == -1 {
		return BucketRemoteTargetNotFound{Bucket: bucket}
	}
	if !update && index != -1 {
		return BucketRemoteAlreadyExists{Bucket: targets.Targets[index].TargetBucket}
	}
	registry, exists, err := readLifecycleTargetRegistry(ctx, obj, bucket, targets)
	if err != nil {
		return err
	}
	if !exists {
		if err = saveLifecycleTargetRegistry(ctx, obj, bucket, registry); err != nil {
			return err
		}
	}
	if index != -1 {
		previous := targets.Targets[index]
		if candidate.Credentials.SecretKey == "" {
			if candidate.Credentials.AccessKey != previous.Credentials.AccessKey {
				return errInvalidArgument
			}
			candidate.Credentials = cloneBucketTarget(previous).Credentials
		}
		if previous.Type != candidate.Type {
			return BucketRemoteArnTypeInvalid{Bucket: bucket}
		}
		physicalChanged := targetDestinationIdentity(previous) != targetDestinationIdentity(candidate)
		labelChanged := !strings.EqualFold(previous.Label, candidate.Label)
		if physicalChanged || labelChanged {
			if targetConfigured(meta, previous) {
				return BucketRemoteRemoveDisallowed{Bucket: bucket}
			}
			if previous.Type == madmin.ILMService {
				used, err := lifecycleTargetReferenced(ctx, obj, bucket, previous.Arn, &registry)
				if err != nil {
					return err
				}
				if used {
					return BucketRemoteRemoveDisallowed{Bucket: bucket}
				}
			}
		}
	}
	if candidate.Credentials.AccessKey == "" || candidate.Credentials.SecretKey == "" {
		return errInvalidArgument
	}
	if err = sys.validateTarget(ctx, bucket, &candidate); err != nil {
		return err
	}
	if index == -1 {
		targets.Targets = append(targets.Targets, candidate)
	} else {
		targets.Targets[index] = candidate
	}
	if err = saveLifecycleTargetRegistry(ctx, obj, bucket, registry); err != nil {
		return err
	}
	data, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	if err = globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketTargetsFile, data, ""); err != nil {
		return err
	}
	sys.UpdateAllTargets(bucket, targets)
	*tgt = cloneBucketTarget(candidate)
	return nil
}

func (sys *BucketTargetSys) validateTarget(ctx context.Context, bucket string, tgt *madmin.BucketTarget) error {
	clnt, err := sys.getRemoteTargetClient(tgt)
	if err != nil {
		return BucketRemoteTargetNotFound{Bucket: tgt.TargetBucket}
	}
	if found, err := clnt.BucketExists(ctx, tgt.TargetBucket); err != nil || !found {
		if !found && err == nil || otterio.ToErrorResponse(err).Code == "NoSuchBucket" {
			return BucketRemoteTargetNotFound{Bucket: tgt.TargetBucket}
		}
		return BucketRemoteConnectionErr{Bucket: tgt.TargetBucket, Err: err}
	}
	if tgt.Type == madmin.ReplicationService && !globalIsErasure {
		return NotImplemented{Message: "Replication requires erasure storage"}
	}
	if tgt.Type == madmin.ReplicationService && !globalBucketVersioningSys.Enabled(bucket) {
		return BucketReplicationSourceNotVersioned{Bucket: bucket}
	}
	if tgt.Type == madmin.ReplicationService || globalBucketVersioningSys.Enabled(bucket) {
		vcfg, err := clnt.GetBucketVersioning(ctx, tgt.TargetBucket)
		if err != nil {
			return BucketRemoteConnectionErr{Bucket: tgt.TargetBucket, Err: err}
		}
		if vcfg.Status != string(versioning.Enabled) {
			return BucketRemoteTargetNotVersioned{Bucket: tgt.TargetBucket}
		}
	}
	if tgt.Type == madmin.ReplicationService && tgt.ReplicationSync && tgt.BandwidthLimit > 0 {
		return NotImplemented{Message: "Synchronous replication does not support bandwidth limits"}
	}
	return nil
}

// RemoveTarget keeps destinations referenced by durable pending/completed jobs.
func (sys *BucketTargetSys) RemoveTarget(ctx context.Context, bucket, arnStr string) error {
	if globalIsGateway {
		return nil
	}
	arn, err := madmin.ParseARN(arnStr)
	if err != nil || !arn.Type.IsValid() {
		return BucketRemoteArnInvalid{Bucket: bucket}
	}
	obj := newObjectLayerFn()
	if obj == nil {
		return errServerNotInitialized
	}
	ctx, release, err := lifecycleTargetMutationContext(ctx, obj, bucket)
	if err != nil {
		return err
	}
	defer release()
	meta, targets, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil {
		return err
	}
	index := -1
	for i, target := range targets.Targets {
		if target.Arn == arnStr {
			if index != -1 {
				return BucketRemoteArnInvalid{Bucket: bucket}
			}
			index = i
		}
	}
	if index == -1 {
		return BucketRemoteTargetNotFound{Bucket: bucket}
	}
	previous := targets.Targets[index]
	if targetConfigured(meta, previous) {
		return BucketRemoteRemoveDisallowed{Bucket: bucket}
	}
	registry, exists, err := readLifecycleTargetRegistry(ctx, obj, bucket, targets)
	if err != nil {
		return err
	}
	if !exists {
		if err = saveLifecycleTargetRegistry(ctx, obj, bucket, registry); err != nil {
			return err
		}
	}
	if previous.Type == madmin.ILMService {
		used, err := lifecycleTargetReferenced(ctx, obj, bucket, arnStr, &registry)
		if err != nil {
			return err
		}
		if used {
			return BucketRemoteRemoveDisallowed{Bucket: bucket}
		}
	}
	targets.Targets = append(targets.Targets[:index], targets.Targets[index+1:]...)
	if err = saveLifecycleTargetRegistry(ctx, obj, bucket, registry); err != nil {
		return err
	}
	data, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	if err = globalBucketMetadataSys.UpdateWithContext(ctx, bucket, bucketTargetsFile, data, ""); err != nil {
		return err
	}
	sys.UpdateAllTargets(bucket, targets)
	return nil
}

func cloneBucketTarget(target madmin.BucketTarget) madmin.BucketTarget {
	if target.Credentials != nil {
		credentials := *target.Credentials
		target.Credentials = &credentials
	}
	return target
}

func publicBucketTarget(target madmin.BucketTarget) madmin.BucketTarget {
	result := cloneBucketTarget(target)
	if result.Credentials != nil {
		result.Credentials = &auth.Credentials{AccessKey: result.Credentials.AccessKey}
	}
	return result
}

func targetConfigured(meta BucketMetadata, target madmin.BucketTarget) bool {
	if target.Type == madmin.ReplicationService {
		return meta.replicationConfig != nil && meta.replicationConfig.RoleArn == target.Arn
	}
	if meta.lifecycleConfig == nil {
		return false
	}
	for _, rule := range meta.lifecycleConfig.Rules {
		if rule.Status == Disabled {
			continue
		}
		if rule.Transition.StorageClass != "" && strings.EqualFold(rule.Transition.StorageClass, target.Label) || rule.NoncurrentVersionTransition.StorageClass != "" && strings.EqualFold(rule.NoncurrentVersionTransition.StorageClass, target.Label) {
			return true
		}
	}
	return false
}

// GetRemoteTargetClient returns otterio-go client for replication target instance
func (sys *BucketTargetSys) GetRemoteTargetClient(_ context.Context, arn string) *TargetClient {
	sys.RLock()
	defer sys.RUnlock()
	return sys.arnRemotesMap[arn]
}

// GetRemoteTargetWithLabel returns bucket target given a target label
func (sys *BucketTargetSys) GetRemoteTargetWithLabel(ctx context.Context, bucket, targetLabel string) *madmin.BucketTarget {
	if sys == nil {
		return nil
	}
	obj := newObjectLayerFn()
	if obj == nil {
		return nil
	}
	_, targets, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil {
		return nil
	}
	var result *madmin.BucketTarget
	for _, target := range targets.Targets {
		if strings.EqualFold(target.Label, targetLabel) {
			if result != nil {
				return nil
			}
			candidate := cloneBucketTarget(target)
			result = &candidate
		}
	}
	return result
}

// GetRemoteArnWithLabel returns bucket target's ARN given its target label
func (sys *BucketTargetSys) GetRemoteArnWithLabel(ctx context.Context, bucket, tgtLabel string) *madmin.ARN {
	tgt := sys.GetRemoteTargetWithLabel(ctx, bucket, tgtLabel)
	if tgt == nil {
		return nil
	}
	arn, err := madmin.ParseARN(tgt.Arn)
	if err != nil {
		return nil
	}
	return arn
}

// GetRemoteLabelWithArn returns a bucket target's label given its ARN
func (sys *BucketTargetSys) GetRemoteLabelWithArn(_ context.Context, bucket, arnStr string) string {
	sys.RLock()
	defer sys.RUnlock()
	for _, t := range sys.targetsMap[bucket] {
		if t.Arn == arnStr {
			return t.Label
		}
	}
	return ""
}

// NewBucketTargetSys - creates new replication system.
func NewBucketTargetSys() *BucketTargetSys {
	return &BucketTargetSys{
		arnRemotesMap: make(map[string]*TargetClient),
		targetsMap:    make(map[string][]madmin.BucketTarget),
		bucketRemotes: make(map[string]map[string]*TargetClient),
	}
}

// Init initializes the bucket targets subsystem for buckets which have targets configured.
func (sys *BucketTargetSys) Init(ctx context.Context, buckets []BucketInfo, objAPI ObjectLayer) error {
	if objAPI == nil {
		return errServerNotInitialized
	}

	// In gateway mode, bucket targets is not supported.
	if globalIsGateway {
		return nil
	}

	// Load bucket targets once during boot in background.
	go sys.load(ctx, buckets, objAPI)
	return nil
}

// UpdateAllTargets publishes immutable clients. Replaced clients remain usable by
// operations that already own a snapshot; only their background health work stops.
func (sys *BucketTargetSys) UpdateAllTargets(bucket string, tgts *madmin.BucketTargets) {
	if sys == nil {
		return
	}
	targets := make([]madmin.BucketTarget, 0)
	clients := make(map[string]*TargetClient)
	if tgts != nil {
		for _, target := range tgts.Targets {
			target = cloneBucketTarget(target)
			targets = append(targets, target)
			if client, err := sys.getRemoteTargetClient(&target); err == nil {
				clients[target.Arn] = client
			}
		}
	}
	sys.Lock()
	if sys.bucketRemotes == nil {
		sys.bucketRemotes = make(map[string]map[string]*TargetClient)
	}
	for arn, client := range sys.bucketRemotes[bucket] {
		client.stopHealthCheck()
		if sys.arnRemotesMap[arn] == client {
			delete(sys.arnRemotesMap, arn)
		}
	}
	if len(targets) == 0 {
		delete(sys.targetsMap, bucket)
		delete(sys.bucketRemotes, bucket)
	} else {
		sys.targetsMap[bucket] = targets
		sys.bucketRemotes[bucket] = clients
		for arn, client := range clients {
			sys.arnRemotesMap[arn] = client
			client.startHealthCheck()
		}
	}
	sys.Unlock()
}

// GetRemoteTargetClientForBucket returns a client scoped to its source bucket, including historical
// ARNs whose original hash did not include the source bucket or endpoint.
// Read durable configuration so a delayed peer/cache publication cannot route
// an object to a stale physical destination after a target mutation.
func (sys *BucketTargetSys) GetRemoteTargetClientForBucket(ctx context.Context, bucket, arn string) *TargetClient {
	obj := newObjectLayerFn()
	if sys == nil || obj == nil {
		return nil
	}
	_, targets, err := freshBucketTargets(ctx, obj, bucket)
	if err != nil {
		return nil
	}
	var target *madmin.BucketTarget
	for i := range targets.Targets {
		if targets.Targets[i].Arn == arn {
			if target != nil {
				return nil
			}
			target = &targets.Targets[i]
		}
	}
	if target == nil {
		return nil
	}
	sys.RLock()
	cached := sys.bucketRemotes[bucket][arn]
	sys.RUnlock()
	if cached != nil && targetClientIdentity(cached.config) == targetClientIdentity(*target) {
		return cached
	}
	client, err := sys.getRemoteTargetClient(target)
	if err != nil {
		return nil
	}
	// Avoid publishing a fresh read out of order with another mutation. An
	// uncached client is still a valid immutable snapshot for this operation.
	return client
}

// create clients for buckets having remote targets without racing map access.
func (sys *BucketTargetSys) load(ctx context.Context, buckets []BucketInfo, _ ObjectLayer) {
	for _, bucket := range buckets {
		cfg, err := globalBucketMetadataSys.GetBucketTargetsConfig(bucket.Name)
		if err != nil {
			logger.LogIf(ctx, err)
			continue
		}
		sys.UpdateAllTargets(bucket.Name, cfg)
	}
}

// getRemoteTargetInstanceTransport contains a singleton roundtripper.
var getRemoteTargetInstanceTransport http.RoundTripper
var getRemoteTargetInstanceTransportOnce sync.Once

// Returns a otterio-go Client configured to access remote host described in replication target config.
func (sys *BucketTargetSys) getRemoteTargetClient(tcfg *madmin.BucketTarget) (*TargetClient, error) {
	if tcfg == nil || tcfg.Credentials == nil {
		return nil, errInvalidArgument
	}
	config := tcfg.Credentials
	creds := credentials.NewStaticV4(config.AccessKey, config.SecretKey, config.SessionToken)

	getRemoteTargetInstanceTransportOnce.Do(func() {
		getRemoteTargetInstanceTransport = NewRemoteTargetHTTPTransport()
	})
	api, err := otterio.New(tcfg.Endpoint, &otteriogo.Options{
		Creds:     creds,
		Secure:    tcfg.Secure,
		Region:    tcfg.Region,
		Transport: getRemoteTargetInstanceTransport,
	})
	if err != nil {
		return nil, err
	}
	hcDuration := defaultHealthCheckDuration
	if tcfg.HealthCheckDuration >= 1 { // require minimum health check duration of 1 sec.
		hcDuration = tcfg.HealthCheckDuration
	}
	tc := &TargetClient{
		Client:              api,
		healthCheckDuration: hcDuration,
		bucket:              tcfg.TargetBucket,
		replicateSync:       tcfg.ReplicationSync,
		config:              cloneBucketTarget(*tcfg),
	}
	return tc, nil
}

func remoteARNFromTargets(bucket string, target *madmin.BucketTarget, targets *madmin.BucketTargets) string {
	if target == nil || !target.Type.IsValid() {
		return ""
	}
	for _, existing := range targets.Targets {
		if targetDestinationIdentity(existing) == targetDestinationIdentity(*target) {
			return existing.Arn
		}
	}
	candidate := cloneBucketTarget(*target)
	candidate.SourceBucket = bucket
	return generateARN(&candidate)
}

func targetDestinationIdentity(t madmin.BucketTarget) string {
	identity := struct {
		Endpoint, TargetBucket, Path, API, Region string
		Secure                                    bool
		Type                                      madmin.ServiceType
	}{
		strings.ToLower(t.Endpoint), t.TargetBucket, t.Path, t.API, t.Region, t.Secure, t.Type,
	}
	data, _ := json.Marshal(identity)
	return string(data)
}

func targetClientIdentity(t madmin.BucketTarget) string {
	creds := ""
	if t.Credentials != nil {
		creds = t.Credentials.AccessKey + "\x00" + t.Credentials.SecretKey + "\x00" + t.Credentials.SessionToken
	}
	return targetDestinationIdentity(t) + "\x00" + creds + fmt.Sprint(t.ReplicationSync, t.HealthCheckDuration)
}

func generateARN(t *madmin.BucketTarget) string {
	if t == nil || !t.Type.IsValid() {
		return ""
	}
	sum := sha256.Sum256([]byte(t.SourceBucket + "\x00" + targetDestinationIdentity(*t)))
	return (madmin.ARN{Type: t.Type, ID: hex.EncodeToString(sum[:]), Region: t.Region, Bucket: t.TargetBucket}).String()
}

// Returns parsed target config. If KMS is configured, remote target is decrypted
func parseBucketTargetConfig(bucket string, cdata, cmetadata []byte) (*madmin.BucketTargets, error) {
	var (
		data []byte
		err  error
		t    madmin.BucketTargets
		meta map[string]string
	)
	if len(cdata) == 0 {
		return nil, nil
	}
	data = cdata
	if len(cmetadata) != 0 {
		if err := json.Unmarshal(cmetadata, &meta); err != nil {
			return nil, err
		}
		if crypto.S3.IsEncrypted(meta) {
			if data, err = decryptBucketMetadata(cdata, bucket, meta, bucketTargetsCtx(bucket)); err != nil {
				return nil, err
			}
		}
	}

	if err = json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// TargetClient is the struct for remote target client.
type TargetClient struct {
	*otteriogo.Client
	up                  int32
	healthCheckDuration time.Duration
	bucket              string // remote bucket target
	replicateSync       bool
	config              madmin.BucketTarget
	healthCancel        context.CancelFunc
}

func (tc *TargetClient) isOffline() bool {
	return atomic.LoadInt32(&tc.up) == 0
}

func (tc *TargetClient) startHealthCheck() {
	ctx, cancel := context.WithCancel(GlobalContext)
	tc.healthCancel = cancel
	go tc.healthCheck(ctx)
}

func (tc *TargetClient) stopHealthCheck() {
	if tc.healthCancel != nil {
		tc.healthCancel()
	}
}

func (tc *TargetClient) healthCheck(ctx context.Context) {
	for {
		probe, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, err := tc.BucketExists(probe, tc.bucket)
		cancel()
		if err != nil {
			atomic.StoreInt32(&tc.up, 0)
		} else {
			atomic.StoreInt32(&tc.up, 1)
		}
		timer := time.NewTimer(tc.healthCheckDuration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
