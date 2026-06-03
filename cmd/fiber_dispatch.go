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
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/otterio/pkg/madmin"
)

// queryRegexCache memoizes anchored query-value patterns. The patterns are
// static route constants, so caching avoids recompiling the same regex on every
// request match attempt (the previous code called regexp.MatchString per call).
var (
	queryRegexMu    sync.RWMutex
	queryRegexCache = map[string]*regexp.Regexp{}
)

func compiledQueryRegex(pattern string) *regexp.Regexp {
	queryRegexMu.RLock()
	re, ok := queryRegexCache[pattern]
	queryRegexMu.RUnlock()
	if ok {
		return re
	}
	// Anchor the full value, matching the previous "^pattern$" semantics. A bad
	// pattern compiles to nil and is treated as a non-match (mirrors the old
	// behavior where regexp.MatchString returned an error and matched=false).
	compiled, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		compiled = nil
	}
	queryRegexMu.Lock()
	queryRegexCache[pattern] = compiled
	queryRegexMu.Unlock()
	return compiled
}

type routeRule struct {
	methods           []string
	queries           map[string]string // key -> value pattern; empty value means key must exist with any value
	headerRegex       map[string]*regexp.Regexp
	requireEmptyQuery bool
	handler           MinioHandler
	apiName           string
	traceHeaders      bool
	maxClients        bool
	skipTrace         bool
}

func (r routeRule) matches(c fiber.Ctx) bool {
	method := c.Method()
	methodOK := false
	for _, m := range r.methods {
		if m == method {
			methodOK = true
			break
		}
	}
	if !methodOK {
		return false
	}

	if r.requireEmptyQuery && c.Request().URI().QueryArgs().Len() > 0 {
		return false
	}

	for key, pattern := range r.queries {
		if pattern == "" {
			if !c.Request().URI().QueryArgs().Has(key) {
				return false
			}
			continue
		}
		if !c.Request().URI().QueryArgs().Has(key) {
			return false
		}
		re := compiledQueryRegex(pattern)
		if re == nil || !re.MatchString(c.Query(key)) {
			return false
		}
	}

	for header, re := range r.headerRegex {
		if !re.MatchString(c.Get(header)) {
			return false
		}
	}
	return true
}

func dispatchRules(c fiber.Ctx, rules []routeRule) (bool, error) {
	for _, rule := range rules {
		if !rule.matches(c) {
			continue
		}
		h := rule.handler
		if rule.apiName != "" {
			if rule.maxClients {
				h = maxClientsFiber(rule.apiName, h)
			} else {
				h = collectAPIStatsFiber(rule.apiName, h)
			}
		}
		if !rule.skipTrace {
			if rule.traceHeaders {
				h = httpTraceHdrsFiber(h)
			} else {
				h = httpTraceAllFiber(h)
			}
		}
		return true, h(c)
	}
	return false, nil
}

func dispatchInternalRules(c fiber.Ctx, rules []routeRule) (bool, error) {
	for _, rule := range rules {
		if !rule.matches(c) {
			continue
		}
		return true, rule.handler(c)
	}
	return false, nil
}

func queryRules(keys ...string) map[string]string {
	m := make(map[string]string, len(keys))
	for _, k := range keys {
		m[k] = ".*"
	}
	return m
}

func wrapS3Handler(api string, traceHeaders bool, h MinioHandler) MinioHandler {
	wrapped := h
	wrapped = maxClientsFiber(api, wrapped)
	wrapped = collectAPIStatsFiber(api, wrapped)
	if traceHeaders {
		wrapped = httpTraceHdrsFiber(wrapped)
	} else {
		wrapped = httpTraceAllFiber(wrapped)
	}
	return wrapped
}

func wrapInternalHandler(h MinioHandler) MinioHandler {
	return httpTraceHdrsFiber(h)
}

func httpTraceAllFiber(f MinioHandler) MinioHandler {
	return func(c fiber.Ctx) error {
		if globalTrace.NumSubscribers() == 0 {
			return f(c)
		}
		traceInfo := TraceFiber(f, true, c)
		globalTrace.Publish(traceInfo)
		return nil
	}
}

