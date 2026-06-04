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
	"context"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gofiber/fiber/v3"
	"github.com/minio/minio-go/v7/pkg/set"
	"github.com/soulteary/otterio/cmd/config/dns"
	"github.com/soulteary/otterio/cmd/crypto"
	xhttp "github.com/soulteary/otterio/cmd/http"
)

// This file contains native Fiber re-implementations of the global middleware
// chain (originally net/http handlers in generic-handlers.go / auth-handler.go,
// listed in globalHandlers). Running them natively rather than through
// adaptor.HTTPMiddleware avoids materializing the request body: the adaptor's
// ConvertRequest -> PostBody copies the entire body into memory on the very
// first middleware, which defeated streaming uploads and inter-node REST data
// transfers. These native versions only inspect headers / URL, so the body
// stays a stream until the actual S3 (or REST) handler reads it.

// sharedRequestKey caches a non-buffering *http.Request for the duration of a
// request so the chain does not rebuild it per middleware.
type sharedRequestKey struct{}

// fiberSharedRequest returns a per-request *http.Request bridged from the Fiber
// context, built once and cached in Locals. The request body is wired to the
// fasthttp body stream (see fiberRequestBody) but is never read by the
// middleware chain, so no buffering occurs. Handlers and proxy paths build
// their own request instances and are the only readers of the body stream.
func fiberSharedRequest(c fiber.Ctx) (*http.Request, error) {
	if v := c.Locals(sharedRequestKey{}); v != nil {
		return v.(*http.Request), nil
	}
	r, err := fiberRequest(c)
	if err != nil {
		return nil, err
	}
	c.Locals(sharedRequestKey{}, r)
	return r, nil
}

// 1. filterReservedMetadata
func filterReservedMetadataFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if containsReservedMetadata(r.Header) {
		writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrUnsupportedMetadata), guessIsBrowserReq(r))
		return nil
	}
	return c.Next()
}

// 2. setSSETLSHandler
func setSSETLSHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if !globalIsTLS && (crypto.SSEC.IsRequested(r.Header) || crypto.SSECopy.IsRequested(r.Header)) {
		if r.Method == http.MethodHead {
			writeErrorResponseHeadersOnlyFiber(c, errorCodes.ToAPIErr(ErrInsecureSSECustomerRequest))
		} else {
			writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrInsecureSSECustomerRequest), guessIsBrowserReq(r))
		}
		return nil
	}
	return c.Next()
}

// 3. setAuthHandler
func setAuthHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	aType := getRequestAuthType(r)
	if isSupportedS3AuthType(aType) {
		return c.Next()
	} else if aType == authTypeJWT {
		if _, _, authErr := webRequestAuthenticate(r); authErr != nil {
			c.Status(http.StatusUnauthorized)
			return c.SendString(authErr.Error())
		}
		return c.Next()
	} else if aType == authTypeSTS {
		return c.Next()
	}
	writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrSignatureVersionNotSupported), guessIsBrowserReq(r))
	atomic.AddUint64(&globalHTTPStats.rejectedRequestsAuth, 1)
	return nil
}

// 4. setTimeValidityHandler
func setTimeValidityHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	aType := getRequestAuthType(r)
	if aType == authTypeSigned || aType == authTypeSignedV2 || aType == authTypeStreamingSigned {
		amzDate, errCode := parseAmzDateHeader(r)
		if errCode != ErrNone {
			writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(errCode), guessIsBrowserReq(r))
			atomic.AddUint64(&globalHTTPStats.rejectedRequestsTime, 1)
			return nil
		}
		curTime := UTCNow()
		if curTime.Sub(amzDate) > globalMaxSkewTime || amzDate.Sub(curTime) > globalMaxSkewTime {
			writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrRequestTimeTooSkewed), guessIsBrowserReq(r))
			atomic.AddUint64(&globalHTTPStats.rejectedRequestsTime, 1)
			return nil
		}
	}
	return c.Next()
}

// 5. setBrowserCacheControlHandler
func setBrowserCacheControlHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if globalBrowserEnabled && r.Method == http.MethodGet && guessIsBrowserReq(r) {
		if HasPrefix(r.URL.Path, otterioReservedBucketPath+SlashSeparator) {
			if HasSuffix(r.URL.Path, ".js") || r.URL.Path == otterioReservedBucketPath+"/favicon.ico" {
				c.Set(xhttp.CacheControl, "max-age=31536000")
			} else {
				c.Set(xhttp.CacheControl, "no-store")
			}
		}
	}
	return c.Next()
}

