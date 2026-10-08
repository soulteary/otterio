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
`458b54c9bc155780c546696a12581450adfee32b`.
[Compare the full unpublished range](https://github.com/soulteary/otterio/compare/c8a09caf06c56483e1d21a1c9cdedc3dc3b76381...458b54c9bc155780c546696a12581450adfee32b).

All 12 commits after the published baseline are included. The first four were
reviewed in the earlier preparation:

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
  examples and the Mint module manifest to published OtterIO SDK and kits modules.
- [`6f6d083`](https://github.com/soulteary/otterio/commit/6f6d0835ddff68020f1491c403b958fade22841f),
  [PR #32](https://github.com/soulteary/otterio/pull/32): the S3 gateway integration
  fixture waits until its upstream server no longer reports storage offline,
  with a regression for HTTP listening before storage initialization. Production
  readiness behavior and the server/NAS fixtures are not changed by this commit.

### Later main changes reconciled for this preparation

- [`0de08e7`](https://github.com/soulteary/otterio/commit/0de08e7bef939d82d208fffd70c43248075790f7),
  [PR #33](https://github.com/soulteary/otterio/pull/33): earlier release materials
  and the fixed/checksum-verified Mint SDK installer described below.
- [`7ee53ea`](https://github.com/soulteary/otterio/commit/7ee53ead68a18a49276303be943d3a87a38d1980),
  [PR #34](https://github.com/soulteary/otterio/pull/34): edge runtime package
  caching with daily/manual refresh, separate cache scopes, and independent
  stable-release refresh. No stable alias policy changes.
- [`9942b8f`](https://github.com/soulteary/otterio/commit/9942b8f142780d093beca88bc20223277d08abe0),
  [PR #35](https://github.com/soulteary/otterio/pull/35): storage-class updates
  change the receiver under the shared lock; snapshots and reads synchronize.
  Correct erasure test teardown ordering and add regressions.
- [`eef2870`](https://github.com/soulteary/otterio/commit/eef2870e2baa44ab3b75b1cebb7424ccc161dce1),
  [PR #36](https://github.com/soulteary/otterio/pull/36): align deployment guides
  with the actual release artifacts, security defaults, admin path and OC.
- [`8fbb20e`](https://github.com/soulteary/otterio/commit/8fbb20e53bc88adc023f3b60966ceda3aa0c50bf):
  durable lifecycle transitions and exact-version temporary restore, target and
  deletion safety, bucket configuration revision updates, self-credential
  operations, response headers and restore notifications. The merge commit
  [`8ead9fb`](https://github.com/soulteary/otterio/commit/8ead9fb67ad2c644d01752707e22ecc6c8098db4)
  is also in the range; it is not a separate feature.
- [`d826678`](https://github.com/soulteary/otterio/commit/d826678b1d0fd7eff74f6d9528635f91c3f619c8):
  restore ordinary overwrite behavior for inconsistent local metadata without
  discarding partial tier references or deletion intents. Preserve transition
  status during healing; writes continue to require write quorum.
- [`458b54c`](https://github.com/soulteary/otterio/commit/458b54c9bc155780c546696a12581450adfee32b),
  [PR #37](https://github.com/soulteary/otterio/pull/37): reconcile the remaining
  English/Chinese capability guides, add contributor guidance and a fixed
  project-status record, add degraded-metadata overwrite protection regressions
  and isolate healing fixtures.

The Mint repair is already merged in #33. At the earlier `6f6d083` cutoff, the
Mint manifest declared the fork SDK, but installation
still fetched upstream MinIO's latest `functional_tests.go`; that requirement
could remain unused. The installer now resolves the Mint module's fixed SDK
version, downloads its checksum-verified module source and copies that module's
functional program. The independent Mint manifest/checksums are refreshed from
the actual imports, retaining SDK `v7.3.1` and Go 1.27.1.

Installation rejects a replaced SDK, disables parent workspaces, builds with
read-only dependencies and requires the resulting binary to link the exact
fork SDK without upstream MinIO modules or replacements. The current preparation
only reconciles release documents; it introduces no additional implementation or
dependency change.

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
Server versions remain unchanged; only the Mint harness manifest and checksums
are completed for the actual functional program. No local replacement is added.
Published source identities and the named-type/hash-callback
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

## Lifecycle, configuration and credential boundaries

Tier transitions require native, local, single-pool erasure storage; FS,
distributed erasure, multiple pools and gateways do not support configuring ILM
targets or transition rules. Destination buckets must exist, and must have
versioning enabled if the source does. Persisted destination references and
deletion intents keep interrupted work retryable after restart or rule removal.
Do not remove referenced remote data, credentials or targets to bypass safety
checks. Legacy references still require an unambiguous matching rule/target;
missing, ambiguous or corrupt metadata is not automatically repaired.

Normal restore creates a temporary local copy of the exact source version,
preserving version ID, modification time, ETag and logical multipart layout.
It runs asynchronously; the deadline makes cleanup eligible for scanning,
rather than guaranteeing completion at that instant. SELECT restore is not
implemented. Local-copy expiry retains the source version and remote data.
Ordinary overwrites retain write-quorum enforcement and cannot discard a tiered
version's durable recovery reference; explicit permanent deletion performs
remote cleanup before dropping the local reference.

Bucket configuration uses its own `X-Otterio-Bucket-Config: v1` protocol with
`X-Otterio-Config-Revision` / `X-Otterio-Config-If-Match`. Policy and lifecycle
support FS and single-pool erasure; versioning supports only single-pool erasure.
Conditional writes require authenticated SigV4 and a signed condition header.
The revision identifies kind plus document bytes; it is not an object ETag or
a general compare-and-swap API. See the
[configuration revision contract](../../cmd/bucket-config-conditional.go).

`/otterio/admin/v3/self-credentials` exposes identity/status and an advisory
rotation hint. Only enabled native IAM users may rotate their own secret;
root/service/STS/directory identities cannot. Distributed storage, etcd IAM and
external OPA authorization disable rotation. Authorization and the old signed
credential are rechecked before atomic persistence. A
`CredentialPropagationIncomplete` error may follow a committed secret change;
verify outcome before retrying with the old key.

The top-level metadata format and FileInfo wire tuple are not version-bumped,
but saved target/transition/delete/restore state adds recovery semantics that
older servers may not manage safely. Validate rollback before enabling these
features and back up local metadata plus remote data. This preparation does
not promise unconditional downgrade or production acceptance for every remote
service. See the [lifecycle guide](../bucket/lifecycle/README.md).

## Preparation checks

### Current cutoff: `458b54c`

The following checks were rerun for this preparation using Go 1.27.1 on macOS
arm64. The working branch starts at exact main `458b54c`; its only edits are
release documents. These are local regressions, not release-asset acceptance:

- All 88 offline release-script regressions pass, including runtime cache,
  manifest, draft/publication and promotion policies. Both release helper shell
  scripts pass `bash -n`.
- All 14 dependency/toolchain policy and seven offline Mint installer tests pass.
- `go test -mod=readonly -race ./pkg/bucket/lifecycle ./cmd/config/storageclass`
  passes, covering rule evaluation and synchronized storage-class configuration.
- The targeted `./cmd` suite passes with `-mod=readonly -race -count=1`, covering
  ordinary/degraded overwrite, pending metadata healing, persisted transition
  and deletion recovery, restore and events, bucket configuration, self
  credentials, target safety and scanner expiry. Selection:
  `Test(ErasurePutObject|XLStorageHealsPendingTransition|TransitionStorage|TransitionDeletionIntent|LifecycleTransition|BeginRestore|BucketConfig|SelfCredentials|BucketTarget|ScannerLifecycle|Expiry|TransitionRestoreCompletionEvents)`.

Full Linux/Windows CI, final-main release gates, the complete live Mint suite,
published artifact verification and staging deployment remain separate checks.
The previous OC integration results below used server `6f6d083`, not the new
`458b54c` implementation. OC's documented compatibility pin and optional P3/ILM
fixtures are not changed by this server preparation.

### Historical preparation at `6f6d083`

The following previously recorded checks ran on the exact `6f6d083` implementation with Go 1.27.1 on
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

### Historical Mint repair checks from #33 preparation

- All seven offline installer regression tests pass: changing the manifest pin
  selects that exact module, workspaces are disabled, download/source failures
  stop the build, read-only builds reject upstream imports without modifying
  manifests, and binary inventories with missing/wrong SDK, upstream modules or
  replacements are rejected.
- All 14 dependency/toolchain policy tests pass, including the new check that
  Mint and the server declare the same SDK version. The existing Go workflow
  runs the installer regressions and a real Linux Mint build with manifest
  equality checks; this preparation does not add a separate workflow.
- The real revised installer passes locally with Go 1.27.1 / macOS arm64 in a
  temporary Mint directory. Its copied source is byte-identical to published
  SDK `v7.3.1`'s functional program. `go.mod` and `go.sum` remain unchanged during
  installation; the binary links SDK `v7.3.1`, kits `crc64nvme v1.1.2` and
  `md5-simd v1.1.3`, with no upstream MinIO modules or replacements.
- Shell syntax and ShellCheck pass for the installer; workflow validation passes.
  These checks build and identify the functional program. They do not execute
  the full Mint suite against a live server or replace release acceptance.

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
  healing and replication with every node upgraded. For the supported local
  single-pool topology, verify tier target/versioning, transition and exact-version
  restore through restart, restored-copy expiry, failed remote cleanup retries,
  target mutation rejection, encrypted/compressed/multipart objects, and
  overwrite/healing under partial disk or metadata failure. Check signed bucket
  configuration revisions and IAM self-rotation/revocation restrictions.
- [ ] Back up local recovery metadata and referenced remote data; validate a
  recovery/rollback plan before enabling transition/restore. Older servers may
  not understand the new persisted state. Do not assume unconditional downgrade.

No tag, GitHub Release or release image is created by this preparation. Only an
actual publication and deployment record may mark acceptance steps complete.
Follow [the release guide](../releasing.md) and
[stable promotion policy](../release-promotion.md) for commands and recovery.

## 中文核对说明

本次以 10 月 7 日已发布版本及其源码 `c8a09caf` 为基线，完整覆盖截至 `458b54c`
的 12 个提交（含一个合并提交）。此前四项变更及 #33 Mint 修复继续纳入，
补齐后续镜像运行包刷新、存储类型并发修复、部署与能力文档，以及生命周期分层迁移、
版本恢复、桶配置条件更新、自助凭据和覆盖写入／修复元数据保护。
此前已发布的 HTTP、macOS、容器与存储加固不重复计入。本 PR 仅更新发布材料。

当前 `458b54c` 本地 Go 1.27.1 / macOS arm64 的定向生命周期、迁移／恢复、覆盖写入、
配置／凭据和存储类型竞态回归通过；88 项发布脚本、14 项依赖策略和七项 Mint 离线测试
通过。此前 `6f6d083` 的 CLI、OC 集成证据，以及 #33 Mint 修复的实际构建证据分别保留，
不能替代新截点的端到端验收；完整 Mint 在线功能用例尚未执行。
正式发布仍需合并后的精确 main SHA 通过门禁，
再选取全新的 UTC 时间戳标记，并实际核对产物、摘要、晋升和部署结果。
以上清单保持未完成状态，不以文档日期或本地测试冒充正式发布验收。
