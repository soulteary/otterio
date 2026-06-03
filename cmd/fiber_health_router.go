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
	"net/http"

	"github.com/gofiber/fiber/v3"
)

func registerHealthCheckRouterFiber(app *fiber.App) {
	prefix := healthCheckPathPrefix

	registerHealthRoute(app, prefix+healthCheckClusterPath, []routeRule{
		{methods: []string{http.MethodGet}, handler: toMinioHandler(ClusterCheckHandler)},
	})
	registerHealthRoute(app, prefix+healthCheckClusterReadPath, []routeRule{
		{methods: []string{http.MethodGet}, handler: toMinioHandler(ClusterReadCheckHandler)},
	})
	registerHealthRoute(app, prefix+healthCheckLivenessPath, []routeRule{
		{methods: []string{http.MethodGet, http.MethodHead}, handler: toMinioHandler(LivenessCheckHandler)},
	})
	registerHealthRoute(app, prefix+healthCheckReadinessPath, []routeRule{
		{methods: []string{http.MethodGet, http.MethodHead}, handler: toMinioHandler(ReadinessCheckHandler)},
	})
}

func registerHealthRoute(app *fiber.App, path string, rules []routeRule) {
	app.All(path, func(c fiber.Ctx) error {
		matched, err := dispatchRules(c, rules)
		if !matched {
			return methodNotAllowedHandlerFiber("Health")(c)
		}
		return err
	})
}
