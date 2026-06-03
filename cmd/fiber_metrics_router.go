/*
 * MinIO Cloud Storage, (C) 2018 MinIO, Inc.
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
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

func registerMetricsRouterFiber(app *fiber.App) {
	prefix := minioReservedBucketPath
	register := func(path string, h fiber.Handler) {
		app.All(prefix+path, h)
	}
	switch getPrometheusAuthType() {
	case prometheusPublic:
		register(prometheusMetricsPathLegacy, adaptor.HTTPHandler(metricsHandler()))
		register(prometheusMetricsV2ClusterPath, adaptor.HTTPHandler(metricsServerHandler()))
		register(prometheusMetricsV2NodePath, adaptor.HTTPHandler(metricsNodeHandler()))
	default:
		register(prometheusMetricsPathLegacy, adaptor.HTTPHandler(AuthMiddleware(metricsHandler())))
		register(prometheusMetricsV2ClusterPath, adaptor.HTTPHandler(AuthMiddleware(metricsServerHandler())))
		register(prometheusMetricsV2NodePath, adaptor.HTTPHandler(AuthMiddleware(metricsNodeHandler())))
	}
}
