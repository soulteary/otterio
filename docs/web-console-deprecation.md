# Embedded Web Console deprecation history and migration gates

OtterIO's embedded Web is being replaced by the independently delivered OC Console.
This source branch is a removal candidate requested for review. It does not certify
a released replacement, completed notice cycles or a completed fallback window.
See the [candidate assessment and configuration](web-console-removal.md).

Technical migration preparation now targets the October 10 stable server to
Web-free main `c3b3665` and newer OC `26a0fa6`. Follow the
[configuration preflight and real upgrade/rollback rehearsal](console-migration-preparation.md).
The accelerated migration decision is now authorized: explicit notice plus
verified older-server/snapshot rollback replaces the original sequence for the
first approved public Web-free release containing the notice. Its revision
becomes effective only at that publication. New code and green source CI cannot
complete a historical release window or enact publication.

The [English announcement draft](releases/console-migration-announcement.en.md)
states the selected accelerated departure explicitly. Its replacement gate
is acceptance of the actual packaged pair, limited support, a tested retained-old
program/snapshot restore boundary and operator approval of publication. The notice
is not issued and its revision is not effective until the first approved public Web-free release is published
with that announcement. The published history below remains unchanged and its
old windows are not marked complete. Current draft/publication/promotion steps
are documented in the [release guide](releasing.md).

## Published history

The original notice shipped in
[OtterIO `RELEASE.2026-10-10T16-38-05Z`](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-10T16-38-05Z),
source `8cc518b31ac8b69c8f855e19b4bd027f44275c47`, published at
`2026-10-10T16:46:29Z` (2026-10-11 00:46:29 Asia/Shanghai).
That is the first notified release cycle. Its browser default remains `on`, and its
embedded login, OpenID login and legacy Web routes remain available. A source change
does not update those existing release assets.

The original notice required at least two notified OtterIO release cycles, resolved
migration blockers, a separately reviewed default-off change and an opt-in fallback
window with a published end release/date before physical deletion. As checked on
2026-10-12, a second notified release and a completed fallback window have not been
recorded. No replacement removal date or release is promised here. Preparing this
candidate does not mark those historical requirements complete.

## Replacement capability and delivery

OC `RELEASE.2026-10-10T11-42-18Z` distributes both `oc` and `oc-console`.
The image's default entry point remains `oc`; use `--entrypoint oc-console` for
the browser program. Select actual archive/checksum/source-manifest and immutable
image identities rather than inferring availability from a merged PR.

