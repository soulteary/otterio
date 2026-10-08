// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

type configurationReadStateKey struct{}

type configurationReadState struct {
	transportBounded bool
	complete         bool
}

func configurationRequestLimit(header *fasthttp.RequestHeader) int {
	u, err := url.ParseRequestURI(string(header.RequestURI()))
	if err != nil {
		return 0
	}
	if u.Path == adminPathPrefix+adminAPIVersionPrefix+"/self-credentials" {
		return 4096
	}
	method := string(header.Method())
	if method == http.MethodPost && u.Query().Has("restore") {
		return maxRestoreObjectRequestSize
	}
	if (method != http.MethodPut && method != http.MethodDelete) || len(header.PeekAll(bucketConfigIfMatchHeader)) == 0 {
		return 0
	}
	query := u.Query()
	if query.Has("policy") {
		return int(maxBucketPolicySize)
	}
	if query.Has("lifecycle") || query.Has("versioning") {
		return int(maxBucketVersioningConfigSize)
	}
	return 0
}

// fasthttp pre-reads up to 8 KiB before the Fiber handler runs. Establish the
// small configuration deadline at header receipt, not after that pre-read.
func configurationRequestConfig(header *fasthttp.RequestHeader) fasthttp.RequestConfig {
	if limit := configurationRequestLimit(header); limit != 0 {
		return fasthttp.RequestConfig{ReadTimeout: 15 * time.Second, MaxRequestBodySize: limit}
	}
	return fasthttp.RequestConfig{}
}

func configurationReadDeadlineFiber(c fiber.Ctx) error {
	if configurationRequestLimit(&c.Request().Header) == 0 {
		return c.Next()
	}
	state := &configurationReadState{transportBounded: true, complete: c.Request().Header.ContentLength() == 0}
	c.RequestCtx().SetUserValue(configurationReadStateKey{}, state)
	defer func() {
		// A rejected request may leave a stream unread. Send its response and
		// close the connection instead of interpreting that body as a next
		// request. Never leave a configuration deadline on a reused socket.
		if !state.complete {
			c.Response().Header.SetConnectionClose()
		}
		if conn := c.RequestCtx().Conn(); conn != nil {
			_ = conn.SetReadDeadline(time.Time{})
		}
	}()
	return c.Next()
}
