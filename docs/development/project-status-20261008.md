# OtterIO 项目现状分析（2026-10-08）

[文档索引](../README.md) · [中文索引](../zh_CN/README.md) · [贡献指南](../../CONTRIBUTING.md)

本分析核对源码基线
[`8ead9fb67ad2c644d01752707e22ecc6c8098db4`](https://github.com/soulteary/otterio/commit/8ead9fb67ad2c644d01752707e22ecc6c8098db4)，
并读取 GitHub 正式发布和该提交的 CI 状态。日期按北京时间标记。它是一份固定基线
的分析记录；之后的功能、发布与检查结果应重新核对，不能把本文当作持续更新的状态页。

## 总体判断

OtterIO 已有独立的服务端、OC 管理客户端、Go SDK、kits 依赖及固定版本发布流程。
项目保留 Apache 2.0 上游基线的对象存储能力，也在持续处理 HTTP、签名、存储与兼容
问题。当前主要文档缺口是：专题指南仍混入上游命令与历史限制，新实现的后端边界没有
同步说明，源码修复、正式发布与实际部署之间的关系容易被误读。

现有代码和检查说明项目具备多种存储与对象功能，不能单凭这些证据给出吞吐、长期
运行稳定性、认证或所有平台投产保证。选择部署方案时应验证实际版本、存储拓扑、
客户端、恢复流程和所需安全边界。

## 架构与组件

| 层次 | 当前实现 | 维护时要注意 |
| --- | --- | --- |
| 服务入口与命令 | `main.go` → `cmd.Main`；`urfave/cli/v3`；server / gateway | Go 集成与自定义 gateway 工厂需遵守 CLI 迁移接口 |
| HTTP | Fiber v3 路由、fasthttp 传输；部分业务仍桥接 `net/http` handler | 签名、路径解码、流式请求与取消行为需要跨层一致 |
| 监听器 | 默认共用端口；可分离 S3 与 console/admin，分别配置 TLS | 管理客户端必须指向正确的 admin 端点 |
| 存储 | 单本地端点使用 FS；多端点使用纠删池；保留 NAS / S3 gateway | 功能支持取决于后端、池数与运行配置 |
| 对象与后台任务 | multipart、versioning、lifecycle、replication、healing、IAM / STS 等 | 路由存在或 XML 可解析，不等于所有后端都有同等运行能力 |
| 控制台 | `browser/` SPA，Go embed 已提交的 production 资源 | 修改依赖或源码后必须重新生成嵌入资源 |
| 客户端与依赖 | OC；`otterio-sdk/v7 v7.3.1`；OtterIO kits | SDK 仍有历史包名，跨模块的具名 Go 类型不能直接混用 |

源码入口：[main.go](../../main.go)、[cmd/main.go](../../cmd/main.go)、
[server-main.go](../../cmd/server-main.go)、[Fiber 路由](../../cmd/fiber_router.go)、
[S3 路由与桥接](../../cmd/fiber_api_router.go)、[存储接口](../../cmd/object-api-interface.go)、
[嵌入资源](../../browser/assets.go)。兼容说明见 [CLI 迁移](../cli-migration.md)与
[SDK/kits 迁移](sdk-kits-migration-20261008.md)。

## 源码与正式版本的界线

查询时最新正式版为
[`RELEASE.2026-10-07T14-09-17Z`](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-07T14-09-17Z)，
源码为 `c8a09caf06c56483e1d21a1c9cdedc3dc3b76381`，GitHub 记录的发布时间为
`2026-10-07T14:19:39Z`，即北京时间 10 月 7 日 22:19:39。该 release 有六个原始
二进制、一个校验和文件及 `release-manifest.json`。版本名中的 UTC 时间不是发布
完成时间。

[该正式版到分析基线的差异](https://github.com/soulteary/otterio/compare/c8a09caf06c56483e1d21a1c9cdedc3dc3b76381...8ead9fb67ad2c644d01752707e22ecc6c8098db4)
包含账户信息与条件写入、CLI v3、SDK/kits 迁移、gateway 测试就绪修正、Mint SDK
安装器修正、CI 缓存调整、存储类型同步和生命周期迁移／版本恢复工作。因此，当前
`main` 文档描述的能力不一定存在于这份正式产物中。

[10 月 8 日发布核对记录](../releases/2026-10-08-release-review.md)截至 `6f6d083`，
另记录 Mint 安装器修正。它早于本次基线，不包含之后的全部变更。根目录
[RELEASE_NOTES.md](../../RELEASE_NOTES.md)是发布流程读取的正文，下一次打标签前必须
对照之后的提交补齐发布范围、兼容性与验收项。本文不替代发布准备，也不触发发布。

## 需要明确写出的功能边界

### 存储拓扑与仲裁

纠删集支持 4–16 块盘，最低盘数针对集合，不是每个节点。四节点各一盘可以组成
四盘集。默认 STANDARD 校验分片不是统一 `N/2`：4–5 盘为 2，6–7 盘为 3，
8–16 盘为 4。对象读仲裁取决于数据分片数；数据与校验分片相等时，写仲裁还需要
多一个分片。节点故障容忍取决于每个集合的实际分布，不能从节点总数直接套公式。

本次同步修正[纠删码](../erasure/README.md)、[存储类型](../erasure/storage-class/README.md)、
[分布式](../distributed/README.md)及[服务限制](../otterio-limits.md)中英指南，移除
“每台至少四盘”和固定半数仲裁的误导表述。

### 条件写入与桶配置更新

当前对象条件写入只承诺 PUT 和完成分片上传时的 `If-None-Match: *`。文件系统与
单池纠删码后端可支持；gateway、多池和 write-back 缓存不声明该能力。已存在对象
返回 412，不支持的条件或后端返回 501；这些写入操作没有 `If-Match` 能力。
客户端应核对 `X-Otterio-Conditional-Writes: v1`，不能把条件失败退化成无条件覆盖。

此外，桶配置有独立的条件更新协议：
[配置条件更新](../../cmd/bucket-config-conditional.go)和
[配置校验](../../cmd/bucket-config-validation.go)负责修订身份、签名覆盖与文档校验。
它不等同于对象的 HTTP 条件写入，也不应被描述为所有后端都支持的通用 CAS。

### 生命周期迁移与恢复

`8fbb20e` 引入了持久化远端对象引用、删除意图与版本恢复工作。实现明确把分层迁移
限定在**本地、单池纠删码服务**，排除 FS、gateway、多池及分布式纠删码部署。
源桶启用版本控制时，目标桶也必须启用。远端目标身份与对象引用参与后台重试、
恢复和删除，不能直接修改或绕过安全检查删除仍被引用的目标。

恢复支持与生命周期扫描是后台行为；SELECT restore 返回未实现。已有测试覆盖多种
迁移、删除、恢复和元数据场景，但不能由此推导任意远端服务、网络故障与旧元数据
都完成了生产验收。部署前按[生命周期指南](../bucket/lifecycle/README.md)验证目标、
凭据、版本、重启、故障重试与清理，特别是带加密、压缩或多分片的业务对象。

源码依据：[支持判断与远端引用](../../cmd/transition-reference.go)、
[目标校验](../../cmd/bucket-targets.go)、[生命周期执行](../../cmd/bucket-lifecycle.go)、
[扫描器](../../cmd/data-scanner.go)、[恢复分片](../../cmd/transition-restore-parts.go)。

### S3 与管理兼容性

OtterIO 管理路径是 `/otterio/admin/v3`，上游 `mc admin` 使用不同路径。管理示例应
使用当前 OC。S3 客户端仍需按自身操作和签名方式验证，不能把管理路径变化扩展成
“所有上游 S3 客户端不能用”，也不能声称任意版本 OC 都有最新参数。

本次修正配置、IAM、管理、通知及加密指南中的确定问题。服务限制同时区分占位 ACL
响应和真实授权、服务级 CORS 和逐桶 CORS、事件通知和 S3 访问日志，并统一对象大小
的二进制单位和分页常量。

## 开发与验证现状

Go 工具链要求 1.27.1，控制台要求 Node.js 24.21.0 以上及 Bun 1.4.2。`make test`
包含安装工具、lint、生成代码核对、构建与 Go 测试，不包含全部 Python、浏览器或
客户端契约检查。`make test-race` 需要 CGO 与 C 编译器。新[贡献指南](../../CONTRIBUTING.md)
把检查入口与使用条件放在一起，避免照搬上游开发流程。

| 验证路径 | 实际覆盖 | 不能据此推导 |
| --- | --- | --- |
| Linux Go CI | 全 Go 单测、race、lint/generated、交叉编译等独立 job | 长期负载或所有硬件平台运行结果 |
| Windows Go CI | 构建、目录专项回归、真实启动及 S3 CRUD smoke | 完整 POSIX 服务端测试套件已执行 |
| CLI CI | Linux/macOS/Windows 命令契约、元数据工具与 gateway 接口兼容检查 | 在线存储全生命周期或所有第三方插件兼容 |
| 控制台 CI | 安装、测试、重新生成生产资源并核对差异 | 浏览器可可靠上传上限大小的对象 |
| Release checks | 离线发布辅助逻辑回归 | 已发布产物或 registry 晋升成功 |
| Mint SDK 步骤 | 固定 SDK 功能程序构建和依赖身份核对 | 全 Mint 客户端套件已对实时服务运行 |
| healing 脚本 | 重启、目录缺失和 safe-mode 情景 | 对象字节完整恢复或备份恢复验收 |

[Go CI](../../.github/workflows/go.yml)、[CLI CI](../../.github/workflows/cli-compat.yml)、
[release checks](../../.github/workflows/release-checks.yml)是具体检查入口。容器相关
workflow 还有路径过滤，不会对每次提交都执行。

本地常规 smoke 优先用 `verify-s3-startup.go`：临时目录、随机回环端口，并仅清理
它创建的进程。旧 `make verify` 使用固定端口，它与 `make verify-healing` 都会全局
查找／停止同名进程，应该在独立 runner 或容器执行。`make verify` 当前没有使用
`-race`，其旧 CI job 已停用；
`make verify-healing` 使用 `-race`，CI job 仍启用。

## 发布与安全维护

发布流程已经有固定源码提交的 main CI 门禁、原始二进制与校验和、镜像摘要记录，
以及独立稳定标签晋升。晋升先验证固定版本摘要，再更新并核对镜像 `latest`，最后
更新 GitHub 的 latest 标记。清单记录产物身份，不是签名或供应链证明；不同服务间的
更新也不具备跨服务原子性。见[发布指南](../releasing.md)、
[release workflow](../../.github/workflows/release.yml)与
[promotion workflow](../../.github/workflows/promote-release.yml)。

安全修复先进入 `main`，`edge` 跟随开发分支，正式产物从固定 `RELEASE.*` 标签构建。
本次修正 [SECURITY.md](../../SECURITY.md) 仍建议直接跟随 main 出货的旧说明，保持
现有 best effort 支持政策，不添加 LTS 或新的维护承诺。

[上游公告清单](../security/upstream-cve-backlog.md)记录适用性、源码修复与回归覆盖。
关闭条目不证明所选产物或实际部署包含修复。本次去掉 README／安全页重复的历史
关闭总数，改为引用滚动清单；本次工作没有重新审计全部安全公告。
[内部存储加固](../security/sn-2026-002-storage-hardening.md)也有明确边界，不能把路径
校验视为能防御完整 host/root compromise。仓库存在 FIPS 相关代码不代表已发布产物
通过认证。

## 本次核对的验证证据

北京时间 2026-10-08 21:32 查询分析基线的 GitHub 检查时，
[Lint](https://github.com/soulteary/otterio/actions/runs/37783363623)、
[Release checks](https://github.com/soulteary/otterio/actions/runs/37783363834)、
[CLI compatibility](https://github.com/soulteary/otterio/actions/runs/37783363883)及
[Docker edge](https://github.com/soulteary/otterio/actions/runs/37783363791)已成功；
[Go workflow](https://github.com/soulteary/otterio/actions/runs/37783364209)仍在运行。
这些是该时间点的状态，不是全部门禁已经通过的声明。

本次文档修正另外执行了：

- 从分析基线构建服务端，核对 server 帮助，并通过临时目录／随机本地端口的 S3
  启动、建桶、上传、下载字节比对和删除 smoke。
- 构建当前 OC，在独立双监听器服务中验证 S3/admin alias、admin info、对象往返、
  IAM policy/user/group、禁用／启用／删除、委派策略管理和配置帮助。
- 容器 entrypoint 与文档示例检查共 36 项；最终提供本次构建二进制运行时，全部
  通过，没有跳过真实 CLI 用例。
- 六个生命周期示例通过项目自身的 `ParseLifecycleConfig` 与 `Validate`，包括将
  AWS CLI JSON 转换为实际发送的 XML 后验证。
- 离线发布辅助检查共 88 项，86 项首次通过；两项首次遇到 10 秒超时，单独重跑
  均通过。没有据此修改发布运行代码。
- 修改文档的相对文件链接、Markdown 锚点、Shell 示例语法及 diff 空白检查。

这些验证未运行新的分布式集群、五种通知后端或真实 KES 服务，也没有重新执行全部
Go 测试。本次 PR 只修改文档；上述本地操作验证的是具体示例与接口，不能代替正式
发布或目标部署的验收。

## 后续维护优先级

1. **发布前补齐范围。** 将 `6f6d083` 之后的实现纳入新的发布核对与正文，等待选定
   精确源码提交的门禁通过，并验收所有新能力；旧准备记录保留历史身份。
2. **验收实际工作负载。** 分布式场景验证节点／磁盘故障、healing 与复制；本地分层
   场景验证远端故障、重启恢复、旧引用、加密／压缩／多分片对象和最终清理。
3. **继续修订历史专题。** 本次修正了确定的高影响问题；仍有历史上游命令、外部
   链接与不同步译文，尤其 STS 示例、缓存、复制和旧平台资料，需要分别按当前
   客户端与部署方案验收，避免全局字符串替换。`pkg/madmin/examples/bucket-target.go`
   还引用已不存在的 `SetBucketTarget`；实际 API 为 `SetRemoteTarget`，示例程序也需
   单独修复和编译验证，不能作为本次生命周期文档的调用依据。
4. **保持文档与代码联动。** 参数、默认值、能力声明或后端支持变化时，同时修改
   英文与对应译文；示例验证应使用当前二进制，并记录哪些检查只是源码核对。
5. **积累运行证据。** 为目标平台和业务场景保存固定版本的兼容、性能与恢复结果。
   交叉编译和单元测试结果应继续与部署承诺分开。
