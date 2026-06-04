/*
 * MinIO Cloud Storage, (C) 2017-2019 MinIO, Inc.
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
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/soulteary/otterio/cmd/config"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/env"
	"github.com/soulteary/otterio/pkg/hash"
	xnet "github.com/soulteary/otterio/pkg/net"

	otterio "github.com/minio/minio-go/v7"
)

var (
	// CanonicalizeETag provides canonicalizeETag function alias.
	CanonicalizeETag = canonicalizeETag

	// MustGetUUID function alias.
	MustGetUUID = mustGetUUID

	// CleanMetadataKeys provides cleanMetadataKeys function alias.
	CleanMetadataKeys = cleanMetadataKeys

	// PathJoin function alias.
	PathJoin = pathJoin

	// ListObjects function alias.
	ListObjects = listObjects

	// FilterListEntries function alias.
	FilterListEntries = filterListEntries

	// IsStringEqual is string equal.
	IsStringEqual = isStringEqual
)

// FromOtterioClientMetadata converts otterio metadata to map[string]string
func FromOtterioClientMetadata(metadata map[string][]string) map[string]string {
	mm := make(map[string]string, len(metadata))
	for k, v := range metadata {
		mm[http.CanonicalHeaderKey(k)] = v[0]
	}
	return mm
}

// FromOtterioClientObjectPart converts otterio ObjectPart to PartInfo
func FromOtterioClientObjectPart(op otterio.ObjectPart) PartInfo {
	return PartInfo{
		Size:         op.Size,
		ETag:         canonicalizeETag(op.ETag),
		LastModified: op.LastModified,
		PartNumber:   op.PartNumber,
	}
}

// FromOtterioClientListPartsInfo converts otterio ListObjectPartsResult to ListPartsInfo
func FromOtterioClientListPartsInfo(lopr otterio.ListObjectPartsResult) ListPartsInfo {
	// Convert otterio ObjectPart to PartInfo
	fromOtterioClientObjectParts := func(parts []otterio.ObjectPart) []PartInfo {
		toParts := make([]PartInfo, len(parts))
		for i, part := range parts {
			toParts[i] = FromOtterioClientObjectPart(part)
		}
		return toParts
	}

	return ListPartsInfo{
		UploadID:             lopr.UploadID,
		Bucket:               lopr.Bucket,
		Object:               lopr.Key,
		StorageClass:         "",
		PartNumberMarker:     lopr.PartNumberMarker,
		NextPartNumberMarker: lopr.NextPartNumberMarker,
		MaxParts:             lopr.MaxParts,
		IsTruncated:          lopr.IsTruncated,
		Parts:                fromOtterioClientObjectParts(lopr.ObjectParts),
	}
}

// FromOtterioClientListMultipartsInfo converts otterio ListMultipartUploadsResult to ListMultipartsInfo
func FromOtterioClientListMultipartsInfo(lmur otterio.ListMultipartUploadsResult) ListMultipartsInfo {
	uploads := make([]MultipartInfo, len(lmur.Uploads))

	for i, um := range lmur.Uploads {
		uploads[i] = MultipartInfo{
			Object:    um.Key,
			UploadID:  um.UploadID,
			Initiated: um.Initiated,
		}
	}

	commonPrefixes := make([]string, len(lmur.CommonPrefixes))
	for i, cp := range lmur.CommonPrefixes {
		commonPrefixes[i] = cp.Prefix
	}

	return ListMultipartsInfo{
		KeyMarker:          lmur.KeyMarker,
		UploadIDMarker:     lmur.UploadIDMarker,
		NextKeyMarker:      lmur.NextKeyMarker,
		NextUploadIDMarker: lmur.NextUploadIDMarker,
		MaxUploads:         int(lmur.MaxUploads),
		IsTruncated:        lmur.IsTruncated,
		Uploads:            uploads,
		Prefix:             lmur.Prefix,
		Delimiter:          lmur.Delimiter,
		CommonPrefixes:     commonPrefixes,
		EncodingType:       lmur.EncodingType,
	}

}

// FromOtterioClientObjectInfo converts otterio ObjectInfo to gateway ObjectInfo
func FromOtterioClientObjectInfo(bucket string, oi otterio.ObjectInfo) ObjectInfo {
	userDefined := FromOtterioClientMetadata(oi.Metadata)
	userDefined[xhttp.ContentType] = oi.ContentType

	return ObjectInfo{
		Bucket:          bucket,
		Name:            oi.Key,
		ModTime:         oi.LastModified,
		Size:            oi.Size,
		ETag:            canonicalizeETag(oi.ETag),
		UserDefined:     userDefined,
		ContentType:     oi.ContentType,
		ContentEncoding: oi.Metadata.Get(xhttp.ContentEncoding),
		StorageClass:    oi.StorageClass,
		Expires:         oi.Expires,
	}
}

// FromOtterioClientListBucketV2Result converts otterio ListBucketResult to ListObjectsInfo
func FromOtterioClientListBucketV2Result(bucket string, result otterio.ListBucketV2Result) ListObjectsV2Info {
	objects := make([]ObjectInfo, len(result.Contents))

	for i, oi := range result.Contents {
		objects[i] = FromOtterioClientObjectInfo(bucket, oi)
	}

	prefixes := make([]string, len(result.CommonPrefixes))
	for i, p := range result.CommonPrefixes {
		prefixes[i] = p.Prefix
	}

	return ListObjectsV2Info{
		IsTruncated: result.IsTruncated,
		Prefixes:    prefixes,
		Objects:     objects,

		ContinuationToken:     result.ContinuationToken,
		NextContinuationToken: result.NextContinuationToken,
	}
}

// FromOtterioClientListBucketResult converts otterio ListBucketResult to ListObjectsInfo
func FromOtterioClientListBucketResult(bucket string, result otterio.ListBucketResult) ListObjectsInfo {
	objects := make([]ObjectInfo, len(result.Contents))

	for i, oi := range result.Contents {
		objects[i] = FromOtterioClientObjectInfo(bucket, oi)
	}

	prefixes := make([]string, len(result.CommonPrefixes))
	for i, p := range result.CommonPrefixes {
		prefixes[i] = p.Prefix
	}

	return ListObjectsInfo{
		IsTruncated: result.IsTruncated,
		NextMarker:  result.NextMarker,
		Prefixes:    prefixes,
		Objects:     objects,
	}
}

// FromOtterioClientListBucketResultToV2Info converts otterio ListBucketResult to ListObjectsV2Info
func FromOtterioClientListBucketResultToV2Info(bucket string, result otterio.ListBucketResult) ListObjectsV2Info {
	objects := make([]ObjectInfo, len(result.Contents))

	for i, oi := range result.Contents {
		objects[i] = FromOtterioClientObjectInfo(bucket, oi)
	}

	prefixes := make([]string, len(result.CommonPrefixes))
	for i, p := range result.CommonPrefixes {
		prefixes[i] = p.Prefix
	}

	return ListObjectsV2Info{
		IsTruncated:           result.IsTruncated,
		Prefixes:              prefixes,
		Objects:               objects,
		ContinuationToken:     result.Marker,
		NextContinuationToken: result.NextMarker,
	}
}

// ToOtterioClientObjectInfoMetadata convertes metadata to map[string][]string
func ToOtterioClientObjectInfoMetadata(metadata map[string]string) map[string][]string {
	mm := make(map[string][]string, len(metadata))
	for k, v := range metadata {
		mm[http.CanonicalHeaderKey(k)] = []string{v}
	}
	return mm
}

// ToOtterioClientMetadata converts metadata to map[string]string
func ToOtterioClientMetadata(metadata map[string]string) map[string]string {
	mm := make(map[string]string, len(metadata))
	for k, v := range metadata {
		mm[http.CanonicalHeaderKey(k)] = v
	}
	return mm
}

// ToOtterioClientCompletePart converts CompletePart to otterio CompletePart
func ToOtterioClientCompletePart(part CompletePart) otterio.CompletePart {
	return otterio.CompletePart{
		ETag:       part.ETag,
		PartNumber: part.PartNumber,
	}
}

// ToOtterioClientCompleteParts converts []CompletePart to otterio []CompletePart
func ToOtterioClientCompleteParts(parts []CompletePart) []otterio.CompletePart {
	mparts := make([]otterio.CompletePart, len(parts))
	for i, part := range parts {
		mparts[i] = ToOtterioClientCompletePart(part)
	}
	return mparts
}

// IsBackendOnline - verifies if the backend is reachable
// by performing a GET request on the URL. returns 'true'
// if backend is reachable.
func IsBackendOnline(ctx context.Context, host string) bool {
	var d net.Dialer

	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return false
	}

	conn.Close()
	return true
}

// ErrorRespToObjectError converts OtterIO errors to otterio object layer errors.
func ErrorRespToObjectError(err error, params ...string) error {
	if err == nil {
		return nil
	}

	bucket := ""
	object := ""
	if len(params) >= 1 {
		bucket = params[0]
	}
	if len(params) == 2 {
		object = params[1]
	}

	if xnet.IsNetworkOrHostDown(err, false) {
		return BackendDown{}
	}

	otterioErr, ok := err.(otterio.ErrorResponse)
	if !ok {
		// We don't interpret non OtterIO errors. As otterio errors will
		// have StatusCode to help to convert to object errors.
		return err
	}

	switch otterioErr.Code {
	case "BucketAlreadyOwnedByYou":
		err = BucketAlreadyOwnedByYou{}
	case "BucketNotEmpty":
		err = BucketNotEmpty{}
	case "NoSuchBucketPolicy":
		err = BucketPolicyNotFound{}
	case "NoSuchLifecycleConfiguration":
		err = BucketLifecycleNotFound{}
	case "InvalidBucketName":
		err = BucketNameInvalid{Bucket: bucket}
	case "InvalidPart":
		err = InvalidPart{}
	case "NoSuchBucket":
		err = BucketNotFound{Bucket: bucket}
	case "NoSuchKey":
		if object != "" {
			err = ObjectNotFound{Bucket: bucket, Object: object}
		} else {
			err = BucketNotFound{Bucket: bucket}
		}
	case "XOtterioInvalidObjectName":
		err = ObjectNameInvalid{}
	case "AccessDenied":
		err = PrefixAccessDenied{
			Bucket: bucket,
			Object: object,
		}
	case "XAmzContentSHA256Mismatch":
		err = hash.SHA256Mismatch{}
	case "NoSuchUpload":
		err = InvalidUploadID{}
	case "EntityTooSmall":
		err = PartTooSmall{}
	}

	return err
}

// ComputeCompleteMultipartMD5 calculates MD5 ETag for complete multipart responses
func ComputeCompleteMultipartMD5(parts []CompletePart) string {
	return getCompleteMultipartMD5(parts)
}

// parse gateway sse env variable
func parseGatewaySSE(s string) (gatewaySSE, error) {
	l := strings.Split(s, ";")
	var gwSlice gatewaySSE
	for _, val := range l {
		v := strings.ToUpper(val)
		switch v {
		case "":
			continue
		case gatewaySSES3:
			fallthrough
		case gatewaySSEC:
			gwSlice = append(gwSlice, v)
			continue
		default:
			return nil, config.ErrInvalidGWSSEValue(nil).Msg("gateway SSE cannot be (%s) ", v)
		}
	}
	return gwSlice, nil
}

// handle gateway env vars
func gatewayHandleEnvVars() {
	// Handle common env vars.
	handleCommonEnvVars()

	if !globalActiveCred.IsValid() {
		logger.Fatal(config.ErrInvalidCredentials(nil),
			"Unable to validate credentials inherited from the shell environment")
	}

	gwsseVal := env.Get("OTTERIO_GATEWAY_SSE", "")
	if gwsseVal != "" {
		var err error
		GlobalGatewaySSE, err = parseGatewaySSE(gwsseVal)
		if err != nil {
			logger.Fatal(err, "Unable to parse OTTERIO_GATEWAY_SSE value (`%s`)", gwsseVal)
		}
	}
}

// shouldMeterRequest checks whether incoming request should be added to prometheus gateway metrics
func shouldMeterRequest(req *http.Request) bool {
	return !(guessIsBrowserReq(req) || guessIsHealthCheckReq(req) || guessIsMetricsReq(req))
}

// MetricsTransport is a custom wrapper around Transport to track metrics
type MetricsTransport struct {
	Transport *http.Transport
	Metrics   *BackendMetrics
}

// RoundTrip implements the RoundTrip method for MetricsTransport
func (m MetricsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	metered := shouldMeterRequest(r)
	if metered && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		m.Metrics.IncRequests(r.Method)
		if r.ContentLength > 0 {
			m.Metrics.IncBytesSent(uint64(r.ContentLength))
		}
	}
	// Make the request to the server.
	resp, err := m.Transport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if metered && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		m.Metrics.IncRequests(r.Method)
		if resp.ContentLength > 0 {
			m.Metrics.IncBytesReceived(uint64(resp.ContentLength))
		}
	}
	return resp, nil
}
