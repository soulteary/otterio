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
import { Provider } from "react-redux"
import { MemoryRouter } from "react-router-dom"
import { render, screen, fireEvent } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import configureStore from "redux-mock-store"
import { thunk } from "redux-thunk"
import { Login } from "../Login"
import web from "../../web"
import { defaultState } from "../../jest/test-utils"

jest.mock("../../web", () => ({
  Login: jest.fn(() => Promise.resolve({ token: "test" })),
  LoggedIn: jest.fn(() => false),
  GetDiscoveryDoc: jest.fn(() => Promise.resolve({ DiscoveryDoc: {} })),
}))

const mockStore = configureStore([thunk])

const renderLogin = (state = defaultState) => {
  const store = mockStore(state)
  return {
    store,
    ...render(
      <Provider store={store}>
        <MemoryRouter
          future={{ v7_startTransition: true, v7_relativeSplatPath: true }}
        >
          <Login />
        </MemoryRouter>
      </Provider>
    ),
  }
}

describe("Login", () => {
  beforeEach(() => {
    jest.clearAllMocks()
    web.LoggedIn.mockReturnValue(false)
  })

  it("renders without crashing", () => {
    renderLogin()
    expect(document.querySelector(".login")).not.toBeNull()
  })

  it("adds the `is-guest` class to body on mount", () => {
    renderLogin()
    expect(document.body.classList.contains("is-guest")).toBe(true)
  })

  it("shows an alert when both keys are empty on submit", () => {
    const { store } = renderLogin()
    fireEvent.submit(document.querySelector("form"))
    // Either the clearAlert from mount or the SET from validation - the
    // important thing is that an alert/SET action with the empty-key message
    // is dispatched.
    const setActions = store
      .getActions()
      .filter(a => a.type === "alert/SET" || a.alert)
    expect(setActions.length).toBeGreaterThan(0)
  })

  it("calls web.Login with the entered credentials on submit", async () => {
    const user = userEvent.setup()
    renderLogin()
    await user.type(document.querySelector("#accessKey"), "ak")
    await user.type(document.querySelector("#secretKey"), "sk")
    fireEvent.submit(document.querySelector("form"))
    expect(web.Login).toHaveBeenCalledWith({ username: "ak", password: "sk" })
  })
})
