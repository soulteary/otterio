/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
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
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	otteriogo "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/tags"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	sse "github.com/soulteary/otterio/pkg/bucket/encryption"
	"github.com/soulteary/otterio/pkg/bucket/lifecycle"
	"github.com/soulteary/otterio/pkg/event"
	"github.com/soulteary/otterio/pkg/hash"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/soulteary/otterio/pkg/s3select"
)

const (
	// Disabled means the lifecycle rule is inactive
	Disabled = "Disabled"
)

// LifecycleSys - Bucket lifecycle subsystem.
type LifecycleSys struct{}

// Get - gets lifecycle config associated to a given bucket name.
func (sys *LifecycleSys) Get(bucketName string) (lc *lifecycle.Lifecycle, err error) {
	if globalIsGateway {
		objAPI := newObjectLayerFn()
		if objAPI == nil {
			return nil, errServerNotInitialized
		}

		return nil, BucketLifecycleNotFound{Bucket: bucketName}
	}

	return globalBucketMetadataSys.GetLifecycleConfig(bucketName)
}

// NewLifecycleSys - creates new lifecycle system.
func NewLifecycleSys() *LifecycleSys {
	return &LifecycleSys{}
}

type expiryTask struct {
	objInfo       ObjectInfo
	versionExpiry bool
}

type expiryState struct {
	expiryCh chan expiryTask
}

func (es *expiryState) queueExpiryTask(oi ObjectInfo, rmVersion bool) {
	if GlobalContext.Err() != nil {
		return
	}
	select {
	case <-GlobalContext.Done():
		return
	case es.expiryCh <- expiryTask{objInfo: oi, versionExpiry: rmVersion}:
	default:
	}
}

var (
	globalExpiryState *expiryState
)

func newExpiryState() *expiryState {
	return &expiryState{
		expiryCh: make(chan expiryTask, 10000),
	}
}

func initBackgroundExpiry(ctx context.Context, objectAPI ObjectLayer) {
	globalExpiryState = newExpiryState()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-globalExpiryState.expiryCh:
				applyExpiryRule(ctx, objectAPI, t.objInfo, false, t.versionExpiry)
			}
		}
	}()
}

type transitionState struct {
	// add future metrics here
	transitionCh chan ObjectInfo
}

func (t *transitionState) queueTransitionTask(oi ObjectInfo) {
	if GlobalContext.Err() != nil {
		return
	}
	select {
	case <-GlobalContext.Done():
		return
	case t.transitionCh <- oi:
	default:
	}
}

var (
	globalTransitionState      *transitionState
	globalTransitionConcurrent = runtime.GOMAXPROCS(0) / 2
)

func newTransitionState() *transitionState {
	// fix minimum concurrent transition to 1 for single CPU setup
	if globalTransitionConcurrent == 0 {
		globalTransitionConcurrent = 1
	}
	return &transitionState{
		transitionCh: make(chan ObjectInfo, 10000),
	}
}

// addWorker creates a new worker to process tasks
func (t *transitionState) addWorker(ctx context.Context, objectAPI ObjectLayer) {
	// Add a new worker.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case oi, ok := <-t.transitionCh:
				if !ok {
					return
				}
				if err := transitionObject(ctx, objectAPI, oi); err != nil {
					logger.LogIf(ctx, err)
				}
			}
		}
	}()
}

func initBackgroundTransition(ctx context.Context, objectAPI ObjectLayer) {
	if globalTransitionState == nil {
		return
	}

	// Start with globalTransitionConcurrent.
	for i := 0; i < globalTransitionConcurrent; i++ {
		globalTransitionState.addWorker(ctx, objectAPI)
	}
}

func validateLifecycleTransition(ctx context.Context, bucket string, lfc *lifecycle.Lifecycle) error {
	for _, rule := range lfc.Rules {
		for _, storageClass := range []string{rule.Transition.StorageClass, rule.NoncurrentVersionTransition.StorageClass} {
			if storageClass == "" {
				continue
			}
			sameTarget, destbucket, err := validateTransitionDestination(ctx, bucket, storageClass)
			if err != nil {
				return err
			}
			if !globalIsErasure || !lifecycleTransitionSupported(newObjectLayerFn()) {
				return NotImplemented{Message: "Lifecycle transition requires erasure storage"}
			}
			if sameTarget && destbucket == bucket {
				return fmt.Errorf("Transition destination cannot be the same as the source bucket")
			}
		}
	}
	return nil
}

