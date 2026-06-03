/*
 * MinIO Cloud Storage, (C) 2016-2020 MinIO, Inc.
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

package cmd

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
	xhttp "github.com/soulteary/otterio/cmd/http"
)

var (
	fiberAmzCopySourceRegex      = regexp.MustCompile(`.*?(\\/|%2F).*?`)
	fiberAmzSnowballExtractRegex = regexp.MustCompile(`(?i)^true$`)
	// SECURITY (CVE-2023-28434): anchor the full Content-Type value so the
	// post-policy route only matches when the media type is exactly
	// "multipart/form-data" (optionally followed by ;parameters). The previous
	// unanchored prefix pattern also matched values like
	// "multipart/form-data-evil", letting the router disagree with the
	// post-policy auth classification (isRequestPostPolicySignatureV4) and
	// enabling reserved/meta bucket protection bypass. Matches the strict
	// media-type check used in auth-handler.go. See minio/minio#16849.
	fiberMultipartFormRegex = regexp.MustCompile(`(?i)^multipart/form-data\s*(;.*)?$`)
)

func notImplementedHandlerFiber(c fiber.Ctx) error {
	writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrNotImplemented), guessIsBrowserReqFiber(c))
	return nil
}

func queriesFromRejected(pairs []string) map[string]string {
	m := make(map[string]string, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		if i+1 < len(pairs) {
			m[pairs[i]] = pairs[i+1]
		} else {
			m[pairs[i]] = ""
		}
	}
	return m
}

func rejectedRouteRule(r rejectedAPI) routeRule {
	return routeRule{
		methods: r.methods,
		queries: queriesFromRejected(r.queries),
		// handler is already wrapped with stats + trace below; skipTrace avoids
		// dispatchRules adding a second trace wrapper (double trace publish).
		handler:   collectAPIStatsFiber(r.api, httpTraceAllFiber(notImplementedHandlerFiber)),
		skipTrace: true,
	}
}

func rejectedObjectAPIRules() []routeRule {
	var rules []routeRule
	for _, r := range rejectedAPIs {
		if r.path != "" {
			rules = append(rules, rejectedRouteRule(r))
		}
	}
	return rules
}

func rejectedBucketAPIRules() []routeRule {
	var rules []routeRule
	for _, r := range rejectedAPIs {
		if r.path == "" {
			rules = append(rules, rejectedRouteRule(r))
		}
	}
	return rules
}

func s3Route(methods []string, api string, traceHdrs bool, h func(http.ResponseWriter, *http.Request), queries map[string]string, headerRegex map[string]*regexp.Regexp) routeRule {
	return routeRule{
		methods:     methods,
		queries:     queries,
		headerRegex: headerRegex,
		// wrapS3Handler already applies stats + maxClients + trace; skipTrace
		// prevents dispatchRules from wrapping a second trace (double publish).
		handler:   wrapS3Handler(api, traceHdrs, toMinioHandler(h)),
		skipTrace: true,
	}
}

// s3RouteStream is like s3Route but bridges the handler with a streaming
// response writer, so large response bodies (e.g. GetObject) are streamed to
// the client instead of being fully buffered in memory.
func s3RouteStream(methods []string, api string, traceHdrs bool, h func(http.ResponseWriter, *http.Request), queries map[string]string, headerRegex map[string]*regexp.Regexp) routeRule {
	return routeRule{
		methods:     methods,
		queries:     queries,
		headerRegex: headerRegex,
		handler:     wrapS3Handler(api, traceHdrs, toMinioStreamHandler(h)),
		skipTrace:   true,
	}
}

func qm(pairs ...string) map[string]string {
	m := make(map[string]string, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		pattern := ""
		if i+1 < len(pairs) {
			pattern = pairs[i+1]
		}
		m[pairs[i]] = pattern
	}
	return m
}

func objectS3APIRules(api objectAPIHandlers) []routeRule {
	return []routeRule{
		s3Route([]string{http.MethodHead}, "headobject", false, api.HeadObjectHandler, nil, nil),
		s3Route([]string{http.MethodPut}, "copyobjectpart", false, api.CopyObjectPartHandler,
			qm("partNumber", "[0-9]+", "uploadId", ".*"),
			map[string]*regexp.Regexp{xhttp.AmzCopySource: fiberAmzCopySourceRegex}),
		s3Route([]string{http.MethodPut}, "putobjectpart", true, api.PutObjectPartHandler,
			qm("partNumber", "[0-9]+", "uploadId", ".*"), nil),
		s3Route([]string{http.MethodGet}, "listobjectparts", false, api.ListObjectPartsHandler,
			qm("uploadId", ".*"), nil),
		s3Route([]string{http.MethodPost}, "completemutipartupload", false, api.CompleteMultipartUploadHandler,
			qm("uploadId", ".*"), nil),
		s3Route([]string{http.MethodPost}, "newmultipartupload", false, api.NewMultipartUploadHandler,
			qm("uploads", ""), nil),
		s3Route([]string{http.MethodDelete}, "abortmultipartupload", false, api.AbortMultipartUploadHandler,
			qm("uploadId", ".*"), nil),
		s3Route([]string{http.MethodGet}, "getobjectacl", true, api.GetObjectACLHandler, qm("acl", ""), nil),
		s3Route([]string{http.MethodPut}, "putobjectacl", true, api.PutObjectACLHandler, qm("acl", ""), nil),
		s3Route([]string{http.MethodGet}, "getobjecttagging", true, api.GetObjectTaggingHandler, qm("tagging", ""), nil),
		s3Route([]string{http.MethodPut}, "putobjecttagging", true, api.PutObjectTaggingHandler, qm("tagging", ""), nil),
		s3Route([]string{http.MethodDelete}, "deleteobjecttagging", true, api.DeleteObjectTaggingHandler, qm("tagging", ""), nil),
		// SelectObjectContent streams framed record/progress/keepalive messages
		// (1s continuation keepalive) and can return large result sets, so it must
		// stream like GetObject: the buffered bridge would accumulate the entire
		// framed output in memory and silently drop the keepalive.
		s3RouteStream([]string{http.MethodPost}, "selectobjectcontent", true, api.SelectObjectContentHandler,
			qm("select", "", "select-type", "2"), nil),
		s3Route([]string{http.MethodGet}, "getobjectretention", false, api.GetObjectRetentionHandler, qm("retention", ""), nil),
		s3Route([]string{http.MethodGet}, "getobjectlegalhold", false, api.GetObjectLegalHoldHandler, qm("legal-hold", ""), nil),
		s3RouteStream([]string{http.MethodGet}, "getobject", true, api.GetObjectHandler, nil, nil),
		s3Route([]string{http.MethodPut}, "copyobject", false, api.CopyObjectHandler, nil,
			map[string]*regexp.Regexp{xhttp.AmzCopySource: fiberAmzCopySourceRegex}),
		s3Route([]string{http.MethodPut}, "putobjectretention", false, api.PutObjectRetentionHandler, qm("retention", ""), nil),
		s3Route([]string{http.MethodPut}, "putobjectlegalhold", false, api.PutObjectLegalHoldHandler, qm("legal-hold", ""), nil),
		s3Route([]string{http.MethodPut}, "putobject", true, api.PutObjectExtractHandler, nil,
			map[string]*regexp.Regexp{xhttp.AmzSnowballExtract: fiberAmzSnowballExtractRegex}),
		s3Route([]string{http.MethodPut}, "putobject", true, api.PutObjectHandler, nil, nil),
		s3Route([]string{http.MethodDelete}, "deleteobject", false, api.DeleteObjectHandler, nil, nil),
		s3Route([]string{http.MethodPost}, "restoreobject", false, api.PostRestoreObjectHandler, qm("restore", ""), nil),
	}
}

func bucketS3APIRules(api objectAPIHandlers) []routeRule {
	return []routeRule{
		s3Route([]string{http.MethodGet}, "getbucketlocation", false, api.GetBucketLocationHandler, qm("location", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketpolicy", false, api.GetBucketPolicyHandler, qm("policy", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketlifecycle", false, api.GetBucketLifecycleHandler, qm("lifecycle", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketencryption", false, api.GetBucketEncryptionHandler, qm("encryption", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketobjectlockconfiguration", false, api.GetBucketObjectLockConfigHandler, qm("object-lock", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketreplicationconfiguration", false, api.GetBucketReplicationConfigHandler, qm("replication", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketversioning", false, api.GetBucketVersioningHandler, qm("versioning", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketnotification", false, api.GetBucketNotificationHandler, qm("notification", ""), nil),
		// listennotification is a long-lived event stream that writes keepalive
		// whitespace and relies on per-write Flush + client-disconnect detection.
		// It MUST use the streaming bridge: the buffered bridge would buffer the
		// (unbounded) body in memory, never deliver it, and never terminate.
		s3RouteStream([]string{http.MethodGet}, "listennotification", false, api.ListenNotificationHandler, qm("events", ".*"), nil),
		s3Route([]string{http.MethodGet}, "getbucketacl", false, api.GetBucketACLHandler, qm("acl", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketacl", false, api.PutBucketACLHandler, qm("acl", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketcors", false, api.GetBucketCorsHandler, qm("cors", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketwebsite", false, api.GetBucketWebsiteHandler, qm("website", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketaccelerate", false, api.GetBucketAccelerateHandler, qm("accelerate", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketrequestpayment", false, api.GetBucketRequestPaymentHandler, qm("requestPayment", ""), nil),
		s3Route([]string{http.MethodGet}, "getbucketlogging", false, api.GetBucketLoggingHandler, qm("logging", ""), nil),
		s3Route([]string{http.MethodGet}, "getbuckettagging", false, api.GetBucketTaggingHandler, qm("tagging", ""), nil),
		s3Route([]string{http.MethodDelete}, "deletebucketwebsite", false, api.DeleteBucketWebsiteHandler, qm("website", ""), nil),
		s3Route([]string{http.MethodDelete}, "deletebuckettagging", false, api.DeleteBucketTaggingHandler, qm("tagging", ""), nil),
		s3Route([]string{http.MethodGet}, "listmultipartuploads", false, api.ListMultipartUploadsHandler, qm("uploads", ""), nil),
		s3Route([]string{http.MethodGet}, "listobjectsv2M", false, api.ListObjectsV2MHandler, qm("list-type", "2", "metadata", "true"), nil),
		s3Route([]string{http.MethodGet}, "listobjectsv2", false, api.ListObjectsV2Handler, qm("list-type", "2"), nil),
		s3Route([]string{http.MethodGet}, "listobjectversions", false, api.ListObjectVersionsHandler, qm("versions", ""), nil),
		s3Route([]string{http.MethodGet}, "getpolicystatus", false, api.GetBucketPolicyStatusHandler, qm("policyStatus", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketlifecycle", false, api.PutBucketLifecycleHandler, qm("lifecycle", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketreplicationconfiguration", false, api.PutBucketReplicationConfigHandler, qm("replication", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketencryption", false, api.PutBucketEncryptionHandler, qm("encryption", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketpolicy", false, api.PutBucketPolicyHandler, qm("policy", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketobjectlockconfig", false, api.PutBucketObjectLockConfigHandler, qm("object-lock", ""), nil),
		s3Route([]string{http.MethodPut}, "putbuckettagging", false, api.PutBucketTaggingHandler, qm("tagging", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketversioning", false, api.PutBucketVersioningHandler, qm("versioning", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucketnotification", false, api.PutBucketNotificationHandler, qm("notification", ""), nil),
		s3Route([]string{http.MethodPut}, "putbucket", false, api.PutBucketHandler, nil, nil),
		s3Route([]string{http.MethodHead}, "headbucket", false, api.HeadBucketHandler, nil, nil),
		s3Route([]string{http.MethodPost}, "postpolicybucket", true, api.PostPolicyBucketHandler, nil,
			map[string]*regexp.Regexp{xhttp.ContentType: fiberMultipartFormRegex}),
		s3Route([]string{http.MethodPost}, "deletemultipleobjects", false, api.DeleteMultipleObjectsHandler, qm("delete", ""), nil),
		s3Route([]string{http.MethodDelete}, "deletebucketpolicy", false, api.DeleteBucketPolicyHandler, qm("policy", ""), nil),
		s3Route([]string{http.MethodDelete}, "deletebucketreplicationconfiguration", false, api.DeleteBucketReplicationConfigHandler, qm("replication", ""), nil),
		s3Route([]string{http.MethodDelete}, "deletebucketlifecycle", false, api.DeleteBucketLifecycleHandler, qm("lifecycle", ""), nil),
		s3Route([]string{http.MethodDelete}, "deletebucketencryption", false, api.DeleteBucketEncryptionHandler, qm("encryption", ""), nil),
		s3Route([]string{http.MethodDelete}, "deletebucket", false, api.DeleteBucketHandler, nil, nil),
		s3Route([]string{http.MethodGet}, "getbucketreplicationmetrics", false, api.GetBucketReplicationMetricsHandler, qm("replication-metrics", ""), nil),
		s3Route([]string{http.MethodGet}, "listobjectsv1", false, api.ListObjectsV1Handler, nil, nil),
	}
}

func rootS3APIRules(api objectAPIHandlers) []routeRule {
	return []routeRule{
		// See bucketS3APIRules: listennotification must stream (see s3RouteStream).
		s3RouteStream([]string{http.MethodGet}, "listennotification", false, api.ListenNotificationHandler, qm("events", ".*"), nil),
		s3Route([]string{http.MethodGet}, "listbuckets", false, api.ListBucketsHandler, nil, nil),
	}
}

func makeS3DispatchHandler(rules []routeRule) fiber.Handler {
	methodNotAllowed := collectAPIStatsFiber("methodnotallowed", httpTraceAllFiber(methodNotAllowedHandlerFiber("S3")))
	return func(c fiber.Ctx) error {
		if matched, err := dispatchRules(c, rules); matched {
			return err
		}
		return methodNotAllowed(c)
	}
}

// makeBucketObjectDispatchHandlers returns bucket and object dispatch handlers.
// Paths like /bucket/ match /:bucket/* with an empty wildcard; those are bucket operations.
func makeBucketObjectDispatchHandlers(objectRules, bucketRules []routeRule) (objectDispatch, bucketDispatch fiber.Handler) {
	objectHandler := makeS3DispatchHandler(objectRules)
	bucketHandler := makeS3DispatchHandler(bucketRules)
	objectDispatch = func(c fiber.Ctx) error {
		if strings.TrimPrefix(c.Params("*"), "/") == "" {
			return bucketHandler(c)
		}
		return objectHandler(c)
	}
	return objectDispatch, bucketHandler
}

// registerAPIRouterFiber registers S3 compatible APIs on a Fiber app.
func registerAPIRouterFiber(app *fiber.App) {
	api := objectAPIHandlers{
		ObjectAPI: newObjectLayerFn,
		CacheAPI:  newCachedObjectLayerFn,
	}

	objectRules := append(rejectedObjectAPIRules(), objectS3APIRules(api)...)
	bucketRules := append(rejectedBucketAPIRules(), bucketS3APIRules(api)...)

	objectDispatch, bucketDispatch := makeBucketObjectDispatchHandlers(objectRules, bucketRules)
	// Raw object handler (without the path-style empty-wildcard check) used to
	// dispatch virtual-host-style object operations, where the whole path is the
	// object key.
	objectHandler := makeS3DispatchHandler(objectRules)
	rootDispatch := makeS3DispatchHandler(rootS3APIRules(api))

	listBucketsDoubleSlash := wrapS3Handler("listbuckets", false, toMinioHandler(api.ListBucketsHandler))
	notFoundHandler := collectAPIStatsFiber("notfound", httpTraceAllFiber(errorResponseHandlerFiber))

	apiGroup := app.Group("", vhostBucketMiddleware)
	// Virtual-host-style requests (bucket in the Host header) must be dispatched
	// before the path-style routes, which would otherwise treat the first path
	// segment as the bucket name and lose the object key.
	apiGroup.Use(func(c fiber.Ctx) error {
		if handled, err := vhostObjectDispatch(c, objectHandler, bucketDispatch); handled {
			return err
		}
		return c.Next()
	})
	apiGroup.All("/:bucket/*", objectDispatch)
	apiGroup.All("/:bucket", bucketDispatch)

	app.All("/", func(c fiber.Ctx) error {
		if pathParamBucket(c) != "" {
			return bucketDispatch(c)
		}
		return rootDispatch(c)
	})
	app.All("//", listBucketsDoubleSlash)

	app.Use(notFoundHandler)
}