// 6. setReservedBucketHandler
func setReservedBucketHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	bucketName, _ := request2BucketObjectName(r)
	if isOtterioReservedBucket(bucketName) || isOtterioMetaBucket(bucketName) {
		if !guessIsRPCReq(r) && !guessIsBrowserReq(r) && !guessIsHealthCheckReq(r) && !guessIsMetricsReq(r) && !isAdminReq(r) {
			writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrAllAccessDisabled), guessIsBrowserReq(r))
			return nil
		}
	}
	return c.Next()
}

// 7. setBrowserRedirectHandler
func setBrowserRedirectHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if globalBrowserEnabled && guessIsBrowserReq(r) {
		if redirectLocation := getRedirectLocation(r.URL.Path); redirectLocation != "" {
			return c.Redirect().Status(http.StatusTemporaryRedirect).To(redirectLocation)
		}
	}
	return c.Next()
}

// 8. setCrossDomainPolicy
func setCrossDomainPolicyFiber(c fiber.Ctx) error {
	if c.Path() == crossDomainXMLEntity {
		return c.Send([]byte(crossDomainXML))
	}
	return c.Next()
}

// 9. setRequestHeaderSizeLimitHandler
func setRequestHeaderSizeLimitHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if isHTTPHeaderSizeTooLarge(r.Header) {
		writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrMetadataTooLarge), guessIsBrowserReq(r))
		atomic.AddUint64(&globalHTTPStats.rejectedRequestsHeader, 1)
		return nil
	}
	return c.Next()
}

// 10. setRequestSizeLimitHandler is intentionally omitted: the equivalent body
// size cap is enforced by fiber's BodyLimit (set to requestMaxBodySize in
// newFiberApp), which rejects oversized bodies at the fasthttp layer. Wrapping
// the body with a MaxBytesReader here would force the stream to materialize.

// 11. setHTTPStatsHandler meters incoming/outgoing traffic. To preserve
// streaming it does not wrap the body with a metering ReadCloser (which would
// hold a reference forcing buffering); instead it approximates input bytes from
// Content-Length and output bytes from the response body length / Content-Length.
// For streamed responses the measurement is deferred to stream completion so it
// reflects the full transfer, matching the net/http metering window.
func setHTTPStatsHandlerFiber(c fiber.Ctx) error {
	err := c.Next()

	inputBytes := int64(c.Request().Header.ContentLength())
	if inputBytes < 0 {
		inputBytes = 0
	}
	// Content-Length is -1 (clamped to 0 above) for chunked / aws-chunked
	// streaming-signature uploads. By the time c.Next() returns the handler has
	// consumed the request body, so prefer the actual number of bytes read,
	// which accurately reflects incoming traffic for those uploads.
	if read := requestInputBytesRead(c); read > inputBytes {
		inputBytes = read
	}
	reserved := strings.HasPrefix(c.Path(), otterioReservedBucketPath)
	record := func(out int64) {
		if out < 0 {
			out = 0
		}
		if reserved {
			globalConnStats.incInputBytes(int(inputBytes))
			globalConnStats.incOutputBytes(int(out))
		} else {
			globalConnStats.incS3InputBytes(int(inputBytes))
			globalConnStats.incS3OutputBytes(int(out))
		}
	}

	if sc := streamCompletionOf(c); sc != nil {
		outLen := int64(c.Response().Header.ContentLength())
		sc.add(func() { record(outLen) })
		return err
	}
	record(int64(len(c.Response().Body())))
	return err
}

// 12. setRequestValidityHandler
func setRequestValidityHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if hasBadPathComponent(r.URL.Path) {
		writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrInvalidResourceName), guessIsBrowserReq(r))
		atomic.AddUint64(&globalHTTPStats.rejectedRequestsInvalid, 1)
		return nil
	}
	for _, vv := range r.URL.Query() {
		for _, v := range vv {
			if hasBadPathComponent(v) {
				writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrInvalidResourceName), guessIsBrowserReq(r))
				atomic.AddUint64(&globalHTTPStats.rejectedRequestsInvalid, 1)
				return nil
			}
		}
	}
	if hasMultipleAuth(r) {
		writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrInvalidRequest), guessIsBrowserReq(r))
		atomic.AddUint64(&globalHTTPStats.rejectedRequestsInvalid, 1)
		return nil
	}
	return c.Next()
}