OC's newer [source candidate](https://github.com/soulteary/oc/commit/d944a0508942b278934a37908f4afc798e84504b)
implements independent native IAM and browser OIDC sessions, conditional IAM writes,
and Range downloads. [Development evidence](https://github.com/soulteary/oc/blob/d944a0508942b278934a37908f4afc798e84504b/docs/console-parallel-plans/next-iteration/handoff.md)
includes native IAM and real-browser OIDC runs against identified development
binaries. Those results do not upgrade the earlier published artifacts or certify
all capabilities on a new released pair.

Use native IAM or OIDC mode when users need separate identities. Local alias mode
shares the startup identity across browser sessions; it is suitable only where that
sharing is intended. Retain each user's actual backend permissions, including
explicit denies. Missing atomic backend capabilities must refuse writes; an OIDC
identity without an authoritative native-parent mapping cannot be granted native
IAM management by substituting administrator credentials.

## Migration and rollback boundaries

Follow the [OC Console guide](https://github.com/soulteary/oc/blob/main/docs/console.md)
and [release gates](https://github.com/soulteary/oc/blob/main/docs/console-release-migration.md).
Preserve data volumes, deployment configuration, actual S3/Admin endpoints, proxy
mappings, credential identity and both CA trust settings. In split mode,
`--console-address` and `--console-certs-dir` still configure OtterIO's Admin
listener. The environment equivalents `OTTERIO_BROWSER_ADDRESS` and
`OTTERIO_BROWSER_CERTS_DIR` remain compatible. Example ports 9000/9001 do not
override an existing deployment's addresses; OC's browser endpoint is separate.

On a version that still includes the embedded Web, opt-in migration uses
`OTTERIO_BROWSER=off`; same-server UI rollback uses `on` and restores the intended
access mapping. This restores only the UI. It does not undo objects, policies,
secret rotation or lifecycle transitions.

In this removal candidate, omission or `OTTERIO_BROWSER=off` permits startup;
explicit `on` fails with guidance to OC. The old webpage cannot be restored with
an environment toggle after its code has been removed. A server rollback therefore
requires a retained supported version and prior data/configuration compatibility
validation; do not require or assume an unverified storage downgrade.

## Remaining release gates

The checklist below preserves the original outstanding commitments. The
selected accelerated decision records an explicit departure from that sequence
rather than completing or silently deleting it. Its revision becomes effective
only at the first approved public Web-free release containing the notice.
An official successful packaged-pair run is a prerequisite for publication;
it does not authorize publication or make the selected revision effective by itself.

The newer [darwin/arm64 source rehearsal](console-migration-preparation.md#verified-development-upgrade-on-2026-10-13-asiashanghai)
now verifies the October 10 published programs → clean candidate → old-program
rollback, with the recorded four-directory/TLS/identity fixture and one new-object
write. It supplies that limited upgrade evidence; it does not complete the
released-pair, other promised-profile or notification/fallback gates below.

The [published-pair evidence](https://github.com/soulteary/oc/blob/d944a0508942b278934a37908f4afc798e84504b/docs/console-released-pair.md)
certifies only the exact older versions and profiles it records. Its IAM creation
gap is addressed by newer development protocols, which still require a compatible
published replacement pair. The [28-entry capability gate](https://github.com/soulteary/oc/blob/d944a0508942b278934a37908f4afc798e84504b/docs/console-parity-gate.md)
has not certified a complete released replacement.

- [ ] Publish and verify the new OC/OtterIO pair, exact source commits, archive
      checksums and image digests; run supported native platforms and both Linux
      amd64/arm64 container architectures.
- [ ] Validate promised single/dual HTTP/TLS, independent CAs, proxies, server/gateway
      modes and identity profiles against those actual release programs.
- [ ] Complete object/settings/IAM, permission/deny, cancellation, uncertain outcome,
      OIDC lifecycle and resource-stability acceptance; record deliberate scope changes.
- [ ] Complete previous-version upgrades, preferences/data persistence and supported
      UI/server/TLS/access rollback with clear compatibility boundaries.
- [ ] Complete at least two notified release cycles and resolve active migration
      blockers, then review default-off separately with a published fallback deadline.
- [ ] Complete that fallback window before release approval of physical Web deletion.

S3, Admin, STS, health, metrics, node communication, shared JWT authentication,
terminal printing and Admin logs remain required. The user's requested source
removal is documented separately from these outstanding release requirements.

---

# 内置 Web Console 弃用历史与迁移门槛

内置网页迁移到独立 OC Console。本源码分支按用户要求提供可审查的删除候选，
不表示正式替代发行、通知周期或回退窗口已经完成。配置与删除边界见
[删除分析](web-console-removal.md)。

本轮提前做技术适配，以 10 月 10 日稳定版到 `c3b3665` 和 OC `26a0fa6` 为
基线，按[配置预检与真实升级回退指南](console-migration-preparation.md)演练。
这不废除历史通知／回退策略；若提前公开发行无内置 Web 版本，须明确记录策略
调整、支持范围与替代回退边界，不能用源码或 CI 通过代替已承诺的发行窗口。

首次公告已随 `RELEASE.2026-10-10T16-38-05Z` 发布，发布时间为北京时间
2026-10-11 00:46:29。这只是第一个通知发行周期；该旧发行物仍默认开启内置网页。
历史要求至少两个已通知发行周期、解决迁移阻塞后单独审查默认关闭，再公布有期限
的显式开启回退窗口，窗口结束后审查删除。截至 2026-10-12，第二次通知和窗口
结束均无完成记录。本候选不伪称这些要求已经满足，也不承诺新的删除日期或版本。

OC 最新源码已实现 native IAM、浏览器 OIDC 独立会话与条件 IAM 协议，并有真实
浏览器开发验收；不能把更早正式 OC 程序当作已经获得新功能。多用户使用相应身份
模式及每个用户自身的服务端权限，不共享管理员 alias。缺少原子协议或权威 native
父关系时拒绝相应写入，不借管理员权限补齐。

迁移保留 S3/Admin 地址、代理映射、数据卷、身份和独立 CA；`--console-address`、
`--console-certs-dir` 与对应环境变量继续配置 Admin 监听器，9001 不是 OC 网页。
含旧 Web 的版本可通过 `off`／`on` 切换网页；本删除候选省略或 `off` 可启动，
显式 `on` 报错并引导 OC，不能通过开关恢复已删除代码。回退到保留的旧服务端前
必须验证数据／配置兼容，不能把网页回退等同于撤销对象、策略、密钥或生命周期变更。

上方清单仍要求新的正式配对、能力与权限矩阵、全平台和双架构运行、真实升级回退、
稳定性、通知周期与回退期限。开发证据不能替代发行认证。保留 S3/Admin/STS、
health/metrics、节点通信、共享 JWT、终端打印及管理日志。

新的 [darwin/arm64 源码演练](console-migration-preparation.md#verified-development-upgrade-on-2026-10-13-asiashanghai)
已完成报告所列四目录／TLS／身份样本的真实跨版本升级、旧程序回退和新增对象／
快照检查；它补足这一限定开发范围，不代表上方正式配对、其他支持场景和历史
通知／回退窗口已完成。