// validateTransitionDestination returns error if transition destination bucket missing or not configured
// It also returns true if transition destination is same as this server.
func validateTransitionDestination(ctx context.Context, bucket string, targetLabel string) (bool, string, error) {
	tgt := globalBucketTargetSys.GetRemoteTargetWithLabel(ctx, bucket, targetLabel)
	if tgt == nil {
		return false, "", BucketRemoteTargetNotFound{Bucket: bucket}
	}
	arn, err := madmin.ParseARN(tgt.Arn)
	if err != nil {
		return false, "", BucketRemoteTargetNotFound{Bucket: bucket}
	}
	if arn.Type != madmin.ILMService {
		return false, "", BucketRemoteArnTypeInvalid{}
	}
	clnt := globalBucketTargetSys.GetRemoteTargetClientForBucket(ctx, bucket, tgt.Arn)
	if clnt == nil {
		return false, "", BucketRemoteTargetNotFound{Bucket: bucket}
	}
	if found, _ := clnt.BucketExists(ctx, arn.Bucket); !found {
		return false, "", BucketRemoteDestinationNotFound{Bucket: arn.Bucket}
	}
	sameTarget, _ := isLocalHost(clnt.EndpointURL().Hostname(), clnt.EndpointURL().Port(), globalOtterioPort)
	return sameTarget, arn.Bucket, nil
}

// transitionSC returns storage class label for this bucket
func transitionSC(_ context.Context, bucket string) string {
	cfg, err := globalBucketMetadataSys.GetLifecycleConfig(bucket)
	if err != nil {
		return ""
	}
	for _, rule := range cfg.Rules {
		if rule.Status == Disabled {
			continue
		}
		if rule.Transition.StorageClass != "" {
			return rule.Transition.StorageClass
		}
	}
	return ""
}

// set PutObjectOptions for PUT operation to transition data to target cluster
func putTransitionOpts(objInfo ObjectInfo) (putOpts otteriogo.PutObjectOptions, err error) {
	meta := make(map[string]string)

	putOpts = otteriogo.PutObjectOptions{
		UserMetadata:    meta,
		ContentType:     objInfo.ContentType,
		ContentEncoding: objInfo.ContentEncoding,
		StorageClass:    objInfo.StorageClass,
		Internal: otteriogo.AdvancedPutOptions{
			SourceVersionID: objInfo.VersionID,
			SourceMTime:     objInfo.ModTime,
			SourceETag:      objInfo.ETag,
		},
	}
	for k, v := range objInfo.UserDefined {
		if strings.HasPrefix(strings.ToLower(k), "x-amz-meta-") && !strings.EqualFold(k, "x-amz-meta-"+transitionIdentityMetadata) {
			putOpts.UserMetadata[k] = v
		}
	}
	if objInfo.TransitionedObject != nil {
		putOpts.Internal.SourceVersionID = objInfo.TransitionedObject.VersionID
		putOpts.UserMetadata[transitionIdentityMetadata] = objInfo.TransitionedObject.Key
	}

	if objInfo.UserTags != "" {
		tag, _ := tags.ParseObjectTags(objInfo.UserTags)
		if tag != nil {
			putOpts.UserTags = tag.ToMap()
		}
	}

	lkMap := caseInsensitiveMap(objInfo.UserDefined)
	if lang, ok := lkMap.Lookup(xhttp.ContentLanguage); ok {
		putOpts.ContentLanguage = lang
	}
	if disp, ok := lkMap.Lookup(xhttp.ContentDisposition); ok {
		putOpts.ContentDisposition = disp
	}
	if cc, ok := lkMap.Lookup(xhttp.CacheControl); ok {
		putOpts.CacheControl = cc
	}
	if mode, ok := lkMap.Lookup(xhttp.AmzObjectLockMode); ok {
		rmode := otteriogo.RetentionMode(mode)
		putOpts.Mode = rmode
	}
	if retainDateStr, ok := lkMap.Lookup(xhttp.AmzObjectLockRetainUntilDate); ok {
		rdate, err := time.Parse(time.RFC3339, retainDateStr)
		if err != nil {
			return putOpts, err
		}
		putOpts.RetainUntilDate = rdate
	}
	if lhold, ok := lkMap.Lookup(xhttp.AmzObjectLockLegalHold); ok {
		putOpts.LegalHold = otteriogo.LegalHoldStatus(lhold)
	}

	return putOpts, nil
}

