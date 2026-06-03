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
	"net/http/httptest"
	"testing"
)

func TestEncodedObjectPathPreserved(t *testing.T) {
	app := newFiberApp()

	var gotObject string
	objectRules := []routeRule{
		s3Route([]string{http.MethodGet}, "getobject", true,
			func(w http.ResponseWriter, r *http.Request) {
				gotObject = urlVar(r, "object")
				w.WriteHeader(http.StatusOK)
			}, nil, nil),
	}
	objectDispatch, bucketDispatch := makeBucketObjectDispatchHandlers(objectRules, nil)

	group := app.Group("")
	group.All("/:bucket/*", objectDispatch)
	group.All("/:bucket", bucketDispatch)

	handler := fiberHTTPTestHandler(app)
	req := httptest.NewRequest(http.MethodGet, "/mybucket/a%2Fb", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	// urlVar uses likelyUnescapeGeneric -> decoded object key for handlers
	if gotObject != "a/b" {
		t.Fatalf("expected decoded object key a/b, got %q", gotObject)
	}
}
