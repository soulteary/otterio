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
	"regexp"
	"testing"

	xhttp "github.com/soulteary/otterio/cmd/http"
)

// TestFiberAmzCopySourceRegex pins the X-Amz-Copy-Source header pattern used
// by the CopyObject/CopyObjectPart routes. A regression here causes mc mv and
// any other CopyObject client to silently fall through to PutObjectHandler,
// which then rejects the request with ErrInvalidCopySource ("Copy Source must
// mention the source bucket and key: sourcebucket/sourcekey.").
func TestFiberAmzCopySourceRegex(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		// mc / minio-go send the header as plain "bucket/key".
		{"plain slash", "mc-test-bucket-455/dir-2338-18488/object-1", true},
		{"leading slash", "/bucket/key", true},
		// AWS SDKs may URL-encode the slash.
		{"percent encoded", "bucket%2Fkey", true},
		// No separator at all must not match: the route should miss and the
		// request should surface the real "no bucket/key" error from the
		// handler's path2BucketObject branch instead of being routed as
		// CopyObject by accident.
		{"no separator", "bucket", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fiberAmzCopySourceRegex.MatchString(tc.value)
			if got != tc.want {
				t.Fatalf("MatchString(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestCopyObjectRouteDispatch is a regression test for the mc mv failure
// observed in CI ("Copy Source must mention the source bucket and key:
// sourcebucket/sourcekey."). It wires a minimal dispatcher with the same two
// PUT object rules used in production - copyobject (gated by
// X-Amz-Copy-Source) and putobject (the catch-all) - and asserts that a PUT
// carrying a plain "bucket/key" copy-source header lands on CopyObjectHandler
// rather than falling through to PutObjectHandler.
func TestCopyObjectRouteDispatch(t *testing.T) {
	app := newFiberApp()

	var hit string
	objectRules := []routeRule{
		s3Route([]string{http.MethodPut}, "copyobject", false,
			func(w http.ResponseWriter, _ *http.Request) {
				hit = "copyobject"
				w.WriteHeader(http.StatusOK)
			}, nil,
			map[string]*regexp.Regexp{xhttp.AmzCopySource: fiberAmzCopySourceRegex}),
		s3Route([]string{http.MethodPut}, "putobject", true,
			func(w http.ResponseWriter, _ *http.Request) {
				hit = "putobject"
				w.WriteHeader(http.StatusOK)
			}, nil, nil),
	}

	objectDispatch, bucketDispatch := makeBucketObjectDispatchHandlers(objectRules, nil)
	group := app.Group("")
	group.All("/:bucket/*", objectDispatch)
	group.All("/:bucket", bucketDispatch)

	req := httptest.NewRequest(http.MethodPut, "/dstbucket/dir/object-2", nil)
	// Mirrors what mc / minio-go sends for `mc mv src/obj dst/obj`.
	req.Header.Set(xhttp.AmzCopySource, "srcbucket/dir/object-1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if hit != "copyobject" {
		t.Fatalf("expected copyobject route to handle PUT with X-Amz-Copy-Source, got %q", hit)
	}
}
