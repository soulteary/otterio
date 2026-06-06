/*
 * MinIO Cloud Storage, (C) 2017-2020 MinIO, Inc.
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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/soulteary/otterio/cmd/crypto"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
)

// SECURITY: trust boundary for fork-private replication headers.
//
// The X-Otterio-Source-* headers below are how OtterIO's replication peers
// communicate persistent metadata (mtime, etag, delete-marker state, version
// purge intent) about an object that is being replicated from another site.
// They are *not* part of the public S3 contract — a regular S3 client has no
// reason to set them. If they are accepted unconditionally, any caller with
// s3:PutObject / s3:DeleteObject can ride a normal SigV4 PUT/DELETE while
// smuggling these headers and silently:
//
//   - rewrite an object's mtime (bypass mtime-based retention / pollute audit
//     timelines),
//   - poison an object's etag (break content-integrity reconciliation),
//   - flip a PUT into a delete-marker creation,
//   - force VersionPurgeStatus = Complete on a DELETE (potentially evading
//     versioning / object-lock protection).
//
// To close that gap, every request that carries any of these headers must
// also carry s3:ReplicateObject authority. We reuse isPutActionAllowed as
// the policy evaluator because it already correctly handles the five caller
// shapes that hit this codepath (anonymous via bucket policy, SigV4, SigV2,
// streaming-signed, JWT). On rejection we surface a sentinel error which
// toAPIErrorCode maps to ErrAccessDeniedReplicationHeader (HTTP 403, S3 code
// "AccessDenied") so handlers do not need any special-casing.
//
// X-Otterio-Source-Proxy-Request is intentionally NOT in this set: it only
// influences whether bucket-replication.go takes the active-active GET proxy
// branch and never persists to object metadata; misusing it cannot corrupt
// data, only make a request fail to proxy.
var otterioSourceHeaderKeys = []string{
	xhttp.OtterIOSourceMTime,
	xhttp.OtterIOSourceETag,
	xhttp.OtterIOSourceDeleteMarker,
	xhttp.OtterIOSourceDeleteMarkerDelete,
	xhttp.OtterIOSourceReplicationRequest,
}

// errAccessDeniedReplicationHeader is the sentinel returned by getOpts /
// putOpts / delOpts when the IAM gate rejects a caller that tried to set one
// of the otterioSourceHeaderKeys. toAPIErrorCode translates it to
// ErrAccessDeniedReplicationHeader (403 / "AccessDenied") at the handler
// boundary, which lets every existing call site keep its `error` return
// shape unchanged.
var errAccessDeniedReplicationHeader = errors.New("caller is not authorized to send X-Otterio-Source-* replication headers")

// hasAnyOtterIOSourceHeader reports whether the request carries any of the
// IAM-gated source headers. It is the cheap pre-check that lets ordinary S3
// traffic skip the IAM evaluation entirely.
//
// Subtleties worth pinning here:
//
//  1. Presence vs value. cmd/handler-utils.go:259 and cmd/notification.go:1429
//     treat OtterIOSourceReplicationRequest as a *boolean flag whose value is
//     irrelevant* - the key alone, even with an empty string value, suppresses
//     replica-creation notifications. So the gate must trigger on *presence*,
//     not on a non-empty value, otherwise an attacker could set
//     `X-Otterio-Source-Replication-Request:` (empty) and silently mute the
//     audit channel.
//
//  2. Case folding. The OtterIOSource* constants in cmd/http/headers.go are
//     spelled lower-case while http.Header maps populated by net/http's parser
//     are in canonical MIME-header form. On a real request the lookup just
//     works because http.Header.Values canonicalises before reading. To stay
//     robust against fixtures or middleware that bypassed canonicalisation
//     (e.g. directly assigning to the underlying map), we additionally walk
//     the map with a case-insensitive comparison. The set of OtterIO source
//     keys is small and fixed so this stays O(headers).
func hasAnyOtterIOSourceHeader(h http.Header) bool {
	if len(h) == 0 {
		return false
	}
	for _, key := range otterioSourceHeaderKeys {
		if _, ok := h[key]; ok {
			return true
		}
		if _, ok := h[http.CanonicalHeaderKey(key)]; ok {
			return true
		}
	}
	for k := range h {
		canonical := strings.ToLower(k)
		for _, key := range otterioSourceHeaderKeys {
			if canonical == strings.ToLower(key) {
				return true
			}
		}
	}
	return false
}

// enforceSourceHeaderIAM gates X-Otterio-Source-* replication headers on
// s3:ReplicateObject. It returns nil for the common case (no source header
// present) and for callers that hold the action; otherwise it returns
// errAccessDeniedReplicationHeader. The IAM evaluation is delegated to
// isPutActionAllowed because that helper already handles anonymous (bucket
// policy), SigV4, SigV2, streaming-signed and JWT callers in one place.
func enforceSourceHeaderIAM(ctx context.Context, r *http.Request, bucket, object string) error {
	if !hasAnyOtterIOSourceHeader(r.Header) {
		return nil
	}
	if s3Err := isPutActionAllowed(ctx, getRequestAuthType(r), bucket, object, r, iampolicy.ReplicateObjectAction); s3Err != ErrNone {
		return errAccessDeniedReplicationHeader
	}
	return nil
}

// set encryption options for pass through to backend in the case of gateway and UserDefined metadata
func getDefaultOpts(header http.Header, copySource bool, metadata map[string]string) (opts ObjectOptions, err error) {
	var clientKey [32]byte
	var sse encrypt.ServerSide

	opts = ObjectOptions{UserDefined: metadata}
	if copySource {
		if crypto.SSECopy.IsRequested(header) {
			clientKey, err = crypto.SSECopy.ParseHTTP(header)
			if err != nil {
				return
			}
			if sse, err = encrypt.NewSSEC(clientKey[:]); err != nil {
				return
			}
			opts.ServerSideEncryption = encrypt.SSECopy(sse)
			return
		}
		return
	}

	if crypto.SSEC.IsRequested(header) {
		clientKey, err = crypto.SSEC.ParseHTTP(header)
		if err != nil {
			return
		}
		if sse, err = encrypt.NewSSEC(clientKey[:]); err != nil {
			return
		}
		opts.ServerSideEncryption = sse
		return
	}
	if crypto.S3.IsRequested(header) || (metadata != nil && crypto.S3.IsEncrypted(metadata)) {
		opts.ServerSideEncryption = encrypt.NewSSE()
	}
	if v, ok := header[xhttp.OtterIOSourceProxyRequest]; ok {
		opts.ProxyHeaderSet = true
		opts.ProxyRequest = strings.Join(v, "") == "true"
	}
	return
}

// get ObjectOptions for GET calls from encryption headers
func getOpts(ctx context.Context, r *http.Request, bucket, object string) (ObjectOptions, error) {
	var (
		encryption encrypt.ServerSide
		opts       ObjectOptions
	)

	if err := enforceSourceHeaderIAM(ctx, r, bucket, object); err != nil {
		return opts, err
	}

	var partNumber int
	var err error
	if pn := r.URL.Query().Get(xhttp.PartNumber); pn != "" {
		partNumber, err = strconv.Atoi(pn)
		if err != nil {
			return opts, err
		}
		if partNumber <= 0 {
			return opts, errInvalidArgument
		}
	}

	vid := strings.TrimSpace(r.URL.Query().Get(xhttp.VersionID))
	if vid != "" && vid != nullVersionID {
		_, err := uuid.Parse(vid)
		if err != nil {
			logger.LogIf(ctx, err)
			return opts, InvalidVersionID{
				Bucket:    bucket,
				Object:    object,
				VersionID: vid,
			}
		}
	}

	if GlobalGatewaySSE.SSEC() && crypto.SSEC.IsRequested(r.Header) {
		key, err := crypto.SSEC.ParseHTTP(r.Header)
		if err != nil {
			return opts, err
		}
		derivedKey := deriveClientKey(key, bucket, object)
		encryption, err = encrypt.NewSSEC(derivedKey[:])
		logger.CriticalIf(ctx, err)
		return ObjectOptions{
			ServerSideEncryption: encryption,
			VersionID:            vid,
			PartNumber:           partNumber,
		}, nil
	}

	// default case of passing encryption headers to backend
	opts, err = getDefaultOpts(r.Header, false, nil)
	if err != nil {
		return opts, err
	}
	opts.PartNumber = partNumber
	opts.VersionID = vid
	delMarker := strings.TrimSpace(r.Header.Get(xhttp.OtterIOSourceDeleteMarker))
	if delMarker != "" {
		switch delMarker {
		case "true":
			opts.DeleteMarker = true
		case "false":
		default:
			err = fmt.Errorf("Unable to parse %s, failed with %w", xhttp.OtterIOSourceDeleteMarker, fmt.Errorf("DeleteMarker should be true or false"))
			logger.LogIf(ctx, err)
			return opts, InvalidArgument{
				Bucket: bucket,
				Object: object,
				Err:    err,
			}
		}

	}
	return opts, nil
}

func delOpts(ctx context.Context, r *http.Request, bucket, object string) (opts ObjectOptions, err error) {
	// Belt-and-suspenders: getOpts (called below) also runs this check, but
	// keeping a direct gate at the top of delOpts makes the trust boundary
	// obvious to readers and decouples delOpts' guarantees from getOpts'
	// internals (so a future refactor that reshuffles getOpts cannot
	// silently widen the DELETE attack surface).
	if err = enforceSourceHeaderIAM(ctx, r, bucket, object); err != nil {
		return opts, err
	}
	versioned := globalBucketVersioningSys.Enabled(bucket)
	opts, err = getOpts(ctx, r, bucket, object)
	if err != nil {
		return opts, err
	}
	opts.Versioned = versioned
	opts.VersionSuspended = globalBucketVersioningSys.Suspended(bucket)
	delMarker := strings.TrimSpace(r.Header.Get(xhttp.OtterIOSourceDeleteMarker))
	if delMarker != "" {
		switch delMarker {
		case "true":
			opts.DeleteMarker = true
		case "false":
		default:
			err = fmt.Errorf("Unable to parse %s, failed with %w", xhttp.OtterIOSourceDeleteMarker, fmt.Errorf("DeleteMarker should be true or false"))
			logger.LogIf(ctx, err)
			return opts, InvalidArgument{
				Bucket: bucket,
				Object: object,
				Err:    err,
			}
		}
	}

	purgeVersion := strings.TrimSpace(r.Header.Get(xhttp.OtterIOSourceDeleteMarkerDelete))
	if purgeVersion != "" {
		switch purgeVersion {
		case "true":
			opts.VersionPurgeStatus = Complete
		case "false":
		default:
			err = fmt.Errorf("Unable to parse %s, failed with %w", xhttp.OtterIOSourceDeleteMarkerDelete, fmt.Errorf("DeleteMarkerPurge should be true or false"))
			logger.LogIf(ctx, err)
			return opts, InvalidArgument{
				Bucket: bucket,
				Object: object,
				Err:    err,
			}
		}
	}

	mtime := strings.TrimSpace(r.Header.Get(xhttp.OtterIOSourceMTime))
	if mtime != "" {
		opts.MTime, err = time.Parse(time.RFC3339Nano, mtime)
		if err != nil {
			return opts, InvalidArgument{
				Bucket: bucket,
				Object: object,
				Err:    fmt.Errorf("Unable to parse %s, failed with %w", xhttp.OtterIOSourceMTime, err),
			}
		}
	} else {
		opts.MTime = UTCNow()
	}
	return opts, nil
}

// get ObjectOptions for PUT calls from encryption headers and metadata
func putOpts(ctx context.Context, r *http.Request, bucket, object string, metadata map[string]string) (opts ObjectOptions, err error) {
	if err = enforceSourceHeaderIAM(ctx, r, bucket, object); err != nil {
		return opts, err
	}
	versioned := globalBucketVersioningSys.Enabled(bucket)
	vid := strings.TrimSpace(r.URL.Query().Get(xhttp.VersionID))
	if vid != "" && vid != nullVersionID {
		_, err := uuid.Parse(vid)
		if err != nil {
			logger.LogIf(ctx, err)
			return opts, InvalidVersionID{
				Bucket:    bucket,
				Object:    object,
				VersionID: vid,
			}
		}
		if !versioned {
			return opts, InvalidArgument{
				Bucket: bucket,
				Object: object,
				Err:    fmt.Errorf("VersionID specified %s, but versioning not enabled on  %s", opts.VersionID, bucket),
			}
		}
	}
	mtimeStr := strings.TrimSpace(r.Header.Get(xhttp.OtterIOSourceMTime))
	mtime := UTCNow()
	if mtimeStr != "" {
		mtime, err = time.Parse(time.RFC3339, mtimeStr)
		if err != nil {
			return opts, InvalidArgument{
				Bucket: bucket,
				Object: object,
				Err:    fmt.Errorf("Unable to parse %s, failed with %w", xhttp.OtterIOSourceMTime, err),
			}
		}
	}
	etag := strings.TrimSpace(r.Header.Get(xhttp.OtterIOSourceETag))
	if etag != "" {
		if metadata == nil {
			metadata = make(map[string]string, 1)
		}
		metadata["etag"] = etag
	}

	// In the case of multipart custom format, the metadata needs to be checked in addition to header to see if it
	// is SSE-S3 encrypted, primarily because S3 protocol does not require SSE-S3 headers in PutObjectPart calls
	if GlobalGatewaySSE.SSES3() && (crypto.S3.IsRequested(r.Header) || crypto.S3.IsEncrypted(metadata)) {
		return ObjectOptions{
			ServerSideEncryption: encrypt.NewSSE(),
			UserDefined:          metadata,
			VersionID:            vid,
			Versioned:            versioned,
			MTime:                mtime,
		}, nil
	}
	if GlobalGatewaySSE.SSEC() && crypto.SSEC.IsRequested(r.Header) {
		opts, err = getOpts(ctx, r, bucket, object)
		opts.VersionID = vid
		opts.Versioned = versioned
		opts.UserDefined = metadata
		return
	}
	if crypto.S3KMS.IsRequested(r.Header) {
		keyID, context, err := crypto.S3KMS.ParseHTTP(r.Header)
		if err != nil {
			return ObjectOptions{}, err
		}
		sseKms, err := encrypt.NewSSEKMS(keyID, context)
		if err != nil {
			return ObjectOptions{}, err
		}
		return ObjectOptions{
			ServerSideEncryption: sseKms,
			UserDefined:          metadata,
			VersionID:            vid,
			Versioned:            versioned,
			MTime:                mtime,
		}, nil
	}
	// default case of passing encryption headers and UserDefined metadata to backend
	opts, err = getDefaultOpts(r.Header, false, metadata)
	if err != nil {
		return opts, err
	}
	opts.VersionID = vid
	opts.Versioned = versioned
	opts.MTime = mtime
	return opts, nil
}

// get ObjectOptions for Copy calls with encryption headers provided on the target side and source side metadata
func copyDstOpts(ctx context.Context, r *http.Request, bucket, object string, metadata map[string]string) (opts ObjectOptions, err error) {
	return putOpts(ctx, r, bucket, object, metadata)
}

// get ObjectOptions for Copy calls with encryption headers provided on the source side
func copySrcOpts(_ context.Context, r *http.Request, bucket, object string) (ObjectOptions, error) {
	var (
		ssec encrypt.ServerSide
		opts ObjectOptions
	)

	if GlobalGatewaySSE.SSEC() && crypto.SSECopy.IsRequested(r.Header) {
		key, err := crypto.SSECopy.ParseHTTP(r.Header)
		if err != nil {
			return opts, err
		}
		derivedKey := deriveClientKey(key, bucket, object)
		ssec, err = encrypt.NewSSEC(derivedKey[:])
		if err != nil {
			return opts, err
		}
		return ObjectOptions{ServerSideEncryption: encrypt.SSECopy(ssec)}, nil
	}

	// default case of passing encryption headers to backend
	opts, err := getDefaultOpts(r.Header, false, nil)
	if err != nil {
		return opts, err
	}
	return opts, nil
}