// handle deletes of transitioned objects or object versions when one of the following is true:
// 1. temporarily restored copies of objects (restored with the PostRestoreObject API) expired.
// 2. life cycle expiry date is met on the object.
// 3. Object is removed through DELETE api call
func deleteTransitionedObject(ctx context.Context, objectAPI ObjectLayer, bucket, object string, lcOpts lifecycle.ObjectOpts, restoredObject, isDeleteTierOnly bool, expected ...ObjectInfo) error {
	if !isDeleteTierOnly {
		queued := ObjectInfo{Bucket: bucket, Name: object, VersionID: lcOpts.VersionID, ModTime: lcOpts.ModTime}
		var source *ObjectInfo
		if len(expected) != 0 {
			queued = expected[0]
			source = &expected[0]
		}
		_, err := executeExpiry(ctx, objectAPI, queued, restoredObject, lcOpts.VersionID != "" && (!lcOpts.IsLatest || lcOpts.DeleteMarker), source)
		return err
	}
	lk := objectAPI.NewNSLock(bucket, encodeDirObject(object))
	ctx, err := lk.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	oi, err := objectAPI.GetObjectInfo(ctx, bucket, object, ObjectOptions{VersionID: lcOpts.VersionID, NoLock: true})
	if err != nil {
		return err
	}
	if len(expected) != 0 && !sameTransitionSource(oi, &expected[0]) || !lcOpts.ModTime.IsZero() && !lcOpts.ModTime.Equal(oi.ModTime) {
		return PreConditionFailed{}
	}
	if oi.TransitionStatus == "" {
		return nil
	}
	return cleanupTransitionBeforeDelete(ctx, oi, ObjectOptions{VersionID: oi.VersionID, NoLock: true, TransitionExpected: &oi})
}

// prepareTransition binds a fresh source version to one durable destination
// before any remote upload. Lock order is target registry, then source object.
func prepareTransition(ctx context.Context, objectAPI ObjectLayer, queued ObjectInfo) (ObjectInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	targetLock := lifecycleTargetLock(objectAPI, queued.Bucket)
	ctx, err := targetLock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer targetLock.Unlock()
	objectLock := objectAPI.NewNSLock(queued.Bucket, encodeDirObject(queued.Name))
	ctx, err = objectLock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer objectLock.Unlock()
	return prepareTransitionLocked(ctx, objectAPI, queued)
}

func prepareTransitionLocked(ctx context.Context, objectAPI ObjectLayer, queued ObjectInfo) (ObjectInfo, error) {
	oi, err := objectAPI.GetObjectInfo(ctx, queued.Bucket, queued.Name, ObjectOptions{VersionID: queued.VersionID, NoLock: true})
	if err != nil {
		return ObjectInfo{}, err
	}
	if oi.DeleteMarker || !oi.ModTime.Equal(queued.ModTime) || oi.ETag != queued.ETag || oi.Size != queued.Size {
		return ObjectInfo{}, PreConditionFailed{}
	}
	if deleting, err := transitionDeletionPending(oi); err != nil {
		return ObjectInfo{}, err
	} else if deleting {
		return ObjectInfo{}, PreConditionFailed{}
	}
	if oi.TransitionStatus == lifecycle.TransitionComplete {
		return oi, nil
	}
	if oi.TransitionStatus == lifecycle.TransitionPending && oi.TransitionedObject != nil {
		return oi, nil
	}
	if oi.UserDefined[transitionReferenceKey] != "" {
		return ObjectInfo{}, fmt.Errorf("corrupt persisted transition reference")
	}
	meta, err := loadBucketMetadata(ctx, objectAPI, oi.Bucket)
	if err != nil {
		return ObjectInfo{}, err
	}
	lc := meta.lifecycleConfig
	if lc == nil {
		return ObjectInfo{}, BucketLifecycleNotFound{Bucket: oi.Bucket}
	}
	selection := lc.Select(lifecycleObjectOpts(oi))
	if selection.Action != lifecycle.TransitionAction && selection.Action != lifecycle.TransitionVersionAction {
		return ObjectInfo{}, PreConditionFailed{}
	}
	if !lifecycleTransitionSupported(objectAPI) {
		return ObjectInfo{}, NotImplemented{Message: "Lifecycle transition requires native single-pool local erasure storage"}
	}
	arn := globalBucketTargetSys.GetRemoteArnWithLabel(ctx, oi.Bucket, selection.StorageClass)
	if arn == nil || arn.Type != madmin.ILMService {
		return ObjectInfo{}, BucketRemoteTargetNotFound{Bucket: oi.Bucket}
	}
	ref := &TransitionedObject{ARN: arn.String(), Key: "otterio-tier/" + mustGetUUID(), StorageClass: selection.StorageClass}
	clnt, remoteBucket, err := transitionClient(ctx, oi.Bucket, ref)
	if err != nil {
		return ObjectInfo{}, err
	}
	versioning, err := clnt.GetBucketVersioning(ctx, remoteBucket)
	if err != nil {
		return ObjectInfo{}, err
	}
	if versioning.Status == "Enabled" {
		ref.VersionID = mustGetUUID()
	}
	if err = pinLifecycleTarget(ctx, objectAPI, oi.Bucket, oi, ref); err != nil {
		return ObjectInfo{}, err
	}
	if _, err = objectAPI.DeleteObject(ctx, oi.Bucket, oi.Name, ObjectOptions{
		VersionID: oi.VersionID, TransitionStatus: lifecycle.TransitionPending,
		TransitionedObject: ref, TransitionExpected: &oi, NoLock: true,
	}); err != nil {
		return ObjectInfo{}, err
	}
	return objectAPI.GetObjectInfo(ctx, oi.Bucket, oi.Name, ObjectOptions{VersionID: oi.VersionID, NoLock: true})
}

