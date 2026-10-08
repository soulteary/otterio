package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	otteriogo "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/madmin"
)

const transitionReferenceKey = ReservedMetadataPrefixLower + "transition-reference"
const transitionIdentityMetadata = "otterio-transition-id"
const transitionDeleteIntentKey = ReservedMetadataPrefixLower + "transition-delete-intent"

// transitionDeletionPending binds an irreversible delete to the exact durable
// destination. A malformed or mismatched record must never authorize deletion.
func transitionDeletionPending(oi ObjectInfo) (bool, error) {
	raw := oi.UserDefined[transitionDeleteIntentKey]
	if raw == "" {
		return false, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	value, err := decodeUniqueBucketJSON(decoder, 0)
	fields, ok := value.(map[string]interface{})
	if err != nil || !ok || !bucketJSONKeys(fields, "arn", "key", "versionId", "storageClass") {
		return false, fmt.Errorf("invalid persisted transition deletion intent")
	}
	var intent TransitionedObject
	if json.Unmarshal([]byte(raw), &intent) != nil || !intent.valid() || oi.DeleteMarker ||
		(oi.TransitionStatus != lifecycle.TransitionPending && oi.TransitionStatus != lifecycle.TransitionComplete) ||
		oi.TransitionedObject == nil || intent != *oi.TransitionedObject {
		return false, fmt.Errorf("transition deletion intent does not match its persisted destination")
	}
	return true, nil
}

// lifecycleTransitionSupported bounds the durable target protocol to backends
// that share the bucket metadata transaction used by conditional settings.
func lifecycleTransitionSupported(object ObjectLayer) bool {
	backend, ok := object.(*erasureServerPools)
	return ok && backend.SinglePool() && !globalIsGateway && !globalIsDistErasure
}

// TransitionedObject identifies one remote object independently of lifecycle
// rules and the source version's position in the version history. It contains
// no credentials. FileInfo's existing metadata map stores this record so the
// storage RPC tuple format remains compatible.
type TransitionedObject struct {
	ARN          string `json:"arn"`
	Key          string `json:"key"`
	VersionID    string `json:"versionId,omitempty"`
	StorageClass string `json:"storageClass,omitempty"`
}

func (r *TransitionedObject) valid() bool {
	if r == nil || r.Key == "" {
		return false
	}
	arn, err := madmin.ParseARN(r.ARN)
	return err == nil && arn.Type == madmin.ILMService && arn.Bucket != ""
}

func parseTransitionedObject(meta map[string]string) *TransitionedObject {
	var ref TransitionedObject
	if json.Unmarshal([]byte(meta[transitionReferenceKey]), &ref) != nil || !ref.valid() {
		return nil
	}
	return &ref
}

func setTransitionedObject(meta map[string]string, ref *TransitionedObject) error {
	if ref == nil {
		return nil
	}
	if !ref.valid() || meta == nil {
		return fmt.Errorf("invalid transition reference")
	}
	data, err := json.Marshal(ref)
	if err == nil {
		meta[transitionReferenceKey] = string(data)
	}
	return err
}

func lifecycleTargetLock(objAPI ObjectLayer, bucket string) RWLocker {
	return objAPI.NewNSLock(otterioMetaBucket, pathJoin(bucketConfigPrefix, bucket, ".targets.lock"))
}

func lifecycleObjectOpts(oi ObjectInfo) lifecycle.ObjectOpts {
	return lifecycle.ObjectOpts{
		Name: oi.Name, UserTags: oi.UserTags, ModTime: oi.ModTime,
		VersionID: oi.VersionID, IsLatest: oi.IsLatest, DeleteMarker: oi.DeleteMarker,
		NumVersions: oi.NumVersions, SuccessorModTime: oi.SuccessorModTime,
		TransitionStatus: oi.TransitionStatus, RestoreOngoing: oi.RestoreOngoing,
		RestoreExpires: oi.RestoreExpires,
	}
}

// Legacy records have no durable destination. Only an unambiguous matching
// current-transition target can be used; a missing or ambiguous configuration
// fails without changing source metadata or guessing another destination.
func transitionReference(ctx context.Context, bucket, object string, oi ObjectInfo) (*TransitionedObject, error) {
	if oi.TransitionedObject != nil {
		if !oi.TransitionedObject.valid() {
			return nil, fmt.Errorf("invalid persisted transition reference")
		}
		ref := *oi.TransitionedObject
		return &ref, nil
	}
	if oi.UserDefined[transitionReferenceKey] != "" {
		return nil, fmt.Errorf("corrupt persisted transition reference")
	}
	lc, err := globalLifecycleSys.Get(bucket)
	if err != nil {
		return nil, err
	}
	var ref *TransitionedObject
	for _, rule := range lc.FilterActionableRules(lifecycleObjectOpts(oi)) {
		if rule.Transition.StorageClass == "" {
			continue
		}
		arn := globalBucketTargetSys.GetRemoteArnWithLabel(ctx, bucket, rule.Transition.StorageClass)
		if arn == nil || arn.Type != madmin.ILMService {
			return nil, BucketRemoteTargetNotFound{Bucket: bucket}
		}
		if ref != nil && ref.ARN != arn.String() {
			return nil, fmt.Errorf("legacy transition destination is ambiguous")
		}
		ref = &TransitionedObject{ARN: arn.String(), Key: object, VersionID: oi.VersionID, StorageClass: rule.Transition.StorageClass}
	}
	if ref == nil {
		return nil, BucketRemoteTargetNotFound{Bucket: bucket}
	}
	return ref, nil
}

func transitionClient(ctx context.Context, sourceBucket string, ref *TransitionedObject) (*TargetClient, string, error) {
	if !ref.valid() {
		return nil, "", fmt.Errorf("invalid transition reference")
	}
	arn, _ := madmin.ParseARN(ref.ARN)
	clnt := globalBucketTargetSys.GetRemoteTargetClientForBucket(ctx, sourceBucket, ref.ARN)
	if clnt == nil {
		return nil, "", BucketRemoteTargetNotFound{Bucket: arn.Bucket}
	}
	return clnt, arn.Bucket, nil
}

func isRemoteObjectMissing(err error) bool {
	code := otteriogo.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NoSuchVersion" || code == "NoSuchObject"
}

// The backend invokes this while holding the source object's write lock and
// before removing its metadata. A failed remote operation leaves the durable
// reference available for a retry. Creating a source delete marker retains the
// old version and therefore must not delete its remote data.
func cleanupTransitionBeforeDelete(ctx context.Context, oi ObjectInfo, opts ObjectOptions) error {
	if opts.TransitionStatus != "" || oi.TransitionStatus == "" || oi.DeleteMarker ||
		(!opts.VersionPurgeStatus.Empty() && opts.VersionPurgeStatus != Complete) ||
		(opts.VersionID == "" && (opts.Versioned || opts.VersionSuspended && normalizeTransitionVersion(oi.VersionID) != "")) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ref, err := transitionReference(ctx, oi.Bucket, oi.Name, oi)
	if err != nil {
		return err
	}
	pending, err := transitionDeletionPending(oi)
	if err != nil {
		return err
	}
	clnt, bucket, err := transitionClient(ctx, oi.Bucket, ref)
	if err != nil {
		return err
	}
	// A pending upload can have committed before its acknowledgement was lost.
	// Its unique key and deterministic native version ID still identify it.
	if oi.TransitionStatus == lifecycle.TransitionPending {
		remote, serr := clnt.StatObject(ctx, bucket, ref.Key, otteriogo.StatObjectOptions{VersionID: ref.VersionID})
		if isRemoteObjectMissing(serr) && ref.VersionID != "" {
			remote, serr = clnt.StatObject(ctx, bucket, ref.Key, otteriogo.StatObjectOptions{})
		}
		if serr != nil {
			if isRemoteObjectMissing(serr) {
				return nil
			}
			return serr
		}
		if remote.Size != oi.Size || !remoteTransitionIdentity(remote.Metadata, ref.Key) {
			return fmt.Errorf("remote transition identity does not match source")
		}
		ref.VersionID = remote.VersionID
	}
	if !pending {
		// Persist the irreversible operation before touching the tier. If the
		// subsequent local commit fails, scanning can finish without relying on
		// the continued existence of the lifecycle rule or a remote HEAD guess.
		obj := newObjectLayerFn()
		if obj == nil {
			return errServerNotInitialized
		}
		intent, err := json.Marshal(ref)
		if err != nil {
			return err
		}
		_, err = obj.PutObjectMetadata(ctx, oi.Bucket, oi.Name, ObjectOptions{
			VersionID: oi.VersionID, MTime: oi.ModTime, NoLock: true,
			TransitionExpected: &oi, TransitionedObject: ref,
			UserDefined: map[string]string{transitionDeleteIntentKey: string(intent)},
		})
		if err != nil {
			return err
		}
	}
	err = clnt.RemoveObject(ctx, bucket, ref.Key, otteriogo.RemoveObjectOptions{VersionID: ref.VersionID})
	if isRemoteObjectMissing(err) {
		return nil
	}
	return err
}

func remoteTransitionIdentity(meta map[string][]string, key string) bool {
	for name, values := range meta {
		if strings.EqualFold(name, "X-Amz-Meta-"+transitionIdentityMetadata) && len(values) == 1 && values[0] == key {
			return true
		}
	}
	return false
}
