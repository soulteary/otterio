/*
 * MinIO Cloud Storage (C) 2019 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

import { navigateTo } from "../navigation"

test("navigation forwards the complete URL without rewriting it", () => {
  const location = { assign: jest.fn() }
  const url = "https://example.invalid/download/a%20b?token=test#fragment"
  navigateTo(url, location)
  expect(location.assign).toHaveBeenCalledTimes(1)
  expect(location.assign).toHaveBeenCalledWith(url)
})
