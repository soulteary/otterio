/*
 * MinIO Cloud Storage, (C) 2016-2020 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/minio
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
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gofiber/fiber/v3/middleware/adaptor"
	xhttp "github.com/minio/minio/cmd/http"
)

func TestBucketTrailingSlashDispatch(t *testing.T) {
	app := newFiberApp()

	var gotBucket string
	bucketRules := []routeRule{
		s3Route([]string{http.MethodPost}, "postpolicybucket", true,
			func(w http.ResponseWriter, r *http.Request) {
				gotBucket = urlVar(r, "bucket")
				w.WriteHeader(http.StatusNoContent)
			}, nil, map[string]*regexp.Regexp{xhttp.ContentType: fiberMultipartFormRegex}),
	}
	objectDispatch, bucketDispatch := makeBucketObjectDispatchHandlers(nil, bucketRules)

	group := app.Group("")
	group.All("/:bucket/*", objectDispatch)
	group.All("/:bucket", bucketDispatch)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("key", "obj")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/testbucket/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("status=%d bucket=%q body=%q", resp.StatusCode, gotBucket, body)

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%q", resp.StatusCode, body)
	}
	if gotBucket != "testbucket" {
		t.Fatalf("expected bucket testbucket, got %q", gotBucket)
	}
}

func TestBucketTrailingSlashDispatchViaHTTPAdaptor(t *testing.T) {
	app := newFiberApp()

	var gotBucket string
	bucketRules := []routeRule{
		s3Route([]string{http.MethodPost}, "postpolicybucket", true,
			func(w http.ResponseWriter, r *http.Request) {
				gotBucket = urlVar(r, "bucket")
				if r.ContentLength < 0 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if _, err := r.MultipartReader(); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, err.Error())
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}, nil, map[string]*regexp.Regexp{xhttp.ContentType: fiberMultipartFormRegex}),
	}
	objectDispatch, bucketDispatch := makeBucketObjectDispatchHandlers(nil, bucketRules)

	group := app.Group("")
	group.All("/:bucket/*", objectDispatch)
	group.All("/:bucket", bucketDispatch)

	httpHandler := adaptor.FiberApp(app)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("key", "obj")
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/testbucket/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.ContentLength = int64(buf.Len())

	rec := httptest.NewRecorder()
	httpHandler.ServeHTTP(rec, req)

	t.Logf("status=%d bucket=%q body=%q", rec.Code, gotBucket, rec.Body.String())

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%q", rec.Code, rec.Body.String())
	}
	if gotBucket != "testbucket" {
		t.Fatalf("expected bucket testbucket, got %q", gotBucket)
	}
}
