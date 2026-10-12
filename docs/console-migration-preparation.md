# Prepare the OC Console migration / OC Console 迁移准备

Technical migration can be prepared now using the Web-free OtterIO source and
the newer OC source. Public release and production retirement remain separate
decisions. This guide covers the October 10 stable server
`RELEASE.2026-10-10T16-38-05Z` (`8cc518b31ac8b69c8f855e19b4bd027f44275c47`)
to OtterIO `c3b3665154264993fa06a28e95ab25fdd03d7c0c`, paired with OC
`26a0fa6c4d2f380ff9eba84c9dcb91a378b73c27` source. Preparation documentation
does not replace validation of the eventual release programs or images.

## Configuration preflight

Review the actual service environment, container/Compose/Kubernetes definitions,
startup arguments and proxy rules before changing the server program:

- Remove explicit `OTTERIO_BROWSER=on`, `true` or `1`; omit the setting or use
  `off`. The candidate refuses enablement before serving requests. Turning it
  back on restores the Web only on an older program that still includes it.
- Keep `--address` and, where used, `--console-address` or
  `OTTERIO_BROWSER_ADDRESS`. S3 and Admin share the first listener by default;
  a separate Admin address must use a different port. These names remain
  compatible and do not start a browser listener.
- Keep `--certs-dir` for S3 and `--console-certs-dir` or
  `OTTERIO_BROWSER_CERTS_DIR` for a separately configured Admin certificate.
  The latter requires a separate Admin address; without it, Admin reuses S3's
  certificates. Keep each endpoint's existing hostname, port and CA trust.
- Preserve data volumes, backend configuration/IAM, existing root encryption
  credentials, IdP configuration and external service settings. Importing an old
  local `config.json` and rotating encryption credentials are distinct operations;
  do not introduce them into a browser migration. See the [configuration guide](config/README.md).
- Give OC an independent browser address. Update bookmarks and browser proxy
  routes to it, while preserving S3/Admin paths for existing clients. Configure
  `--s3-url`/`--s3-ca` and `--admin-url`/`--admin-ca` independently; OC's
  `--tls-cert`/`--tls-key` configure browser HTTPS separately.

For example, these existing server paths keep their meanings:

```sh
otterio server \
  --address ":9000" \
  --console-address ":9001" \
  --certs-dir /etc/otterio/certs/s3 \
  --console-certs-dir /etc/otterio/certs/console \
  /data
```

Port 9001 above is Admin, not the Web. Preserve custom deployment ports instead
of replacing them with these examples. The behavior is enforced by
[startup validation](../cmd/common-main.go), [listener options](../cmd/server-main.go)
and the [production router](../cmd/fiber_router.go).

## Select the identity and acceptance scope

