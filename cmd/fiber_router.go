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
	"net/http"
	"reflect"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	xhttp "github.com/soulteary/otterio/cmd/http"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/wildcard"
)

func criticalErrorHandlerFiber(c fiber.Ctx) error {
	defer func() {
		if err := recover(); err == logger.ErrCritical {
			writeErrorResponseFiber(c.Context(), c, errorCodes.ToAPIErr(ErrInternalError), guessIsBrowserReqFiber(c))
		} else if err != nil {
			panic(err)
		}
	}()
	return c.Next()
}

func corsMiddlewareFiber() fiber.Handler {
	commonS3Headers := []string{
		xhttp.Date,
		xhttp.ETag,
		xhttp.ServerInfo,
		xhttp.Connection,
		xhttp.AcceptRanges,
		xhttp.ContentRange,
		xhttp.ContentEncoding,
		xhttp.ContentLength,
		xhttp.ContentType,
		xhttp.ContentDisposition,
		xhttp.LastModified,
		xhttp.ContentLanguage,
		xhttp.CacheControl,
		xhttp.RetryAfter,
		xhttp.AmzBucketRegion,
		xhttp.Expires,
		"X-Amz*",
		"x-amz*",
		"*",
	}

	base := cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			for _, allowedOrigin := range globalAPIConfig.getCorsAllowOrigins() {
				if wildcard.MatchSimple(allowedOrigin, origin) {
					return true
				}
			}
			return false
		},
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPut,
			http.MethodHead,
			http.MethodPost,
			http.MethodDelete,
			http.MethodOptions,
			http.MethodPatch,
		},
		AllowHeaders:     commonS3Headers,
		ExposeHeaders:    commonS3Headers,
		AllowCredentials: true,
	})

	return func(c fiber.Ctx) error {
		// Legacy rs/cors treated OPTIONS+Origin as a CORS request even without
		// Access-Control-Request-Method; Fiber's CORS middleware skips that case.
		if c.Method() == fiber.MethodOptions &&
			c.Get(fiber.HeaderOrigin) != "" &&
			c.Get(fiber.HeaderAccessControlRequestMethod) == "" {
			c.Request().Header.Set(fiber.HeaderAccessControlRequestMethod, http.MethodGet)
		}
		return base(c)
	}
}

// addSecurityHeadersFiber is the native-Fiber equivalent of addSecurityHeaders.
// Converting it avoids a fasthttp<->net/http adaptor round-trip (which also
// spawns a goroutine and materializes the request body via PostBody) for what
// is a pair of static response headers.
func addSecurityHeadersFiber(c fiber.Ctx) error {
	c.Set("X-XSS-Protection", "1; mode=block")
	c.Set("Content-Security-Policy", "block-all-mixed-content")
	return c.Next()
}

// addCustomHeadersFiber is the native-Fiber equivalent of addCustomHeaders. It
// sets x-amz-request-id directly on the response header where downstream
// handlers (via fiberRequestID / seedResponseHeader) read it back.
func addCustomHeadersFiber(c fiber.Ctx) error {
	c.Set(xhttp.AmzRequestID, mustGetRequestID(UTCNow()))
	return c.Next()
}

// globalFiberHandlers mirrors globalHandlers (cmd/routers.go) with fully native
// Fiber middleware. Crucially none of these read the request body, so it stays
// a fasthttp stream until the actual handler consumes it - large uploads and
// inter-node REST transfers are no longer buffered fully in memory (which the
// previous adaptor.HTTPMiddleware chain did via ConvertRequest -> PostBody on
// the very first middleware). Order matches globalHandlers exactly; see
// fiber_middleware.go for the per-handler translations.
var globalFiberHandlers = []fiber.Handler{
	filterReservedMetadataFiber,
	setSSETLSHandlerFiber,
	setAuthHandlerFiber,
	setTimeValidityHandlerFiber,
	setBrowserCacheControlHandlerFiber,
	setReservedBucketHandlerFiber,
	setBrowserRedirectHandlerFiber,
	setCrossDomainPolicyFiber,
	setRequestHeaderSizeLimitHandlerFiber,
	// setRequestSizeLimitHandler is covered by fiber's BodyLimit (newFiberApp).
	setHTTPStatsHandlerFiber,
	setRequestValidityHandlerFiber,
	setBucketForwardingHandlerFiber,
	addSecurityHeadersFiber,
	addCustomHeadersFiber,
	setRedirectHandlerFiber,
}

// s3OnlyFiberHandlers returns the middleware chain used by the S3 listener
// when --console-address is set. It strips setBrowserRedirectHandlerFiber so
// browsers hitting the S3 port no longer get redirected to the web UI - they
// receive standard S3 error responses instead.
func s3OnlyFiberHandlers() []fiber.Handler {
	out := make([]fiber.Handler, 0, len(globalFiberHandlers))
	for _, h := range globalFiberHandlers {
		// Skip handlers that only make sense when the web console is mounted
		// on the same listener.
		if isWebOnlyHandler(h) {
			continue
		}
		out = append(out, h)
	}
	return out
}

// isWebOnlyHandler reports whether a middleware should be omitted from the
// dedicated S3 listener. We compare by function pointer because
// fiber.Handler is a non-comparable closure type for inline funcs but the
// names below are package-level functions, which are comparable.
func isWebOnlyHandler(h fiber.Handler) bool {
	return fmtHandlerPointer(h) == fmtHandlerPointer(setBrowserRedirectHandlerFiber)
}

