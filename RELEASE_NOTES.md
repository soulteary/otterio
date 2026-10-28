# OIDC validation retry hardening

This release follows [RELEASE.2026-10-22T15-10-13Z](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-22T15-10-13Z).

## Changes

- Preserve the configured JWT signing-algorithm allowlist when retrying OIDC token validation after refreshing JWKS. The retry now uses the same parser as the initial validation, so a valid signature cannot bypass the configured algorithm policy.
- Add regression coverage for PS256/384/512 and HS256/384/512 rejection after a real JWKS refresh, with RS256/384/512 key-refresh success cases. The original implementation accepted the RSA-PSS cases despite their absence from the allowlist.
- Clarify the repository selection and draft-release visibility requirements for the cross-repository publication credential.

This is an OIDC validation hardening change. OtterIO does not use an OIDC ClientSecret as a token verification key; this release does not claim that OtterIO was affected by MinIO CVE-2026-33322. Legitimate tokens must use an algorithm supported by the existing OIDC allowlist. There is no storage-format or configuration migration for this update.

## Upgrade and verification

Back up deployment configuration and retain the previous verified binary or image. Verify the release checksums and manifest, then test OIDC sign-in, token refresh/key rotation, restricted-user permissions and normal S3 operations in staging before rollout.

The independently deployed OC Console and the existing migration, supported-topology and rollback boundaries remain applicable. Publication requires official acceptance of the exact packaged OtterIO/OC pair; source tests and cross-compilation alone do not certify release artifacts. See the [release guide](docs/releasing.md) and [Console migration guide](docs/console-migration-preparation.md).

---

# OIDC 令牌验签重试加固

本次更新接续 [RELEASE.2026-10-22T15-10-13Z](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-22T15-10-13Z)。

- 刷新 JWKS 后复用带算法白名单的 JWT 解析器，确保首次验证与重试遵守相同限制。
- 增加真实 JWKS 刷新回归测试：拒绝白名单外的 PS256/384/512 和 HS256/384/512，并确认合法 RS256/384/512 在刷新后仍可通过。
- 澄清跨仓库发布凭据的仓库选择及草稿访问要求。

OtterIO 不使用 OIDC ClientSecret 作为令牌验签密钥，本次发布不表示 OtterIO 受 MinIO CVE-2026-33322 影响。此更新不改变存储格式或配置格式；合法令牌仍需使用原有白名单支持的算法。

升级前保留旧版本并备份部署配置，核对校验和与发布清单，在测试环境验证 OIDC 登录、密钥刷新、受限用户权限及常规 S3 操作。独立 OC Console 的迁移、支持范围与回退边界继续适用。正式发布以实际 OtterIO/OC 发布产物的官方配对验收结果为准。