Use [OC's Console guide](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/docs/console.md)
for the selected program. This documentation commit's production Go sources,
`go.mod` and `go.sum` are unchanged from the tested OC `26a0fa6` candidate.
Local mode shares one configured alias identity;
native IAM and OIDC provide independent sessions with the user's own backend
permissions. Native/OIDC mode requires browser HTTPS and fixed HTTPS S3/Admin
endpoints. Keep a fixed OIDC issuer and its CA/client configuration; its callback
is the configured browser origin plus `/api/oidc/callback`. Do not substitute a
shared administrator for an unsupported user or unknown native-parent mapping.
OC defaults to read-only; enable each supported write only after its permissions,
explicit denies and outcome handling have been checked.

Conditional IAM changes require the server's declared protocols: initialized
single-writer native IAM object storage, without distributed erasure, shared etcd
or external OPA. See [binding support](../cmd/admin-iam-bindings.go) and
[relationship snapshots](../cmd/admin-iam-relationships.go). Missing capabilities
must refuse those writes. OIDC native IAM management remains read-only where
native authority is unknown. LDAP's STS API is retained, but OC has no LDAP browser
login mode. Ordinary-user infrastructure/capacity/full policy-prefix disclosure
is intentionally withdrawn, as recorded in the [removal boundary](web-console-removal.md).

Declare a concrete supported scope for a rollout: operating system/architecture,
storage topology, shared or split listeners, TLS/CA/proxy configuration and identity
mode. Validate that scope; do not claim every deployment is supported from one
local profile. A limited first rollout can omit unverified topologies, while
keeping their limitations explicit. That does not waive the published migration
policy or the acceptance needed for the capabilities actually offered.

## Rehearse the actual upgrade and rollback

1. Retain the published old program/image and its checksums/manifest. Identify
   the clean candidate and OC program hashes and source revisions. Use isolated
   data and test-only credentials, not a production data directory or saved alias.
2. Start the actual old release with the intended listener and CA configuration.
   Create a representative fixture and record object contents, relevant settings,
   identities and permission denials. Stop the server and take a consistent
   storage/configuration snapshot, retaining the decryption credentials.
3. Start the candidate on the same isolated data and endpoints. Check unset/off
   startup and explicit-on refusal, old Web route removal, S3/Admin readiness,
   retained TLS certificates and the seeded fixture. Check OC using the intended
   identity and independent S3/Admin trust; include preferences/restart if offered.
4. Stop the candidate and restart the retained old release. Check the same stored
   fixture and intended old Web access. Record exactly which state survives this
   direct program rollback. Test candidate-side writes or IAM/configuration changes
   separately before promising their backward compatibility.
5. If the required state cannot safely be opened by the old program, restore the
   pre-upgrade snapshot in the isolated rehearsal. Document recovery of writes
   made after that snapshot and external effects before any real rollout.

The companion OC preparation adds
[`buildscripts/console_upgrade_acceptance.py`](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/buildscripts/console_upgrade_acceptance.py)
for actual published-old → candidate → published-old runs. It checks the previous
binary against the supplied published checksum file and records program/source
identities. The old OtterIO checksum file covers six programs and does not cover
`release-manifest.json`. Supply `--previous-manifest-sha256` from an independently
trusted source before the runner parses that manifest or starts a profile; a
hash computed only from the supplied manifest is not an independent check.
Its `single-tls`/`dual-tls` profiles use isolated data and preserve the
same fixture between programs. Run it with explicit old/new server, OC CLI/Console,
source revisions and an output report. A script's existence is not a passed run;
the report, including any failures/skips and exact checked state, is the evidence.

### Verified development upgrade on 2026-10-13 (Asia/Shanghai)

The separate [darwin/arm64 upgrade report](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/docs/evidence/console-migration/upgrade-darwin-arm64.json)
passed both profiles, with 13 recorded check groups and successful process
cleanup per profile. Its SHA-256 is
`4d948227dec0576b31af97875e821ddcd650667518ecbf11e510b63af4a03991`.
It binds all six programs: the October 10 published OtterIO and OC CLI/Console
assets, and clean darwin/arm64 source builds at OtterIO `c3b3665` and OC `26a0fa6`.
Old assets are bound to their published manifests/checksums and the extracted
OC archive, rather than treated as clean development builds.
The old OtterIO manifest was independently bound to the digest recorded by the
[official GitHub release asset API, asset 628471077](https://api.github.com/repos/soulteary/otterio/releases/assets/628471077):
`2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3`.
The report records the expected and observed manifest hashes separately, then
checks its release/source identity and the program checksum.

- A single-node erasure fixture using four storage directories passed shared TLS and split S3/Admin
  TLS with independent CAs. The candidate actually started with the browser
  setting omitted and with `off`; five old Web routes were rejected.
- Seeded object bytes, historical versions, tags, bucket versioning, a native
  user/group policy, explicit read/write denials and OC CLI configuration survived
  the upgrade and direct old-program rollback on the same volumes.
- A new object written by the candidate remained readable by the restricted
  user after rollback to the old server. Restoring the frozen pre-upgrade snapshot
  recovered the baseline and confirmed that this subsequent object was absent.
- Old local-mode HTTP preferences remained readable by the new local Console
  and the old Console after rollback. New native-mode HTTPS preferences began
  independently and persisted across a Console restart.

The initial failed attempts are retained with their narrower results. The final
harness waits for the identified server's IAM-initialization message, a signed
root S3 read and, after seeding, authenticated Admin information for the known
user. These read-only readiness probes handle asynchronous initialization;
acceptance writes and permission allow/deny assertions are not retried. Fifteen
focused harness regressions, including trusted manifest hash rejection before
startup, first termination during cleanup and real child-process/temporary-storage
cleanup, also passed. The report's four harness
fingerprints match the tested files. The
[earlier passing report](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/docs/evidence/console-migration/upgrade-before-cleanup-hardening.json)
is retained unchanged as historical evidence before cleanup hardening; it does
not certify the updated harness. The later cleanup-hardened passing report is
also retained unchanged as
[`upgrade-before-manifest-binding.json`](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/docs/evidence/console-migration/upgrade-before-manifest-binding.json), SHA-256
`b2737bc3e16a2f9e9ff8d9d6f4c4464e3a737f8a342c12a6ccb170105b718237`;
it predates independent manifest verification. The main report above comes from
a fresh full run after both fixes.

`releaseAssetCertification` remains `false`: the new programs are source builds,
not new published artifacts or container images. This verifies only the stated
darwin/arm64 fixture, including the single new-object write. It does not certify
arbitrary IAM/configuration mutations, secret rotation, all erasure metadata,
multiple pools, distributed/external stores, lifecycle/tier transitions, OIDC
upgrades, every IdP/proxy or other platforms. Restoration of a UI also does not
reverse business writes or external effects.

The older
[removal reports](web-console-removal.md#removal-boundary-and-validation-status)
and [published-image trial](https://github.com/soulteary/oc/blob/26a0fa6c4d2f380ff9eba84c9dcb91a378b73c27/docs/console-next-deployment-release.md)
retain their named binaries and narrower historical scope. Neither proves an
actual previous-version upgrade; the new report supplies that separate evidence
only for its declared path.

## Before a public Web-free release

The [original notice](web-console-deprecation.md#published-history) requires at
least two notified releases, a separately reviewed default-off transition and
a completed opt-in fallback window with a published deadline before deletion.
Only the first notified release is recorded. Technical acceleration is authorized;
public release, merge and a policy waiver are not authorized by that work alone.
Either fulfill the original sequence or obtain and record an explicit policy
decision that describes the departure, supported scope and replacement rollback
boundary. A Web-free binary cannot supply the promised `on` fallback window.

After the preparation is reviewed and merged, require new checks on the exact
resulting main commits. Use the [release workflow](releasing.md) to generate
verified archives/manifests and fixed image digests. Recheck the offered profiles
on the resulting OC/OtterIO artifacts before separate promotion. Historical
development acceptance does not certify those new artifacts.

---

## 中文操作说明

可立即使用当前源码做技术迁移，先限定实际要支持的平台、存储拓扑、单／双监听、
TLS／CA／代理和登录模式，再验收这些范围。本记录以 10 月 10 日 OtterIO 稳定版
`8cc518b3` 为起点，候选为 `c3b3665`，OC 源码为 `26a0fa6`；它不等于正式
发行或所有部署场景均已通过。

- 检查真实服务／容器配置，将显式 `OTTERIO_BROWSER=on`、`true`、`1` 改为
  省略或 `off`。删除候选会拒绝开启；只有仍含旧 Web 的程序能用 `on` 恢复网页。
- S3 的 `--address`、`--certs-dir` 与 Admin 的 `--console-address`、
  `--console-certs-dir` 及对应旧环境变量保持原义。Admin 独立地址不能复用 S3
  端口，独立证书目录要求独立 Admin 地址；未配置时共享 S3 证书。保留实际端口、
  主机名、代理映射和独立 CA；9001 示例不是网页端口。
- 保留数据卷、后端配置／IAM、root 加密凭据和 IdP／外部系统配置。不要重新
  生成 root 凭据，也不要把旧 `config.json` 导入或凭据轮换混入网页迁移。
  中文[配置指南](zh_CN/config/README.md)提供对应说明。
- OC 单独监听网页；分别配置实际 S3/Admin URL 和 CA，以及 OC 浏览器 HTTPS
  证书。local alias 共享启动身份，多用户使用 native IAM／OIDC 和自身后端
  权限，不借共享管理员补齐。native／OIDC 要求固定 HTTPS 后端和浏览器 HTTPS；
  OIDC 使用固定 issuer，回调为浏览器 origin 加 `/api/oidc/callback`。
- 条件 IAM 仅支持已初始化的单写入者原生对象存储，分布式纠删码、共享 etcd、
  外部 OPA 不声明该保证。缺少协议拒绝写入；OIDC 缺少 native 权威关系时保持
  相应管理只读。LDAP STS API 保留，但 OC 没有 LDAP 网页登录模式。

回退必须实际演练：校验并保留旧正式程序，在隔离数据上建立对象／设置／权限
样本，停止旧服务取得一致快照；使用同一数据、身份、地址和 CA 启动新构建，
检查省略／off、on 拒绝、旧路由移除、S3/Admin、OC 与样本；然后停止新服务，
真实重启旧正式程序并核对相同样本。新构建后的业务写入、IAM／设置变更与密钥
轮换应单独测，不因基础样本通过就承诺其回退兼容。若旧程序不能安全打开所需
状态，恢复升级前快照，并明确快照后的写入和外部作用如何恢复。

配套 OC 的升级脚本执行真实旧版 → 候选 → 旧版，记录校验和、源码和实际检查
状态。旧 OtterIO 校验和文件只覆盖六个程序，不覆盖 `release-manifest.json`；
`--previous-manifest-sha256` 必须来自独立可信来源，不能把所提交清单自身计算的
摘要当成独立证明。本次采用[官方 GitHub 发行附件 API 的 628471077 号附件摘要](https://api.github.com/repos/soulteary/otterio/releases/assets/628471077)
`2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3`，
在解析清单或启动场景前核对字节，再核对发行／源码身份与程序校验和。
新的 [darwin/arm64 报告](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/docs/evidence/console-migration/upgrade-darwin-arm64.json)
已通过 single／dual TLS，每个场景 13 组检查并完成进程清理。使用单节点四个
存储目录的纠删码样本，同卷真实升级／回退，核对对象、历史版本、标签、桶版本设置、原生用户／
组策略、显式读写拒绝及 CLI 配置；新增对象在旧程序回退后仍可由受限用户读取。
恢复升级前快照后，确认基础样本保留、新增对象不存在。旧 local HTTP 偏好在
新 local 和回退旧 Console 后保留；新 native HTTPS 偏好隔离且重启后保持。

最终脚本仅重试存储／IAM 的只读就绪探测，不重试验收写入或权限允许／拒绝
断言；15 项脚本回归包含启动前可信清单摘要校验失败、清理期间首次终止信号、
真实子进程及临时目录清理。
6 个程序身份与 4 个脚本摘要绑定在报告中。
[清理加固前的旧通过报告](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/docs/evidence/console-migration/upgrade-before-cleanup-hardening.json)
原样保留为历史证据，不证明新脚本已通过；清理加固后、独立清单校验前的通过
报告也原样保留为
[`upgrade-before-manifest-binding.json`](https://github.com/soulteary/oc/blob/dc8ddfbbabe09b5acc1d90a4b5bdc82175f4c14d/docs/evidence/console-migration/upgrade-before-manifest-binding.json)，其 SHA-256 为
`b2737bc3e16a2f9e9ff8d9d6f4c4464e3a737f8a342c12a6ccb170105b718237`。
主报告是两项修复后完整重跑的结果，SHA-256 为
`4d948227dec0576b31af97875e821ddcd650667518ecbf11e510b63af4a03991`。
初次失败和此前历史报告继续保留，不把旧报告改写为新通过结果。
新程序仍是干净源码构建，`releaseAssetCertification: false`；本结果不覆盖
任意 IAM／配置变更、密钥轮换、全部纠删码元数据、多 pool、分布式／外部存储、
生命周期、OIDC 升级、所有 IdP／代理或其他平台，也不能代替最终发行附件／镜像验收。

两次通知、单独默认关闭及完整回退窗口仍未满足。若提前公开发行无内置 Web
版本，必须明确记录调整历史策略的决定、范围和回退边界，不能把技术适配授权
当作发布／合并或策略豁免。最终提交重新通过主分支检查后，由流水线生成并核对
正式产物，在实际配对上验收支持范围，再单独晋升；本准备不执行这些发布动作。
