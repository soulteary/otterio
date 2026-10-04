/*
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

import React from "react"
import { render } from "@testing-library/react"
import { Provider } from "react-redux"
import configureStore from "redux-mock-store"
// We provide the thunk middleware ourselves so action creators returning
// functions get invoked (giving us a chance to capture inner dispatches via
// spies) instead of just being recorded as opaque function actions.
// Redux Thunk 3 exposes its middleware as the named thunk export.
import { thunk } from "redux-thunk"

const mockStore = configureStore([thunk])

// Render a component wrapped in a Redux <Provider>. We wrap the store's
// dispatch with a Jest mock so tests can use `dispatchSpy` to assert on raw
// action objects, while the original implementation (mock-store + thunk) still
// runs so thunks execute and nested dispatches show up in getActions().
export function renderWithStore(ui, initialState = {}) {
  const store = mockStore(initialState)
  const originalDispatch = store.dispatch
  const dispatchSpy = jest.fn(action => originalDispatch(action))
  store.dispatch = dispatchSpy
  const result = render(<Provider store={store}>{ui}</Provider>)
  return { ...result, store, dispatchSpy }
}

// Convenience: build a sensible default state so most tests do not have to
// repeat the boilerplate. Tests that need a specific slice override only the
// keys they care about.
export const defaultState = {
  alert: { show: false, type: "danger", message: "", id: 0 },
  browser: {
    serverInfo: { version: "", platform: "", runtime: "", otterioVersion: "" },
    storageInfo: { used: 0, total: 0, free: 0 },
    sidebarOpen: false,
  },
  buckets: {
    list: [],
    filter: "",
    currentBucket: "",
    showMakeBucketModal: false,
    policies: [],
    showBucketPolicy: false,
  },
  objects: {
    list: [],
    filter: "",
    listLoading: false,
    sortBy: "name",
    sortOrder: false,
    currentPrefix: "",
    prefixWritable: false,
    shareObject: { show: false, object: "", url: "", showExpiryDate: false },
    checkedList: [],
  },
  uploads: { files: {}, showAbortModal: false },
}