// transitionObject retains the source write lock until the remote upload and
// its exact destination version have committed. Retry uses the persisted key.
func transitionObject(ctx context.Context, objectAPI ObjectLayer, queued ObjectInfo) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	prepared, err := prepareTransition(ctx, objectAPI, queued)
	if err != nil {
		return err
	}
	queued = prepared
	if prepared.TransitionStatus == lifecycle.TransitionPending && !lifecycleTransitionSupported(objectAPI) {
		return NotImplemented{Message: "Lifecycle transition requires native single-pool local erasure storage"}
	}
	objectLock := objectAPI.NewNSLock(queued.Bucket, encodeDirObject(queued.Name))
	ctx, err = objectLock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return err
	}
	defer objectLock.Unlock()
	oi, err := objectAPI.GetObjectInfo(ctx, queued.Bucket, queued.Name, ObjectOptions{VersionID: queued.VersionID, NoLock: true})
	if err != nil {
		return err
	}
	if !sameTransitionSource(oi, &prepared) || oi.TransitionStatus != lifecycle.TransitionComplete &&
		(oi.TransitionStatus != lifecycle.TransitionPending || oi.TransitionedObject == nil ||
			prepared.TransitionedObject == nil || *oi.TransitionedObject != *prepared.TransitionedObject) {
		return PreConditionFailed{}
	}
	if deleting, err := transitionDeletionPending(oi); err != nil {
		return err
	} else if deleting {
		return PreConditionFailed{}
	}
	if oi.TransitionStatus == lifecycle.TransitionComplete {
		if oi.RestoreOngoing {
			days, err := strconv.Atoi(oi.UserDefined[xhttp.AmzRestoreExpiryDays])
			if err != nil || days <= 0 {
				return fmt.Errorf("invalid persisted restore duration")
			}
			requested, err := time.Parse(http.TimeFormat, oi.UserDefined[xhttp.AmzRestoreRequestDate])
			if err != nil {
				return err
			}
			return restoreTransitionedObjectLocked(ctx, oi.Bucket, oi.Name, objectAPI, oi,
				&RestoreObjectRequest{Days: days}, lifecycle.ExpectedExpiryTime(requested, days))
		}
		return nil
	}
	ref := *oi.TransitionedObject
	clnt, remoteBucket, err := transitionClient(ctx, oi.Bucket, &ref)
	if err != nil {
		return err
	}
	remote, statErr := clnt.StatObject(ctx, remoteBucket, ref.Key, otteriogo.StatObjectOptions{VersionID: ref.VersionID})
	if statErr != nil && isRemoteObjectMissing(statErr) && ref.VersionID != "" {
		// S3 implementations may allocate their own version ID. The unique key
		// and identity marker recover an upload whose acknowledgement was lost.
		remote, statErr = clnt.StatObject(ctx, remoteBucket, ref.Key, otteriogo.StatObjectOptions{})
	}
	if statErr != nil && !isRemoteObjectMissing(statErr) {
		return statErr
	}
	if statErr == nil {
		if remote.Size != oi.Size || !remoteTransitionIdentity(remote.Metadata, ref.Key) {
			return fmt.Errorf("remote transition identity does not match source")
		}
		ref.VersionID = remote.VersionID
	} else {
		gr, err := objectAPI.GetObjectNInfo(ctx, oi.Bucket, oi.Name, nil, http.Header{}, noLock,
			ObjectOptions{VersionID: oi.VersionID, TransitionStatus: lifecycle.TransitionPending, NoLock: true})
		if err != nil {
			return err
		}
		putOpts, err := putTransitionOpts(oi)
		if err != nil {
			gr.Close()
			return err
		}
		uploaded, putErr := clnt.PutObject(ctx, remoteBucket, ref.Key, gr, oi.Size, putOpts)
		gr.Close()
		if putErr != nil {
			return putErr
		}
		ref.VersionID = uploaded.VersionID
	}
	_, err = objectAPI.DeleteObject(ctx, oi.Bucket, oi.Name, ObjectOptions{
		VersionID: oi.VersionID, TransitionStatus: lifecycle.TransitionComplete,
		TransitionedObject: &ref, TransitionExpected: &oi, NoLock: true,
	})
	return err
}

