/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
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
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// buildVhostTestHandler wires the same dispatch structure used by
// registerAPIRouterFiber (vhost middleware + vhost dispatch + path-style routes)
// around stub handlers that record the resolved bucket/object, so the routing
// decision can be asserted without a full object layer.
func buildVhostTestHandler(rec *struct {
	kind, bucket, object string
}) http.Handler {
	app := newFiberApp()

	objectHandler := func(c fiber.Ctx) error {
		rec.kind = "object"
		rec.bucket = pathParamBucket(c)
		rec.object = pathParamObject(c)
		return c.SendStatus(fiber.StatusOK)
	}
	bucketHandler := func(c fiber.Ctx) error {
		rec.kind = "bucket"
		rec.bucket = pathParamBucket(c)
		rec.object = pathParamObject(c)
		return c.SendStatus(fiber.StatusOK)
	}
	objectDispatch := func(c fiber.Ctx) error {
		if strings.TrimPrefix(c.Params("*"), "/") == "" {
			return bucketHandler(c)
		}
		return objectHandler(c)
	}

	apiGroup := app.Group("", vhostBucketMiddleware)
	apiGroup.Use(func(c fiber.Ctx) error {
		if handled, err := vhostObjectDispatch(c, objectHandler, bucketHandler); handled {
			return err
		}
		return c.Next()
	})
	apiGroup.All("/:bucket/*", objectDispatch)
	apiGroup.All("/:bucket", bucketHandler)

	return fiberHTTPTestHandler(app)
}

func TestFiberVhostRouting(t *testing.T) {
	saved := globalDomainNames
	globalDomainNames = []string{"s3.example.com"}
	defer func() { globalDomainNames = saved }()

	testCases := []struct {
		name       string
		host       string
		path       string
		wantKind   string
		wantBucket string
		wantObject string
	}{
		{
			name:       "vhost object",
			host:       "mybucket.s3.example.com",
			path:       "/path/to/object.txt",
			wantKind:   "object",
			wantBucket: "mybucket",
			wantObject: "path/to/object.txt",
		},
		{
			name:       "vhost encoded object",
			host:       "mybucket.s3.example.com",
			path:       "/a%2Fb",
			wantKind:   "object",
			wantBucket: "mybucket",
			wantObject: "a/b",
		},
		{
			name:       "vhost bucket root",
			host:       "mybucket.s3.example.com",
			path:       "/",
			wantKind:   "bucket",
			wantBucket: "mybucket",
			wantObject: "",
		},
		{
			name:       "path-style object",
			host:       "s3.example.com",
			path:       "/mybucket/path/to/object.txt",
			wantKind:   "object",
			wantBucket: "mybucket",
			wantObject: "path/to/object.txt",
		},
		{
			name:       "path-style bucket",
			host:       "s3.example.com",
			path:       "/mybucket",
			wantKind:   "bucket",
			wantBucket: "mybucket",
			wantObject: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var rec struct{ kind, bucket, object string }
			handler := buildVhostTestHandler(&rec)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Host = tc.host
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("unexpected status %d", resp.Code)
			}
			if rec.kind != tc.wantKind {
				t.Errorf("kind: got %q want %q", rec.kind, tc.wantKind)
			}
			if rec.bucket != tc.wantBucket {
				t.Errorf("bucket: got %q want %q", rec.bucket, tc.wantBucket)
			}
			if rec.object != tc.wantObject {
				t.Errorf("object: got %q want %q", rec.object, tc.wantObject)
			}
		})
	}
}
