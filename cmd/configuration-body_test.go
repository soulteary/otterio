// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

type configurationDeadlineFailureConn struct {
	net.Conn
	closed bool
}

func (c *configurationDeadlineFailureConn) SetReadDeadline(time.Time) error {
	return errors.New("fixture cannot set deadline")
}

func (c *configurationDeadlineFailureConn) Close() error {
	c.closed = true
	return c.Conn.Close()
}

func TestBucketConfigBodyReadEarlyFailure(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		server, client := net.Pipe()
		conn := &configurationDeadlineFailureConn{Conn: server}
		ctx, cancel := context.WithCancel(context.Background())
		if canceled {
			cancel()
		}
		r := httptest.NewRequest("PUT", "/configuration", nil)
		r = r.WithContext(context.WithValue(ctx, configurationReadConnKey{}, conn))
		r.Body = io.NopCloser(server)
		_, err := readConfigurationBody(ctx, r, 4096)
		cancel()
		_ = client.Close()
		_ = server.Close()
		if err == nil || !conn.closed {
			t.Fatalf("early body failure left a connection open for draining: canceled=%v error=%v", canceled, err)
		}
	}
}

func TestBucketConfigBodyReadDeadline(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		server, client := net.Pipe()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		r := httptest.NewRequest("PUT", "/configuration", nil)
		r = r.WithContext(context.WithValue(ctx, configurationReadConnKey{}, server))
		r.Body = io.NopCloser(server)
		finished := make(chan error, 1)
		go func() {
			_, err := readConfigurationBody(ctx, r, 4096)
			finished <- err
		}()
		// A caller supplies only the beginning of a body, then never finishes.
		if _, err := client.Write([]byte("partial")); err != nil {
			t.Fatal(err)
		}
		if cancelEarly {
			cancel()
		}
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("stalled read did not return cancellation: %v", err)
			}
		case <-time.After(time.Second):
			_ = client.Close()
			_ = server.Close()
			t.Fatal("stalled configuration body was not interrupted")
		}
		cancel()
		_ = client.Close()
		_ = server.Close()
	}
	// A completed read clears its deadline and cancellation hook before a
	// keep-alive connection can be reused by another request.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	r := httptest.NewRequest("PUT", "/configuration", nil)
	r = r.WithContext(context.WithValue(ctx, configurationReadConnKey{}, server))
	r.Body = io.NopCloser(bytes.NewReader([]byte("complete")))
	r.ContentLength = 8
	data, err := readConfigurationBody(ctx, r, 4096)
	cancel()
	if err != nil || string(data) != "complete" {
		t.Fatalf("complete body rejected: %s %v", data, err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := io.CopyN(io.Discard, server, 4)
		done <- err
	}()
	if _, err := client.Write([]byte("next")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("canceled prior body deadline affected reuse: %v", err)
	}
}