func getTransitionedObjectReader(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, oi ObjectInfo, opts ObjectOptions) (gr *GetObjectReader, err error) {
	ref, err := transitionReference(ctx, bucket, object, oi)
	if err != nil {
		return nil, err
	}
	clnt, remoteBucket, err := transitionClient(ctx, oi.Bucket, ref)
	if err != nil {
		return nil, err
	}
	fn, off, length, err := NewGetObjectReader(rs, oi, opts)
	if err != nil {
		return nil, err
	}
	gopts := otteriogo.GetObjectOptions{VersionID: ref.VersionID}
	if off >= 0 && length > 0 {
		if err = gopts.SetRange(off, off+length-1); err != nil {
			return nil, err
		}
	}
	reader, err := clnt.GetObject(ctx, remoteBucket, ref.Key, gopts)
	if err != nil {
		return nil, err
	}
	return fn(reader, h, opts.CheckPrecondFn, func() { _ = reader.Close() })
}

// RestoreRequestType represents type of restore.
type RestoreRequestType string

const (
	// SelectRestoreRequest specifies select request. This is the only valid value
	SelectRestoreRequest RestoreRequestType = "SELECT"
)

// Encryption specifies encryption setting on restored bucket
type Encryption struct {
	EncryptionType sse.SSEAlgorithm `xml:"EncryptionType"`
	KMSContext     string           `xml:"KMSContext,omitempty"`
	KMSKeyID       string           `xml:"KMSKeyId,omitempty"`
}

// MetadataEntry denotes name and value.
type MetadataEntry struct {
	Name  string `xml:"Name"`
	Value string `xml:"Value"`
}

// S3Location specifies s3 location that receives result of a restore object request
type S3Location struct {
	BucketName   string          `xml:"BucketName,omitempty"`
	Encryption   Encryption      `xml:"Encryption,omitempty"`
	Prefix       string          `xml:"Prefix,omitempty"`
	StorageClass string          `xml:"StorageClass,omitempty"`
	Tagging      *tags.Tags      `xml:"Tagging,omitempty"`
	UserMetadata []MetadataEntry `xml:"UserMetadata"`
}

// OutputLocation specifies bucket where object needs to be restored
type OutputLocation struct {
	S3 S3Location `xml:"S3,omitempty"`
}

// IsEmpty returns true if output location not specified.
func (o *OutputLocation) IsEmpty() bool {
	return o.S3.BucketName == ""
}

// SelectParameters specifies sql select parameters
type SelectParameters struct {
	s3select.S3Select
}

// IsEmpty returns true if no select parameters set
func (sp *SelectParameters) IsEmpty() bool {
	return sp == nil
}

var (
	selectParamsXMLName = "SelectParameters"
)

// UnmarshalXML - decodes XML data.
func (sp *SelectParameters) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	// Essentially the same as S3Select barring the xml name.
	if start.Name.Local == selectParamsXMLName {
		start.Name = xml.Name{Space: "", Local: "SelectRequest"}
	}
	return sp.S3Select.UnmarshalXML(d, start)
}

// RestoreObjectRequest - xml to restore a transitioned object
type RestoreObjectRequest struct {
	XMLName          xml.Name           `xml:"http://s3.amazonaws.com/doc/2006-03-01/ RestoreRequest" json:"-"`
	Days             int                `xml:"Days,omitempty"`
	Type             RestoreRequestType `xml:"Type,omitempty"`
	Tier             string             `xml:"Tier,-"`
	Description      string             `xml:"Description,omitempty"`
	SelectParameters *SelectParameters  `xml:"SelectParameters,omitempty"`
	OutputLocation   OutputLocation     `xml:"OutputLocation,omitempty"`
}

