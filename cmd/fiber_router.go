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

func newFiberApp() *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit:    int(requestMaxBodySize),
		UnescapePath: false, // preserve encoded path segments (mux UseEncodedPath equivalent)
		ServerHeader: "MinIO",
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
