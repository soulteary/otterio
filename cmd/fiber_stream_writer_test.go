// Copyright (C) 2026 soulteary, https://github.com/soulteary/otterio
// Licensed under the Apache License, Version 2.0.

package cmd

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newStreamWriterTest(t *testing.T) (*fiberStreamResponseWriter, *io.PipeReader) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	return &fiberStreamResponseWriter{
		header: make(http.Header), status: http.StatusOK,
		pw: pw, ready: make(chan struct{}),
	}, pr
}

func awaitStreamResult(t *testing.T, w *fiberStreamResponseWriter) fiberStreamResult {
	t.Helper()
	select {
	case <-w.ready:
		return w.result
	case <-time.After(5 * time.Second):
		t.Fatal("response state was not published")
		return fiberStreamResult{}
	}
}

func TestStreamWriterPanicBeforeHeaders(t *testing.T) {
	w, pr := newStreamWriterTest(t)
	go w.run(func(http.ResponseWriter, *http.Request) { panic("before headers") }, httptest.NewRequest(http.MethodGet, "/", nil))
	result := awaitStreamResult(t, w)
	if result.panicVal != "before headers" {
		t.Fatalf("missing pre-header panic: %#v", result.panicVal)
	}
	if _, err := io.ReadAll(pr); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("expected failed stream, got %v", err)
	}
}

func TestStreamWriterPanicAfterHeaders(t *testing.T) {
	// Repetition exercises publication and recovery concurrently under -race.
	for i := 0; i < 100; i++ {
		w, pr := newStreamWriterTest(t)
		go w.run(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header()["ETag"] = []string{`"original"`}
			rw.WriteHeader(http.StatusPartialContent)
			rw.Header()["ETag"][0] = `"changed"`
			rw.Header().Set("X-Late", "ignored")
			panic("after headers")
		}, httptest.NewRequest(http.MethodGet, "/", nil))
		result := awaitStreamResult(t, w)
		if result.panicVal != nil || result.status != http.StatusPartialContent {
			t.Fatalf("committed state changed: %#v", result)
		}
		if result.header["ETag"][0] != `"original"` || result.header.Get("X-Late") != "" {
			t.Fatalf("headers were not snapshotted: %v", result.header)
		}
		if _, err := io.ReadAll(pr); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("expected failed stream, got %v", err)
		}
	}
}

func TestStreamWriterPanicAfterPartialBody(t *testing.T) {
	w, pr := newStreamWriterTest(t)
	go w.run(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(rw, "partial")
		panic("after body")
	}, httptest.NewRequest(http.MethodGet, "/", nil))
	result := awaitStreamResult(t, w)
	body, err := io.ReadAll(pr)
	if result.panicVal != nil || result.status != http.StatusOK || string(body) != "partial" || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("unexpected partial response: result=%#v body=%q err=%v", result, body, err)
	}
}

func TestStreamWriterEmptyResponse(t *testing.T) {
	w, pr := newStreamWriterTest(t)
	go w.run(func(http.ResponseWriter, *http.Request) {}, httptest.NewRequest(http.MethodGet, "/", nil))
	result := awaitStreamResult(t, w)
	body, err := io.ReadAll(pr)
	if result.status != http.StatusOK || result.panicVal != nil || len(body) != 0 || err != nil {
		t.Fatalf("unexpected empty response: %#v, %q, %v", result, body, err)
	}
}

func TestStreamWriterFirstHeaderAndFlush(t *testing.T) {
	w, pr := newStreamWriterTest(t)
	go w.run(func(rw http.ResponseWriter, _ *http.Request) {
		rw.(http.Flusher).Flush()
		rw.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(rw, "ok")
	}, httptest.NewRequest(http.MethodGet, "/", nil))
	result := awaitStreamResult(t, w)
	body, err := io.ReadAll(pr)
	if result.status != http.StatusOK || string(body) != "ok" || err != nil {
		t.Fatalf("flush did not commit the initial status: %#v, %q, %v", result, body, err)
	}
}

func TestStreamWriterConsumerCloseUnblocksHandler(t *testing.T) {
	w, pr := newStreamWriterTest(t)
	writeErr := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(func(rw http.ResponseWriter, _ *http.Request) {
			_, err := io.WriteString(rw, strings.Repeat("x", 4096))
			writeErr <- err
		}, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	_ = awaitStreamResult(t, w)
	_ = pr.Close()
	select {
	case <-done:
		if err := <-writeErr; !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("expected disconnect error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler remained blocked after consumer close")
	}
}
