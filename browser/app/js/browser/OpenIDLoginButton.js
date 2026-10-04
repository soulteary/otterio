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
import React from "react"
import storage from "local-storage-fallback"
import { getRandomString } from "../utils"
import { buildOpenIDAuthURL, OPEN_ID_NONCE_KEY } from "./utils"

export const OpenIDLoginButton = ({
  children,
  className,
  authEp,
  authScopes,
  clientId,
}) => {
  const handleClick = event => {
    event.stopPropagation()

    let redirectURI = window.location.href.split("#")[0]
    redirectURI += redirectURI.endsWith("/") ? "openid" : "/openid"

    // Store nonce in localstorage to check again after the redirect.
    const nonce = getRandomString(16)
    storage.setItem(OPEN_ID_NONCE_KEY, nonce)

    const authURL = buildOpenIDAuthURL(
      authEp,
      authScopes,
      redirectURI,
      clientId,
      nonce
    )
    navigateTo(authURL)
  }

  return (
    <div onClick={handleClick} className={className}>
      {children}
    </div>
  )
}

export default OpenIDLoginButton
