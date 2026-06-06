/*
 * OtterIO Cloud Storage, (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNoImportIAMHandlerRegistered pins down the absence of upstream MinIO's
// ImportIAM / ImportIAMV2 family of admin endpoints. Upstream
// GHSA-cwq8-g58r-32hg (minio/minio#20756) traced a privilege-escalation to a
// race in the import handler that wrote IAM mappings before the admin auth
// check completed; OtterIO has never carried that handler. We assert the
// "never" property here so that any future cherry-pick from upstream that
// inadvertently re-introduces an import-iam route trips this test instead of
// silently widening the attack surface.
//
// We register the production admin router with both the IAM and config
// subsystems enabled (i.e. the maximum exposure surface) and walk every
// resulting route. Any path containing the forbidden substrings is rejected.
func TestNoImportIAMHandlerRegistered(t *testing.T) {
	t.Parallel()

	app := newFiberApp()
	registerAdminRouterFiber(app, true, true)

	forbidden := []string{"import-iam", "import-bucket-metadata", "/import"}

	for _, route := range app.GetRoutes(true) {
		lower := strings.ToLower(route.Path)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Fatalf("forbidden admin route registered: %s %s (GHSA-cwq8-g58r-32hg / minio/minio#20756 - the import handler family is not back-ported and must not appear in the admin router)", route.Method, route.Path)
			}
		}
	}
}

// TestImportIAMEndpointReturnsNon2xx complements the route-table assertion
// above by going through the actual HTTP path. Even if a future contributor
// bypasses registerAdminRoute and sticks `app.Post("/otterio/admin/v3/import-iam", ...)`
// directly on the Fiber app, this test catches it: the request must reach the
// catch-all not-found / method-not-allowed branch and not a 2xx success.
func TestImportIAMEndpointReturnsNon2xx(t *testing.T) {
	t.Parallel()

	app := newFiberApp()
	registerAdminRouterFiber(app, true, true)
	h := fiberHTTPTestHandler(app)

	for _, target := range []string{
		"/otterio/admin/v3/import-iam",
		"/otterio/admin/v3/import-iam-v2",
		"/otterio/admin/v3/import-bucket-metadata",
	} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code >= 200 && rec.Code < 300 {
			t.Fatalf("forbidden admin path %s returned 2xx (%d); the import handler family must not be reachable (GHSA-cwq8-g58r-32hg)", target, rec.Code)
		}
	}
}