// Maximum 2MiB size per restore object request.
const maxRestoreObjectRequestSize = 2 << 20

// parseRestoreRequest parses RestoreObjectRequest from xml
func parseRestoreRequest(reader io.Reader) (*RestoreObjectRequest, error) {
	// Consume the authenticated body through EOF before decoding it. XML
	// decoding alone can stop at the closing root before a signed hash reader
	// reports a digest mismatch or a transport reports a truncated body.
	data, err := io.ReadAll(io.LimitReader(reader, maxRestoreObjectRequestSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRestoreObjectRequestSize {
		return nil, fmt.Errorf("restore request exceeds its size limit")
	}
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	depth, roots := 0, 0
	seen := make(map[string]bool)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots != 1 || token.Name.Local != "RestoreRequest" {
					return nil, fmt.Errorf("restore request must contain one RestoreRequest root")
				}
			}
			if depth == 1 {
				if seen[token.Name.Local] {
					return nil, fmt.Errorf("duplicate restore field %s", token.Name.Local)
				}
				seen[token.Name.Local] = true
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return nil, fmt.Errorf("data outside restore request")
			}
		case xml.Directive:
			return nil, fmt.Errorf("XML directives are not supported in restore requests")
		case xml.ProcInst:
			if roots != 0 || token.Target != "xml" {
				return nil, fmt.Errorf("unexpected processing instruction in restore request")
			}
		}
	}
	req := RestoreObjectRequest{}
	if err := xml.NewDecoder(strings.NewReader(string(data))).Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

// validate a RestoreObjectRequest as per AWS S3 spec https://docs.aws.amazon.com/AmazonS3/latest/API/API_RestoreObject.html
func (r *RestoreObjectRequest) validate(ctx context.Context, objAPI ObjectLayer) error {
	if r.Type != "" && r.Type != SelectRestoreRequest {
		return fmt.Errorf("unsupported restore request type %s", r.Type)
	}
	if r.Days < 0 {
		return fmt.Errorf("restoration days cannot be negative")
	}
	if r.Type != SelectRestoreRequest && !r.SelectParameters.IsEmpty() {
		return fmt.Errorf("Select parameters can only be specified with SELECT request type")
	}
	if r.Type == SelectRestoreRequest && r.SelectParameters.IsEmpty() {
		return fmt.Errorf("SELECT restore request requires select parameters to be specified")
	}

	if r.Type != SelectRestoreRequest && !r.OutputLocation.IsEmpty() {
		return fmt.Errorf("OutputLocation required only for SELECT request type")
	}
	if r.Type == SelectRestoreRequest && r.OutputLocation.IsEmpty() {
		return fmt.Errorf("OutputLocation required for SELECT requests")
	}

	if r.Days != 0 && r.Type == SelectRestoreRequest {
		return fmt.Errorf("Days cannot be specified with SELECT restore request")
	}
	if r.Days <= 0 && r.Type != SelectRestoreRequest {
		return fmt.Errorf("restoration days should be at least 1")
	}
	// Check if bucket exists.
	if !r.OutputLocation.IsEmpty() {
		if _, err := objAPI.GetBucketInfo(ctx, r.OutputLocation.S3.BucketName); err != nil {
			return err
		}
		if r.OutputLocation.S3.Prefix == "" {
			return fmt.Errorf("Prefix is a required parameter in OutputLocation")
		}
		if r.OutputLocation.S3.Encryption.EncryptionType != xhttp.AmzEncryptionAES {
			return NotImplemented{}
		}
	}
	return nil
}

