/*
 * MinIO Cloud Storage (C) 2018 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

import "@testing-library/jest-dom"
import { TextDecoder, TextEncoder } from "node:util"

// jsdom omits these Web APIs used by React Router and QR encoding.
global.TextEncoder ??= TextEncoder
global.TextDecoder ??= TextDecoder

// jsdom (jest 29+) no longer exposes setImmediate, which some tests rely on.
if (typeof global.setImmediate === "undefined") {
  global.setImmediate = (fn, ...args) => global.setTimeout(fn, 0, ...args)
}

// React + Testing Library expect this flag to be set in development/test
// environments to silence a warning about not using IS_REACT_ACT_ENVIRONMENT.
if (typeof global.IS_REACT_ACT_ENVIRONMENT === "undefined") {
  global.IS_REACT_ACT_ENVIRONMENT = true
}
