/*
 * MinIO Cloud Storage, (C) 2016 MinIO, Inc.
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

// webAPI container for Web API.
type webAPIHandlers struct {
	ObjectAPI func() ObjectLayer
	CacheAPI  func() CacheObjectLayer
}

const assetPrefix = "production"

// specialAssets are files which are unique files not embedded inside index_bundle.js.
const specialAssets = "index_bundle.*.js|loader.css|logo.png|favicon-16x16.png|favicon-32x32.png|favicon-96x96.png"
