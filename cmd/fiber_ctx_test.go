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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestFiberResponseWriterHeaders(t *testing.T) {
	app := fiber.New()
	app.Get("/test", toOtterioHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("X-Test", "value")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "ok")
	}))

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/test", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("expected Content-Type application/xml, got %q", ct)
	}
	if resp.Header.Get("X-Test") != "value" {
		t.Fatalf("expected X-Test header value")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("expected body ok, got %q", string(body))
	}
}

func TestPathParamObjectFromLocals(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c fiber.Ctx) error {
		setPathVars(c, "bucket", "object.txt")
		if got := pathParamObject(c); got != "object.txt" {
			t.Fatalf("expected object.txt, got %q", got)
		}
		r, err := fiberRequest(c)
		if err != nil {
			t.Fatal(err)
		}
		r = setURLVarsOnRequest(r, allPathParams(c))
		if urlVar(r, "object") != "object.txt" {
			t.Fatalf("expected urlVar object object.txt, got %q", urlVar(r, "object"))
		}
		return c.SendStatus(fiber.StatusOK)
	})
	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/test", nil)); err != nil {
		t.Fatal(err)
	}
}
