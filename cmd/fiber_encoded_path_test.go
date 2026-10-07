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

	"github.com/gofiber/fiber/v3"
)

func TestEncodedObjectPathPreserved(t *testing.T) {
	savedDomains := globalDomainNames
	globalDomainNames = []string{"s3.example.com"}
	defer func() { globalDomainNames = savedDomains }()
	for _, streamed := range []bool{false, true} {
		app := newFiberApp()
		var gotObject string
		capture := func(w http.ResponseWriter, r *http.Request) {
			var err error
			// Match all legacy object handlers, which decode the captured path.
			gotObject, err = unescapePath(urlVar(r, "object"))
			if err != nil {
				t.Errorf("object capture could not be decoded: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}
		methods := []string{http.MethodGet, http.MethodPut, http.MethodHead, http.MethodPost, http.MethodDelete}
		rule := s3Route(methods, "objecttest", true, capture, nil, nil)
		if streamed {
			rule = s3RouteStream(methods, "objecttest", true, capture, nil, nil)
		}
		objectDispatch, bucketDispatch := makeBucketObjectDispatchHandlers([]routeRule{rule}, nil)
		objectHandler := makeS3DispatchHandler([]routeRule{rule})
		group := app.Group("", vhostBucketMiddleware)
		group.Use(func(c fiber.Ctx) error {
			if handled, err := vhostObjectDispatch(c, objectHandler, bucketDispatch); handled {
				return err
			}
			return c.Next()
		})
		group.All("/:bucket/*", objectDispatch)
		group.All("/:bucket", bucketDispatch)
		handler := fiberHTTPTestHandler(app)
		cases := []struct{ encoded, object string }{
			{"a%2Fb", "a/b"},
			{"a%252Fb", "a%2Fb"},
			{"a%2541b", "a%41b"},
			{"a%2525b", "a%25b"},
			{"a%25b", "a%b"},
			{"a+b", "a+b"},
			{"a%2Bb", "a+b"},
		}
		for _, method := range methods {
			for _, tc := range cases {
				for _, vhost := range []bool{false, true} {
					path, host := "/mybucket/"+tc.encoded, "s3.example.com"
					if vhost {
						path, host = "/"+tc.encoded, "mybucket.s3.example.com"
					}
					req := httptest.NewRequest(method, path, nil)
					req.Host = host
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					if rec.Code != http.StatusOK || gotObject != tc.object {
						t.Fatalf("stream=%v vhost=%v %s %s: status %d object %q, want %q", streamed, vhost, method, path, rec.Code, gotObject, tc.object)
					}
				}
			}
		}
	}
}
