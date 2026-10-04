# Safer containers, refreshed dependencies and verified releases — October 2026

This update builds on `RELEASE.2026-10-04T09-24-10Z`. The new changes are
streaming-response concurrency fixes, safer container startup, local-source
Docker builds, staged release publication, dependency refreshes and CI
improvements (#18–#24). The SigV4 and Windows fixes described below were already
included in that previous release; they remain important for users upgrading
from the June releases.

## Upgrade compatibility: read before replacing a container

| Area | Required action |
| --- | --- |
| Container credentials | `server` and `gateway` now require an explicit, non-default credential pair. Commands that previously started with default credentials will stop. Configure credentials before upgrading. |
| Secret files | Each value supports its own `_FILE`. Explicit paths must be readable, regular and nonempty; an environment value plus an existing file for the same setting is rejected. Check mounts and permissions. |
| Non-root deployment | The secure Compose profile opts into a fixed non-root UID/GID; the image's default UID and existing volume ownership are not silently changed. Plan permissions before selecting the profile. |
| Source builds | Go **1.27.1** is the minimum. Browser development uses Node **24.21.0** and Bun **1.4.2**. Running a prebuilt binary/image does not require these tools on the host. |
| Custom S3 clients | Retain the previous release's signing migration: supply operation headers when signing, not afterwards. Do not disable signature verification to bypass a rejection. |

Modern `OTTERIO_ROOT_USER` / `OTTERIO_ROOT_PASSWORD` and legacy
`OTTERIO_ACCESS_KEY` / `OTTERIO_SECRET_KEY` pairs remain supported; do not mix
half of each pair. A username environment variable and password file can now be
used together. The container-entrypoint credential policy does not change
bare-metal binary startup. `--help` and `--version` still work without credentials.
`OTTERIO_ALLOW_DEFAULT_CREDENTIALS=1` is only an explicit opt-in for an isolated,
loopback-only local demo, not a production migration strategy.

See the [container migration guide](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md)
and [secure Compose profile](https://github.com/soulteary/otterio/blob/main/docker-compose.secure.yml).

## New in this update

- **Streaming-response reliability (#19).** Publish an immutable snapshot of
  headers, status and pre-header panic state across goroutines. Later header
  mutation or a post-header panic no longer rewrites committed response state;
  stream failures and consumer cleanup have focused regression coverage.
- **Docker builds use your source (#20).** Build from the supplied checkout
  instead of cloning remote `main`, including the committed browser assets.
  Explicit source metadata is validated; unknown identity is reported as
  `unknown`, not invented. This is source correctness, not a claim of
  byte-for-byte reproducible builds.
- **Safer container startup (#21).** Validate credentials and resolve secret
  files independently, including KMS/SSE settings, without printing secret
  values in errors. The opt-in secure profile adds a non-root user, loopback
  ports, a read-only root filesystem, bounded temporary storage, dropped
  capabilities and `no-new-privileges`.
- **Staged publication and digest promotion (#22).** Keep the exact-commit main
  CI gate. Build fixed-version images without moving `latest`, smoke-test the
  pushed GHCR digest on Linux amd64, upload assets to a draft, then download and
  compare every asset before publication. `release-manifest.json` records the
  source commit and image identities. Serialized promotion verifies recorded
  digests and version ordering before updating registry aliases and finally
  GitHub's latest marker; delayed older releases cannot roll it back through
  this workflow.
- **Dependencies and embedded console (#23).** Move to Go 1.27.1 and refresh
  dependencies including Fiber 3.5.0, fasthttp 1.74.0, etcd 3.7.2, minio-go 7.3.0,
  gRPC 1.84.0 and protobuf 1.36.12. Refresh the browser toolchain, including React
  19.3, React Router 7.18, Babel 8 and Jest 30.5; regenerate Go code, the browser
  lockfile and the actual embedded production assets. See the
  [version and compatibility record](https://github.com/soulteary/otterio/blob/main/docs/development/dependency-upgrade-20261004.md).
- **CI toolchain consistency and race diagnostics (#24).** Read Go versions from
  the checked-out `go.mod`, separate module/build caches and remove an unused
  ordinary-build prerequisite from the race target. Retain full race execution
  with `-count=1`, fail on package-discovery errors, and collect test/resource
  diagnostics. Tests are not shortened or sharded; no measured speedup is claimed.

## Retained security and compatibility fixes

The previous October release fixed SigV4 request-header coverage (#13). An
otherwise valid delegated upload could acquire server-side copy semantics through
an unsigned operation header, using the signing identity's source-read
permissions. Reading copied bytes additionally required destination-read access.
The shared verifier now rejects unsigned S3/OtterIO operation headers with
HTTP 403 `AccessDenied` for presigned URLs, Authorization signatures and streaming
seed signatures. Legitimate signed copies, the payload-hash exception and
matching signed query values remain supported. Server-loaded object tags no
longer mutate signed request headers.

The affected code was confirmed in `RELEASE.2026-06-07T11-32-46Z`. Users still on
that version should upgrade and verify the actual running artifact rather than
trust an older cached `latest`. No OtterIO CVE assignment is asserted here; a
related identifier in another project is not an OtterIO identifier.

Also retained are Windows directory-enumeration/startup fixes and startup/S3
regressions (#12), deterministic dynamic-timeout test samples without production
behavior changes (#14), and community spelling improvements (#3, #5, #7, #9).

## Acknowledgements

Thank you **Oren Yomtov of Act Security** for privately reporting the SigV4 issue
and providing analysis and a reproducible test case. This uses the reporter's
approved public attribution, reflected in #18; contact details and private
correspondence remain unpublished.

Thank you [@luojiyin1987](https://github.com/luojiyin1987) for the spelling fixes
in #3, #5, #7 and #9; [@929496959](https://github.com/929496959) for the Windows
report, diagnosis and proposed fix in #10; and
[@MikhailIzvekov](https://github.com/MikhailIzvekov) for the patch proposal in
#11. Its approach was retained and extended in #12; #11 itself was not merged.
See the [contribution records](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md).

## Rollout and limits

Back up data and configuration and test restoration. In staging, verify
credential loading, storage permissions, Windows startup where applicable,
normal S3 upload/download/delete and properly signed copy operations. Pin a
published version or verified digest, check binary SHA-256 values, and compare
image identity with `release-manifest.json` before broad rollout. The
[release guide](https://github.com/soulteary/otterio/blob/main/docs/releasing.md)
describes publication and recovery; the older baseline release predates the
new manifest/promotion flow.

Publication across GitHub and registries is not atomic. The manifest records
identity; it is not a signature, attestation or guarantee of reproducibility.
The pushed-image S3 smoke test runs on Linux amd64, not every image architecture.
No storage-format migration is introduced by the changes summarized here, but
this is not an unconditional downgrade guarantee. This update does not certify
that every inherited security issue has been resolved or replace access controls,
TLS, backups and an application-specific deployment review.

---

# 容器安全、依赖升级与可验证发布 — 2026 年 10 月

本次更新基于 `RELEASE.2026-10-04T09-24-10Z`，新增内容包括流式响应并发修复、
容器凭据与密钥文件校验、本地源码构建、分阶段发布、依赖升级及 CI 改进（#18–#24）。
SigV4、Windows 启动及动态超时测试修复已包含在上一版中，本次继续保留；
仍在使用六月版本的用户需要同时关注这些修复。

## 升级前必须检查

容器的 `server` / `gateway` 命令现在要求显式设置非默认凭据。
过去省略凭据就能启动的命令会停止，不能直接沿用。现代和旧版凭据变量仍然支持，
但不能混用两组变量的半对值。用户名环境变量与密码文件可以独立组合；
显式指定的 `_FILE` 必须可读、为普通文件且非空，环境值与同一设置的已有文件冲突会报错。

容器入口脚本的新策略不改变裸机二进制的启动行为。镜像默认 UID 和已有数据目录归属
不会被自动修改；非 root 运行通过安全 Compose 配置显式启用，使用前应检查文件权限，
不要直接递归修改生产数据目录。默认凭据的例外开关仅用于回环地址上的隔离本地演示。

源码构建最低要求 **Go 1.27.1**；浏览器开发使用 **Node 24.21.0 / Bun 1.4.2**。
直接运行发布二进制或容器不要求宿主机安装这些工具。
自定义 S3 客户端仍须在签名时提供操作请求头，不得在签名后追加或通过关闭校验绕过拒绝。

## 本次主要变化

- **流式响应与构建可靠性：** 用不可变快照同步响应头、状态和异常，避免跨协程读取
  可变状态；Docker 直接使用当前构建上下文，不再另行克隆远程 `main`。
- **容器部署：** 独立加载凭据及 KMS/SSE 文件并检查冲突；安全 Compose 配置提供
  非 root、回环端口、只读根文件系统、受限临时目录、删除 capabilities 和禁止提权。
- **发布链路：** 先构建固定版本镜像，按已推送摘要完成 Linux amd64 的 S3 冒烟验证；
  草稿附件上传后逐个下载比对，再发布 Release。新增 `release-manifest.json` 记录源码
  提交与镜像摘要。最后串行校验并提升 `latest`，避免较旧任务覆盖较新的稳定版本。
- **依赖与控制台：** 升级 Go、Fiber、fasthttp、etcd、S3 SDK 及浏览器依赖，重新生成
  Go 代码、锁文件和实际嵌入的前端产物，不是只修改版本声明。
- **CI：** 从 `go.mod` 读取工具链，调整编译缓存并增加竞态测试诊断；保留完整竞态执行，
  不以缓存成功结果、减少测试或放宽超时换取“提速”，也不宣称未经测量的性能收益。

上一版的 SigV4 修复拒绝未被签名覆盖的 S3/OtterIO 操作请求头，合法签名的复制请求仍可使用。
本说明不宣称 OtterIO 获得了专属 CVE 编号，也不把其他项目的编号当作本项目编号。

**特别感谢 Oren Yomtov of Act Security 的安全反馈、分析与复现用例。**
按其确认的署名公开致谢，不公开联系方式或私人邮件。
同时感谢 @luojiyin1987、@929496959 和 @MikhailIzvekov 的拼写改进、Windows 问题分析
及补丁建议；#11 的思路在 #12 中完善，原 PR 未直接合并。

部署前请备份并验证恢复，在测试环境检查凭据、目录权限、S3 操作与签名兼容性，
使用明确版本、摘要及校验和确认实际产物。多个发布平台之间不是原子事务，
身份清单不是签名或可复现构建证明；本次更新也不代表所有继承的安全问题均已解决。