func httpTraceHdrsFiber(f MinioHandler) MinioHandler {
	return func(c fiber.Ctx) error {
		if globalTrace.NumSubscribers() == 0 {
			return f(c)
		}
		traceInfo := TraceFiber(f, false, c)
		globalTrace.Publish(traceInfo)
		return nil
	}
}

func collectAPIStatsFiber(api string, f MinioHandler) MinioHandler {
	return func(c fiber.Ctx) error {
		globalHTTPStats.currentS3Requests.Inc(api)
		start := time.Now()

		finished := false
		// Panic safety: if f panics (or a streamed response is never set up),
		// still decrement the in-flight counter on unwind.
		defer func() {
			if !finished {
				globalHTTPStats.currentS3Requests.Dec(api)
			}
		}()

		err := f(c)
		finished = true

		// For streamed responses the body is written by fasthttp AFTER this
		// returns (and after fiber recycles the Ctx), so the completion hook
		// must not touch c. Snapshot the final status/path now and only compute
		// the latency / decrement the in-flight counter at completion, so the
		// measurement reflects the full transfer (matching net/http).
		if sc := streamCompletionOf(c); sc != nil {
			status := c.Response().StatusCode()
			if status == 0 {
				status = fiber.StatusOK
			}
			path := strings.Clone(c.Path())
			sc.add(func() {
				globalHTTPStats.updateStatsFiber(api, path, status, time.Since(start))
				globalHTTPStats.currentS3Requests.Dec(api)
			})
			return err
		}

		status := c.Response().StatusCode()
		if status == 0 {
			status = fiber.StatusOK
		}
		globalHTTPStats.updateStatsFiber(api, c.Path(), status, time.Since(start))
		globalHTTPStats.currentS3Requests.Dec(api)
		return err
	}
}

func maxClientsFiber(api string, f MinioHandler) MinioHandler {
	return func(c fiber.Ctx) error {
		pool, deadline := globalAPIConfig.getRequestsPool()
		if pool == nil {
			return f(c)
		}

		globalHTTPStats.addRequestsInQueue(1)
		deadlineTimer := time.NewTimer(deadline)
		defer deadlineTimer.Stop()

		select {
		case pool <- struct{}{}:
			globalHTTPStats.addRequestsInQueue(-1)
			// Release the slot when the handler returns, EXCEPT for streamed
			// responses whose body is written by fasthttp after this returns;
			// for those, hold the slot until the stream completes so the
			// concurrency limit covers the whole transfer (matching net/http).
			releaseOnReturn := true
			defer func() {
				if releaseOnReturn {
					<-pool
				}
			}()
			err := f(c)
			if sc := streamCompletionOf(c); sc != nil {
				releaseOnReturn = false
				sc.add(func() { <-pool })
			}
			return err
		case <-deadlineTimer.C:
			writeErrorResponseFiber(c.Context(), c,
				errorCodes.ToAPIErr(ErrOperationMaxedOut),
				guessIsBrowserReqFiber(c))
			globalHTTPStats.addRequestsInQueue(-1)
			return nil
		case <-c.Context().Done():
			globalHTTPStats.addRequestsInQueue(-1)
			return c.Context().Err()
		}
	}
}

