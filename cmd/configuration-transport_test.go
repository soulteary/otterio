// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

func configurationTestReadTimeout(t *testing.T, app *fiber.App) {
	t.Helper()
	installed := app.Server().HeaderReceived
	if installed == nil {
		t.Fatal("configuration read deadline was not installed on the production app")
	}
	for _, tc := range []struct {
		uri   string
		limit int
	}{
		{adminPathPrefix + adminAPIVersionPrefix + "/self-credentials", 4096},
		{"/bucket?policy", int(maxBucketPolicySize)},
		{"/bucket?lifecycle", int(maxBucketVersioningConfigSize)},
		{"/bucket?versioning", int(maxBucketVersioningConfigSize)},
	} {
		var header fasthttp.RequestHeader
		header.SetMethod(http.MethodPut)
		header.SetRequestURI(tc.uri)
		header.Set(bucketConfigIfMatchHeader, strings.Repeat("0", 64))
		cfg := installed(&header)
		if cfg.ReadTimeout != 15*time.Second || cfg.MaxRequestBodySize != tc.limit {
			t.Fatalf("production configuration read limits for %s: %+v", tc.uri, cfg)
		}
	}
	var restoreHeader fasthttp.RequestHeader
	restoreHeader.SetMethod(http.MethodPost)
	restoreHeader.SetRequestURI("/bucket/object?restore&versionId=owned-version")
	if cfg := installed(&restoreHeader); cfg.ReadTimeout != 15*time.Second || cfg.MaxRequestBodySize != maxRestoreObjectRequestSize {
		t.Fatalf("production restore body limits missing: %+v", cfg)
	}
	app.Server().HeaderReceived = func(header *fasthttp.RequestHeader) fasthttp.RequestConfig {
		cfg := installed(header)
		if cfg.ReadTimeout != 0 {
			cfg.ReadTimeout = 50 * time.Millisecond
		}
		return cfg
	}
}

func TestBucketConfigTransportPreReadDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, uri, extra string
		length           int
	}{
		{"self-small", adminPathPrefix + adminAPIVersionPrefix + "/self-credentials", "", 1024},
		{"policy-large", "/bucket?policy", bucketConfigIfMatchHeader + ": " + strings.Repeat("0", 64) + "\r\n", 9000},
		{"lifecycle-small", "/bucket?lifecycle", bucketConfigIfMatchHeader + ": " + strings.Repeat("0", 64) + "\r\n", 1024},
		{"versioning-small", "/bucket?versioning", bucketConfigIfMatchHeader + ": " + strings.Repeat("0", 64) + "\r\n", 1024},
		{"restore-small", "/bucket/object?restore", "", 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newFiberApp()
			configurationTestReadTimeout(t, app)
			var called atomic.Bool
			method := http.MethodPut
			if strings.Contains(tc.uri, "?restore") {
				method = http.MethodPost
				app.Post(strings.Split(tc.uri, "?")[0], func(c fiber.Ctx) error { called.Store(true); return c.SendStatus(http.StatusNoContent) })
			} else {
				app.Put(strings.Split(tc.uri, "?")[0], func(c fiber.Ctx) error { called.Store(true); return c.SendStatus(http.StatusNoContent) })
			}
			_ = app.Handler()
			server, client := net.Pipe()
			defer client.Close()
			done := make(chan error, 1)
			go func() { done <- app.Server().ServeConn(server) }()
			_ = client.SetDeadline(time.Now().Add(time.Second))
			request := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\n%s\r\npartial", method, tc.uri, tc.length, tc.extra)
			if _, err := io.WriteString(client, request); err != nil {
				t.Fatal(err)
			}
			// Consume any timeout response so a net.Pipe write cannot obscure
			// the server's completion. The handler must never be entered.
			_, _ = io.ReadAll(client)
			select {
			case <-done:
			case <-time.After(time.Second):
				_ = server.Close()
				t.Fatal("configuration stalled before the handler without a bounded read")
			}
			if called.Load() {
				t.Fatal("incomplete body entered the handler")
			}
		})
	}
}

func TestBucketConfigTransportScopeAndReuse(t *testing.T) {
	var header fasthttp.RequestHeader
	header.SetMethod(http.MethodPut)
	header.SetRequestURI("/bucket/object")
	header.Set(bucketConfigIfMatchHeader, strings.Repeat("0", 64))
	if cfg := configurationRequestConfig(&header); cfg.ReadTimeout != 0 || cfg.MaxRequestBodySize != 0 {
		t.Fatalf("ordinary object upload was limited: %+v", cfg)
	}
	header.SetRequestURI("/bucket?lifecycle")
	header.Del(bucketConfigIfMatchHeader)
	if cfg := configurationRequestConfig(&header); cfg.ReadTimeout != 0 || cfg.MaxRequestBodySize != 0 {
		t.Fatalf("legacy upload was limited: %+v", cfg)
	}
	app := newFiberApp()
	configurationTestReadTimeout(t, app)
	app.Put("/bucket", toOtterioHandler(func(w http.ResponseWriter, r *http.Request) {
		if _, err := readConfigurationBody(context.Background(), r, 4096); err != nil {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	app.Put("/bucket/object", func(c fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })
	_ = app.Handler()
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() { done <- app.Server().ServeConn(server) }()
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(client)
	if _, err := io.WriteString(client, "PUT /bucket?policy HTTP/1.1\r\nHost: localhost\r\nContent-Length: 8\r\n"+bucketConfigIfMatchHeader+": "+strings.Repeat("0", 64)+"\r\n\r\ncomplete"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.Close {
		t.Fatalf("completed configuration did not permit reuse: %+v", response)
	}
	// Let the first request's read deadline expire before sending an object
	// upload on the same socket. Its semantics must remain unchanged.
	time.Sleep(75 * time.Millisecond)
	if _, err = io.WriteString(client, "PUT /bucket/object HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\nConnection: close\r\n\r\nnext"); err != nil {
		t.Fatal(err)
	}
	response, err = http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("prior configuration deadline affected reuse: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("upload after configuration: %d", response.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		_ = server.Close()
		t.Fatal("connection did not finish")
	}
}

func TestBucketConfigTransportEarlyReject(t *testing.T) {
	app := newFiberApp()
	configurationTestReadTimeout(t, app)
	app.Put("/bucket", func(c fiber.Ctx) error { return c.SendStatus(http.StatusForbidden) })
	_ = app.Handler()
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() { done <- app.Server().ServeConn(server) }()
	_ = client.SetDeadline(time.Now().Add(time.Second))
	request := "PUT /bucket?policy HTTP/1.1\r\nHost: localhost\r\nContent-Length: 9000\r\n" + bucketConfigIfMatchHeader + ": " + strings.Repeat("0", 64) + "\r\n\r\n" + strings.Repeat("x", 8192)
	if _, err := io.WriteString(client, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden || !response.Close {
		t.Fatalf("unread rejected body kept a socket alive: %+v", response)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		_ = server.Close()
		t.Fatal("early rejection attempted an unbounded body drain")
	}
}
