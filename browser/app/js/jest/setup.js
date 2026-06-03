/*
 * MinIO Cloud Storage (C) 2018 MinIO, Inc.
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

import "jest-enzyme"
import { configure } from "enzyme"
import Adapter from "enzyme-adapter-react-16"

// jsdom (jest 29+) no longer exposes setImmediate, which some tests rely on.
if (typeof global.setImmediate === "undefined") {
  global.setImmediate = (fn, ...args) => global.setTimeout(fn, 0, ...args)
}

configure({ adapter: new Adapter() })
