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
	"errors"
	"net"
	"net/http"
	"os"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/gofiber/fiber/v3"

	"github.com/minio/minio-go/v7/pkg/set"
	"github.com/soulteary/otterio/cmd/config"
	"github.com/soulteary/otterio/cmd/config/api"
	"github.com/soulteary/otterio/pkg/certs"
	"github.com/soulteary/otterio/pkg/env"
	"github.com/soulteary/otterio/pkg/fips"
)

const (
	serverShutdownPoll = 500 * time.Millisecond

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

	srv.App.Use(func(c fiber.Ctx) error {
		if atomic.LoadUint32(&srv.inShutdown) != 0 {
			c.Set("Connection", "close")
			return c.Status(fiber.StatusForbidden).SendString(http.ErrServerClosed.Error())
		}
		atomic.AddInt32(&srv.requestCount, 1)
		defer atomic.AddInt32(&srv.requestCount, -1)
		return c.Next()
	})

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

	if err := srv.App.Shutdown(); err != nil {
		return err
	}

	srv.listenerMutex.Lock()
	err := srv.listener.Close()
	srv.listenerMutex.Unlock()
	if err != nil {
		return err
	}

	shutdownTimeout := srv.ShutdownTimeout
	shutdownTimer := time.NewTimer(shutdownTimeout)
	ticker := time.NewTicker(serverShutdownPoll)
	defer ticker.Stop()
	for {
		select {
		case <-shutdownTimer.C:
			tmp, err := os.CreateTemp("", "otterio-goroutines-*.txt")
			if err == nil {
				_ = pprof.Lookup("goroutine").WriteTo(tmp, 1)
				tmp.Close()
				return errors.New("timed out. some connections are still active. goroutines written to " + tmp.Name())
			}
			return errors.New("timed out. some connections are still active")
		case <-ticker.C:
			if atomic.LoadInt32(&srv.requestCount) <= 0 {
				return nil
			}
		}
	}
}

// NewServer - creates new Fiber server using given arguments.
func NewServer(addrs []string, app *fiber.App, getCert certs.GetCertificateFunc) *Server {
	secureCiphers := env.Get(api.EnvAPISecureCiphers, config.EnableOn) == config.EnableOn

	var tlsConfig *tls.Config
	if getCert != nil {
		tlsConfig = &tls.Config{
			PreferServerCipherSuites: true,
			MinVersion:               tls.VersionTLS12,
			NextProtos:               []string{"http/1.1", "h2"},
			GetCertificate:           getCert,
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
