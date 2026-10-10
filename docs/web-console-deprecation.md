# Embedded Web Console deprecation notice

The embedded Web Console is deprecated in favor of the independently delivered
OC Console for local, single-identity operation. This notice takes effect when
it is included in a published OtterIO release; merging this document is not a
release or the start of a completed deprecation cycle.

## What remains available

`OTTERIO_BROWSER` still defaults to `on`. Existing deployments keep the embedded
page, browser login, OpenID login and legacy Web routes. Set it to `off` only
after validating your replacement. No removal date or removal release is fixed.

This deprecation concerns the embedded page, its static assets, Web JSON-RPC
and Web-specific upload/download/ZIP routes. It does not deprecate S3, Admin,
STS, health, metrics, node communication, terminal printing or admin log streams.
`--console-address` and `--console-certs-dir` remain supported: the dedicated
listener also serves Admin, including when the embedded Web is disabled.
Shared JWT authentication is still used outside the Web and must be retained.

## Replacement scope and installation

Read the [OC Console guide](https://github.com/soulteary/oc/blob/main/docs/console.md)
and [release gates](https://github.com/soulteary/oc/blob/main/docs/console-release-migration.md).
OC provides browsing, object writes, downloads, ZIP, presigned sharing, bucket
settings and IAM management, subject to permissions and server capabilities.

The console runs with one startup alias identity; all its browser sessions share
that identity. It is not a replacement for independent-user browser login or
OIDC/centralized deployments. Keep the embedded Web for those deployments until
a replacement or an explicit supported-scope decision is published. Do not
relabel container wildcard listening as multi-user deployment support.

OC PR #19 adds both executables to the timestamp release builder and release
image. Do not infer availability from the merged PR: select a published release
containing `oc-console`, check its checksums/source manifest and image digest,
and pin both OC and OtterIO versions. Older releases are unchanged. The archive
contains both executables; the image keeps `oc` as its default entry point and
uses `--entrypoint oc-console` for the Web program. Source builds remain an option.

## Opt-in migration and rollback

Back up deployment configuration and retain the current server image/binary.
Keep data volumes and the selected identity's endpoint, addressing and CA settings.
Start OC against the existing S3 endpoint and the actual Admin endpoint. In split
mode, S3 is on 9000 and Admin is on 9001; neither is OC's browser endpoint.
The OC bridge example exposes its browser only at host loopback 9090.

After verifying the replacement, set `OTTERIO_BROWSER=off` and restart/recreate
OtterIO using the same version and data volumes. With a split listener, keep
`--console-address :9001` and configure OC's `--admin-url` accordingly. Do not
remove the management listener or its TLS configuration. Single-port deployments
continue using their shared endpoint. Preserve separate CA trust where configured.

For UI rollback, set `OTTERIO_BROWSER=on` and restart/recreate the same server.
If the management port was internal-only, restore the intended browser access
mapping; the local Compose example can publish 9001 on host loopback. Do not
expose the port more broadly as part of rollback. This restores the UI, not
previous objects, policies, lifecycle settings or credentials. It does not require
a server downgrade. Any storage downgrade needs separate data/configuration
compatibility verification, especially for lifecycle transition references.

## Acceptance checklist before changing defaults or deleting routes

Record results against exact release tags, source commits, binary SHA-256 values
and image digests. An unchecked item is pending, not a supported compatibility
claim. Historical source/patch-profile results cannot establish a new released pair.

- [ ] Publish OC archives containing both executables and verify checksums,
      release identity, native execution on supported platforms, and both Linux
      amd64/arm64 image architectures.
- [ ] Validate actual released OC/OtterIO pairs in single/dual HTTP and TLS,
      independent CAs and supported proxy/container configurations.
- [ ] Verify browser-off behavior: the old page and Web RPC/upload/download/ZIP
      routes are unavailable; S3, Admin, STS, health and metrics still work on
      their intended listeners with their normal authentication requirements.
- [ ] Verify browse, upload/download, deletion, ZIP, bucket settings and IAM,
      restricted identities/explicit denies, cancellation and uncertain results.
      Missing server capabilities must refuse mutations or use the documented
      read-only behavior; never substitute administrator permissions.
- [ ] Verify OC preference persistence after restart and a real deployment
      upgrade, same-server UI rollback and TLS/access configuration rollback.
- [ ] Publish a decision and migration path for independent-user/OIDC use.
- [ ] Publish the notice in at least two notified OtterIO release cycles, record
      their tags/dates and resolve active migration blockers. These are release
      events, not PR merges, and no fixed elapsed-day substitute is defined.
- [ ] Only then review a separate default-off change with an explicitly bounded
      opt-in fallback window; publish its end release/date before starting it.
- [ ] After that window, review Web-specific deletion separately, preserving
      shared authentication, protocols, node communication and admin logging.

## Current evidence and status

OC PR #19 was merged on 2026-10-10. At preparation of this notice, the latest
published OC tag was `RELEASE.2026-10-09T17-33-03Z`, predating that change;
this is a dated snapshot, not a claim about future release availability.
The [browser-off source acceptance](https://github.com/soulteary/oc/blob/main/docs/console-browser-off-results.json)
records OtterIO source `f0830a2c357043f0b8f821f49e899cf6daeadbb3` and its
actual binary identities. It is useful evidence, but does not complete the
released-pair, native-platform, upgrade/rollback or deprecation-window gates.

No checklist item above is certified by this documentation PR. No runtime default,
route, static asset, credential handling or storage format changes here.

---

# 内置 Web 控制台弃用公告

内置 Web 控制台将逐步迁移到独立交付的 OC Console。本公告随 OtterIO
正式发行后生效；合并文档不代表发布，也不算已经完成一个弃用周期。
当前 `OTTERIO_BROWSER` 默认仍为 `on`，旧网页、账号登录和 OpenID 登录继续可用，
尚未指定删除日期或版本。

本阶段针对本机、单一身份操作。OC 所有浏览器会话共享启动 alias 的身份，
尚不能替代独立用户登录、OIDC 或多人集中部署；这些使用者应保留旧 Web，
直到公布替代方案或明确支持范围。先确认选定的正式 OC 版本实际包含
`oc-console`，核对校验和与镜像摘要，再固定两端版本。PR #19 合并不等于
已有包含它的正式发行版，旧发行物不会自动更新。

迁移时保留数据卷、现有服务端版本、S3/Admin 地址与独立 CA 配置。
验证 OC 后设置 `OTTERIO_BROWSER=off` 并重建／重启同版本服务。
`--console-address` 同时承载 Admin API，不能随网页关闭一起删除。
恢复旧网页只需改回 `on` 并重启同一服务端，必要时恢复原先的浏览器访问映射；
无需降级存储。界面回退不会撤销对象写入、策略变更、密钥轮换或生命周期转换。

上方验收清单均为待验收项目，要求固定版本和程序摘要、正式发行组合、
单／双入口 HTTP/TLS、权限回归、双架构镜像、真实升级与回退，以及 OIDC
支持范围的明确决定。至少经过两个已通知的正式发行周期并消除迁移阻塞后，
才单独审查默认关闭；公布有期限的显式开启回退窗口，窗口结束后再审查删除。
保留 S3、Admin、STS、health、metrics、节点通信、共享 JWT 和终端日志功能。
