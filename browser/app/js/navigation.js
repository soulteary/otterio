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

// Keep real browser navigation behind a small, testable boundary.
export const navigateTo = (url, location = window.location) => {
  location.assign(url)
}