// set ObjectOptions for PUT call to restore temporary copy of transitioned data
func putRestoreOpts(bucket string, _ string, rreq *RestoreObjectRequest, objInfo ObjectInfo) (putOpts ObjectOptions) {
	meta := make(map[string]string)
	sc := rreq.OutputLocation.S3.StorageClass
	if sc == "" {
		sc = objInfo.StorageClass
	}
	meta[strings.ToLower(xhttp.AmzStorageClass)] = sc

	if rreq.Type == SelectRestoreRequest {
		for _, v := range rreq.OutputLocation.S3.UserMetadata {
			if !strings.HasPrefix("x-amz-meta", strings.ToLower(v.Name)) {
				meta["x-amz-meta-"+v.Name] = v.Value
				continue
			}
			meta[v.Name] = v.Value
		}
		if tags := rreq.OutputLocation.S3.Tagging.String(); tags != "" {
			meta[xhttp.AmzObjectTagging] = tags
		}
		if rreq.OutputLocation.S3.Encryption.EncryptionType != "" {
			meta[xhttp.AmzServerSideEncryption] = xhttp.AmzEncryptionAES
		}
		return ObjectOptions{
			Versioned:        globalBucketVersioningSys.Enabled(bucket),
			VersionSuspended: globalBucketVersioningSys.Suspended(bucket),
			UserDefined:      meta,
		}
	}
	for k, v := range objInfo.UserDefined {
		meta[k] = v
	}
	meta["etag"] = objInfo.ETag
	if len(objInfo.UserTags) != 0 {
		meta[xhttp.AmzObjectTagging] = objInfo.UserTags
	}

	return ObjectOptions{
		Versioned:          globalBucketVersioningSys.Enabled(bucket),
		VersionSuspended:   globalBucketVersioningSys.Suspended(bucket),
		UserDefined:        meta,
		VersionID:          objInfo.VersionID,
		MTime:              objInfo.ModTime,
		Expires:            objInfo.Expires,
		TransitionStatus:   lifecycle.TransitionComplete,
		TransitionedObject: objInfo.TransitionedObject,
		TransitionExpected: &objInfo,
		TransitionRestore:  &objInfo,
		NoLock:             true,
	}
}

var (
	errRestoreHDRMissing   = fmt.Errorf("x-amz-restore header not found")
	errRestoreHDRMalformed = fmt.Errorf("x-amz-restore header malformed")
)

// parse x-amz-restore header from user metadata to get the status of ongoing request and expiry of restoration
// if any. This header value is of format: ongoing-request=true|false, expires=time
func parseRestoreHeaderFromMeta(meta map[string]string) (ongoing bool, expiry time.Time, err error) {
	restoreHdr, ok := meta[xhttp.AmzRestore]
	if !ok {
		return ongoing, expiry, errRestoreHDRMissing
	}
	rslc := strings.SplitN(restoreHdr, ",", 2)
	if len(rslc) == 1 && strings.TrimSpace(rslc[0]) == "ongoing-request=true" {
		return true, time.Time{}, nil
	}
	if len(rslc) != 2 {
		return ongoing, expiry, errRestoreHDRMalformed
	}
	rstatusSlc := strings.SplitN(rslc[0], "=", 2)
	if len(rstatusSlc) != 2 {
		return ongoing, expiry, errRestoreHDRMalformed
	}
	rExpSlc := strings.SplitN(rslc[1], "=", 2)
	if len(rExpSlc) != 2 {
		return ongoing, expiry, errRestoreHDRMalformed
	}

	expiry, err = time.Parse(http.TimeFormat, rExpSlc[1])
	if err != nil {
		return
	}
	return rstatusSlc[1] == "true", expiry, nil
}

// restoreTransitionedObject is similar to PostObjectRestore from AWS GLACIER
// storage class. When PostObjectRestore API is called, a temporary copy of the object
// is restored locally to the bucket on source cluster until the restore expiry date.
// The copy that was transitioned continues to reside in the transitioned tier.
func restoreTransitionedObject(ctx context.Context, bucket, object string, objAPI ObjectLayer, objInfo ObjectInfo, rreq *RestoreObjectRequest, restoreExpiry time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	objectLock := objAPI.NewNSLock(bucket, encodeDirObject(object))
	ctx, err := objectLock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return err
	}
	defer objectLock.Unlock()
	fresh, err := objAPI.GetObjectInfo(ctx, bucket, object, ObjectOptions{VersionID: objInfo.VersionID, NoLock: true})
	if err != nil {
		return err
	}
	if !fresh.ModTime.Equal(objInfo.ModTime) || fresh.ETag != objInfo.ETag || fresh.TransitionStatus != lifecycle.TransitionComplete {
		return PreConditionFailed{}
	}
	objInfo = fresh
	if !objInfo.RestoreOngoing && !objInfo.RestoreExpires.IsZero() && time.Now().Before(objInfo.RestoreExpires) {
		return nil
	}
	return restoreTransitionedObjectLocked(ctx, bucket, object, objAPI, objInfo, rreq, restoreExpiry)
}

