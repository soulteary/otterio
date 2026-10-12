# OC Console replacement and embedded Web removal candidate

This branch separates the browser interface from OtterIO: OC Console provides
the Web application while OtterIO retains storage, administration and identity
protocols. The removal reduces maintenance of the embedded front end
and the storage server's exposed Web handlers. Release migration gates remain
open; this candidate is not a claim that production retirement is certified.

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
- [Native IAM](web-console-removal-evidence/native.json): 25 groups in each of
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
  full unfiltered test suite was not run.
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

The CLI archive comparison has four local DNS differences in 133 cases; the same
four responses occur on unchanged current OtterIO main. Comparing against that
same-source baseline yields no unexpected differences after 35 intentional help
text changes. The archive is retained; no DNS behavior is waived or changed here.

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
