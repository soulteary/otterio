/*
 * MinIO Cloud Storage, (C) 2015, 2016 MinIO, Inc.
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

import "net/http"

// List of some generic handlers which are applied for all incoming requests.
// These are adapted to Fiber via adaptor.HTTPMiddleware in globalFiberHandlers.
//
//nolint:unused
var globalHandlers = []func(http.Handler) http.Handler{
	filterReservedMetadata,
	setSSETLSHandler,
	setAuthHandler,
	setTimeValidityHandler,
	setBrowserCacheControlHandler,
	setReservedBucketHandler,
	setBrowserRedirectHandler,
	setCrossDomainPolicy,
	setRequestHeaderSizeLimitHandler,
	setRequestSizeLimitHandler,
	setHTTPStatsHandler,
	setRequestValidityHandler,
	setBucketForwardingHandler,
	addSecurityHeaders,
	addCustomHeaders,
	setRedirectHandler,
}