func restoreTransitionedObjectLocked(ctx context.Context, bucket, object string, objAPI ObjectLayer, objInfo ObjectInfo, rreq *RestoreObjectRequest, restoreExpiry time.Time) error {
	if deleting, err := transitionDeletionPending(objInfo); err != nil {
		return err
	} else if deleting {
		return PreConditionFailed{}
	}
	var rs *HTTPRangeSpec
	gr, err := getTransitionedObjectReader(ctx, bucket, object, rs, http.Header{}, objInfo, ObjectOptions{
		VersionID: objInfo.VersionID, TransitionStatus: lifecycle.TransitionPending})
	if err != nil {
		return err
	}
	defer gr.Close()
	hashReader, err := hash.NewReader(gr, objInfo.Size, "", "", objInfo.Size)
	if err != nil {
		return err
	}
	pReader := NewPutObjReader(hashReader)
	opts := putRestoreOpts(bucket, object, rreq, objInfo)
	opts.UserDefined[xhttp.AmzRestore] = fmt.Sprintf("ongoing-request=%t, expiry-date=%s", false, restoreExpiry.Format(http.TimeFormat))
	restored, err := objAPI.PutObject(ctx, bucket, object, pReader, opts)
	if err != nil {
		return err
	}
	args, ok := ctx.Value(transitionRestoreEventContextKey{}).(eventArgs)
	if !ok {
		args = eventArgs{ReqParams: map[string]string{"region": globalServerRegion}, Host: "Internal: [RESTORE]"}
	}
	args.EventName, args.BucketName, args.Object = event.ObjectRestorePostCompleted, bucket, restored
	sendEvent(args)

	return nil
}

// A request snapshot is optional: scanner recovery can complete a durable
// restore after the original HTTP request and process have gone away.
type transitionRestoreEventContextKey struct{}

func withTransitionRestoreEvent(ctx context.Context, args eventArgs) context.Context {
	args.ReqParams = cloneMSS(args.ReqParams)
	args.RespElements = cloneMSS(args.RespElements)
	return context.WithValue(ctx, transitionRestoreEventContextKey{}, args)
}

var errTransitionRestoreInProgress = fmt.Errorf("transition restore already in progress")

// Reserve the exact source version atomically. The durable request date and
// duration let the scanner resume unfinished work after a process restart.
func beginTransitionRestore(ctx context.Context, objAPI ObjectLayer, bucket, object string, expected ObjectInfo, days int) (ObjectInfo, bool, error) {
	lk := objAPI.NewNSLock(bucket, encodeDirObject(object))
	ctx, err := lk.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return ObjectInfo{}, false, err
	}
	defer lk.Unlock()
	oi, err := objAPI.GetObjectInfo(ctx, bucket, object, ObjectOptions{VersionID: expected.VersionID, NoLock: true})
	if err != nil {
		return ObjectInfo{}, false, err
	}
	if days <= 0 || oi.TransitionStatus != lifecycle.TransitionComplete || !oi.ModTime.Equal(expected.ModTime) || oi.ETag != expected.ETag {
		return ObjectInfo{}, false, PreConditionFailed{}
	}
	if deleting, err := transitionDeletionPending(oi); err != nil {
		return ObjectInfo{}, false, err
	} else if deleting {
		return ObjectInfo{}, false, PreConditionFailed{}
	}
	if oi.RestoreOngoing {
		return ObjectInfo{}, false, errTransitionRestoreInProgress
	}
	alreadyRestored := !oi.RestoreExpires.IsZero() && time.Now().Before(oi.RestoreExpires)
	meta := cloneMSS(oi.UserDefined)
	now := time.Now().UTC()
	meta[xhttp.AmzRestoreExpiryDays] = strconv.Itoa(days)
	meta[xhttp.AmzRestoreRequestDate] = now.Format(http.TimeFormat)
	if alreadyRestored {
		meta[xhttp.AmzRestore] = fmt.Sprintf("ongoing-request=false, expiry-date=%s", lifecycle.ExpectedExpiryTime(now, days).Format(http.TimeFormat))
	} else {
		meta[xhttp.AmzRestore] = "ongoing-request=true"
	}
	oi, err = objAPI.PutObjectMetadata(ctx, bucket, object, ObjectOptions{
		VersionID: oi.VersionID, MTime: oi.ModTime, UserDefined: meta,
		NoLock: true, TransitionExpected: &oi,
	})
	return oi, alreadyRestored, err
}
