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
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type ctxBindTestKey struct{}

// TestFiberRequestBindsContext verifies that the *http.Request handed to legacy
// handlers via the Fiber bridge is anchored to the fasthttp request context
// rather than degrading to a detached context.Background(). This keeps the
// audit/ReqInfo chain and server-shutdown cancellation tied to the request. We
// assert it by storing a value on the fasthttp request ctx and checking it is
// visible through r.Context().Value, which is only true when the bind occurred.
func TestFiberRequestBindsContext(t *testing.T) {
	app := newFiberApp()

	var isBackground bool
	var sawRequestValue bool
	app.Post("/ctxcheck", func(c fiber.Ctx) error {
		c.RequestCtx().SetUserValue(ctxBindTestKey{}, "bound")
		return toMinioHandler(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			// The zero-value *http.Request returns the context.Background
			// singleton; after binding it must be the request-scoped context.
			isBackground = ctx == context.Background()
			if v, ok := ctx.Value(ctxBindTestKey{}).(string); ok && v == "bound" {
				sawRequestValue = true
			}
			w.WriteHeader(http.StatusOK)
		})(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/ctxcheck", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if isBackground {
		t.Fatal("r.Context() must be bound to the request context, got context.Background()")
	}
	if !sawRequestValue {
		t.Fatal("r.Context() is not anchored to the fasthttp request context (request-scoped value not visible)")
	}
}

// TestFiberResponseWriterTrailer verifies that a legacy handler can declare an
// HTTP response trailer (as peer NetInfoHandler does with "Trailer: FinalStatus")
// and that the value set after WriteHeader is emitted to the client. Without
// trailer support the FinalStatus set after the body would be silently dropped.
func TestFiberResponseWriterTrailer(t *testing.T) {
	app := newFiberApp()

	app.Post("/netinfo", toMinioHandler(func(w http.ResponseWriter, r *http.Request) {
		// Mirror peer-rest-server.go NetInfoHandler semantics.
		w.Header().Set("Trailer", "FinalStatus")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("hello"))
		w.Header().Set("FinalStatus", "Success")
	}))

	req := httptest.NewRequest(http.MethodPost, "/netinfo", strings.NewReader("ping"))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("expected body %q, got %q", "hello", string(body))
	}
	// Go's HTTP client only populates resp.Trailer after the body is fully read.
	if got := resp.Trailer.Get("FinalStatus"); got != "Success" {
		t.Fatalf("expected FinalStatus trailer %q, got %q (trailer=%v)", "Success", got, resp.Trailer)
	}
}

// TestFiberRequestInputByteCounting verifies that the request body byte counter
// reflects the actual number of bytes read, which setHTTPStatsHandlerFiber uses
// to meter input traffic accurately for chunked / aws-chunked uploads where
// Content-Length is unknown (-1) and a Content-Length-only estimate would
// undercount to zero.
func TestFiberRequestInputByteCounting(t *testing.T) {
	app := newFiberApp()

	const payload = "the quick brown fox jumps over the lazy dog"
	var counted int64
	app.Post("/upload", func(c fiber.Ctx) error {
		h := toMinioHandler(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		})
		err := h(c)
		counted = requestInputBytesRead(c)
		return err
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(payload))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if counted != int64(len(payload)) {
		t.Fatalf("expected %d input bytes counted, got %d", len(payload), counted)
	}
}

// TestPostPolicyContentTypeMatchStrict locks in the CVE-2023-28434 hardening:
// the post-policy route matcher and the post-policy auth predicate must accept
// exactly the "multipart/form-data" media type (optionally with parameters) and
// reject look-alike values such as "multipart/form-data-evil". A mismatch
// between the router and the auth classification is what enabled bypassing the
// reserved/meta bucket protection.
func TestPostPolicyContentTypeMatchStrict(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{"multipart/form-data", true},
		{"multipart/form-data; boundary=abc", true},
		{"multipart/form-data ; boundary=abc", true},
		{"MULTIPART/FORM-DATA; boundary=abc", true},
		{"multipart/form-data-evil", false},
		{"multipart/form-dataa", false},
		{"application/json", false},
		{"", false},
	}
	for _, tc := range cases {
		// Router matcher.
		gotRoute := fiberMultipartFormRegex.MatchString(tc.contentType)
		// Auth predicate.
		r := httptest.NewRequest(http.MethodPost, "/bucket", nil)
		if tc.contentType != "" {
			r.Header.Set("Content-Type", tc.contentType)
		}
		gotAuth := isRequestPostPolicySignatureV4(r)

		if gotRoute != tc.want {
			t.Errorf("router match for %q = %v, want %v", tc.contentType, gotRoute, tc.want)
		}
		if gotAuth != tc.want {
			t.Errorf("auth predicate for %q = %v, want %v", tc.contentType, gotAuth, tc.want)
		}
		// The router and auth predicate must always agree (root cause of the CVE).
		if gotRoute != gotAuth {
			t.Errorf("router/auth disagree for %q: route=%v auth=%v", tc.contentType, gotRoute, gotAuth)
		}
	}
}
