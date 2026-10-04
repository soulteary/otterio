# Release scope review / 发布范围核对 — 2026-10-05

这是本轮发布准备的核对记录，不是新版本已发布、CI 已全绿或部署验收已完成的证明。
日期是文档整理日期，正式版本在准备 PR 合并、同一 main 提交的 CI 通过后再生成。
根目录 [RELEASE_NOTES.md](../../RELEASE_NOTES.md) 是工作流实际读取的双语正文；
[中文公告](2026-10-05-announcement.zh-CN.md) 是发布后使用的文章草稿。

## 1. 比较基线与本次范围

| 项目 | 核对值 |
| --- | --- |
| 上次发布基线 | [RELEASE.2026-10-04T09-24-10Z](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-04T09-24-10Z)；本次检查时 GitHub latest 也指向它 |
| 基线源码 | `560e1046431af99675092e39a21d46be14fa060c` |
| 上一份准备稿 | [7db1d42 / #25](https://github.com/soulteary/otterio/commit/7db1d42027710d65c002456e5b703a6c31c5075b) |
| 本轮实现核对截止 | `c5ecf86801011576468737d2a1d349be7a5e3999`，已包含 #26 和 #27 |
| 增量范围 | 基线之后共 10 个合并提交，对应 #18–#27；本准备 PR 自身是后续文档增量 |
| 最终发布标签 / 源码 SHA | 待准备 PR 合并并核验 main CI 后确定；不得把本文截止 SHA 当作最终发布提交 |

[固定截止点的完整比较](https://github.com/soulteary/otterio/compare/560e1046431af99675092e39a21d46be14fa060c...c5ecf86801011576468737d2a1d349be7a5e3999)。
如 main 在准备期间继续前进，发布前必须补查后续提交并更新说明。
GitHub latest 只是发布标记，不能代替对各 registry 固定标签、摘要和运行产物的验证。

## 2. 逐项提交核对

| PR | 合并提交 | 变化与发布说明处理 |
| --- | --- | --- |
| [#18](https://github.com/soulteary/otterio/pull/18) | `be06908` | 使用 Oren Yomtov of Act Security 批准的公开署名；这是致谢更新，不是新的运行时修复。 |
| [#19](https://github.com/soulteary/otterio/pull/19) | `1802864` | 流式响应不可变状态快照、提交前异常与流错误边界；保留并发可靠性说明，不宣称性能提升。 |
| [#20](https://github.com/soulteary/otterio/pull/20) | `0484b9b` | Docker 从给定本地上下文构建，保留嵌入前端和源码身份校验；不是逐字节可复现构建承诺。 |
| [#21](https://github.com/soulteary/otterio/pull/21) | `73a3c43` | 容器凭据与独立 secret 文件校验、安全 Compose 和兼容帮助命令；必须保留升级前配置提醒。 |
| [#22](https://github.com/soulteary/otterio/pull/22) | `e5d808b` | 固定版本镜像、草稿下载比对、清单、独立串行晋升；包含 Docker Hub 引用规范化和草稿状态保护。 |
| [#23](https://github.com/soulteary/otterio/pull/23) | `6742405` | Go 1.27.1、服务端/浏览器依赖、生成代码和实际前端产物更新；引用版本记录，不扩大为全依赖零风险声明。 |
| [#24](https://github.com/soulteary/otterio/pull/24) | `20d625b` | 工具链读取 go.mod、缓存分离和完整 race 诊断；不把减少测试或未经测量的加速写成成果。 |
| [#25](https://github.com/soulteary/otterio/pull/25) | `7db1d42` | 原双语说明、中文公告、索引和发布指南；本 PR 在这四份文档上继续补充，不改写历史提交。 |
| [#26](https://github.com/soulteary/otterio/pull/26) | `707c0f9` | 新增内部存储边界、元数据/算术、MessagePack 与缓冲资源限制；补齐威胁模型、全节点升级及兼容性。 |
| [#27](https://github.com/soulteary/otterio/pull/27) | `c5ecf86` | 修复 secret 过滤导致跨 job 仓库名丢失；生成和晋升共用校验，缺失构建元数据提前失败。 |

### 不重复计为本次新增

基线已包含 SigV4 请求头覆盖修复 #13、Windows 目录枚举/启动修复 #12、
动态超时测试确定性改进 #14，以及 #3/#5/#7/#9 的拼写改进。
对仍使用六月版本的用户保留这些升级提醒，但不把它们归入本轮新增修复。
#11 是补丁建议，其思路在 #12 中保留完善，原 PR 未直接合并。
致谢归属以 [ACKNOWLEDGMENTS.md](../../ACKNOWLEDGMENTS.md) 为准；
SigV4 反馈者的署名不应被改写为本轮所有安全问题的报告者。

## 3. 上次失败尝试与本轮处理

`RELEASE.2026-10-04T18-48-37Z` 对应旧源码 `7db1d42`。
[运行 37225865246 的晋升任务](https://github.com/soulteary/otterio/actions/runs/37225865246/job/111511269838)
在清单身份校验阶段报错：

```text
release promotion refused: unexpected or duplicate image repository
```

同次运行的镜像构建任务成功，但结束时提示 `ghcr_repository` 和
`dockerhub_repository` 两个输出因可能包含 secret 被省略。
旧生成器会把空 GHCR 仓库名写入清单，并静默省略空的 Docker Hub 条目；
晋升拒绝该清单是正确行为。该工作流没有 GPG 附件验签阶段，
不能把此次失败或修复描述为 GPG 问题。

#27 通过本地重建公开仓库身份、传递明确构建选择和生成前校验预防该问题，
不会修改已经发布的错误附件。即便旧发布的元数据问题被处理，旧标签也不包含
后来合并的 #26/#27。因此本轮应使用新标签完整发布，不复用旧尝试的身份。
删除 Release 页面不能证明同名 Git 标签或 registry 镜像不存在。

恢复决策必须区分两类情况：清单有效且固定标签摘要一致、仅晋升未完成，可以单独恢复
晋升；清单损坏、缺失、漏记应有仓库或与源码/镜像不匹配，应停止并修复后发新标签。
不要覆盖正式附件、重建旧版本或放宽白名单。见[发布指南](../releasing.md)。

## 4. 安全与升级说明核对

#26 面向已确认的内部存储缺陷类别，不借用其他项目 CVE，也不宣称整个外部公告
原样适用。内部存储 REST 使用分布式节点/root JWT；匿名 S3 请求或普通受限 S3
密钥不足以调用。共享存储代码仍需加固，不能用单节点部署代替修复。

64 MiB 是内部缓冲操作和元数据请求体限制，不是 S3 对象大小上限；
1,000 是单批内部版本删除上限。合法大对象继续使用流式路径。
历史损坏元数据将被拒绝，不会自动重写或删除；升级所有分布式节点，先备份并验证恢复。
路径校验不是符号链接/TOCTOU 沙箱，单请求限制也不是全局并发内存预算。
完整边界和回归命令见[存储加固说明](../security/sn-2026-002-storage-hardening.md)。

#21 的入口策略只针对容器服务启动，不改变裸机启动策略或镜像默认 UID。
非 root 配置显式启用；不要直接递归修改生产数据权限。
源码工具链和运行预编译产物的宿主机依赖应分开说明，版本以
[依赖升级记录](../development/dependency-upgrade-20261004.md)和 go.mod 为准。

## 5. 发布验收清单（待实际执行）

以下均保留为待办，不能用历史实现 PR 的测试结果或本轮文档检查替代。
核验记录应保存到对应 PR/发布运行，不把尚未发生的结果填入公告。

- [ ] 合并准备 PR，确定实际发布 main SHA，补查截止点后的所有提交；同一 SHA 的
  Go、Lint、Release checks 均成功。保留完整 race/平台测试，不绕过失败门禁。
- [ ] 从干净且已同步的 main 生成新的 UTC `RELEASE.*` 标签，通过只读预检后签名或
  注释标签并推送；不要另外手动创建同名正式 Release。最终标签/SHA/运行链接单独记录。
- [ ] 核对六个二进制、一个 SHA-256 校验文件和一个 schema_version=1 身份清单，共八个
  上传附件；自动生成的源码归档不计入八个。清单必须匹配最终标签和源码 SHA。
  GHCR 必须存在；构建实际包含 Docker Hub 时清单也必须记录它，不假设两边用户名一致。
- [ ] 对比两个已配置 registry 的固定标签与清单摘要，检查三种镜像平台及 Linux amd64
  实际 S3 冒烟结果；需要晋升时确认 registry latest 与 GitHub latest，较旧版本应跳过。
  在测试环境验证凭据、权限、已有对象、multipart、大对象、签名复制、Windows 启动及
  分布式修复/复制；最后才把公告准备提示替换成实际版本、Release 链接及真实验收结果。

六个二进制目标为 Linux amd64/arm64/ppc64le、macOS amd64/arm64、Windows amd64；
镜像目标为 Linux amd64/arm64/ppc64le。构建成功不等于每个平台都完成运行验证。
清单是身份记录，不是签名或 attestation；校验文件只覆盖六个二进制。
整个发布不具备跨平台原子性，也没有新增存储格式迁移或无条件降级保证。
