// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

// Native Fiber request bodies can be streams without a transport read timeout.
// Scope the deadline to small configuration reads; object uploads keep their
// existing behavior. Cancellation interrupts the socket read without spawning
// a goroutine that could continue reading a recycled request body.
func readConfigurationBody(ctx context.Context, r *http.Request, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, _ := r.Context().Value(configurationReadConnKey{}).(net.Conn)
	if err := ctx.Err(); err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, err
	}
	if conn == nil {
		data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return data, err
	}
	deadline, _ := ctx.Deadline()
	state, _ := r.Context().Value(configurationReadStateKey{}).(*configurationReadState)
	if state == nil || !state.transportBounded {
		if err := conn.SetReadDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetReadDeadline(time.Now())
		close(done)
	})
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if !stop() {
		<-done
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		err = context.DeadlineExceeded
	}
	fullyConsumed := r.ContentLength >= 0 && int64(len(data)) == r.ContentLength
	if err != nil {
		code := toAPIErrorCode(ctx, err)
		// Digest failures happen at EOF. Their small error response can still
		// be sent normally because no request bytes remain to be drained.
		fullyConsumed = fullyConsumed && (code == ErrContentSHA256Mismatch || code == ErrBadDigest)
	}
	if state != nil && fullyConsumed {
		state.complete = true
	}
	if !fullyConsumed || int64(len(data)) > limit {
		// A failed/overflowing stream may still contain unread bytes. Close
		// this connection so fasthttp cannot drain it without the deadline.
		_ = conn.Close()
	} else if resetErr := conn.SetReadDeadline(time.Time{}); resetErr != nil {
		_ = conn.Close()
		return nil, resetErr
	}
	return data, err
}
