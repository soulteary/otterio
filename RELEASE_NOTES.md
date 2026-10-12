# OC Console migration and native IAM authority — release preparation

This preparation covers the production changes after
`RELEASE.2026-10-10T16-38-05Z` (`8cc518b31ac8b69c8f855e19b4bd027f44275c47`)
through `c3b3665154264993fa06a28e95ab25fdd03d7c0c`: conditional native IAM
protocols and current-parent STS authorization (#41), followed by embedded Web
removal (#42). It does not create a release or certify future release artifacts.

## Changes

- Add conditional native IAM policy bindings and identity relationships for
  user, group, service-account and self-secret changes. Relationship revisions bind
  the full native IAM graph and affected access keys; signed conditions, permissions and identity
  are checked again under the persistence lock. Creation cannot replace an
  existing identity. Conflicts, expired proofs and server restarts require a
  fresh read and review; uncertain write outcomes must be checked before retry.
- Evaluate native AssumeRole credentials against both their issuance authority
  and the parent's current direct/group permissions. A missing, deleted or
  disabled native parent denies access. WebIdentity and LDAP retain their
  distinct authority models; an external identity is not a guessed native parent.
- Remove embedded browser assets and build dependencies, Web JSON-RPC,
  browser login/URL tokens, Web upload/download/ZIP adapters and redirects.
  Web access moves to the independently deployed `oc-console`. S3, Admin,
  STS/OpenID validation, health, authenticated metrics, node communication,
  shared JWT/TLS and management logs remain server responsibilities.
- Withdraw ordinary-user platform/runtime, global-capacity and full
  prefix-policy summaries formerly exposed through Web RPC. These disclosures
  are deliberately outside the replacement scope.

Conditional IAM is declared only for an initialized, single-writer native IAM
object store without distributed erasure, shared etcd or external OPA. Legacy
Admin clients remain compatible; OC must refuse unsupported conditional writes
or use its documented read-only behavior. Newer OC source implements native IAM
and browser OIDC; an older OC release does not acquire those features by changing
the server. Use an identified compatible pair and each user's actual backend
permissions. Local alias mode shares its startup identity and must not substitute
a shared administrator for independent users.

## Configuration, migration and rollback

Leave `OTTERIO_BROWSER` unset or set it to `off`. Explicit `on` fails startup with
OC guidance; it cannot restore deleted code. `--console-address`,
`--console-certs-dir`, `OTTERIO_BROWSER_ADDRESS` and
`OTTERIO_BROWSER_CERTS_DIR` retain their Admin-listener meanings. Preserve the
actual S3/Admin ports, proxy routes and independent CA trust. Example port 9001
is Admin; the Web application listens separately in OC Console.

Before changing programs, stop writes, retain the old verified program/image and
deployment configuration, and take a consistent backup including backend
configuration/IAM and the credentials that decrypt them. Do not rotate root
credentials as part of this UI migration. Rehearse the actual old release → new
build → old release sequence on isolated data. Verify object bytes, restricted
permissions, settings, listener/TLS configuration and the state your deployment
promises to preserve. New writes, IAM edits, secret rotation, lifecycle transitions
and external systems require their own rollback plan; restoring the old UI does
not undo them. See the [migration and configuration preflight](docs/console-migration-preparation.md).

An actual published-old → clean candidate → published-old rehearsal passed on
darwin/arm64, using single-node erasure with four storage directories and shared/split TLS. It preserved
the seeded object/version/tag/IAM/deny state, local preferences and isolated native
preferences, verified rollback after one new object and restored the pre-upgrade
snapshot. Read the [exact report and limits](docs/console-migration-preparation.md#verified-development-upgrade-on-2026-10-13-asiashanghai);
this is candidate source evidence, not new release or all-platform/OIDC-upgrade certification.

The first deprecation notice shipped in the October 10 stable release. The
published two-notice requirement, separately reviewed default-off transition and
completed fallback window have not been fulfilled. Accelerating technical
adaptation does not waive that policy: a public Web-free release needs either its
completion or an explicit, documented policy decision and replacement rollback
boundary. No new removal date is promised. See the
[notice history and outstanding gates](docs/web-console-deprecation.md).

Go 1.27.2 and the earlier version/tag/cache authorization fixes are retained from
the stable baseline, not new dependency or permission changes in this range.
Source validation applies only to its named programs and profiles. Follow the
[release guide](docs/releasing.md) for checks on the eventual exact main commit,
archive checksums, source manifest and immutable image digests; repeat applicable
acceptance on the resulting artifacts before promotion. The release pipeline's
Linux amd64 smoke test does not certify every cross-compiled platform or deployment.

---

# OC Console 迁移与原生 IAM 权限更新——发布准备

本次准备以 `RELEASE.2026-10-10T16-38-05Z`
（`8cc518b31ac8b69c8f855e19b4bd027f44275c47`）为基线，覆盖截至
`c3b3665154264993fa06a28e95ab25fdd03d7c0c` 的全部生产改动：#41 的条件
native IAM 协议与 STS 当前父账户授权，以及 #42 的内置 Web 移除。本次准备
不创建正式发行，也不代表未来发行产物已通过验收。

## 本次变化

- 用户、组、服务账户、策略绑定和自身改密使用条件 native IAM 协议。关系修订
  绑定完整原生 IAM 图及受影响 access key，持久化锁内再次检查已签名条件、权限与身份；
  创建不能覆盖已有身份。冲突、证明过期或服务端重启后，须重新读取并确认；
  写入结果不确定时先核对实际状态，再决定是否重试。
- 原生 AssumeRole 临时凭据同时受签发时权限与父账户当前直接／组权限限制。
  父账户不存在、已删除或已禁用时拒绝访问。WebIdentity 与 LDAP 保留各自的
  权限模型，不把外部身份猜测为原生父账户。
- 删除内置网页资产及构建依赖、Web JSON-RPC、网页登录／URL token、Web
  上传／下载／ZIP 适配和重定向。浏览器应用由独立部署的 `oc-console` 提供。
  服务端保留 S3、Admin、STS/OpenID 验证、health、带认证的 metrics、节点通信、
  共享 JWT/TLS 与管理日志。
- 明确撤销旧 Web RPC 向普通用户暴露的平台／runtime、全局容量与完整策略
  前缀摘要；这些信息不属于替代范围。

条件 IAM 仅在已初始化、单写入者的原生 IAM 对象存储下声明支持，不适用于
分布式纠删码、共享 etcd 或外部 OPA。旧 Admin 客户端保持兼容，OC 对缺少协议
的写入必须拒绝或采用其明确的只读行为。新 OC 源码已实现 native IAM 与浏览器
OIDC，不能把旧 OC 正式程序当作已经获得这些功能。选用身份明确的兼容配对，
保留每个用户自身的后端权限；local alias 模式共享启动身份，不能用共享管理员
替代独立用户。

## 配置、迁移与回退

省略 `OTTERIO_BROWSER` 或设置 `off` 可启动；显式 `on` 报错并引导 OC，无法
恢复已删除代码。`--console-address`、`--console-certs-dir` 及对应
`OTTERIO_BROWSER_ADDRESS`、`OTTERIO_BROWSER_CERTS_DIR` 继续配置 Admin
监听器。保留现有 S3/Admin 端口、代理映射和独立 CA；9001 示例是 Admin 端口，
网页使用 OC Console 自身的地址。

更换程序前停止写入，保留经校验的旧程序／镜像和部署配置，取得包括后端配置、
IAM 及解密凭据的一致备份。不要把 root 凭据轮换混入网页迁移。在隔离数据上
实际演练旧正式版 → 新构建 → 旧正式版，核对对象内容、受限权限、设置、监听／TLS
以及部署承诺保留的状态。新增写入、IAM 修改、密钥轮换、生命周期转换和外部
系统另需回退方案；恢复旧网页不会撤销这些变化。操作见
[迁移与配置预检](docs/console-migration-preparation.md)。

已在 darwin/arm64 单节点四目录纠删码样本与单／双 TLS 上，实际通过旧正式版 → 干净
候选 → 旧正式版演练：保留样本对象／版本／标签、IAM／拒绝权限、local 偏好和
隔离的 native 偏好；确认新增一个对象后的回退读取和升级前快照恢复。见
[准确报告与边界](docs/console-migration-preparation.md#verified-development-upgrade-on-2026-10-13-asiashanghai)。
这是候选源码证据，不是新发行、全平台或 OIDC 升级认证。

10 月 10 日稳定版只完成首次弃用通知。历史要求的两次通知、单独审查默认关闭
和完整回退窗口尚未满足。提前完成功能适配不等于废除该策略；公开发行无内置
Web 的版本前，应完成原策略，或明确记录调整策略的决定和新的回退边界，不
承诺新的删除日期。见[公告历史与待完成门槛](docs/web-console-deprecation.md)。

Go 1.27.2 和此前版本／标签／缓存授权修复沿用稳定版基线，不是本范围新增的
依赖或权限改动。源码验收只覆盖报告指定的程序和场景。按[发布指南](docs/releasing.md)
核对最终主分支提交、校验和、源码清单与固定镜像摘要，并在晋升前对实际产物
重跑适用验收；发行流水线的 Linux amd64 冒烟不能证明所有平台和部署均可运行。
