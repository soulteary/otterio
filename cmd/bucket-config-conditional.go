// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"
)

type bucketConfigCommit struct {
	revision string
	exists   bool
}

var errBucketConfigInvalidState = errors.New("versioning cannot be suspended while object lock or replication is configured")

const (
	bucketConfigCapabilityHeader = "X-Otterio-Bucket-Config"
	bucketConfigRevisionHeader   = "X-Otterio-Config-Revision"
	bucketConfigIfMatchHeader    = "X-Otterio-Config-If-Match"
	bucketConfigExistsHeader     = "X-Otterio-Config-Exists"
)

func bucketMetadataTransactionKey(bucket string) string {
	return path.Join(bucketConfigPrefix, bucket, ".transaction.lock")
}

func bucketMetadataTransactionSupported(object ObjectLayer) bool {
	switch object.(type) {
	case *FSObjects, *erasureServerPools:
		return true
	default:
		return false
	}
}

func bucketMetadataErasureBackend(object ObjectLayer) bool {
	_, ok := object.(*erasureServerPools)
	return ok
}

func bucketConfigSupported(file string) bool {
	if globalIsGateway {
		return false
	}
	switch obj := newObjectLayerFn().(type) {
	case *FSObjects:
		return file == bucketPolicyConfig || file == bucketLifecycleConfig
	case *erasureServerPools:
		return obj.SinglePool() && (file == bucketPolicyConfig || file == bucketLifecycleConfig || file == bucketVersioningConfig)
	default:
		return false
	}
}

func bucketConfigKind(file string) string {
	switch file {
	case bucketPolicyConfig:
		return "policy"
	case bucketLifecycleConfig:
		return "lifecycle"
	case bucketVersioningConfig:
		return "versioning"
	default:
		return file
	}
}

func bucketConfigBytes(meta BucketMetadata, file string) []byte {
	switch file {
	case bucketPolicyConfig:
		return meta.PolicyConfigJSON
	case bucketLifecycleConfig:
		return meta.LifecycleConfigXML
	case bucketVersioningConfig:
		return meta.VersioningConfigXML
	default:
		return nil
	}
}

func bucketConfigRevision(file string, data []byte) string {
	h := sha256.New()
	h.Write([]byte(bucketConfigKind(file)))
	h.Write([]byte{0})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// Called after authentication. Vendor headers are covered by SigV4's mandatory
// signed-header check, but V2 and anonymous policy grants cannot use this CAS.
func bucketConfigCondition(r *http.Request, file string) (string, APIErrorCode) {
	values := r.Header.Values(bucketConfigIfMatchHeader)
	if len(values) == 0 {
		return "", ErrNone
	}
	if len(values) != 1 || len(values[0]) != 64 || strings.ToLower(values[0]) != values[0] {
		return "", ErrInvalidRequest
	}
	if _, err := hex.DecodeString(values[0]); err != nil {
		return "", ErrInvalidRequest
	}
	if getRequestAuthType(r) != authTypeSigned {
		return "", ErrAccessDenied
	}
	signed, err := parseSignV4(r.Header.Get("Authorization"), "", serviceS3)
	if err != ErrNone || !contains(signed.SignedHeaders, strings.ToLower(bucketConfigIfMatchHeader)) {
		return "", ErrUnsignedHeaders
	}
	if !bucketConfigSupported(file) {
		return "", ErrNotImplemented
	}
	return values[0], ErrNone
}

// ReadConfigWithRevision protects a fresh persistent read against legacy migration and
// updates too. Never calculate a revision from a possibly stale peer cache.
func (sys *BucketMetadataSys) ReadConfigWithRevision(ctx context.Context, bucket, file string) ([]byte, string, error) {
	if !bucketConfigSupported(file) {
		return nil, "", NotImplemented{}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	obj := newObjectLayerFn()
	lock := obj.NewNSLock(otterioMetaBucket, bucketMetadataTransactionKey(bucket))
	ctx, err := lock.GetLock(ctx, newDynamicTimeout(10*time.Second, time.Second))
	if err != nil {
		return nil, "", err
	}
	defer lock.Unlock()
	if _, err := obj.GetBucketInfo(ctx, bucket); err != nil {
		return nil, "", err
	}
	meta, err := loadBucketMetadataUnlocked(ctx, obj, bucket)
	if err != nil {
		return nil, "", err
	}
	sys.Set(bucket, meta)
	data := append([]byte(nil), bucketConfigBytes(meta, file)...)
	return data, bucketConfigRevision(file, data), nil
}

func bucketConfigResponse(w http.ResponseWriter, revision string, exists bool) {
	w.Header().Set(bucketConfigCapabilityHeader, "v1")
	w.Header().Set(bucketConfigRevisionHeader, revision)
	if exists {
		w.Header().Set(bucketConfigExistsHeader, "true")
	} else {
		w.Header().Set(bucketConfigExistsHeader, "false")
	}
}

// Authentication and bucket existence have already been checked by the S3
// handler. Returning false preserves reads on legacy or gateway backends.
func writeFreshBucketConfiguration(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, file string) bool {
	if !bucketConfigSupported(file) {
		return false
	}
	data, revision, err := globalBucketMetadataSys.ReadConfigWithRevision(ctx, bucket, file)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL, guessIsBrowserReq(r))
		return true
	}
	bucketConfigResponse(w, revision, len(data) != 0)
	if len(data) == 0 {
		switch file {
		case bucketPolicyConfig:
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchBucketPolicy), r.URL, guessIsBrowserReq(r))
			return true
		case bucketLifecycleConfig:
			writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrNoSuchLifecycleConfiguration), r.URL, guessIsBrowserReq(r))
			return true
		case bucketVersioningConfig:
			data = []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></VersioningConfiguration>`)
		}
	}
	if file == bucketPolicyConfig {
		writeSuccessResponseJSON(w, data)
	} else {
		writeSuccessResponseXML(w, data)
	}
	return true
}

func updateBucketConfiguration(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, file string, data []byte) bool {
	expected, code := bucketConfigCondition(r, file)
	if code != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(code), r.URL, guessIsBrowserReq(r))
		return false
	}
	var commit bucketConfigCommit
	if err := globalBucketMetadataSys.update(ctx, bucket, file, data, expected, &commit); err != nil {
		if errors.Is(err, errBucketConfigInvalidState) {
			writeErrorResponse(ctx, w, APIError{Code: "InvalidBucketState", Description: errBucketConfigInvalidState.Error(), HTTPStatusCode: http.StatusConflict}, r.URL, guessIsBrowserReq(r))
		} else {
			writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL, guessIsBrowserReq(r))
		}
		return false
	}
	if bucketConfigSupported(file) {
		bucketConfigResponse(w, commit.revision, commit.exists)
	}

	return true
}
