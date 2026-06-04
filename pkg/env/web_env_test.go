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
 *
 */

package env

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func GetenvHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/webhook/v1/getenv/"), "/")
	if len(parts) != 2 {
		http.Error(w, "invalid path", http.StatusNotFound)
		return
	}
	if parts[0] != "default" {
		http.Error(w, "namespace not found", http.StatusNotFound)
		return
	}
	if parts[1] != "otterio" {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}
	if r.URL.Query().Get("key") != "OTTERIO_ARGS" {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	w.Write([]byte("http://127.0.0.{1..4}:9000/data{1...4}"))
	w.(http.Flusher).Flush()
}

func startTestServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/v1/getenv/", GetenvHandler)

	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.Close()
	})

	return ts
}

func TestWebEnv(t *testing.T) {
	ts := startTestServer(t)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	v, user, pwd, err := getEnvValueFromHTTP(
		fmt.Sprintf("env://otterio:otterio123@%s/webhook/v1/getenv/default/otterio",
			u.Host),
		"OTTERIO_ARGS")
	if err != nil {
		t.Fatal(err)
	}

	if v != "http://127.0.0.{1..4}:9000/data{1...4}" {
		t.Fatalf("Unexpected value %s", v)
	}

	if user != "otterio" {
		t.Fatalf("Unexpected value %s", v)
	}

	if pwd != "otterio123" {
		t.Fatalf("Unexpected value %s", v)
	}
}
