# OC Console replacement and embedded Web removal candidate

This branch separates the browser interface from OtterIO: OC Console provides
the Web application while OtterIO retains storage, administration and identity
protocols. The removal reduces maintenance of the embedded front end
and the storage server's exposed Web handlers. Release migration gates remain
open; this candidate is not a claim that production retirement is certified.

## Current migration preparation

The removal is now in main at `c3b3665154264993fa06a28e95ab25fdd03d7c0c`.
The [current migration guide](console-migration-preparation.md) covers configuration
preflight and actual October 10 stable → candidate → rollback rehearsal with
OC `26a0fa6c4d2f380ff9eba84c9dcb91a378b73c27`. The
[release body](../RELEASE_NOTES.md) covers the full production delta from that
stable baseline, including native IAM/STS authority. A new
[darwin/arm64 rehearsal](console-migration-preparation.md#verified-development-upgrade-on-2026-10-13-asiashanghai)
passed actual published-old → clean candidate → published-old on a four-directory
erasure fixture and both TLS profiles, including one new-object write and snapshot
recovery. This source result does not certify new release artifacts, all
platforms or OIDC upgrades. It is recorded separately; all dated assessments and reports below remain evidence
of their original program/source identities. Merging removal code does not
complete the published notice/default-off/fallback sequence or authorize release.

当前迁移指南已补入 darwin/arm64 四目录纠删码样本、单／双 TLS 的真实旧正式版 →
候选 → 旧正式版结果，包括新增对象后的回退读取及快照恢复。以下有日期的
评估和报告保留其原程序／脚本身份；新结果不代表正式产物、全平台或 OIDC
升级已验收，也不完成通知／默认关闭／回退窗口。

## Assessment as of 2026-10-12

The reviewed bases are OtterIO `703eef50e36c3ef163810561c8cd4f7f8c7c1d28`
and OC `d944a0508942b278934a37908f4afc798e84504b`. At this check, the latest
published stable tags remain OtterIO `RELEASE.2026-10-10T16-38-05Z` and OC
`RELEASE.2026-10-10T11-42-18Z`; they do not contain the full newer source candidate.
Evidence below applies to its named binaries and fixtures, not automatically to
this branch or every later merge.

- Object browsing, writes, versions, downloads, ZIP and sharing have OC replacements
  through signed storage operations. New development combinations have feature,
  settings, copy and Range acceptance; a complete new released pair remains pending.
- Native IAM login uses independent session clients and backend permissions. The
  newer server provides conditional identity relationships and user/group/service
  mutations with locked authorization checks. Older servers lacking the declared
  protocols must refuse those writes.
- Browser OIDC is implemented, with real Keycloak login/refresh/logout, fixed
  session expiry, browser isolation and independent S3/Admin CA development evidence.
  This supports continued OIDC development, not a certification of the old release.
  Unknown external-to-native parent relationships remain unknown and disable native
  IAM management/self-secret changes where that authority is required.
- OC provides authorized management diagnostics. Ordinary-user platform/runtime,
  global capacity and full prefix-policy summaries from old Web RPC are deliberately
  withdrawn here to reduce sensitive infrastructure and policy disclosure. No
  claim of one-for-one equivalence is made for those legacy disclosures.
- Published-image trial deployment is verified only for the recorded local alias,
  single-instance Linux arm64 scope. New identity modes, all supported platforms,
  amd64 runtime, proxy/gateway coverage, stability and full upgrades need their own
  evidence. The complete capability matrix remains uncertified for release.
- The first notified OtterIO release exists. A second notified release, separate
  default-off transition and completed fallback window are not recorded. This
  authorized source candidate does not count as those release events.

Source-specific OC evidence: [handoff](https://github.com/soulteary/oc/blob/d944a0508942b278934a37908f4afc798e84504b/docs/console-parallel-plans/next-iteration/handoff.md),
[real-browser OIDC](https://github.com/soulteary/oc/blob/d944a0508942b278934a37908f4afc798e84504b/docs/console-oidc-browser-acceptance.md),
[deployment limits](https://github.com/soulteary/oc/blob/d944a0508942b278934a37908f4afc798e84504b/docs/console-next-deployment-release.md)
and [capability gate](https://github.com/soulteary/oc/blob/d944a0508942b278934a37908f4afc798e84504b/docs/console-parity-gate.md).
Earlier Range/STS/wrapper failures retain their original reports; later-source
success does not establish the cause of unresolved initial failures.

## Configuration and migration

S3 and Admin remain shared on `--address` unless `--console-address` selects
a separate Admin listener. The existing names are retained for compatibility:

```sh
otterio server \
  --address ":9000" \
  --console-address ":9001" \
  --certs-dir /etc/otterio/certs/s3 \
  --console-certs-dir /etc/otterio/certs/console \
  /data
```

`OTTERIO_BROWSER_ADDRESS` and `OTTERIO_BROWSER_CERTS_DIR` retain those Admin
address/TLS meanings. Keep the actual configured addresses, proxy mappings and
independent CA trust; examples do not replace custom ports. Configure OC's S3
endpoint and `--admin-url` independently. Keep Admin reachable by authorized
management clients and OC, and open OC's own browser endpoint for Web access.
The retained [router](../cmd/fiber_router.go) and [shared authentication](../cmd/jwt.go)
remain backend responsibilities.

Use [OC's installation and Console guide](https://github.com/soulteary/oc/blob/main/docs/console.md)
with an exact compatible version. A local alias shares one startup identity;
independent users require native IAM or OIDC mode and their actual backend
permissions. Do not substitute a shared administrator identity. Missing capability
headers must lead to refusal or documented read-only behavior.

This candidate accepts omission or `OTTERIO_BROWSER=off`. Explicit `on` fails
with a removal message and OC guidance. It does not restore assets or routes.
On earlier Web-bearing releases, `off`/`on` remains their opt-out/rollback mechanism.
Before upgrading to a Web-free server, retain the previous program/image and
deployment configuration, and verify supported data/configuration rollback.
UI rollback does not undo storage, policies, secret rotation or lifecycle changes.

## Removal boundary and validation status

The candidate targets the embedded assets/toolchain, Web JSON-RPC, Web login/URL
tokens, Web upload/download/ZIP adapters, browser redirects/cache branches and
Web-only trace/RPC dependencies. The [dependency inventory](web-console-removal-inventory.md)
records the distinction between Web adapters and shared code.

Keep S3, Admin, STS/OpenID validation, health/metrics, node communication, JWT,
TLS, bucket/IAM permissions and management listeners. The [terminal console](../pkg/console/console.go),
[Admin log producer](../cmd/consolelogger.go), [metrics authentication](../cmd/metrics.go)
and [remote environment loader](../pkg/env/web_env.go) are separate capabilities.
Removing browser code reduces OtterIO's Web surface; OC remains a browser service
that requires its own authentication, permission, deployment and dependency review.

The final native darwin/arm64 server has SHA-256
`4f53709351d03fa7a604e341c770fef5529c7f24cec79aff537f8509c0851638`.
Its embedded VCS revision is the reviewed OtterIO base and `vcs.modified=true`;
this is a changed source build, not a release. The OC CLI and Console are clean
`d944a050` Go builds. The [runtime source manifest](web-console-removal-evidence/runtime-source-manifest.json)
and [verification summary](web-console-removal-evidence/summary.json) bind the
changes and results. These exact program hashes passed:

- [Settings and writes](web-console-removal-evidence/settings.json): 33 groups in
  each of single/dual HTTP and single/dual TLS; 132 groups total.
- [Features and version-copy authorization](web-console-removal-evidence/features.json):
  20 groups per single HTTP, dual HTTP and dual TLS; 60 groups and 372 copy requests,
  including explicit denials and ZIP read authorization.
- [Initial corrected native IAM](web-console-removal-evidence/native.json): 25 groups in each of
  single/dual HTTPS; 50 groups. Independent permissions, session retirement,
  secret rotation, conditional IAM changes and cascade deletion are verified.
- [Range, diagnostics and OIDC/STS](web-console-removal-evidence/protocol.json):
  historical/current Range authorization, actual server diagnostic permissions
  and real STS with a controlled HTTPS IdP passed. This is not a fresh third-party
  Keycloak/graphical browser certification.
- Targeted server race tests: 79 top-level tests and 999 leaf cases passed, with
  no skips/failures. They cover production listener routing, legacy-route refusal,
  shared metrics bearer auth, S3/version/copy/cache/tagging permissions, conditional
  IAM and native STS parent authority. All Go test packages also compiled; the
  full unfiltered test suite was not run locally.
- OC authentication/storage/Console Go suites passed; its Web behavior suite
  passed 88/88. Nine real CLI cases passed for server restart, NAS/S3 gateways,
  separate TLS and old Web route refusal. Repository lint reports zero issues;
  module tidy proposes no changes.

The first native fixture [failed](web-console-removal-evidence/native-initial-failed.json)
because its secret scanner also prohibited a newly created public accessKey.
Its expectations also predated current conditional IAM cascade deletion and
self-session retirement. The [harness patch](web-console-removal-evidence/oc-native-acceptance.patch)
allows only registered public identifiers, continues checking every secret,
and verifies child keys and old cookies stay invalid after parent recreation.
The acceptance fix is submitted separately in [OC PR #45](https://github.com/soulteary/oc/pull/45).
Five harness regression checks and four binary-identity checks passed. Only Python
acceptance code changed in OC; the Go programs above were not replaced. Original
failed runs remain historical evidence and are not counted as successes.

OC PR #45's [review follow-up](https://github.com/soulteary/oc/pull/45#discussion_r4249561639)
strengthens this fixture at `555c1812`: acknowledgement keys must match the full
expected parent/service-child set, and the same signed service-child read must
return 403 after parent disable and 200 after enable. Missing, extra, duplicate
and malformed scope entries are rejected. The [new native report](web-console-removal-evidence/native-review-follow-up.json)
passed 25 groups each in single/dual HTTPS against the same program hashes;
eight native fixture regressions and four binary-identity checks passed.
The [follow-up summary](web-console-removal-evidence/review-follow-up-summary.json)
records the new report and harness fingerprints. Original reports and the recorded
patch keep their historical scope. This verifies the fixture's known service child,
not complete coverage of every STS child or a new production binary.

The [next review](https://github.com/soulteary/oc/pull/45#discussion_r4249720452)
also requires inventory evidence of deletion, since a 403 alone only establishes
revoked access. At OC `c379e31f`, successful authenticated management reads confirm
the known service child exists after creation, disappears after parent deletion,
and remains absent after identical parent recreation. Failed reads or malformed
responses cannot establish removal. The [inventory follow-up report](web-console-removal-evidence/native-cascade-inventory.json)
again passed 25 groups per HTTPS listener arrangement; 11 native regressions and
four binary-identity checks passed. Its new fingerprints and scope are recorded
separately in the follow-up summary, with earlier reports preserved.

The [direct-key review](https://github.com/soulteary/oc/pull/45#discussion_r4249811333)
further narrows what the earlier inventory can establish: a parent-filtered list
cannot detect a child detached from that parent. At OC `d7269c1b`, the manager
also queries the child access key directly through signed Admin Info requests,
requiring 200 at creation and an explicit service-account-not-found 404 after
deletion and identical parent recreation. The complete wire Code, Message and
Resource must match; authorization failures, generic 404s, malformed responses
and network failures cannot prove absence. The [new direct-key report](web-console-removal-evidence/native-direct-child-info.json)
passed 25 groups per HTTPS arrangement, with 16 native regressions and four
binary-identity checks. The [initial strict failure](web-console-removal-evidence/native-direct-child-info-first-failure.json)
is retained: its expected Message omitted the underlying cause appended by
`toAdminAPIErr`. The successful run uses the source-derived full wire message.
Earlier inventory reports retain their narrower historical meaning.

The CLI archive comparison has four local DNS differences in 133 cases; the same
four responses occur on unchanged current OtterIO main. Comparing against that
same-source baseline yields no unexpected differences after 35 intentional help
text changes. The archive is retained; no DNS behavior is waived or changed here.

The initial [Windows CLI check](https://github.com/soulteary/otterio/actions/runs/38268280288/job/114875920190)
failed because those approved help snapshots only recorded Unix output. Exact
Windows before/after snapshots now preserve `.exe`, `%ENV%` and `C:\\>` formatting;
the comparator selects both sides for the same platform and still rejects
unapproved output, invocation, exit-status and side-effect changes. The 133-case
Windows artifact replay has no unexpected differences after the same 35 help
changes; 29 local comparator units passed, with five optional/platform skips.
The before observations are PR #41's candidate artifact, strictly matched against
the fixed `52e2d95` baseline by its successful CI job with no delta exemptions;
they are not presented as a direct baseline build. Artifact identities and this
scope are recorded in the follow-up summary.
The [fresh Windows job](https://github.com/soulteary/otterio/actions/runs/38271297327/job/114885922231)
subsequently passed on `308263fb`, along with that commit's Linux/macOS CLI checks.

Packaging/entrypoint unit checks passed with two optional image checks skipped;
five optional CLI-contract unit fixtures were also skipped. Linux image runtime,
the supported platform matrix, reverse proxies, distributed/federated deployments,
real upgrades and storage rollback still need separate validation. The
[historical release gates](web-console-deprecation.md) remain outstanding where
unchecked; no production rollout or release is represented as completed.

## 中文说明

本分支按用户要求提供内置 Web 的源码删除候选：浏览器应用由 OC Console 独立
承担，OtterIO 保留存储、管理和认证协议。新 OC 已实现 native IAM 与 OIDC，
有真实浏览器开发验收，不能继续把它描述为只有共享 alias 登录；但正式配对、
完整28项能力矩阵、平台／升级／稳定性和通知回退窗口仍未完成。

旧 Web 对普通用户暴露的平台／runtime、全局容量和全策略前缀摘要明确不保留，
以减少敏感信息暴露；这是一项支持范围变更，不冒称 OC 与旧页面逐项等价。
多用户必须选择 OC 的 native IAM／OIDC 模式并使用自身后端权限。OIDC 没有权威
native 父关系时拒绝相应 IAM 写入，不共享或借用管理员权限。

9001 示例仍是 Admin 端口，`--console-address`、`--console-certs-dir` 与对应
环境配置保持兼容。网页使用 OC 自身监听地址。候选省略或设置
`OTTERIO_BROWSER=off` 可启动，显式 `on` 报错；删除后不能通过开关恢复网页。
旧版本回退须保留程序／镜像并事先验证数据与配置兼容。最终程序已通过 999 个
带竞态检测的权限与路由叶子用例、132 组设置／写入、60 组功能、50 组 native
HTTPS，以及 Range／诊断／受控 IdP 的真实 STS 验收；见上方逐项证据。原生账号
旧验收脚本的公开用户名误报和旧级联／会话断言已修正，秘密检测仍保留。正式发行
组合、第三方 IdP 新一轮真实浏览器、跨平台镜像、真实升级回退与通知窗口尚未完成。
