// Copyright (C) 2026 soulteary, https://github.com/soulteary/otterio
// Licensed under the Apache License, Version 2.0.

package cmd

import (
	"io"
	"net/http"
	"sync"
)

// fiberStreamResult is published exactly once, before ready is closed. The
// dispatcher must only read this snapshot, never the handler-owned fields.
type fiberStreamResult struct {
	header   http.Header
	status   int
	panicVal interface{}
}

// fiberStreamResponseWriter runs on the legacy handler goroutine. Its mutable
// fields are confined to that goroutine; result is immutable after publication.
// The pipe keeps response bodies streamed rather than buffered in memory.
type fiberStreamResponseWriter struct {
	header      http.Header
	status      int
	pw          *io.PipeWriter
	ready       chan struct{}
	once        sync.Once
	wroteHeader bool
	panicVal    interface{}
	result      fiberStreamResult
}

func (w *fiberStreamResponseWriter) Header() http.Header { return w.header }

func (w *fiberStreamResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = statusCode
	w.signalReady()
}

func (w *fiberStreamResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.pw.Write(b)
}

// Flush commits headers even when no body has been written yet. Body writes
// flow directly through the pipe; transport buffering remains fasthttp's job.
func (w *fiberStreamResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *fiberStreamResponseWriter) signalReady() {
	w.once.Do(func() {
		w.result = fiberStreamResult{
			header:   w.header.Clone(),
			status:   w.status,
			panicVal: w.panicVal,
		}
		close(w.ready)
	})
}

// run recovers on the child goroutine. A panic before headers is carried in the
// initial snapshot and re-raised by the dispatcher. A panic after headers only
// terminates the stream: it must never mutate the already published result.
func (w *fiberStreamResponseWriter) run(h func(http.ResponseWriter, *http.Request), r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			w.panicVal = rec
			w.signalReady()
			_ = w.pw.CloseWithError(io.ErrClosedPipe)
			return
		}
		w.signalReady()
		_ = w.pw.Close()
	}()
	h(w, r)
}
