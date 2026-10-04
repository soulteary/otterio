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

import React from "react"
import { render } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { OpenIDLoginButton } from "../OpenIDLoginButton"
import { navigateTo } from "../../navigation"

jest.mock("../../navigation", () => ({ navigateTo: jest.fn() }))

jest.mock("../utils", () => ({
  buildOpenIDAuthURL: jest.fn(() => "https://idp.invalid/auth"),
  OPEN_ID_NONCE_KEY: "openIDKey",
}))

jest.mock("../../utils", () => ({
  getRandomString: () => "test-nonce",
}))

const { buildOpenIDAuthURL } = require("../utils")

describe("OpenIDLoginButton", () => {
  beforeEach(() => jest.clearAllMocks())

  it("renders the children inside the clickable wrapper", () => {
    const { container } = render(
      <OpenIDLoginButton
        className="btn"
        authEp="https://idp.invalid/authorize"
        authScopes={["openid"]}
        clientId="cid"
      >
        Log in with OpenID
      </OpenIDLoginButton>
    )
    expect(container.textContent).toBe("Log in with OpenID")
  })

  it("redirects to the OpenID provider on click", async () => {
    const user = userEvent.setup()
    const { container } = render(
      <OpenIDLoginButton
        className="btn"
        authEp="https://idp.invalid/authorize"
        authScopes={["openid"]}
        clientId="cid"
      >
        Sign in
      </OpenIDLoginButton>
    )
    await user.click(container.firstChild)
    expect(buildOpenIDAuthURL).toHaveBeenCalled()
    expect(navigateTo).toHaveBeenCalledWith("https://idp.invalid/auth")
  })
})
