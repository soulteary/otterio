/*
 * MinIO Cloud Storage, (C) 2015-2020 MinIO, Inc.
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
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/valyala/fasthttp"
)

func registerBucketLevelFuncFiber(objectRules, bucketRules *[]routeRule, api objectAPIHandlers, apiFunctions ...string) {
	for _, apiFunction := range apiFunctions {
		switch apiFunction {
		case "PostPolicy":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodPost}, "postpolicybucket", true, api.PostPolicyBucketHandler, nil,
				map[string]*regexp.Regexp{xhttp.ContentType: fiberMultipartFormRegex}))
		case "HeadObject":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodHead}, "headobject", false, api.HeadObjectHandler, nil, nil))
		case "GetObject":
			*objectRules = append(*objectRules, s3RouteStream([]string{http.MethodGet}, "getobject", true, api.GetObjectHandler, nil, nil))
		case "PutObject":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodPut}, "putobject", true, api.PutObjectHandler, nil, nil))
		case "DeleteObject":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodDelete}, "deleteobject", false, api.DeleteObjectHandler, nil, nil))
		case "CopyObject":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodPut}, "copyobject", false, api.CopyObjectHandler, nil,
				map[string]*regexp.Regexp{xhttp.AmzCopySource: fiberAmzCopySourceRegex}))
		case "PutBucketPolicy":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodPut}, "putbucketpolicy", false, api.PutBucketPolicyHandler, qm("policy", ""), nil))
		case "DeleteBucketPolicy":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodDelete}, "deletebucketpolicy", false, api.DeleteBucketPolicyHandler, qm("policy", ""), nil))
		case "GetBucketPolicy":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodGet}, "getbucketpolicy", false, api.GetBucketPolicyHandler, qm("policy", ""), nil))
		case "GetBucketLifecycle":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodGet}, "getbucketlifecycle", false, api.GetBucketLifecycleHandler, qm("lifecycle", ""), nil))
		case "PutBucketLifecycle":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodPut}, "putbucketlifecycle", false, api.PutBucketLifecycleHandler, qm("lifecycle", ""), nil))
		case "DeleteBucketLifecycle":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodDelete}, "deletebucketlifecycle", false, api.DeleteBucketLifecycleHandler, qm("lifecycle", ""), nil))
		case "GetBucketLocation":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodGet}, "getbucketlocation", false, api.GetBucketLocationHandler, qm("location", ""), nil))
		case "HeadBucket":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodHead}, "headbucket", false, api.HeadBucketHandler, nil, nil))
		case "DeleteMultipleObjects":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodPost}, "deletemultipleobjects", false, api.DeleteMultipleObjectsHandler, qm("delete", ""), nil))
		case "NewMultipart":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodPost}, "newmultipartupload", false, api.NewMultipartUploadHandler, qm("uploads", ""), nil))
		case "CopyObjectPart":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodPut}, "copyobjectpart", false, api.CopyObjectPartHandler,
				qm("partNumber", "[0-9]+", "uploadId", ".*"),
				map[string]*regexp.Regexp{xhttp.AmzCopySource: fiberAmzCopySourceRegex}))
		case "PutObjectPart":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodPut}, "putobjectpart", true, api.PutObjectPartHandler,
				qm("partNumber", "[0-9]+", "uploadId", ".*"), nil))
		case "ListObjectParts":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodGet}, "listobjectparts", false, api.ListObjectPartsHandler, qm("uploadId", ".*"), nil))
		case "ListMultipartUploads":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodGet}, "listmultipartuploads", false, api.ListMultipartUploadsHandler, qm("uploads", ""), nil))
		case "CompleteMultipart":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodPost}, "completemutipartupload", false, api.CompleteMultipartUploadHandler, qm("uploadId", ".*"), nil))
		case "AbortMultipart":
			*objectRules = append(*objectRules, s3Route([]string{http.MethodDelete}, "abortmultipartupload", false, api.AbortMultipartUploadHandler, qm("uploadId", ".*"), nil))
		case "GetBucketNotification":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodGet}, "getbucketnotification", false, api.GetBucketNotificationHandler, qm("notification", ""), nil))
		case "PutBucketNotification":
			*bucketRules = append(*bucketRules, s3Route([]string{http.MethodPut}, "putbucketnotification", false, api.PutBucketNotificationHandler, qm("notification", ""), nil))
		case "ListenNotification":
			*bucketRules = append(*bucketRules, s3RouteStream([]string{http.MethodGet}, "listennotification", false, api.ListenNotificationHandler, qm("events", ".*"), nil))
		}
	}
}

func registerAPIFunctionsFiber(app *fiber.App, objLayer ObjectLayer, apiFunctions ...string) {
	if len(apiFunctions) == 0 {
		registerAPIRouterFiber(app)
		return
	}

	globalObjLayerMutex.Lock()
	globalObjectAPI = objLayer
	globalObjLayerMutex.Unlock()

	api := objectAPIHandlers{
		ObjectAPI: func() ObjectLayer {
			return globalObjectAPI
		},
		CacheAPI: func() CacheObjectLayer {
			return globalCacheObjectAPI
		},
	}

	var objectRules, bucketRules []routeRule
	registerBucketLevelFuncFiber(&objectRules, &bucketRules, api, apiFunctions...)

	// Mirror gorilla/mux's default MethodNotAllowedHandler (HTTP 405) used by the
	// legacy test router; the production router intentionally returns 400 instead.
	objectHandler := makeTestS3DispatchHandler(objectRules)
	bucketHandler := makeTestS3DispatchHandler(bucketRules)
	objectDispatch := func(c fiber.Ctx) error {
		if strings.TrimPrefix(c.Params("*"), "/") == "" {
			return bucketHandler(c)
		}
		return objectHandler(c)
	}
	rootDispatch := makeTestS3DispatchHandler([]routeRule{
		s3Route([]string{http.MethodGet}, "listbuckets", false, api.ListBucketsHandler, nil, nil),
	})

	apiGroup := app.Group("", vhostBucketMiddleware)
	apiGroup.All("/:bucket/*", objectDispatch)
	apiGroup.All("/:bucket", bucketHandler)

	app.All("/", func(c fiber.Ctx) error {
		if pathParamBucket(c) != "" {
			return bucketHandler(c)
		}
		return rootDispatch(c)
	})
}

// makeTestS3DispatchHandler dispatches S3 routes for the unit-test router and,
// when no rule matches, replies with HTTP 405 like gorilla/mux's default handler.
func makeTestS3DispatchHandler(rules []routeRule) fiber.Handler {
	return func(c fiber.Ctx) error {
		if matched, err := dispatchRules(c, rules); matched {
			return err
		}
		return c.SendStatus(fiber.StatusMethodNotAllowed)
	}
}

// testContentLengthKey is a per-request fasthttp user-value key used by
// fiberHTTPTestHandler to carry the client-declared Content-Length (or -1 for an
// intentionally unknown length) into fiberRequest. It is request-scoped to keep
// concurrent tests free of data races; adaptor otherwise buffers the body and
// assigns a positive length, while legacy handlers rely on ContentLength == -1.
type testContentLengthKey struct{}

// fiberHTTPTestHandler wraps a Fiber app for httptest/recorder usage.
//
// Unlike adaptor.FiberApp it copies response header names verbatim from fasthttp
// instead of going through net/http canonicalization, so literal casings such as
// "ETag" (set directly via map assignment by legacy handlers) survive into tests.
func fiberHTTPTestHandler(app *fiber.App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI == "" {
			r.RequestURI = r.URL.RequestURI()
		}

		fctx := new(fasthttp.RequestCtx)
		fasthttpReq, err := fiberTestFasthttpRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		remoteAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
		fctx.Init(fasthttpReq, remoteAddr, nil)

		if r.ContentLength < 0 {
			fctx.SetUserValue(testContentLengthKey{}, int64(-1))
		} else {
			fctx.SetUserValue(testContentLengthKey{}, r.ContentLength)
		}

		app.Handler()(fctx)

		resp := &fctx.Response
		resp.Header.DisableNormalizing()
		resp.Header.VisitAll(func(key, value []byte) {
			w.Header()[string(key)] = append(w.Header()[string(key)], string(value))
		})
		w.WriteHeader(resp.StatusCode())
		_, _ = w.Write(resp.Body())
	})
}

// fiberTestFasthttpRequest builds a fasthttp.Request from a net/http request for tests.
func fiberTestFasthttpRequest(r *http.Request) (*fasthttp.Request, error) {
	req := new(fasthttp.Request)
	req.Header.SetMethod(r.Method)
	req.SetRequestURI(r.RequestURI)
	req.Header.SetHost(r.Host)
	req.SetHost(r.Host)
	for k, vv := range r.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		req.SetBody(body)
		if r.ContentLength >= 0 {
			req.Header.SetContentLength(len(body))
		}
	}
	return req, nil
}

// initTestAPIEndPoints registers selected S3 API endpoints for unit tests.
func initTestAPIEndPoints(objLayer ObjectLayer, apiFunctions []string) http.Handler {
	app := newFiberApp()
	app.Use(corsMiddlewareFiber())

	if len(apiFunctions) > 0 {
		registerAPIFunctionsFiber(app, objLayer, apiFunctions...)
	} else {
		globalObjLayerMutex.Lock()
		globalObjectAPI = objLayer
		globalObjLayerMutex.Unlock()
		registerAPIRouterFiber(app)
	}

	return fiberHTTPTestHandler(app)
}

// initTestWebRPCEndPoint registers web RPC handlers for unit tests.
func initTestWebRPCEndPoint(objLayer ObjectLayer) http.Handler {
	globalObjLayerMutex.Lock()
	globalObjectAPI = objLayer
	globalObjLayerMutex.Unlock()

	app := newFiberApp()
	if err := registerWebRouterFiber(app); err != nil {
		panic(err)
	}
	return fiberHTTPTestHandler(app)
}
