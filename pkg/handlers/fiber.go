/*
 * MinIO Cloud Storage, (C) 2018 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/minio
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

package handlers

import (
	"net"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// GetSourceSchemeFiber retrieves the scheme from forwarded headers on a Fiber context.
func GetSourceSchemeFiber(c fiber.Ctx) string {
	if proto := c.Get(xForwardedProto); proto != "" {
		return strings.ToLower(proto)
	}
	if proto := c.Get(xForwardedScheme); proto != "" {
		return strings.ToLower(proto)
	}
	if proto := c.Get(forwarded); proto != "" {
		if match := forRegex.FindStringSubmatch(proto); len(match) > 1 {
			if match = protoRegex.FindStringSubmatch(match[2]); len(match) > 1 {
				return strings.ToLower(match[2])
			}
		}
	}
	if c.Protocol() == "https" {
		return "https"
	}
	return "http"
}

// GetSourceIPFromHeadersFiber retrieves the IP from forwarded headers on a Fiber context.
func GetSourceIPFromHeadersFiber(c fiber.Ctx) string {
	var addr string

	if fwd := c.Get(xForwardedFor); fwd != "" {
		s := strings.Index(fwd, ", ")
		if s == -1 {
			s = len(fwd)
		}
		addr = fwd[:s]
	} else if fwd := c.Get(xRealIP); fwd != "" {
		addr = fwd
	} else if fwd := c.Get(forwarded); fwd != "" {
		if match := forRegex.FindStringSubmatch(fwd); len(match) > 1 {
			addr = strings.Trim(match[1], `"`)
		}
	}
	return addr
}

// GetSourceIPFiber retrieves the client IP from a Fiber context.
func GetSourceIPFiber(c fiber.Ctx) string {
	addr := GetSourceIPFromHeadersFiber(c)
	if addr != "" {
		return addr
	}
	return c.IP()
}

// SplitHostPortFiber splits host and port from a Fiber hostname.
func SplitHostPortFiber(host string) (string, string, error) {
	return net.SplitHostPort(host)
}