// fmtHandlerPointer returns a stable identifier for a fiber.Handler so we
// can compare against package-level handler functions without importing
// reflect at the call sites.
func fmtHandlerPointer(h fiber.Handler) uintptr {
	return reflect.ValueOf(h).Pointer()
}

func newFiberApp() *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:    int(requestMaxBodySize),
		UnescapePath: false, // preserve encoded path segments (mux UseEncodedPath equivalent)
		ServerHeader: "OtterIO",
		// Stream request bodies instead of buffering them fully in memory. This
		// keeps large uploads from being held entirely in RAM once the request
		// reaches a native (non-adaptor) handler; the bridge in fiberRequest
		// wires the body to this stream when available.
		StreamRequestBody: true,
	})
	return app
}

// configureServerHandler registers all routers and middleware on a Fiber app.
func configureServerHandler(endpointServerPools EndpointServerPools) (*fiber.App, error) {
	app := newFiberApp()

	app.Use(criticalErrorHandlerFiber)
	app.Use(corsMiddlewareFiber())

	for _, h := range globalFiberHandlers {
		app.Use(h)
	}

	if globalIsDistErasure {
		registerDistErasureRoutersFiber(app, endpointServerPools)
	}

	if globalBrowserEnabled {
		if err := registerWebRouterFiber(app); err != nil {
			return nil, err
		}
	}

	registerAdminRouterFiber(app, true, true)
	registerHealthCheckRouterFiber(app)
	registerMetricsRouterFiber(app)
	registerSTSRouterFiber(app)
	registerAPIRouterFiber(app)

	return app, nil
}

// configureServerHandlers builds two separate Fiber apps: one for S3 traffic
// and one for the web console + admin API. It is used when the operator opts
// into a dedicated console listener via --console-address. consoleApp is nil
// in single-port mode and the caller should fall back to configureServerHandler.
func configureServerHandlers(endpointServerPools EndpointServerPools) (s3App, consoleApp *fiber.App, err error) {
	if globalOtterioConsoleAddr == "" {
		s3App, err = configureServerHandler(endpointServerPools)
		return s3App, nil, err
	}

	s3App = newFiberApp()
	s3App.Use(criticalErrorHandlerFiber)
	s3App.Use(corsMiddlewareFiber())
	for _, h := range s3OnlyFiberHandlers() {
		s3App.Use(h)
	}

	if globalIsDistErasure {
		registerDistErasureRoutersFiber(s3App, endpointServerPools)
	}

	registerHealthCheckRouterFiber(s3App)
	registerMetricsRouterFiber(s3App)
	registerSTSRouterFiber(s3App)
	registerAPIRouterFiber(s3App)

	consoleApp = newFiberApp()
	consoleApp.Use(criticalErrorHandlerFiber)
	consoleApp.Use(corsMiddlewareFiber())
	for _, h := range globalFiberHandlers {
		consoleApp.Use(h)
	}

	registerAdminRouterFiber(consoleApp, true, true)
	registerHealthCheckRouterFiber(consoleApp)

	if globalBrowserEnabled {
		if werr := registerWebRouterFiber(consoleApp); werr != nil {
			return nil, nil, werr
		}
	}

	return s3App, consoleApp, nil
}

// configureGatewayHandler registers gateway-specific routers on a Fiber app.
func configureGatewayHandler(enableConfigOps, enableIAMOps, enableSTS bool) (*fiber.App, error) {
	app := newFiberApp()

	app.Use(criticalErrorHandlerFiber)
	app.Use(corsMiddlewareFiber())

	for _, h := range globalFiberHandlers {
		app.Use(h)
	}

	if enableSTS {
		registerSTSRouterFiber(app)
	}

	registerAdminRouterFiber(app, enableConfigOps, enableIAMOps)
	registerHealthCheckRouterFiber(app)
	registerMetricsRouterFiber(app)

	if globalBrowserEnabled {
		if err := registerWebRouterFiber(app); err != nil {
			return nil, err
		}
	}

	registerAPIRouterFiber(app)
	return app, nil
}

// configureGatewayHandlers builds two Fiber apps for gateway mode mirroring
// configureServerHandlers. consoleApp is nil when the operator did not set
// --console-address.
func configureGatewayHandlers(enableConfigOps, enableIAMOps, enableSTS bool) (s3App, consoleApp *fiber.App, err error) {
	if globalOtterioConsoleAddr == "" {
		s3App, err = configureGatewayHandler(enableConfigOps, enableIAMOps, enableSTS)
		return s3App, nil, err
	}

	s3App = newFiberApp()
	s3App.Use(criticalErrorHandlerFiber)
	s3App.Use(corsMiddlewareFiber())
	for _, h := range s3OnlyFiberHandlers() {
		s3App.Use(h)
	}

	if enableSTS {
		registerSTSRouterFiber(s3App)
	}

	registerHealthCheckRouterFiber(s3App)
	registerMetricsRouterFiber(s3App)
	registerAPIRouterFiber(s3App)

	consoleApp = newFiberApp()
	consoleApp.Use(criticalErrorHandlerFiber)
	consoleApp.Use(corsMiddlewareFiber())
	for _, h := range globalFiberHandlers {
		consoleApp.Use(h)
	}

	registerAdminRouterFiber(consoleApp, enableConfigOps, enableIAMOps)
	registerHealthCheckRouterFiber(consoleApp)

	if globalBrowserEnabled {
		if werr := registerWebRouterFiber(consoleApp); werr != nil {
			return nil, nil, werr
		}
	}

	return s3App, consoleApp, nil
}
