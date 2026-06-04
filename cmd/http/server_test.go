/*
 * MinIO Cloud Storage, (C) 2017 MinIO, Inc.
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
	"reflect"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/soulteary/otterio/pkg/certs"
)

func TestNewServer(t *testing.T) {
	nonLoopBackIP := getNonLoopBackIP(t)
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("Hello, world")
	})

	testCases := []struct {
		addrs  []string
		app    *fiber.App
		certFn certs.GetCertificateFunc
	}{
		{[]string{"127.0.0.1:9000"}, app, nil},
		{[]string{nonLoopBackIP + ":9000"}, app, nil},
		{[]string{"127.0.0.1:9000", nonLoopBackIP + ":9000"}, app, nil},
		{[]string{"127.0.0.1:9000"}, app, getCert},
		{[]string{nonLoopBackIP + ":9000"}, app, getCert},
		{[]string{"127.0.0.1:9000", nonLoopBackIP + ":9000"}, app, getCert},
	}

	for i, testCase := range testCases {
		server := NewServer(testCase.addrs, testCase.app, testCase.certFn)
		if server == nil {
			t.Fatalf("Case %v: server: expected: <non-nil>, got: <nil>", (i + 1))
		}

		if !reflect.DeepEqual(server.Addrs, testCase.addrs) {
			t.Fatalf("Case %v: server.Addrs: expected: %v, got: %v", (i + 1), testCase.addrs, server.Addrs)
		}

		// Interfaces are not comparable even with reflection.
		// if !reflect.DeepEqual(server.Handler, testCase.handler) {
		// 	t.Fatalf("Case %v: server.Handler: expected: %v, got: %v", (i + 1), testCase.handler, server.Handler)
		// }

		if testCase.certFn == nil {
			if server.TLSConfig != nil {
				t.Fatalf("Case %v: server.TLSConfig: expected: <nil>, got: %v", (i + 1), server.TLSConfig)
			}
		} else {
			if server.TLSConfig == nil {
				t.Fatalf("Case %v: server.TLSConfig: expected: <non-nil>, got: <nil>", (i + 1))
			}
		}

		if server.ShutdownTimeout != DefaultShutdownTimeout {
			t.Fatalf("Case %v: server.ShutdownTimeout: expected: %v, got: %v", (i + 1), DefaultShutdownTimeout, server.ShutdownTimeout)
		}
	}
}
