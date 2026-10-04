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

import React from "react"
import userEvent from "@testing-library/user-event"
import { renderWithStore, defaultState } from "../../jest/test-utils"
import { ObjectsBulkActions } from "../ObjectsBulkActions"
import * as objectsActions from "../actions"

jest.mock("../../navigation", () => ({ navigateTo: jest.fn() }))

const stateWith = checkedList => ({
  ...defaultState,
  objects: { ...defaultState.objects, checkedList },
})

describe("ObjectsBulkActions", () => {
  beforeEach(() => jest.clearAllMocks())

  it("renders without crashing", () => {
    renderWithStore(<ObjectsBulkActions />, stateWith([]))
  })

  it("toggles list-actions class when there are checked objects", () => {
    const { container } = renderWithStore(<ObjectsBulkActions />, stateWith(["a"]))
    expect(container.querySelector(".list-actions").classList.contains("list-actions-toggled")).toBe(true)
  })

  it("dispatches downloadObject for a single non-folder selection", async () => {
    const user = userEvent.setup()
    const spy = jest.spyOn(objectsActions, "downloadObject")
    renderWithStore(<ObjectsBulkActions />, stateWith(["test"]))
    await user.click(document.querySelector("#download-checked"))
    expect(spy).toHaveBeenCalledWith("test")
    spy.mockRestore()
  })

  it("dispatches downloadCheckedObjects for multiple selections", async () => {
    const user = userEvent.setup()
    const spy = jest.spyOn(objectsActions, "downloadCheckedObjects")
    renderWithStore(<ObjectsBulkActions />, stateWith(["a", "b"]))
    await user.click(document.querySelector("#download-checked"))
    expect(spy).toHaveBeenCalled()
    spy.mockRestore()
  })

  it("dispatches resetCheckedList when the close button is clicked", async () => {
    const user = userEvent.setup()
    const spy = jest.spyOn(objectsActions, "resetCheckedList")
    renderWithStore(<ObjectsBulkActions />, stateWith(["a"]))
    await user.click(document.querySelector("#close-bulk-actions"))
    expect(spy).toHaveBeenCalled()
    spy.mockRestore()
  })

  it("shows the delete confirmation modal when delete-checked is clicked", async () => {
    const user = userEvent.setup()
    renderWithStore(<ObjectsBulkActions />, stateWith(["a"]))
    await user.click(document.querySelector("#delete-checked"))
    expect(document.querySelector(".modal-confirm")).not.toBeNull()
  })
})
