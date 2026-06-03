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

	xhttp "github.com/soulteary/otterio/cmd/http"
)

func newHTTPServerFn() *xhttp.Server {
	globalObjLayerMutex.RLock()
	defer globalObjLayerMutex.RUnlock()
	return globalHTTPServer
}

func setHTTPServer(h *xhttp.Server) {
	globalObjLayerMutex.Lock()
	globalHTTPServer = h
	globalObjLayerMutex.Unlock()
}

func newObjectLayerFn() ObjectLayer {
	globalObjLayerMutex.RLock()
	defer globalObjLayerMutex.RUnlock()
	return globalObjectAPI
}

func newCachedObjectLayerFn() CacheObjectLayer {
	globalObjLayerMutex.RLock()
	defer globalObjLayerMutex.RUnlock()
	return globalCacheObjectAPI
}

func setCacheObjectLayer(c CacheObjectLayer) {
	globalObjLayerMutex.Lock()
	globalCacheObjectAPI = c
	globalObjLayerMutex.Unlock()
}

func setObjectLayer(o ObjectLayer) {
	globalObjLayerMutex.Lock()
	globalObjectAPI = o
	globalObjLayerMutex.Unlock()
}

// objectAPIHandler implements and provides http handlers for S3 API.
type objectAPIHandlers struct {
	ObjectAPI func() ObjectLayer
	CacheAPI  func() CacheObjectLayer
}

// getHost tries its best to return the request host.
// According to section 14.23 of RFC 2616 the Host header
// can include the port number if the default value of 80 is not used.
func getHost(r *http.Request) string {
	if r.URL.IsAbs() {
		return r.URL.Host
	}
	return r.Host
}

func notImplementedHandler(w http.ResponseWriter, r *http.Request) {
	writeErrorResponse(r.Context(), w, errorCodes.ToAPIErr(ErrNotImplemented), r.URL, guessIsBrowserReq(r))
}

type rejectedAPI struct {
	api     string
	methods []string
	queries []string
	path    string
}

var rejectedAPIs = []rejectedAPI{
	{
		api:     "inventory",
		methods: []string{http.MethodGet, http.MethodPut, http.MethodDelete},
		queries: []string{"inventory", ""},
	},
	{
		api:     "cors",
		methods: []string{http.MethodPut, http.MethodDelete},
		queries: []string{"cors", ""},
	},
	{
		api:     "metrics",
		methods: []string{http.MethodGet, http.MethodPut, http.MethodDelete},
		queries: []string{"metrics", ""},
	},
	{
		api:     "website",
		methods: []string{http.MethodPut},
		queries: []string{"website", ""},
	},
	{
		api:     "logging",
		methods: []string{http.MethodPut, http.MethodDelete},
		queries: []string{"logging", ""},
	},
	{
		api:     "accelerate",
		methods: []string{http.MethodPut, http.MethodDelete},
		queries: []string{"accelerate", ""},
	},
	{
		api:     "requestPayment",
		methods: []string{http.MethodPut, http.MethodDelete},
		queries: []string{"requestPayment", ""},
	},
	{
		api:     "torrent",
		methods: []string{http.MethodPut, http.MethodDelete, http.MethodGet},
		queries: []string{"torrent", ""},
		path:    "/{object:.+}",
	},
	{
		api:     "acl",
		methods: []string{http.MethodDelete},
		queries: []string{"acl", ""},
		path:    "/{object:.+}",
	},
	{
		api:     "acl",
		methods: []string{http.MethodDelete, http.MethodPut, http.MethodHead},
		queries: []string{"acl", ""},
	},
	{
		api:     "publicAccessBlock",
		methods: []string{http.MethodDelete, http.MethodPut, http.MethodGet},
		queries: []string{"publicAccessBlock", ""},
	},
	{
		api:     "ownershipControls",
		methods: []string{http.MethodDelete, http.MethodPut, http.MethodGet},
		queries: []string{"ownershipControls", ""},
	},
	{
		api:     "intelligent-tiering",
		methods: []string{http.MethodDelete, http.MethodPut, http.MethodGet},
		queries: []string{"intelligent-tiering", ""},
	},
	{
		api:     "analytics",
		methods: []string{http.MethodDelete, http.MethodPut, http.MethodGet},
		queries: []string{"analytics", ""},
	},
}
