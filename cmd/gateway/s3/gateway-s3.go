/*
 * MinIO Cloud Storage, (C) 2017-2020 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
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

package s3

import (
	"context"
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/cli"
	otteriogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/minio/minio-go/v7/pkg/s3utils"
	"github.com/minio/minio-go/v7/pkg/tags"
	otterio "github.com/soulteary/otterio/cmd"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/bucket/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

func init() {
	const s3GatewayTemplate = `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} {{if .VisibleFlags}}[FLAGS]{{end}} [ENDPOINT]
{{if .VisibleFlags}}
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
ENDPOINT:
  s3 server endpoint. Default ENDPOINT is https://s3.amazonaws.com

EXAMPLES:
  1. Start otterio gateway server for AWS S3 backend
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_USER{{.AssignmentOperator}}accesskey
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_PASSWORD{{.AssignmentOperator}}secretkey
     {{.Prompt}} {{.HelpName}}

  2. Start otterio gateway server for AWS S3 backend with edge caching enabled
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_USER{{.AssignmentOperator}}accesskey
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_ROOT_PASSWORD{{.AssignmentOperator}}secretkey
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_CACHE_DRIVES{{.AssignmentOperator}}"/mnt/drive1,/mnt/drive2,/mnt/drive3,/mnt/drive4"
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_CACHE_EXCLUDE{{.AssignmentOperator}}"bucket1/*,*.png"
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_CACHE_QUOTA{{.AssignmentOperator}}90
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_CACHE_AFTER{{.AssignmentOperator}}3
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_CACHE_WATERMARK_LOW{{.AssignmentOperator}}75
     {{.Prompt}} {{.EnvVarSetCommand}} OTTERIO_CACHE_WATERMARK_HIGH{{.AssignmentOperator}}85
     {{.Prompt}} {{.HelpName}}
`

	otterio.RegisterGatewayCommand(cli.Command{
		Name:               otterio.S3BackendGateway,
		Usage:              "Amazon Simple Storage Service (S3)",
		Action:             s3GatewayMain,
		CustomHelpTemplate: s3GatewayTemplate,
		HideHelpCommand:    true,
	})
}

// Handler for 'otterio gateway s3' command line.
func s3GatewayMain(ctx *cli.Context) {
	args := ctx.Args()
	if !ctx.Args().Present() {
		args = cli.Args{"https://s3.amazonaws.com"}
	}

	serverAddr := ctx.GlobalString("address")
	if serverAddr == "" || serverAddr == ":"+otterio.GlobalOtterioDefaultPort {
		serverAddr = ctx.String("address")
	}
	// Validate gateway arguments.
	logger.FatalIf(otterio.ValidateGatewayArguments(serverAddr, args.First()), "Invalid argument")

	// Start the gateway..
	otterio.StartGateway(ctx, &S3{args.First()})
}

// S3 implements Gateway.
type S3 struct {
	host string
}

// Name implements Gateway interface.
func (g *S3) Name() string {
	return otterio.S3BackendGateway
}

const letterBytes = "abcdefghijklmnopqrstuvwxyz01234569"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

// randString generates random names and prepends them with a known prefix.
func randString(n int, src rand.Source, prefix string) string {
	b := make([]byte, n)
	// A rand.Int63() generates 63 random bits, enough for letterIdxMax letters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}
	return prefix + string(b[0:30-len(prefix)])
}

// Chains all credential types, in the following order:
//   - AWS env vars (i.e. AWS_ACCESS_KEY_ID)
//   - AWS creds file (i.e. AWS_SHARED_CREDENTIALS_FILE or ~/.aws/credentials)
//   - Static credentials provided by user (i.e. OTTERIO_ROOT_USER/OTTERIO_ACCESS_KEY)
var defaultProviders = []credentials.Provider{
	&credentials.EnvAWS{},
	&credentials.FileAWSCredentials{},
	&credentials.EnvMinio{},
}

// Chains all credential types, in the following order:
//   - AWS env vars (i.e. AWS_ACCESS_KEY_ID)
//   - AWS creds file (i.e. AWS_SHARED_CREDENTIALS_FILE or ~/.aws/credentials)
//   - IAM profile based credentials. (performs an HTTP
//     call to a pre-defined endpoint, only valid inside
//     configured ec2 instances)
//   - Static credentials provided by user (i.e. OTTERIO_ROOT_USER/OTTERIO_ACCESS_KEY)
var defaultAWSCredProviders = []credentials.Provider{
	&credentials.EnvAWS{},
	&credentials.FileAWSCredentials{},
	&credentials.IAM{
		Client: &http.Client{
			Transport: otterio.NewGatewayHTTPTransport(),
		},
	},
	&credentials.EnvMinio{},
}

// newS3 - Initializes a new client by auto probing S3 server signature.
func newS3(urlStr string, tripper http.RoundTripper) (*otteriogo.Core, error) {
	if urlStr == "" {
		urlStr = "https://s3.amazonaws.com"
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	// Override default params if the host is provided
	endpoint, secure, err := otterio.ParseGatewayEndpoint(urlStr)
	if err != nil {
		return nil, err
	}

	var creds *credentials.Credentials
	if s3utils.IsAmazonEndpoint(*u) {
		// If we see an Amazon S3 endpoint, then we use more ways to fetch backend credentials.
		// Specifically IAM style rotating credentials are only supported with AWS S3 endpoint.
		creds = credentials.NewChainCredentials(defaultAWSCredProviders)

	} else {
		creds = credentials.NewChainCredentials(defaultProviders)
	}

	options := &otteriogo.Options{
		Creds:        creds,
		Secure:       secure,
		Region:       s3utils.GetRegionFromURL(*u),
		BucketLookup: otteriogo.BucketLookupAuto,
		Transport:    tripper,
	}

	clnt, err := otteriogo.New(endpoint, options)
	if err != nil {
		return nil, err
	}

	return &otteriogo.Core{Client: clnt}, nil
}

// NewGatewayLayer returns s3 ObjectLayer.
func (g *S3) NewGatewayLayer(creds auth.Credentials) (otterio.ObjectLayer, error) {
	metrics := otterio.NewMetrics()

	t := &otterio.MetricsTransport{
		Transport: otterio.NewGatewayHTTPTransport(),
		Metrics:   metrics,
	}

	// creds are ignored here, since S3 gateway implements chaining
	// all credentials.
	clnt, err := newS3(g.host, t)
	if err != nil {
		return nil, err
	}

	probeBucketName := randString(60, rand.NewSource(time.Now().UnixNano()), "probe-bucket-sign-")

	// Check if the provided keys are valid.
	if _, err = clnt.BucketExists(context.Background(), probeBucketName); err != nil {
		if otteriogo.ToErrorResponse(err).Code != "AccessDenied" {
			return nil, err
		}
	}

	s := s3Objects{
		Client:  clnt,
		Metrics: metrics,
		HTTPClient: &http.Client{
			Transport: t,
		},
	}

	// Enables single encryption of KMS is configured.
	if otterio.GlobalKMS != nil {
		encS := s3EncObjects{s}

		// Start stale enc multipart uploads cleanup routine.
		go encS.cleanupStaleEncMultipartUploads(otterio.GlobalContext,
			otterio.GlobalStaleUploadsCleanupInterval, otterio.GlobalStaleUploadsExpiry)

		return &encS, nil
	}
	return &s, nil
}

// Production - s3 gateway is production ready.
func (g *S3) Production() bool {
	return true
}

// s3Objects implements gateway for OtterIO and S3 compatible object storage servers.
type s3Objects struct {
	otterio.GatewayUnsupported
	Client     *otteriogo.Core
	HTTPClient *http.Client
	Metrics    *otterio.BackendMetrics
}

// GetMetrics returns this gateway's metrics
func (l *s3Objects) GetMetrics(ctx context.Context) (*otterio.BackendMetrics, error) {
	return l.Metrics, nil
}

// Shutdown saves any gateway metadata to disk
// if necessary and reload upon next restart.
func (l *s3Objects) Shutdown(ctx context.Context) error {
	return nil
}

// StorageInfo is not relevant to S3 backend.
func (l *s3Objects) StorageInfo(ctx context.Context) (si otterio.StorageInfo, _ []error) {
	si.Backend.Type = madmin.Gateway
	host := l.Client.EndpointURL().Host
	if l.Client.EndpointURL().Port() == "" {
		host = l.Client.EndpointURL().Host + ":" + l.Client.EndpointURL().Scheme
	}
	si.Backend.GatewayOnline = otterio.IsBackendOnline(ctx, host)
	return si, nil
}

// MakeBucket creates a new container on S3 backend.
func (l *s3Objects) MakeBucketWithLocation(ctx context.Context, bucket string, opts otterio.BucketOptions) error {
	if opts.LockEnabled || opts.VersioningEnabled {
		return otterio.NotImplemented{}
	}

	// Verify if bucket name is valid.
	// We are using a separate helper function here to validate bucket
	// names instead of IsValidBucketName() because there is a possibility
	// that certains users might have buckets which are non-DNS compliant
	// in us-east-1 and we might severely restrict them by not allowing
	// access to these buckets.
	// Ref - http://docs.aws.amazon.com/AmazonS3/latest/dev/BucketRestrictions.html
	if s3utils.CheckValidBucketName(bucket) != nil {
		return otterio.BucketNameInvalid{Bucket: bucket}
	}
	err := l.Client.MakeBucket(ctx, bucket, otteriogo.MakeBucketOptions{Region: opts.Location})
	if err != nil {
		return otterio.ErrorRespToObjectError(err, bucket)
	}
	return err
}

// GetBucketInfo gets bucket metadata..
func (l *s3Objects) GetBucketInfo(ctx context.Context, bucket string) (bi otterio.BucketInfo, e error) {
	buckets, err := l.Client.ListBuckets(ctx)
	if err != nil {
		// Listbuckets may be disallowed, proceed to check if
		// bucket indeed exists, if yes return success.
		var ok bool
		if ok, err = l.Client.BucketExists(ctx, bucket); err != nil {
			return bi, otterio.ErrorRespToObjectError(err, bucket)
		}
		if !ok {
			return bi, otterio.BucketNotFound{Bucket: bucket}
		}
		return otterio.BucketInfo{
			Name:    bi.Name,
			Created: time.Now().UTC(),
		}, nil
	}

	for _, bi := range buckets {
		if bi.Name != bucket {
			continue
		}

		return otterio.BucketInfo{
			Name:    bi.Name,
			Created: bi.CreationDate,
		}, nil
	}

	return bi, otterio.BucketNotFound{Bucket: bucket}
}

// ListBuckets lists all S3 buckets
func (l *s3Objects) ListBuckets(ctx context.Context) ([]otterio.BucketInfo, error) {
	buckets, err := l.Client.ListBuckets(ctx)
	if err != nil {
		return nil, otterio.ErrorRespToObjectError(err)
	}

	b := make([]otterio.BucketInfo, len(buckets))
	for i, bi := range buckets {
		b[i] = otterio.BucketInfo{
			Name:    bi.Name,
			Created: bi.CreationDate,
		}
	}

	return b, err
}

// DeleteBucket deletes a bucket on S3
func (l *s3Objects) DeleteBucket(ctx context.Context, bucket string, forceDelete bool) error {
	err := l.Client.RemoveBucket(ctx, bucket)
	if err != nil {
		return otterio.ErrorRespToObjectError(err, bucket)
	}
	return nil
}

// ListObjects lists all blobs in S3 bucket filtered by prefix
func (l *s3Objects) ListObjects(ctx context.Context, bucket string, prefix string, marker string, delimiter string, maxKeys int) (loi otterio.ListObjectsInfo, e error) {
	result, err := l.Client.ListObjects(bucket, prefix, marker, delimiter, maxKeys)
	if err != nil {
		return loi, otterio.ErrorRespToObjectError(err, bucket)
	}

	return otterio.FromOtterioClientListBucketResult(bucket, result), nil
}

// ListObjectsV2 lists all blobs in S3 bucket filtered by prefix
func (l *s3Objects) ListObjectsV2(ctx context.Context, bucket, prefix, continuationToken, delimiter string, maxKeys int, fetchOwner bool, startAfter string) (loi otterio.ListObjectsV2Info, e error) {
	result, err := l.Client.ListObjectsV2(bucket, prefix, startAfter, continuationToken, delimiter, maxKeys)
	if err != nil {
		return loi, otterio.ErrorRespToObjectError(err, bucket)
	}

	return otterio.FromOtterioClientListBucketV2Result(bucket, result), nil
}

// GetObjectNInfo - returns object info and locked object ReadCloser
func (l *s3Objects) GetObjectNInfo(ctx context.Context, bucket, object string, rs *otterio.HTTPRangeSpec, h http.Header, lockType otterio.LockType, opts otterio.ObjectOptions) (gr *otterio.GetObjectReader, err error) {
	var objInfo otterio.ObjectInfo
	objInfo, err = l.GetObjectInfo(ctx, bucket, object, opts)
	if err != nil {
		return nil, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	fn, off, length, err := otterio.NewGetObjectReader(rs, objInfo, opts)
	if err != nil {
		return nil, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	pr, pw := io.Pipe()
	go func() {
		err := l.getObject(ctx, bucket, object, off, length, pw, objInfo.ETag, opts)
		pw.CloseWithError(err)
	}()

	// Setup cleanup function to cause the above go-routine to
	// exit in case of partial read
	pipeCloser := func() { pr.Close() }
	return fn(pr, h, opts.CheckPrecondFn, pipeCloser)
}

// GetObject reads an object from S3. Supports additional
// parameters like offset and length which are synonymous with
// HTTP Range requests.
//
// startOffset indicates the starting read location of the object.
// length indicates the total length of the object.
func (l *s3Objects) getObject(ctx context.Context, bucket string, key string, startOffset int64, length int64, writer io.Writer, etag string, o otterio.ObjectOptions) error {
	if length < 0 && length != -1 {
		return otterio.ErrorRespToObjectError(otterio.InvalidRange{}, bucket, key)
	}

	opts := otteriogo.GetObjectOptions{}
	opts.ServerSideEncryption = o.ServerSideEncryption

	if startOffset >= 0 && length >= 0 {
		if err := opts.SetRange(startOffset, startOffset+length-1); err != nil {
			return otterio.ErrorRespToObjectError(err, bucket, key)
		}
	}

	if etag != "" {
		opts.SetMatchETag(etag)
	}

	object, _, _, err := l.Client.GetObject(ctx, bucket, key, opts)
	if err != nil {
		return otterio.ErrorRespToObjectError(err, bucket, key)
	}
	defer object.Close()
	if _, err := io.Copy(writer, object); err != nil {
		return otterio.ErrorRespToObjectError(err, bucket, key)
	}
	return nil
}

// GetObjectInfo reads object info and replies back ObjectInfo
func (l *s3Objects) GetObjectInfo(ctx context.Context, bucket string, object string, opts otterio.ObjectOptions) (objInfo otterio.ObjectInfo, err error) {
	oi, err := l.Client.StatObject(ctx, bucket, object, otteriogo.StatObjectOptions{
		ServerSideEncryption: opts.ServerSideEncryption,
	})
	if err != nil {
		return otterio.ObjectInfo{}, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	return otterio.FromOtterioClientObjectInfo(bucket, oi), nil
}

// PutObject creates a new object with the incoming data,
func (l *s3Objects) PutObject(ctx context.Context, bucket string, object string, r *otterio.PutObjReader, opts otterio.ObjectOptions) (objInfo otterio.ObjectInfo, err error) {
	data := r.Reader
	var tagMap map[string]string
	if tagstr, ok := opts.UserDefined[xhttp.AmzObjectTagging]; ok && tagstr != "" {
		tagObj, err := tags.ParseObjectTags(tagstr)
		if err != nil {
			return objInfo, otterio.ErrorRespToObjectError(err, bucket, object)
		}
		tagMap = tagObj.ToMap()
		delete(opts.UserDefined, xhttp.AmzObjectTagging)
	}
	putOpts := otteriogo.PutObjectOptions{
		UserMetadata:         opts.UserDefined,
		ServerSideEncryption: opts.ServerSideEncryption,
		UserTags:             tagMap,
	}
	ui, err := l.Client.PutObject(ctx, bucket, object, data, data.Size(), data.MD5Base64String(), data.SHA256HexString(), putOpts)
	if err != nil {
		return objInfo, otterio.ErrorRespToObjectError(err, bucket, object)
	}
	// On success, populate the key & metadata so they are present in the notification
	oi := otteriogo.ObjectInfo{
		ETag:     ui.ETag,
		Size:     ui.Size,
		Key:      object,
		Metadata: otterio.ToOtterioClientObjectInfoMetadata(opts.UserDefined),
	}

	return otterio.FromOtterioClientObjectInfo(bucket, oi), nil
}

// CopyObject copies an object from source bucket to a destination bucket.
func (l *s3Objects) CopyObject(ctx context.Context, srcBucket string, srcObject string, dstBucket string, dstObject string, srcInfo otterio.ObjectInfo, srcOpts, dstOpts otterio.ObjectOptions) (objInfo otterio.ObjectInfo, err error) {
	if srcOpts.CheckPrecondFn != nil && srcOpts.CheckPrecondFn(srcInfo) {
		return otterio.ObjectInfo{}, otterio.PreConditionFailed{}
	}
	// Set this header such that following CopyObject() always sets the right metadata on the destination.
	// metadata input is already a trickled down value from interpreting x-amz-metadata-directive at
	// handler layer. So what we have right now is supposed to be applied on the destination object anyways.
	// So preserve it by adding "REPLACE" directive to save all the metadata set by CopyObject API.
	srcInfo.UserDefined["x-amz-metadata-directive"] = "REPLACE"
	srcInfo.UserDefined["x-amz-copy-source-if-match"] = srcInfo.ETag
	header := make(http.Header)
	if srcOpts.ServerSideEncryption != nil {
		encrypt.SSECopy(srcOpts.ServerSideEncryption).Marshal(header)
	}

	if dstOpts.ServerSideEncryption != nil {
		dstOpts.ServerSideEncryption.Marshal(header)
	}

	for k, v := range header {
		srcInfo.UserDefined[k] = v[0]
	}

	if _, err = l.Client.CopyObject(ctx, srcBucket, srcObject, dstBucket, dstObject, srcInfo.UserDefined, otteriogo.CopySrcOptions{}, otteriogo.PutObjectOptions{}); err != nil {
		return objInfo, otterio.ErrorRespToObjectError(err, srcBucket, srcObject)
	}
	return l.GetObjectInfo(ctx, dstBucket, dstObject, dstOpts)
}

// DeleteObject deletes a blob in bucket
func (l *s3Objects) DeleteObject(ctx context.Context, bucket string, object string, opts otterio.ObjectOptions) (otterio.ObjectInfo, error) {
	err := l.Client.RemoveObject(ctx, bucket, object, otteriogo.RemoveObjectOptions{})
	if err != nil {
		return otterio.ObjectInfo{}, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	return otterio.ObjectInfo{
		Bucket: bucket,
		Name:   object,
	}, nil
}

func (l *s3Objects) DeleteObjects(ctx context.Context, bucket string, objects []otterio.ObjectToDelete, opts otterio.ObjectOptions) ([]otterio.DeletedObject, []error) {
	errs := make([]error, len(objects))
	dobjects := make([]otterio.DeletedObject, len(objects))
	for idx, object := range objects {
		_, errs[idx] = l.DeleteObject(ctx, bucket, object.ObjectName, opts)
		if errs[idx] == nil {
			dobjects[idx] = otterio.DeletedObject{
				ObjectName: object.ObjectName,
			}
		}
	}
	return dobjects, errs
}

// ListMultipartUploads lists all multipart uploads.
func (l *s3Objects) ListMultipartUploads(ctx context.Context, bucket string, prefix string, keyMarker string, uploadIDMarker string, delimiter string, maxUploads int) (lmi otterio.ListMultipartsInfo, e error) {
	result, err := l.Client.ListMultipartUploads(ctx, bucket, prefix, keyMarker, uploadIDMarker, delimiter, maxUploads)
	if err != nil {
		return lmi, err
	}

	return otterio.FromOtterioClientListMultipartsInfo(result), nil
}

// NewMultipartUpload upload object in multiple parts
func (l *s3Objects) NewMultipartUpload(ctx context.Context, bucket string, object string, o otterio.ObjectOptions) (uploadID string, err error) {
	var tagMap map[string]string
	if tagStr, ok := o.UserDefined[xhttp.AmzObjectTagging]; ok {
		tagObj, err := tags.Parse(tagStr, true)
		if err != nil {
			return uploadID, otterio.ErrorRespToObjectError(err, bucket, object)
		}
		tagMap = tagObj.ToMap()
		delete(o.UserDefined, xhttp.AmzObjectTagging)
	}
	// Create PutObject options
	opts := otteriogo.PutObjectOptions{
		UserMetadata:         o.UserDefined,
		ServerSideEncryption: o.ServerSideEncryption,
		UserTags:             tagMap,
	}
	uploadID, err = l.Client.NewMultipartUpload(ctx, bucket, object, opts)
	if err != nil {
		return uploadID, otterio.ErrorRespToObjectError(err, bucket, object)
	}
	return uploadID, nil
}

// PutObjectPart puts a part of object in bucket
func (l *s3Objects) PutObjectPart(ctx context.Context, bucket string, object string, uploadID string, partID int, r *otterio.PutObjReader, opts otterio.ObjectOptions) (pi otterio.PartInfo, e error) {
	data := r.Reader
	info, err := l.Client.PutObjectPart(ctx, bucket, object, uploadID, partID, data, data.Size(), otteriogo.PutObjectPartOptions{
		Md5Base64: data.MD5Base64String(),
		Sha256Hex: data.SHA256HexString(),
		SSE:       opts.ServerSideEncryption,
	})
	if err != nil {
		return pi, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	return otterio.FromOtterioClientObjectPart(info), nil
}

// CopyObjectPart creates a part in a multipart upload by copying
// existing object or a part of it.
func (l *s3Objects) CopyObjectPart(ctx context.Context, srcBucket, srcObject, destBucket, destObject, uploadID string,
	partID int, startOffset, length int64, srcInfo otterio.ObjectInfo, srcOpts, dstOpts otterio.ObjectOptions) (p otterio.PartInfo, err error) {
	if srcOpts.CheckPrecondFn != nil && srcOpts.CheckPrecondFn(srcInfo) {
		return otterio.PartInfo{}, otterio.PreConditionFailed{}
	}
	srcInfo.UserDefined = map[string]string{
		"x-amz-copy-source-if-match": srcInfo.ETag,
	}
	header := make(http.Header)
	if srcOpts.ServerSideEncryption != nil {
		encrypt.SSECopy(srcOpts.ServerSideEncryption).Marshal(header)
	}

	if dstOpts.ServerSideEncryption != nil {
		dstOpts.ServerSideEncryption.Marshal(header)
	}
	for k, v := range header {
		srcInfo.UserDefined[k] = v[0]
	}

	completePart, err := l.Client.CopyObjectPart(ctx, srcBucket, srcObject, destBucket, destObject,
		uploadID, partID, startOffset, length, srcInfo.UserDefined)
	if err != nil {
		return p, otterio.ErrorRespToObjectError(err, srcBucket, srcObject)
	}
	p.PartNumber = completePart.PartNumber
	p.ETag = completePart.ETag
	return p, nil
}

// GetMultipartInfo returns multipart info of the uploadId of the object
func (l *s3Objects) GetMultipartInfo(ctx context.Context, bucket, object, uploadID string, opts otterio.ObjectOptions) (result otterio.MultipartInfo, err error) {
	result.Bucket = bucket
	result.Object = object
	result.UploadID = uploadID
	return result, nil
}

// ListObjectParts returns all object parts for specified object in specified bucket
func (l *s3Objects) ListObjectParts(ctx context.Context, bucket string, object string, uploadID string, partNumberMarker int, maxParts int, opts otterio.ObjectOptions) (lpi otterio.ListPartsInfo, e error) {
	result, err := l.Client.ListObjectParts(ctx, bucket, object, uploadID, partNumberMarker, maxParts)
	if err != nil {
		return lpi, err
	}
	lpi = otterio.FromOtterioClientListPartsInfo(result)
	if lpi.IsTruncated && maxParts > len(lpi.Parts) {
		partNumberMarker = lpi.NextPartNumberMarker
		for {
			result, err = l.Client.ListObjectParts(ctx, bucket, object, uploadID, partNumberMarker, maxParts)
			if err != nil {
				return lpi, err
			}

			nlpi := otterio.FromOtterioClientListPartsInfo(result)

			partNumberMarker = nlpi.NextPartNumberMarker

			lpi.Parts = append(lpi.Parts, nlpi.Parts...)
			if !nlpi.IsTruncated {
				break
			}
		}
	}
	return lpi, nil
}

// AbortMultipartUpload aborts a ongoing multipart upload
func (l *s3Objects) AbortMultipartUpload(ctx context.Context, bucket string, object string, uploadID string, opts otterio.ObjectOptions) error {
	err := l.Client.AbortMultipartUpload(ctx, bucket, object, uploadID)
	return otterio.ErrorRespToObjectError(err, bucket, object)
}

// CompleteMultipartUpload completes ongoing multipart upload and finalizes object
func (l *s3Objects) CompleteMultipartUpload(ctx context.Context, bucket string, object string, uploadID string, uploadedParts []otterio.CompletePart, opts otterio.ObjectOptions) (oi otterio.ObjectInfo, e error) {
	uploadInfo, err := l.Client.CompleteMultipartUpload(ctx, bucket, object, uploadID, otterio.ToOtterioClientCompleteParts(uploadedParts), otteriogo.PutObjectOptions{
		ServerSideEncryption: opts.ServerSideEncryption,
	})
	if err != nil {
		return oi, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	return otterio.ObjectInfo{Bucket: bucket, Name: object, ETag: strings.Trim(uploadInfo.ETag, "\"")}, nil
}

// SetBucketPolicy sets policy on bucket
func (l *s3Objects) SetBucketPolicy(ctx context.Context, bucket string, bucketPolicy *policy.Policy) error {
	data, err := json.Marshal(bucketPolicy)
	if err != nil {
		// This should not happen.
		logger.LogIf(ctx, err)
		return otterio.ErrorRespToObjectError(err, bucket)
	}

	if err := l.Client.SetBucketPolicy(ctx, bucket, string(data)); err != nil {
		return otterio.ErrorRespToObjectError(err, bucket)
	}

	return nil
}

// GetBucketPolicy will get policy on bucket
func (l *s3Objects) GetBucketPolicy(ctx context.Context, bucket string) (*policy.Policy, error) {
	data, err := l.Client.GetBucketPolicy(ctx, bucket)
	if err != nil {
		return nil, otterio.ErrorRespToObjectError(err, bucket)
	}

	bucketPolicy, err := policy.ParseConfig(strings.NewReader(data), bucket)
	return bucketPolicy, otterio.ErrorRespToObjectError(err, bucket)
}

// DeleteBucketPolicy deletes all policies on bucket
func (l *s3Objects) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	if err := l.Client.SetBucketPolicy(ctx, bucket, ""); err != nil {
		return otterio.ErrorRespToObjectError(err, bucket, "")
	}
	return nil
}

// GetObjectTags gets the tags set on the object
func (l *s3Objects) GetObjectTags(ctx context.Context, bucket string, object string, opts otterio.ObjectOptions) (*tags.Tags, error) {
	var err error
	if _, err = l.GetObjectInfo(ctx, bucket, object, opts); err != nil {
		return nil, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	t, err := l.Client.GetObjectTagging(ctx, bucket, object, otteriogo.GetObjectTaggingOptions{})
	if err != nil {
		return nil, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	return t, nil
}

// PutObjectTags attaches the tags to the object
func (l *s3Objects) PutObjectTags(ctx context.Context, bucket, object string, tagStr string, opts otterio.ObjectOptions) (otterio.ObjectInfo, error) {
	tagObj, err := tags.Parse(tagStr, true)
	if err != nil {
		return otterio.ObjectInfo{}, otterio.ErrorRespToObjectError(err, bucket, object)
	}
	if err = l.Client.PutObjectTagging(ctx, bucket, object, tagObj, otteriogo.PutObjectTaggingOptions{VersionID: opts.VersionID}); err != nil {
		return otterio.ObjectInfo{}, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	objInfo, err := l.GetObjectInfo(ctx, bucket, object, opts)
	if err != nil {
		return otterio.ObjectInfo{}, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	return objInfo, nil
}

// DeleteObjectTags removes the tags attached to the object
func (l *s3Objects) DeleteObjectTags(ctx context.Context, bucket, object string, opts otterio.ObjectOptions) (otterio.ObjectInfo, error) {
	if err := l.Client.RemoveObjectTagging(ctx, bucket, object, otteriogo.RemoveObjectTaggingOptions{}); err != nil {
		return otterio.ObjectInfo{}, otterio.ErrorRespToObjectError(err, bucket, object)
	}
	objInfo, err := l.GetObjectInfo(ctx, bucket, object, opts)
	if err != nil {
		return otterio.ObjectInfo{}, otterio.ErrorRespToObjectError(err, bucket, object)
	}

	return objInfo, nil
}

// IsCompressionSupported returns whether compression is applicable for this layer.
func (l *s3Objects) IsCompressionSupported() bool {
	return false
}

// IsEncryptionSupported returns whether server side encryption is implemented for this layer.
func (l *s3Objects) IsEncryptionSupported() bool {
	return otterio.GlobalKMS != nil || otterio.GlobalGatewaySSE.IsSet()
}

func (l *s3Objects) IsTaggingSupported() bool {
	return true
}
