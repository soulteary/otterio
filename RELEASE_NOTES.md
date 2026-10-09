# Lifecycle, restore and client compatibility update

This release follows `RELEASE.2026-10-07T14-09-17Z`. The reviewed main implementation
ends at `458b54c9bc155780c546696a12581450adfee32b`, including the earlier SDK,
CLI and Mint preparation. The complete change range and
publication checklist are in [the release review](https://github.com/soulteary/otterio/blob/main/docs/releases/2026-10-08-release-review.md).

## Changes

- Persist remote tier references, transition/restore work and deletion intents
  so background scanning can resume interrupted work after a restart. Ordinary
  GET reads transitioned data through the source server; asynchronous restore
  creates a temporary local copy of the exact source version and preserves its
  identity and logical multipart layout. Expiry removes that copy while keeping
  remote data. Tier transitions require native, local, single-pool erasure
  storage; filesystem, distributed, multi-pool and gateway deployments are
  excluded. SELECT restore is not implemented. See the
  [lifecycle guide](https://github.com/soulteary/otterio/blob/main/docs/bucket/lifecycle/README.md).
- Protect targets still referenced by rules or persisted versions, and recheck
  queued expiration against current rules, object identity and retention state.
  Keep recovery references when uploads or healing encounter partial tier
  metadata. Ordinary uploads can replace inconsistent local metadata while
  continuing to enforce write quorum; insufficient quorum remains an error.
- Add a separate revision-based bucket configuration protocol: policy/lifecycle
  on filesystem and single-pool erasure storage, and versioning on single-pool
  erasure storage. Conditional updates require SigV4 with the condition header
  signed. This is separate from object create-only writes; check capabilities
  with a matching OC client.
- Add self-credential information and atomic secret rotation for enabled native
  IAM users. Root, service, STS and directory identities cannot self-rotate;
  distributed storage, etcd IAM and external OPA authorization disable rotation.
  A propagation error can follow a committed secret change; verify the outcome
  before retrying with old credentials.
- Synchronize storage-class configuration reads and updates, correct tier
  storage-class/restore response headers, and improve restore notifications.
- Use the published `github.com/soulteary/otterio-sdk/v7 v7.3.1` and all six
  OtterIO kits modules in the server dependency graph. Update server imports,
  examples and the Mint SDK harness. Mint now installs checksum-verified
  functional-test source from its fixed SDK version, builds without changing
  dependencies, and checks the linked SDK identity. Custom Go consumers should follow the
  [SDK and kits compatibility notes](https://github.com/soulteary/otterio/blob/main/docs/development/sdk-kits-migration-20261008.md).
- Migrate command parsing to `urfave/cli/v3 v3.14.0`. Preserve the reviewed
  command grammar, flag scope, help output, startup configuration and signal
  handling. Custom Go gateways must use the command factory API described in
  [CLI migration](https://github.com/soulteary/otterio/blob/main/docs/cli-migration.md).
- Correct owner account information using the built-in administrator policy.
  Ordinary, LDAP and delegated principals retain their applicable IAM and
  session restrictions when reporting bucket access.
- Support atomic `If-None-Match: *` for PUT and multipart completion on filesystem
  and single-pool erasure storage. Existing objects, including empty objects,
  return HTTP 412. Supported configurations advertise
  `X-Otterio-Conditional-Writes: v1`; gateways, multiple pools, write-back cache
  and uninitialized storage do not advertise it. Unsupported conditions or
  backends return HTTP 501. `If-Match` is not supported.
- Make the S3 gateway compatibility test wait for its upstream storage to finish
  initialization. This changes the test fixture, not production readiness checks.
- Cache edge image runtime packages with daily or explicit refresh; every stable
  release run refreshes independently. Reconcile English/Chinese deployment,
  lifecycle, configuration, IAM, notification and storage guides with current
  capabilities, release artifacts and OC commands.

## Upgrade

Back up data and configuration. In staging, verify account information with owner
and restricted credentials, normal S3 CRUD, multipart uploads, signed copies,
existing encrypted objects, and server/gateway shutdown. Before enabling
conditional writes in a client, check the capability header and test concurrent
creation, existing empty objects, and a destination created before multipart
completion. Preserve failures instead of falling back to unconditional writes.

Before enabling tier transitions, verify the supported topology, remote target
credentials and destination versioning. Keep remote data and credentials
available while source versions reference them. Test transition/restore/deletion
retries across restart, restored-copy expiry, encrypted/compressed/multipart
objects, target mutation rejection and overwrite/healing with degraded disks.
Use the persisted references when reviewing recovery; removing a rule does not
cancel already persisted work. Back up local metadata as well as remote data.
Test conditional bucket changes and credential rotation/revocation with owner,
restricted and delegated identities before exposing them to operators.

Source builds require Go 1.27.2. Running prebuilt artifacts does not
require the development toolchain. Go integrations must update module imports
and SDK types together, migrate custom gateway factories, and rebuild; this is
not a source-compatible module-path substitution.

The existing container credential policy, SigV4 validation, HTTP lifecycle fixes
and internal storage hardening remain applicable. Distributed deployments should
upgrade every node and test healing/replication. Historical corrupt metadata is
rejected rather than repaired automatically. See
[the previous release](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-07T14-09-17Z),
[container migration](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md) and
[storage hardening](https://github.com/soulteary/otterio/blob/main/docs/security/sn-2026-002-storage-hardening.md).

Transition and restore add persisted recovery metadata. An older server may not
understand or safely manage that state; validate a recovery/rollback plan before
enabling these features. Local compatibility checks and cross-compilation do
not replace runtime acceptance on each deployment platform. Before rollout,
verify the published checksums, manifest source SHA, image digests and stable
promotion results using [the release guide](https://github.com/soulteary/otterio/blob/main/docs/releasing.md).

## Acknowledgements

Thank you **Oren Yomtov of Act Security** for the previously addressed SigV4
report, analysis and reproducer. Community contribution records remain in
[ACKNOWLEDGMENTS.md](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md).

---

# 生命周期、版本恢复与客户端兼容更新

本次更新接续 `RELEASE.2026-10-07T14-09-17Z`，主分支实现核对截至
`458b54c9bc155780c546696a12581450adfee32b`，包括此前 SDK、CLI 与 Mint 发布准备。
完整变化与发布验收步骤见
[发布核对记录](https://github.com/soulteary/otterio/blob/main/docs/releases/2026-10-08-release-review.md)。

## 本次变化

- 持久化远端分层引用、迁移／恢复任务和删除意图，重启后由后台扫描继续未完成工作。
  普通 GET 可经源服务读取远端数据；异步恢复为指定源版本创建临时本地副本，保留版本
  身份与逻辑分片布局。副本到期后清理本地副本，保留远端数据。分层迁移仅支持原生、
  本地、单池纠删码存储，不支持文件系统、分布式、多池或 gateway；SELECT restore
  尚未实现。见[生命周期指南](https://github.com/soulteary/otterio/blob/main/docs/bucket/lifecycle/README.md)。
- 保护仍被规则或持久化版本引用的目标，并在执行排队到期任务前重新核对当前规则、
  对象身份与保留状态。上传和修复遇到部分分层元数据时保留恢复引用；普通上传可覆盖
  不一致的本地元数据，同时继续执行写仲裁，仲裁不足仍返回错误。
- 增加独立的桶配置修订协议：policy／lifecycle 支持文件系统与单池纠删码，versioning
  仅支持单池纠删码。条件更新要求 SigV4 且条件请求头参与签名；它与对象条件创建
  是独立协议，使用匹配版本的 OC 并核对服务端能力声明。
- 增加自身凭据信息，以及启用状态的原生 IAM 用户的原子密钥轮换。root、service、STS
  及目录身份不能自助轮换；分布式存储、etcd IAM 和外部 OPA 授权禁用轮换。
  密钥已保存后仍可能返回传播错误，应确认结果，不能直接用旧凭据重试。
- 同步存储类型配置的读写，修正分层存储类型／恢复响应头，完善恢复通知。
- 服务端使用已发布的 `github.com/soulteary/otterio-sdk/v7 v7.3.1` 及全部六个
  OtterIO kits 模块，同步更新服务端引用、示例和 Mint SDK 验证程序。
  Mint 按固定 SDK 版本安装经过校验的 functional-test 源码，以只读依赖方式构建，
  并检查实际链接的 SDK 身份。
  自定义 Go 集成见 [SDK 与 kits 兼容说明](https://github.com/soulteary/otterio/blob/main/docs/development/sdk-kits-migration-20261008.md)。
- 命令解析迁移到 `urfave/cli/v3 v3.14.0`，保持已核对的命令语法、参数作用域、
  帮助输出、启动配置和信号处理。自定义 Go gateway 需要迁移到命令工厂接口，
  见 [CLI 迁移说明](https://github.com/soulteary/otterio/blob/main/docs/cli-migration.md)。
- 使用内置管理员策略修正 owner 账户信息；普通用户、LDAP 用户和委派身份的
  bucket 访问结果继续受各自 IAM 与会话权限限制。
- 文件系统及单池纠删码后端支持 PUT 和分片完成时的原子 `If-None-Match: *`。
  已有对象（包括空对象）返回 HTTP 412。支持的配置通过
  `X-Otterio-Conditional-Writes: v1` 声明能力；gateway、多池、write-back 缓存
  及尚未初始化的存储不声明该能力。不支持的条件或后端返回 HTTP 501，
  当前不支持 `If-Match`。
- S3 gateway 兼容测试会等待上游存储初始化完成；仅调整测试准备过程，
  不改变生产环境的 readiness 检查。
- edge 镜像运行包按日或显式刷新后复用缓存，每次稳定版本发布独立刷新。
  同步中英文部署、生命周期、配置、IAM、通知与存储指南，使其与当前能力、发布产物
  及 OC 命令一致。

## 升级前检查

先备份数据和配置。在测试环境使用 owner 与受限凭据核对账户信息，验证普通 S3
增删读写、分片上传、合法签名复制、已有加密对象及 server/gateway 退出。
客户端启用条件写入前，先核对能力声明，再测试并发创建、已有空对象，以及分片完成前
已被其他请求创建的目标。保留条件失败，不能退回无条件覆盖写入。

启用分层迁移前，核对支持的拓扑、远端凭据及目标版本控制。源版本仍引用远端对象时，
必须保留远端数据和有效凭据。验证重启后的迁移／恢复／删除重试、恢复副本到期、
加密／压缩／多分片对象、目标变更拒绝，以及退化磁盘下的覆盖写入与修复。
恢复核对应以持久化引用为准；删除规则不会取消已经持久化的任务。
同时备份本地元数据与远端数据。向运维人员开放新接口前，以 owner、受限和委派身份
验证桶配置条件更新及凭据轮换／撤销。

源码构建要求 Go 1.27.2，运行预编译产物不需要安装开发工具链。
Go 集成需要同时更新模块引用与 SDK 类型、迁移自定义 gateway 工厂并重新构建；
模块路径替换不保证源码类型兼容。

此前的容器凭据策略、SigV4 校验、HTTP 生命周期修复和内部存储加固继续适用。
分布式部署应升级全部节点并验证修复与复制；历史损坏元数据会被拒绝，不会自动修复。
详见[上一正式版本](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-07T14-09-17Z)、
[容器迁移指南](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md)和
[存储加固说明](https://github.com/soulteary/otterio/blob/main/docs/security/sn-2026-002-storage-hardening.md)。

迁移与恢复增加了持久化恢复元数据，旧服务可能无法理解或安全管理这些状态；
启用功能前应验证恢复与回退方案。本地兼容检查与交叉编译不能代替
各部署平台的运行验收。发布后按[发布指南](https://github.com/soulteary/otterio/blob/main/docs/releasing.md)
核对校验和、清单源码 SHA、镜像摘要及稳定版本晋升结果，再逐步部署。

感谢 **Oren Yomtov of Act Security** 对此前 SigV4 问题提供报告、分析和复现用例。
社区贡献继续按[致谢索引](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md)保留。
