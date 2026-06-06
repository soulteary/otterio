# Security Policy

[简体中文](#中文版)

## Baseline & threat model

OtterIO is forked from the last Apache 2.0 release of upstream MinIO
(approximately `RELEASE.2021-04-22T15-44-28Z`). Every CVE / GHSA published
against `minio/minio` after that date is treated as **potentially applicable
to OtterIO until proven otherwise**, and is tracked in
[`docs/security/upstream-cve-backlog.md`](docs/security/upstream-cve-backlog.md).

**Backlog status (as of 2026-06): 14 closed, 2 not-applicable, 0 open.**
New upstream advisories are triaged into the same table with status
`Pending` and resolved on `main`.

As with any infrastructure component, adopters are expected to evaluate
fitness against their own deployment context — workload profile, capacity
and throughput targets, compliance and data-residency requirements, and
internal change-management policy — and to validate the relevant
codepaths against their own regression suite before promoting to
production.

## Supported versions

The project currently ships from `main`. Public Docker images and source
tarballs are snapshots of `main` and roll forward as security and
compatibility fixes land — track `main` and rebuild when a relevant fix
is merged.

| Branch | Supported | Notes |
| --- | --- | --- |
| `main` | yes | All security fixes land here first. |
| Tagged releases | best effort | Upgrade to the latest `main` for the fastest fix availability. |
| Forks / vendored copies | no | The maintainers cannot patch unknown forks; please rebase. |

## Reporting a vulnerability

Please **do not** open public GitHub issues for suspected vulnerabilities.
Instead, contact the maintainer privately:

- Email: `soulteary@gmail.com`
- Subject prefix: `[otterio-security]`
- If you require encrypted communication, mention this in the first email and
  the maintainer will reply with a public key.

When reporting, include:

1. The OtterIO commit hash you tested against (`git rev-parse HEAD`).
2. A minimal reproduction (curl / `mc` / Go program). For CVEs that map to a
   known upstream MinIO advisory, citing the GHSA / CVE identifier is
   sufficient.
3. The impact you observed (information disclosure, privilege escalation,
   data corruption, denial of service, …).
4. Any logs / traces you can share.

You will receive an acknowledgement within **5 business days**. The
maintainer will agree on a target fix date with you (typically 30 days for
high-severity issues, 90 days for everything else) and coordinate
disclosure.

## Disclosure process

1. The reporter and the maintainer agree on a CVD timeline (default 90 days,
   shorter for actively exploited bugs).
2. A private patch is prepared on a dedicated branch and reviewed against
   regression tests where possible.
3. The fix is merged to `main`; the corresponding entry in
   [`docs/security/upstream-cve-backlog.md`](docs/security/upstream-cve-backlog.md)
   moves from `Pending` to `Done` and links the merge commit.
4. A post-fix advisory is published on the OtterIO GitHub repository,
   crediting the reporter unless they prefer to remain anonymous, and the
   README security advisory is updated if the operator-facing risk profile
   has changed.

## Out of scope

- CVEs against MinIO Console, `mc`, `kes`, or other MinIO satellite projects
  (OtterIO ships its own console under [`browser/`](browser)).
- Pure dependency CVEs already fixed by a `go.mod` upgrade — please open a
  regular pull request.
- Bugs that require attacker control of the **server** binary or the
  underlying disk store; the threat model assumes those are trusted.
- Theoretical timing or side-channel issues without a concrete reproduction.

---

## 中文版

### 基线与威胁模型

OtterIO 派生自 MinIO 最后一个 Apache 2.0 版本（约
`RELEASE.2021-04-22T15-44-28Z`）。该版本之后上游 `minio/minio` 发布的所有
CVE / GHSA，**在被明确排除之前**都视为可能影响 OtterIO，相关清单维护在
[`docs/security/upstream-cve-backlog.md`](docs/security/upstream-cve-backlog.md)。

**当前积压清单状态（2026-06）：14 项已修复、2 项不适用、0 项待处理。**
上游若有新公告，会以 `Pending` 状态滚动纳入同一张表，并在 `main` 上
完成修复。

与任何基础设施组件一样，请结合自身部署场景做严谨的适用性评估：业务
负载特征、容量与吞吐目标、合规与数据驻留要求，以及组织内部的变更
管理规范；并在投产之前，于贵方自有的回归测试集中验证相关代码路径。

### 受支持的版本

项目目前从 `main` 分支出货。公开的 Docker 镜像与源码 tarball 是 `main`
的快照，会随安全与兼容性修复持续滚动 —— 请直接跟随 `main`，并在相关
修复合入后重新构建。

### 漏洞报告

请**不要**直接在 GitHub 公开 issue 中报告疑似漏洞，而应通过私下渠道联系
维护者：

- 邮箱：`soulteary@gmail.com`
- 主题前缀：`[otterio-security]`
- 如需加密通信，请在第一封邮件中说明，维护者会回复公钥。

报告时请附上：

1. 复现使用的 OtterIO commit（`git rev-parse HEAD`）。
2. 最小复现（curl / `mc` / Go 程序）。若是已知上游 MinIO advisory，引用
   对应的 GHSA / CVE 编号即可。
3. 观察到的影响（信息泄露、权限提升、数据损坏、拒绝服务……）。
4. 可分享的日志或调用栈。

维护者会在 **5 个工作日内**确认收到，并就修复时间线达成一致（高危默认
30 天，其他默认 90 天），随后协调披露。

### 披露流程

1. 报告者与维护者协商 CVD 时间线（默认 90 天，活跃利用的漏洞会更短）。
2. 在独立分支上准备私有补丁，尽量补充回归测试。
3. 补丁合入 `main`；
   [`docs/security/upstream-cve-backlog.md`](docs/security/upstream-cve-backlog.md)
   中的对应条目由 `Pending` 改为 `Done`，并附上合入 commit。
4. 在仓库发布修复后的安全公告，除非报告者要求匿名，否则会致谢；如果运维侧
   风险面发生变化，README 中的安全公告也会同步更新。

### 不在范围内的项目

- 针对 MinIO Console、`mc`、`kes` 等 MinIO 周边项目的 CVE（OtterIO 自带
  控制台位于 [`browser/`](browser)）。
- 已经可以通过升级 `go.mod` 修复的纯依赖 CVE — 请直接提交 PR。
- 需要攻击者控制 **服务端二进制** 或底层磁盘存储才能利用的问题；威胁模型
  假定这些是可信的。
- 没有具体复现的时序或旁路猜想。