// 13. setBucketForwardingHandler
func setBucketForwardingHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if globalDNSConfig == nil || len(globalDomainNames) == 0 || !globalBucketFederation ||
		guessIsHealthCheckReq(r) || guessIsMetricsReq(r) ||
		guessIsRPCReq(r) || guessIsLoginSTSReq(r) || isAdminReq(r) {
		return c.Next()
	}

	// For browser requests, when federation is setup we need to specifically
	// handle download and upload for browser requests.
	if guessIsBrowserReq(r) {
		var bucket string
		switch r.Method {
		case http.MethodPut:
			if getRequestAuthType(r) == authTypeJWT {
				bucket, _ = path2BucketObjectWithBasePath(otterioReservedBucketPath+"/upload", r.URL.Path)
			}
		case http.MethodGet:
			if t := r.URL.Query().Get("token"); t != "" {
				bucket, _ = path2BucketObjectWithBasePath(otterioReservedBucketPath+"/download", r.URL.Path)
			} else if getRequestAuthType(r) != authTypeJWT && !strings.HasPrefix(r.URL.Path, otterioReservedBucketPath) {
				bucket, _ = request2BucketObjectName(r)
			}
		}
		if bucket == "" {
			return c.Next()
		}
		sr, derr := globalDNSConfig.Get(bucket)
		if derr != nil {
			if derr == dns.ErrNoEntriesFound {
				writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrNoSuchBucket), guessIsBrowserReq(r))
			} else {
				writeErrorResponseFiber(c.Context(), c, toAPIError(c.Context(), derr), guessIsBrowserReq(r))
			}
			return nil
		}
		if globalDomainIPs.Intersection(set.CreateStringSet(getHostsSlice(sr)...)).IsEmpty() {
			return fiberForwardRequest(c, getHostFromSrv(sr))
		}
		return c.Next()
	}

	bucket, object := request2BucketObjectName(r)

	// Requests in federated setups for STS type calls which are performed at '/'
	// resource should be routed by the muxer, the assumption is simply such that
	// requests without a bucket in a federated setup cannot be proxied, so serve
	// them at current server.
	if bucket == "" {
		return c.Next()
	}

	// MakeBucket requests should be handled at current endpoint.
	if r.Method == http.MethodPut && bucket != "" && object == "" && r.URL.RawQuery == "" {
		return c.Next()
	}

	// CopyObject requests should be handled at current endpoint as path style
	// requests have target bucket and object in URI and source details are in
	// header fields.
	if r.Method == http.MethodPut && r.Header.Get(xhttp.AmzCopySource) != "" {
		bucket, object = path2BucketObject(r.Header.Get(xhttp.AmzCopySource))
		if bucket == "" || object == "" {
			return c.Next()
		}
	}
	sr, derr := globalDNSConfig.Get(bucket)
	if derr != nil {
		if derr == dns.ErrNoEntriesFound {
			writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrNoSuchBucket), guessIsBrowserReq(r))
		} else {
			writeErrorResponseFiber(c.Context(), c, toAPIError(c.Context(), derr), guessIsBrowserReq(r))
		}
		return nil
	}
	if globalDomainIPs.Intersection(set.CreateStringSet(getHostsSlice(sr)...)).IsEmpty() {
		return fiberForwardRequest(c, getHostFromSrv(sr))
	}
	return c.Next()
}

// fiberForwardRequest proxies the current request to host via globalForwarder,
// streaming the body (no buffering). It clears any response headers set by the
// chain so far, mirroring the net/http handler.
func fiberForwardRequest(c fiber.Ctx, host string) error {
	r, err := fiberRequest(c)
	if err != nil {
		return err
	}
	r = setURLVarsOnRequest(r, allPathParams(c))
	r.URL.Scheme = "http"
	if globalIsTLS {
		r.URL.Scheme = "https"
	}
	r.URL.Host = host

	c.Response().Header.Reset()
	w := newFiberResponseWriter(c)
	globalForwarder.ServeHTTP(w, r)
	w.finalize()
	return nil
}

// 16. setRedirectHandler
func setRedirectHandlerFiber(c fiber.Ctx) error {
	r, err := fiberSharedRequest(c)
	if err != nil {
		return err
	}
	if !shouldProxy() || guessIsRPCReq(r) || guessIsBrowserReq(r) ||
		guessIsHealthCheckReq(r) || guessIsMetricsReq(r) || isAdminReq(r) {
		return c.Next()
	}
	// If this server is still initializing, proxy the request to any other
	// online server to avoid 503 for incoming API calls.
	if idx := getOnlineProxyEndpointIdx(); idx >= 0 {
		return fiberProxyRequest(c, globalProxyEndpoints[idx])
	}
	return c.Next()
}

// fiberProxyRequest proxies the current request to the given proxy endpoint,
// streaming the body. proxyRequest clears the writer headers itself; we reset
// the fasthttp response header too so chain-set headers do not leak.
func fiberProxyRequest(c fiber.Ctx, ep ProxyEndpoint) error {
	r, err := fiberRequest(c)
	if err != nil {
		return err
	}
	r = setURLVarsOnRequest(r, allPathParams(c))
	c.Response().Header.Reset()
	w := newFiberResponseWriter(c)
	proxyRequest(context.TODO(), w, r, ep)
	w.finalize()
	return nil
}
