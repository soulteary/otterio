# SDK, CLI and conditional-write update

This release follows `RELEASE.2026-10-07T14-09-17Z`. The reviewed main implementation
ends at `6f6d0835ddff68020f1491c403b958fade22841f`; this preparation also fixes
the Mint SDK installer. The change range, additional fix and
publication checklist are in [the release review](https://github.com/soulteary/otterio/blob/main/docs/releases/2026-10-08-release-review.md).

## Changes

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

## Upgrade

Back up data and configuration. In staging, verify account information with owner
and restricted credentials, normal S3 CRUD, multipart uploads, signed copies,
existing encrypted objects, and server/gateway shutdown. Before enabling
conditional writes in a client, check the capability header and test concurrent
creation, existing empty objects, and a destination created before multipart
completion. Preserve failures instead of falling back to unconditional writes.

Source builds require Go 1.27.1, as before. Running prebuilt artifacts does not
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

This update introduces no storage-format migration. It does not guarantee an
unconditional downgrade. Local compatibility checks and cross-compilation do
not replace runtime acceptance on each deployment platform. Before rollout,
verify the published checksums, manifest source SHA, image digests and stable
promotion results using [the release guide](https://github.com/soulteary/otterio/blob/main/docs/releasing.md).

## Acknowledgements

Thank you **Oren Yomtov of Act Security** for the previously addressed SigV4
report, analysis and reproducer. Community contribution records remain in
[ACKNOWLEDGMENTS.md](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md).

---

# SDK、CLI 与条件写入更新

本次更新接续 `RELEASE.2026-10-07T14-09-17Z`，主分支实现核对截至
`6f6d0835ddff68020f1491c403b958fade22841f`，本准备过程还修正了 Mint SDK 安装器。
完整变化、追加修复与发布验收步骤见
[发布核对记录](https://github.com/soulteary/otterio/blob/main/docs/releases/2026-10-08-release-review.md)。

## 本次变化

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

## 升级前检查

先备份数据和配置。在测试环境使用 owner 与受限凭据核对账户信息，验证普通 S3
增删读写、分片上传、合法签名复制、已有加密对象及 server/gateway 退出。
客户端启用条件写入前，先核对能力声明，再测试并发创建、已有空对象，以及分片完成前
已被其他请求创建的目标。保留条件失败，不能退回无条件覆盖写入。

源码构建继续要求 Go 1.27.1，运行预编译产物不需要安装开发工具链。
Go 集成需要同时更新模块引用与 SDK 类型、迁移自定义 gateway 工厂并重新构建；
模块路径替换不保证源码类型兼容。

此前的容器凭据策略、SigV4 校验、HTTP 生命周期修复和内部存储加固继续适用。
分布式部署应升级全部节点并验证修复与复制；历史损坏元数据会被拒绝，不会自动修复。
详见[上一正式版本](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-07T14-09-17Z)、
[容器迁移指南](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md)和
[存储加固说明](https://github.com/soulteary/otterio/blob/main/docs/security/sn-2026-002-storage-hardening.md)。

本次没有引入存储格式迁移，也不构成无条件降级保证。本地兼容检查与交叉编译不能代替
各部署平台的运行验收。发布后按[发布指南](https://github.com/soulteary/otterio/blob/main/docs/releasing.md)
核对校验和、清单源码 SHA、镜像摘要及稳定版本晋升结果，再逐步部署。

感谢 **Oren Yomtov of Act Security** 对此前 SigV4 问题提供报告、分析和复现用例。
社区贡献继续按[致谢索引](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md)保留。