func methodNotAllowedHandlerFiber(api string) MinioHandler {
	return func(c fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions {
			return nil
		}
		version := extractAPIVersionFiber(c)
		reqURL := requestURL(c)
		switch {
		case strings.HasPrefix(c.Path(), peerRESTPrefix):
			desc := fmt.Sprintf("Server expects 'peer' API version '%s', instead found '%s' - *rolling upgrade is not allowed* - please make sure all servers are running the same MinIO version (%s)", peerRESTVersion, version, ReleaseTag)
			writeErrorResponseStringFiber(c.Context(), c, APIError{
				Code:           "XMinioPeerVersionMismatch",
				Description:    desc,
				HTTPStatusCode: fiber.StatusUpgradeRequired,
			})
		case strings.HasPrefix(c.Path(), storageRESTPrefix):
			desc := fmt.Sprintf("Server expects 'storage' API version '%s', instead found '%s' - *rolling upgrade is not allowed* - please make sure all servers are running the same MinIO version (%s)", storageRESTVersion, version, ReleaseTag)
			writeErrorResponseStringFiber(c.Context(), c, APIError{
				Code:           "XMinioStorageVersionMismatch",
				Description:    desc,
				HTTPStatusCode: fiber.StatusUpgradeRequired,
			})
		case strings.HasPrefix(c.Path(), lockRESTPrefix):
			desc := fmt.Sprintf("Server expects 'lock' API version '%s', instead found '%s' - *rolling upgrade is not allowed* - please make sure all servers are running the same MinIO version (%s)", lockRESTVersion, version, ReleaseTag)
			writeErrorResponseStringFiber(c.Context(), c, APIError{
				Code:           "XMinioLockVersionMismatch",
				Description:    desc,
				HTTPStatusCode: fiber.StatusUpgradeRequired,
			})
		case strings.HasPrefix(c.Path(), adminPathPrefix):
			var desc string
			if version == "v1" {
				desc = fmt.Sprintf("Server expects client requests with 'admin' API version '%s', found '%s', please upgrade the client to latest releases", madmin.AdminAPIVersion, version)
			} else if version == madmin.AdminAPIVersion {
				desc = fmt.Sprintf("This 'admin' API is not supported by server in '%s'", getMinioMode())
			} else {
				desc = fmt.Sprintf("Unexpected client 'admin' API version found '%s', expected '%s', please downgrade the client to older releases", version, madmin.AdminAPIVersion)
			}
			writeErrorResponseJSONFiber(c.Context(), c, APIError{
				Code:           "XMinioAdminVersionMismatch",
				Description:    desc,
				HTTPStatusCode: fiber.StatusUpgradeRequired,
			})
		default:
			writeErrorResponseFiber(c.Context(), c, APIError{
				Code: "BadRequest",
				Description: fmt.Sprintf("An error occurred when parsing the HTTP request %s at '%s'",
					c.Method(), c.Path()),
				HTTPStatusCode: fiber.StatusBadRequest,
			}, guessIsBrowserReqFiber(c))
		}
		_ = reqURL
		return nil
	}
}

func errorResponseHandlerFiber(c fiber.Ctx) error {
	writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrInvalidRequest), guessIsBrowserReqFiber(c))
	return nil
}

func extractAPIVersionFiber(c fiber.Ctx) string {
	if matches := regexVersion.FindStringSubmatch(c.Path()); len(matches) > 1 {
		return matches[1]
	}
	return "unknown"
}

func vhostBucketMiddleware(c fiber.Ctx) error {
	host := requestHost(c)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for _, domainName := range globalDomainNames {
		if IsKubernetes() {
			if host == minioReservedBucket+"."+domainName {
				return c.Next()
			}
		}
		if strings.HasSuffix(host, "."+domainName) {
			bucket := strings.TrimSuffix(host, "."+domainName)
			if bucket != "" {
				c.Locals(fiberVhostBucketParam, bucket)
			}
			break
		}
	}
	return c.Next()
}

// vhostObjectDispatch handles virtual-host-style requests, where the bucket was
// taken from the Host header (by vhostBucketMiddleware) and the entire request
// path is therefore the object key. This mirrors the legacy
// router.Host("{bucket}.<domain>").Path("/{object:.+}") routing; without it the
// path-style routes would mistake the first path segment for the bucket name
// and drop the object key entirely. Returns handled=false when the request is
// not virtual-host-style so the caller can fall through to path-style routing.
func vhostObjectDispatch(c fiber.Ctx, objectHandler, bucketHandler fiber.Handler) (handled bool, err error) {
	bucket, ok := c.Locals(fiberVhostBucketParam).(string)
	if !ok || bucket == "" {
		return false, nil
	}
	// c.Path() preserves percent-encoding (UnescapePath is disabled); the object
	// helpers unescape it, matching the legacy UseEncodedPath behavior.
	object := strings.TrimPrefix(c.Path(), "/")
	if object == "" {
		return true, bucketHandler(c)
	}
	c.Locals(fiberObjectParam, object)
	return true, objectHandler(c)
}
