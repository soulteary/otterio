/*
 * MinIO Cloud Storage, (C) 2017 MinIO, Inc.
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
	"bytes"
	"io"
	"net"
	"net/http"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/otterio/cmd/logger"
	"github.com/soulteary/otterio/pkg/handlers"
	jsonrpc "github.com/soulteary/otterio/pkg/rpc"
	trace "github.com/soulteary/otterio/pkg/trace"
)

// recordRequest - records the first recLen bytes
// of a given io.Reader
type recordRequest struct {
	// Data source to record
	io.Reader
	// Response body should be logged
	logBody bool
	// Internal recording buffer
	buf bytes.Buffer
	// request headers
	headers http.Header
	// total bytes read including header size
	bytesRead int
}

func (r *recordRequest) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.bytesRead += n

	if r.logBody {
		r.buf.Write(p[:n])
	}
	if err != nil {
		return n, err
	}
	return n, err
}
func (r *recordRequest) Size() int {
	sz := r.bytesRead
	for k, v := range r.headers {
		sz += len(k) + len(v)
	}
	return sz
}

// Return the bytes that were recorded.
func (r *recordRequest) Data() []byte {
	// If body logging is enabled then we return the actual body
	if r.logBody {
		return r.buf.Bytes()
	}
	// ... otherwise we return <BODY> placeholder
	return logger.BodyPlaceHolder
}

var ldapPwdRegex = regexp.MustCompile("(^.*?)LDAPPassword=([^&]*?)(&(.*?))?$")

// redact LDAP password if part of string
func redactLDAPPwd(s string) string {
	parts := ldapPwdRegex.FindStringSubmatch(s)
	if len(parts) > 0 {
		return parts[1] + "LDAPPassword=*REDACTED*" + parts[3]
	}
	return s
}

// getOpName sanitizes the operation name for mc
func getOpName(name string) (op string) {
	op = strings.TrimPrefix(name, "github.com/soulteary/otterio/cmd.")
	op = strings.TrimSuffix(op, "Handler-fm")
	op = strings.Replace(op, "objectAPIHandlers", "s3", 1)
	op = strings.Replace(op, "adminAPIHandlers", "admin", 1)
	op = strings.Replace(op, "(*webAPIHandlers)", "web", 1)
	op = strings.Replace(op, "(*storageRESTServer)", "internal", 1)
	op = strings.Replace(op, "(*peerRESTServer)", "internal", 1)
	op = strings.Replace(op, "(*lockRESTServer)", "internal", 1)
	op = strings.Replace(op, "(*stsAPIHandlers)", "sts", 1)
	op = strings.Replace(op, "LivenessCheckHandler", "healthcheck", 1)
	op = strings.Replace(op, "ReadinessCheckHandler", "healthcheck", 1)
	op = strings.Replace(op, "-fm", "", 1)
	return op
}

// WebTrace gets trace of web request
func WebTrace(ri *jsonrpc.RequestInfo) trace.Info {
	r := ri.Request
	w := ri.ResponseWriter

	name := ri.Method
	// Setup a http request body recorder
	reqHeaders := r.Header.Clone()
	reqHeaders.Set("Host", r.Host)
	if len(r.TransferEncoding) == 0 {
		reqHeaders.Set("Content-Length", strconv.Itoa(int(r.ContentLength)))
	} else {
		reqHeaders.Set("Transfer-Encoding", strings.Join(r.TransferEncoding, ","))
	}

	now := time.Now().UTC()
	t := trace.Info{TraceType: trace.HTTP, FuncName: name, Time: now}
	t.NodeName = r.Host
	if globalIsDistErasure {
		t.NodeName = globalLocalNodeName
	}
	if t.NodeName == "" {
		t.NodeName = globalLocalNodeName
	}

	// strip only standard port from the host address
	if host, port, err := net.SplitHostPort(t.NodeName); err == nil {
		if port == "443" || port == "80" {
			t.NodeName = host
		}
	}

	rq := trace.RequestInfo{
		Time:     now,
		Proto:    r.Proto,
		Method:   r.Method,
		Path:     SlashSeparator + pathJoin(urlVar(r, "bucket"), urlVar(r, "object")),
		RawQuery: redactLDAPPwd(r.URL.RawQuery),
		Client:   handlers.GetSourceIP(r),
		Headers:  reqHeaders,
	}

	rw, ok := w.(*logger.ResponseWriter)
	if ok {
		rs := trace.ResponseInfo{
			Time:       time.Now().UTC(),
			Headers:    rw.Header().Clone(),
			StatusCode: rw.StatusCode,
			Body:       logger.BodyPlaceHolder,
		}

		if rs.StatusCode == 0 {
			rs.StatusCode = http.StatusOK
		}

		t.RespInfo = rs
		t.CallStats = trace.CallStats{
			Latency:         rs.Time.Sub(rw.StartTime),
			InputBytes:      int(r.ContentLength),
			OutputBytes:     rw.Size(),
			TimeToFirstByte: rw.TimeToFirstByte,
		}
	}

	t.ReqInfo = rq
	return t
}

// Trace gets trace of http request
func Trace(f http.HandlerFunc, logBody bool, w http.ResponseWriter, r *http.Request) trace.Info {
	name := getOpName(runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name())

	// Setup a http request body recorder
	reqHeaders := r.Header.Clone()
	reqHeaders.Set("Host", r.Host)
	if len(r.TransferEncoding) == 0 {
		reqHeaders.Set("Content-Length", strconv.Itoa(int(r.ContentLength)))
	} else {
		reqHeaders.Set("Transfer-Encoding", strings.Join(r.TransferEncoding, ","))
	}

	reqBodyRecorder := &recordRequest{Reader: r.Body, logBody: logBody, headers: reqHeaders}
	r.Body = io.NopCloser(reqBodyRecorder)

	now := time.Now().UTC()
	t := trace.Info{TraceType: trace.HTTP, FuncName: name, Time: now}

	t.NodeName = r.Host
	if globalIsDistErasure {
		t.NodeName = globalLocalNodeName
	}

	if t.NodeName == "" {
		t.NodeName = globalLocalNodeName
	}

	// strip only standard port from the host address
	if host, port, err := net.SplitHostPort(t.NodeName); err == nil {
		if port == "443" || port == "80" {
			t.NodeName = host
		}
	}

	rq := trace.RequestInfo{
		Time:     now,
		Proto:    r.Proto,
		Method:   r.Method,
		Path:     r.URL.Path,
		RawQuery: redactLDAPPwd(r.URL.RawQuery),
		Client:   handlers.GetSourceIP(r),
		Headers:  reqHeaders,
	}

	rw := logger.NewResponseWriter(w)
	rw.LogErrBody = true
	rw.LogAllBody = logBody

	// Execute call.
	f(rw, r)

	rs := trace.ResponseInfo{
		Time:       time.Now().UTC(),
		Headers:    rw.Header().Clone(),
		StatusCode: rw.StatusCode,
		Body:       rw.Body(),
	}

	// Transfer request body
	rq.Body = reqBodyRecorder.Data()

	if rs.StatusCode == 0 {
		rs.StatusCode = http.StatusOK
	}

	t.ReqInfo = rq
	t.RespInfo = rs

	t.CallStats = trace.CallStats{
		Latency:         rs.Time.Sub(rw.StartTime),
		InputBytes:      reqBodyRecorder.Size(),
		OutputBytes:     rw.Size(),
		TimeToFirstByte: rw.TimeToFirstByte,
	}
	return t
}

// fiberRequestHeaders builds an http.Header snapshot of the Fiber request
// headers, mirroring the net/http Trace path (Host + Content-Length /
// Transfer-Encoding are always present).
func fiberRequestHeaders(c fiber.Ctx) http.Header {
	h := make(http.Header)
	c.Request().Header.VisitAll(func(key, value []byte) {
		h.Add(string(key), string(value))
	})
	h.Set("Host", requestHost(c))
	if te := c.Get("Transfer-Encoding"); te != "" {
		h.Set("Transfer-Encoding", te)
	} else if h.Get("Content-Length") == "" {
		h.Set("Content-Length", strconv.Itoa(c.Request().Header.ContentLength()))
	}
	return h
}

// fiberResponseHeaders builds an http.Header snapshot of the Fiber response.
func fiberResponseHeaders(c fiber.Ctx) http.Header {
	h := make(http.Header)
	c.Response().Header.VisitAll(func(key, value []byte) {
		h.Add(string(key), string(value))
	})
	return h
}

// headerByteSize returns the approximate on-wire byte size of a header set.
func headerByteSize(h http.Header) int {
	sz := 0
	for k, vv := range h {
		for _, v := range vv {
			sz += len(k) + len(v)
		}
	}
	return sz
}

// TraceFiber gets trace of a Fiber HTTP request.
func TraceFiber(f OtterioHandler, logBody bool, c fiber.Ctx) trace.Info {
	name := getOpName(runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name())

	now := time.Now().UTC()
	t := trace.Info{TraceType: trace.HTTP, FuncName: name, Time: now}

	t.NodeName = requestHost(c)
	if globalIsDistErasure {
		t.NodeName = globalLocalNodeName
	}
	if t.NodeName == "" {
		t.NodeName = globalLocalNodeName
	}

	if host, port, err := net.SplitHostPort(t.NodeName); err == nil {
		if port == "443" || port == "80" {
			t.NodeName = host
		}
	}

	reqHeaders := fiberRequestHeaders(c)

	// Capture the request body before invoking the handler so it reflects the
	// client payload regardless of what the handler does with it. Use the RAW
	// fasthttp body rather than fiber's c.Body(): the latter decodes per
	// Content-Encoding (corrupting e.g. aws-chunked payloads) and can emit a
	// 415 side-effect for unsupported encodings.
	rawReqBody := c.Request().Body()
	reqBodyLen := len(rawReqBody)
	reqBody := logger.BodyPlaceHolder
	if logBody {
		cp := make([]byte, len(rawReqBody))
		copy(cp, rawReqBody)
		reqBody = cp
	}

	bucket := pathParamBucket(c)
	object := pathParamObject(c)
	rq := trace.RequestInfo{
		Time:     now,
		Proto:    c.Protocol(),
		Method:   c.Method(),
		Path:     SlashSeparator + pathJoin(bucket, object),
		RawQuery: redactLDAPPwd(string(c.Request().URI().QueryString())),
		Client:   handlers.GetSourceIPFiber(c),
		Headers:  reqHeaders,
	}

	start := time.Now()
	_ = f(c)

	status := c.Response().StatusCode()
	if status == 0 {
		status = http.StatusOK
	}

	respBody := logger.BodyPlaceHolder
	respBodyLen := 0
	// Avoid touching the buffered body when the response is streamed
	// (SetBodyStream); reading it there would drain the stream before the
	// client receives it.
	if c.Response().BodyStream() == nil {
		respBodyLen = len(c.Response().Body())
		if logBody {
			src := c.Response().Body()
			cp := make([]byte, len(src))
			copy(cp, src)
			respBody = cp
		}
	}

	rq.Body = reqBody

	rs := trace.ResponseInfo{
		Time:       time.Now().UTC(),
		StatusCode: status,
		Headers:    fiberResponseHeaders(c),
		Body:       respBody,
	}

	t.ReqInfo = rq
	t.RespInfo = rs
	t.CallStats = trace.CallStats{
		Latency:         rs.Time.Sub(start),
		InputBytes:      reqBodyLen + headerByteSize(reqHeaders),
		OutputBytes:     respBodyLen,
		TimeToFirstByte: time.Since(start),
	}
	return t
}
