# OtterIO release preparation for 2026 10 08 / 发布准备核对

Prepare a fresh UTC `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tag after this preparation is
merged and the exact resulting main commit passes the release gates. This date
identifies the review; it does not reserve a tag or announce publication.

## Source range

The latest published release checked during preparation is
[`RELEASE.2026-10-07T14-09-17Z`](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-07T14-09-17Z),
published at `2026-10-07T14:19:39Z`, source
`c8a09caf06c56483e1d21a1c9cdedc3dc3b76381`.
Reviewed implementation cutoff:
`6f6d0835ddff68020f1491c403b958fade22841f`.
[Compare the full unpublished range](https://github.com/soulteary/otterio/compare/c8a09caf06c56483e1d21a1c9cdedc3dc3b76381...6f6d0835ddff68020f1491c403b958fade22841f).

All four commits after the published baseline are included:

- [`52e2d95`](https://github.com/soulteary/otterio/commit/52e2d95b7d4fb58fb212f4a2ce3f351b891e0343):
  owner account information uses the built-in administrator policy; scoped IAM,
  LDAP and delegated credentials retain their restrictions. Add atomic
  create-only PUT and multipart completion on the supported storage backends.
- [`1d7ad44`](https://github.com/soulteary/otterio/commit/1d7ad44d943559ad842d0067586450e3c4c3797f),
  [PR #30](https://github.com/soulteary/otterio/pull/30): replace the CLI parser with
  `urfave/cli/v3 v3.14.0`, preserve the reviewed behavior and introduce fresh
  gateway command factories with compatibility fixtures.
- [`7dc4888`](https://github.com/soulteary/otterio/commit/7dc48889db1109e1dda8f25f505322a8a9100888),
  [PR #31](https://github.com/soulteary/otterio/pull/31): migrate server code,
  examples and the Mint harness to published OtterIO SDK and kits modules.
- [`6f6d083`](https://github.com/soulteary/otterio/commit/6f6d0835ddff68020f1491c403b958fade22841f),
  [PR #32](https://github.com/soulteary/otterio/pull/32): the S3 gateway integration
  fixture waits until its upstream server no longer reports storage offline,
  with a regression for HTTP listening before storage initialization. Production
  readiness behavior and the server/NAS fixtures are not changed by this commit.

Root `RELEASE_NOTES.md` is the bilingual body consumed by the release workflow.
The October 5 and October 7 preparation documents remain historical records.
The preceding HTTP/administration lifecycle, macOS supervision, container
credential and storage-hardening work is already in the published baseline;
it is not counted again as newly unreleased work.

## Dependency and source compatibility

The root module and `mint/run/core/minio-go/go.mod` already require
`github.com/soulteary/otterio-sdk/v7 v7.3.1` and Go 1.27.1. The server graph contains
all six published kits: `crc64nvme v1.1.2`, `highwayhash v1.0.5`, `md5-simd v1.1.3`,
`sha256-simd v1.0.2`, `simdjson-go v0.4.6` and `sio v0.5.2`.
This documentation preparation adds no module/version changes or local module
replacements. Published source identities and the named-type/hash-callback
migration requirements are in [SDK and kits compatibility](../development/sdk-kits-migration-20261008.md).

The retained SDK package name `minio` does not make old and new module types
identical. External Go integrations must update signatures and rebuild.
Custom gateways also need the [CLI factory API](../cli-migration.md).
The fixed CLI snapshot covers the reviewed command catalog, flags, scope, help,
errors and stream behavior; passing it does not prove compatibility for every
third-party plugin or every platform.

## Conditional-write contract

`X-Otterio-Conditional-Writes: v1` promises atomic `If-None-Match: *` only for PUT
and CompleteMultipartUpload. The existence check and final commit use the same
destination namespace write lock. An existing object, including a zero-byte
object or a key created after multipart parts were uploaded, returns HTTP 412
and retains the existing content.

Filesystem and single-pool erasure storage support this contract; a configured
write-through cache does not disable it. Gateways, multiple pools, write-back
cache and uninitialized/unknown storage do not advertise the capability.
`If-Match`, non-wildcard/duplicate `If-None-Match`, and conditions on unsupported
backends return HTTP 501. This is not a general ETag compare-and-swap API.
Clients must check capability and must not convert a failed condition into an
unconditional overwrite.

## Preparation checks

The following checks ran on the exact `6f6d083` implementation with Go 1.27.1 on
macOS arm64, before editing these documents. They are preparation evidence, not
publication or all-platform runtime acceptance:

- Build the native server and both standalone English/Chinese `xl.meta` tools
  with `-mod=readonly`; server build information identifies the exact clean
  source SHA, SDK `v7.3.1`, all six kits versions and no replacement modules.
- Verify root and Mint dependency graphs and module checksums (`go list -m all`
  and `go mod verify`). The root graph still contains upstream
  `github.com/minio/simdjson-go v0.4.5` through CoreDNS module metadata; it is not
  compiled by this server. The compiled package list (`go list -deps ./...`)
  contains no `github.com/minio/*` package. The root graph's SDK and six kits
  resolve to the published versions above without local replacements.
- Compare the candidate CLI against the fixed archived `52e2d95` parser baseline:
  **133 cases match**, without rewriting the snapshot. The 32 runner regression
  tests pass with five skips for native-Windows or optional-binary tests; the
  actual candidate comparison runs separately above.
- Both standalone metadata tools pass all seven fixture tests, including exact
  comparison with archived tools. The English no-argument default remains
  `xl.meta`; the Chinese no-argument default remains help.
- The external gateway API/fresh-flag fixture passes. The eight local CLI
  integration/readiness tests pass with the candidate server and updated OC:
  server data restart/TLS, NAS and S3 gateway CRUD and SIGTERM, invalid console
  startup/port conflict, and upstream storage initialization.
- The companion OC passes 1,026 core integration checks against this server:
  five HTTP/TLS deployments, a 30-second TLS stability run, a four-drive extended
  run and migration checks. Three console scenarios with writes enabled also
  pass. These are local macOS arm64 results, not published-artifact acceptance.
- The owner/scoped-principal and conditional-write tests pass with the race
  detector. They cover competing create-only writers, empty-object rejection,
  multipart conflicts and capability rejection.
- All 83 release-script regression tests and 13 dependency/toolchain policy
  tests pass. The real release preflight is still pending a clean synchronized
  main and a fresh tag; it is not run on this preparation branch.

## Release acceptance

- [ ] Reconcile any later main commits, merge this preparation and record the
  exact resulting source SHA. Recheck the release baseline if another release
  is published before this one.
- [ ] Require **Go**, **Lint** and **Release checks** success on that exact main
  SHA. Review applicable CLI, Docker source/container and security checks;
  a successful PR run does not replace the exact-main release gate.
- [ ] From clean synchronized main, run the read-only preflight and create a
  fresh signed or annotated UTC tag. Do not reuse a published/partially used tag
  or manually duplicate the tag-triggered GitHub Release.
- [ ] Verify actual Release and requested Stable release promotion results,
  all six binaries, the SHA-256 checksum file and `release-manifest.json`
  (eight uploaded assets).
- [ ] Check manifest source identity, configured registries' fixed tags and
  image digests for Linux amd64/arm64/ppc64le, Linux amd64 image S3 smoke results,
  and newest-release stable alias promotion. Cross-compilation is not runtime
  acceptance for macOS, Windows or every image architecture.
- [ ] In staging, check existing ordinary/encrypted objects, multipart and
  signed copies; owner and restricted/LDAP/delegated account access; supported
  conditional-create contention and unsupported capability rejection;
  server/NAS/S3 gateway shutdown and restart; backup/restore, distributed
  healing and replication with every node upgraded.

No tag, GitHub Release or release image is created by this preparation. Only an
actual publication and deployment record may mark acceptance steps complete.
Follow [the release guide](../releasing.md) and
[stable promotion policy](../release-promotion.md) for commands and recovery.

## 中文核对说明

本次以 10 月 7 日已发布版本及其源码 `c8a09caf` 为基线，完整覆盖截至 `6f6d083`
的四项未发布变更：账户信息与条件写入、CLI v3、SDK/kits 迁移，以及 S3 gateway
测试等待上游存储初始化。此前已发布的 HTTP、macOS、容器与存储加固不重复计入。
依赖已在主分支完成升级，本准备 PR 不重复修改模块清单。

本地 Go 1.27.1 / macOS arm64 的构建、133 项 CLI 基线比较、双语工具、gateway API、
本地生命周期与竞态回归均已通过。正式发布仍需合并后的精确 main SHA 通过门禁，
再选取全新的 UTC 时间戳标记，并实际核对产物、摘要、晋升和部署结果。
以上清单保持未完成状态，不以文档日期或本地测试冒充正式发布验收。
