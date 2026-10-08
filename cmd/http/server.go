/*
 * MinIO Cloud Storage, (C) 2017, 2018 MinIO, Inc.
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

package http

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/gofiber/fiber/v3"

	"github.com/soulteary/otterio-sdk/v7/pkg/set"
	"github.com/soulteary/otterio/cmd/config"
	"github.com/soulteary/otterio/cmd/config/api"
	"github.com/soulteary/otterio/pkg/certs"
	"github.com/soulteary/otterio/pkg/env"
	"github.com/soulteary/otterio/pkg/fips"
	"github.com/valyala/fasthttp"
)

const (
	// DefaultShutdownTimeout - default shutdown timeout used for graceful http server shutdown.
	DefaultShutdownTimeout = 5 * time.Second

	// DefaultMaxHeaderBytes - default maximum HTTP header size in bytes.
	DefaultMaxHeaderBytes = 1 * humanize.MiByte
)

// Server - extended server supports multiple addresses and Fiber app serving.
type Server struct {
	App             *fiber.App
	Addrs           []string
	ShutdownTimeout time.Duration
	TLSConfig       *tls.Config
	BaseContext     func(net.Listener) context.Context
	listenerMutex   sync.Mutex
	listener        *httpListener
	inShutdown      uint32
	requestCount    int32
	activeConns     sync.Map
}

// GetRequestCount - returns number of request in progress.
func (srv *Server) GetRequestCount() int {
	return int(atomic.LoadInt32(&srv.requestCount))
}

// Start - start HTTP server using Fiber on the configured listener(s).
func (srv *Server) Start() (err error) {
	var tlsConfig *tls.Config
	if srv.TLSConfig != nil {
		tlsConfig = srv.TLSConfig.Clone()
	}

	addrs := set.CreateStringSet(srv.Addrs...).ToSlice()

	var listener *httpListener
	listener, err = newHTTPListener(addrs)
	if err != nil {
		return err
	}

	// Wrap the transport handler: middleware appended here would follow all
	// registered routes, whose terminal handlers do not call Next.
	transport := srv.App.Server()
	handler := transport.Handler
	transport.Handler = func(c *fasthttp.RequestCtx) {
		if atomic.LoadUint32(&srv.inShutdown) != 0 {
			c.SetConnectionClose()
			c.Error(http.ErrServerClosed.Error(), http.StatusForbidden)
			return
		}
		handler(c)
	}
	previousConnState := transport.ConnState
	transport.ConnState = func(conn net.Conn, state fasthttp.ConnState) {
		// StateActive covers the handler and the subsequent streamed response.
		// Keep-alive idle connections and failed/new connections are not counted.
		switch state {
		case fasthttp.StateActive:
			if _, loaded := srv.activeConns.LoadOrStore(conn, struct{}{}); !loaded {
				atomic.AddInt32(&srv.requestCount, 1)
			}
		case fasthttp.StateIdle, fasthttp.StateClosed, fasthttp.StateHijacked:
			if _, loaded := srv.activeConns.LoadAndDelete(conn); loaded {
				atomic.AddInt32(&srv.requestCount, -1)
			}
		}
		if previousConnState != nil {
			previousConnState(conn, state)
		}
	}

	srv.listenerMutex.Lock()
	srv.listener = listener
	srv.listenerMutex.Unlock()

	var ln net.Listener = listener
	if tlsConfig != nil {
		ln = tls.NewListener(listener, tlsConfig)
	}

	return srv.App.Listener(ln, fiber.ListenConfig{
		DisableStartupMessage: true,
	})
}

// Shutdown - shuts down HTTP server.
func (srv *Server) Shutdown() error {
	srv.listenerMutex.Lock()
	if srv.listener == nil {
		srv.listenerMutex.Unlock()
		return http.ErrServerClosed
	}
	srv.listenerMutex.Unlock()

	if atomic.AddUint32(&srv.inShutdown, 1) > 1 {
		return http.ErrServerClosed
	}

	// Fiber already waits for handlers and response streams to finish. Use
	// one deadline, without a second polling window after a successful drain.
	defer srv.listener.Close()
	return srv.App.ShutdownWithTimeout(srv.ShutdownTimeout)
}

// NewServer - creates new Fiber server using given arguments.
func NewServer(addrs []string, app *fiber.App, getCert certs.GetCertificateFunc) *Server {
	secureCiphers := env.Get(api.EnvAPISecureCiphers, config.EnableOn) == config.EnableOn

	var tlsConfig *tls.Config
	if getCert != nil {
		tlsConfig = &tls.Config{
			PreferServerCipherSuites: true,
			MinVersion:               tls.VersionTLS12,
			// fasthttp serves HTTP/1.1; advertising h2 makes capable clients send
			// HTTP/2 frames to an HTTP/1.1 parser after the TLS handshake.
			NextProtos:     []string{"http/1.1"},
			GetCertificate: getCert,
		}
		if secureCiphers || fips.Enabled() {
			tlsConfig.CipherSuites = fips.CipherSuitesTLS()
			tlsConfig.CurvePreferences = fips.EllipticCurvesTLS()
		}
	}

	return &Server{
		App:             app,
		Addrs:           addrs,
		ShutdownTimeout: DefaultShutdownTimeout,
		TLSConfig:       tlsConfig,
	}
}
