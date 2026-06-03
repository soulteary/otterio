/*
 * MinIO Cloud Storage, (C) 2015, 2016 MinIO, Inc.
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

package cmd

import (
	"github.com/gofiber/fiber/v3"
)

// registerDistErasureRoutersFiber registers distributed erasure REST routers on a Fiber app.
func registerDistErasureRoutersFiber(app *fiber.App, endpointServerPools EndpointServerPools) {
	registerStorageRESTHandlersFiber(app, endpointServerPools)
	registerPeerRESTHandlersFiber(app)
	registerBootstrapRESTHandlersFiber(app)
	registerLockRESTHandlersFiber(app)
}
