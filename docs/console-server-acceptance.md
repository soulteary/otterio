# 控制台服务端分层验收

OC 固定服务端来源为 `6f6d0835ddff68020f1491c403b958fade22841f`。本 OtterIO 工作区的源码 HEAD 已包含 P3 桶设置、自身改密与生命周期运行时；它不是未应用补丁的固定 pin。当前工作区修改后的源码状态与实际测试结果另行记录，不能仅用 HEAD SHA 表示未提交修改。

OC 导出的 `console-server-base.patch`、`lifecycle-storage.patch` 与必带的 `lifecycle-storage-hardening.patch` 用于在固定 pin 的干净副本重建不同验收来源，不应再次应用到本工作区。旧 `console-server-p3.patch` 冻结为历史快照，不再作为当前部署入口，也不与新补丁混用。

## 独立来源与覆盖

1. **未补丁固定 pin**：核心 S3/管理与 CLI 行为、只读设置安全降级和对象写入。
2. **固定 pin + base**：桶策略/版本/生命周期配置比较交换、自身原生 IAM 密钥轮换、元数据事务；独立构建后分别运行设置与对象写入验收。
3. **固定 pin + base + versions + IAM**：五功能的首要验收来源，含实际 GET/HEAD、CopyObject/UploadPartCopy 版本授权，及 create-only、secret-only 与 revision 保护的 IAM 协议；无需 storage 层。
4. **固定 pin + base + storage + hardening**：独立源码和程序上的生命周期转换、恢复、目标引用、准确远端版本清理与删除保护；真实进程验收使用独立临时四盘源端和归档端。
5. **固定 pin + base + storage + hardening + versions + IAM**：另一份程序上的组合回归，分别记录五功能和设置/对象结果。
6. **当前 OtterIO 源码**：HEAD80保留core之外的12个存储加固源码/测试文件，且未启用独立IAM补丁；工作区的版本授权修复另记源码状态和测试。它与固定pin core组合不全等，当前源码测试不能代替含IAM组合的验收。

基础协议的条件生命周期写入只接受前缀到期与非当前版本到期。它拒绝标签过滤、转换和到期删除标记配置，因为固定 pin 尚无匹配的过滤/扫描保证；GET 完整保留已有配置，普通无条件 S3 接口保留 pin 行为。storage 层补齐标签/非当前过滤和删除标记扫描，并支持转换与恢复。完整生命周期profile仍拒绝 `NewerNoncurrentVersions` 以及标签过滤和 `ExpiredObjectDeleteMarker=true` 的组合。

基础桶配置按响应的 `X-Otterio-Bucket-Config: v1` 和已签名 revision 比较交换，冲突要求重新读取。自身改密受原生 IAM、账号状态、权限与部署模式限制。生命周期转换协议仅宣告单节点、单 pool erasure；FS、gateway、多 pool、分布式和未知实现不宣告该保证。SELECT restore 返回 501。新远端引用需要理解其语义的服务端，降级前须验证存储兼容性。

版本授权使用规范化的实际版本值：空值/纯空白表示当前对象，显式 `null` 和非空版本值使用历史版本权限。复制源按 `X-Amz-Copy-Source` 中源版本检查，不能由目标 query 的 `versionId` 冒充，也不改动签名后的 headers/query。当前对象权限和历史对象权限相互独立，显式 deny 仍有效。

版本/对象标签条件授权只从实际存储的所选对象/版本标签派生
`s3:ExistingObjectTag/<key>`，过滤 header、query、JWT claims 的保留条件值注入；
`RequestObjectTag` 按本次操作实际将写入的标签派生，详见本轮授权复核。GET/HEAD 与普通/分片复制源在取得实际元数据后
复核授权，先于条件响应、元数据输出与复制目标提交。既有 Get/Put/DeleteObjectTagging
路由和动作保留，PUT/DELETE 在对象写锁内再次基于当前实际标签检查，覆盖并发标签修改。
该修复不改变基础 P3 base 补丁。

## 本轮授权复核

OC 的 `console-features-server.patch` 现为
`ea620befaad0c09abc0f91396c04c1d7666e527ec7ed24e5916fb14bedcc1d1c`，覆盖 22 个 Go 文件。
base、storage、hardening、IAM 补丁及全部模块文件未改。旧版本补丁
`c938ec24d3bd48b0eed7633abe876df9dda3cee6b5facd08833aa49a6e772d4f` 已保存在 OC 的
[修正前补丁](https://github.com/soulteary/oc/blob/main/docs/console-features-evidence/console-features-server-before-review.patch)。


本次测试辅助函数 lint 跟进只调整 context 参数顺序及未使用的 bucket 参数，完整 lint 为 0 issues，受影响的 198 个 HTTP/race 用例通过，见[lint 跟进记录](console-authorization-lint-results.json)。下列完整运行报告仍对应修正前 `259313` 补丁及原程序；该补丁原样保存在 OC [lint 前补丁](https://github.com/soulteary/oc/blob/main/docs/console-features-evidence/console-features-server-before-lint.patch)。当前补丁应用顺序及源码等价另有[静态记录](console-authorization-lint-static-results.json)。

本轮按实际读写的数据重新绑定标签条件：

- PUT ObjectTagging 的 `RequestObjectTag` 来自解析后的 XML；授权与最终写入使用同一组标签。
- CopyObject 的 COPY/默认指令使用所选源对象的实际有效标签；REPLACE 使用解析后将写入的请求标签。复制源的 `ExistingObjectTag` 始终取所选源版本的存储标签。
- UploadPartCopy 不写入对象标签，不能把未写入的 tagging header 当作请求标签参与授权。
- 缓存 GET 在授权前读取后端权威元数据，以实际标签和完整时间精度核对快照；HEAD、显式版本、分片和 Range 读取直达后端，避免缓存 key 缺少这些身份信息。
- Range、删除标记与远端 tier 引用错误先按选中的元数据授权；回调已写出 `PreConditionFailed` 响应后立即结束并关闭读取，不再尝试副本代理。
- 版本能力只由受支持且非空的 FS/erasure 与已知缓存实现宣告；gateway、未知实现和空值不宣告 GET/HEAD 或版本列表能力。

受保护的 HTTP GET 即使命中缓存也要先做权威 metadata stat。后端元数据不可用时
没有离线缓存回退。该保证以本次 stat 的快照为边界：之后的标签变化不会撤销
已经开始的读取，也不承诺在整个响应期间持有对象锁。增加的 metadata 请求与
HEAD/版本/分片/Range 绕过缓存属于本次修正的代价。

OC 同时修正了已知桶删除入口：用户可手填桶名，不再把 `ListAllMyBuckets` 当作
删除前置权限，仍保留写入门控和精确确认。ZIP 新增 current-only/history-only 实际
读取授权验收，混合可读/不可读引用整包失败并禁止下载；局部 Go race 用例验证
读取阶段拒绝后部分 ZIP 文件清除、任务槽释放并可复用。Web 行为 61 组已通过。

最新完整记录为[本轮授权复核汇总](console-authorization-review-results.json)，
[console-server-acceptance-results.json](console-server-acceptance-results.json)作为最新入口。
最终三份来源的构建与原生 race 均通过：current 和完整组合各 788 个 HTTP 叶子
用例，base + versions + IAM 776 个。base 没有生命周期运行时，12 个 tier 用例
明确 Skip，未计入其通过数量。Web 行为 61/61 及以下五份真实服务报告全部通过；
结论限定于本轮报告记录的状态、源码、补丁和程序，不使用旧记录替代。

OC 新的独立进程报告为：

- [base 五功能](https://github.com/soulteary/oc/blob/main/docs/console-review-base-features-results.json)：单 HTTP、双 HTTP、双 TLS 各 19 组通过，每个场景含 ZIP 权限 4 例和版本复制 124 例。
- [完整组合五功能](https://github.com/soulteary/oc/blob/main/docs/console-review-combined-features-results.json)：相同三个场景各 19 组通过，每个场景含 ZIP 权限 4 例和版本复制 124 例。
- [实际 current 版本/复制](https://github.com/soulteary/oc/blob/main/docs/console-review-current-version-results.json)：三个场景各 12 组通过，每个场景含版本复制 124 例；没有独立 IAM 扩展。
- [完整组合设置/对象](https://github.com/soulteary/oc/blob/main/docs/console-review-combined-settings-object-results.json)：三个场景各 32 组通过。
- [完整组合生命周期](https://github.com/soulteary/oc/blob/main/docs/console-review-combined-lifecycle-results.json)：单 HTTP、双 TLS 各 9 组通过。

导出时已从固定 pin + 冻结 P3 重建无 commit 的 Git index，没有把初始版本草稿的
index 当作最终基线。`base + storage + hardening + versions + IAM` 和
`base + versions + IAM + storage + hardening` 两种顺序，与独立 current + IAM
副本的 1,031 个 Go/根 module 路径逐字一致；base 独立来源的差异限定在生命周期层。
静态结论限定于上述 Go/根 module 源码等价；运行通过结论由前述原生与真实服务报告另行支持。
实际 current 仍未装 IAM 扩展；既有嵌套 Mint module 差异保留。

## CI 与复现

[Go CI](../.github/workflows/go.yml) 将基础协议、生命周期存储和版本授权拆成独立 Linux 步骤。`^TestConsoleVersionAuthorization` 包含 GET/HEAD 和生产路由加真实 erasure 的普通/分片复制源授权回归；完整单元与 race 套件仍保留。

OC 的 [Go CI](https://github.com/soulteary/oc/blob/main/.github/workflows/go.yml) 从模块 pin 创建独立源码副本，构建 base、base/version/IAM、base/storage/hardening 和完整组合，分别记录补丁和程序摘要。详细本地命令见 OC [开发指南](https://github.com/soulteary/oc/blob/main/docs/development.md#reproduce-the-optional-console-protocol-fixture)。拆分验证入口只读导出固定 pin，分别构建和测试 base / lifecycle：

```sh
python3 buildscripts/test-console-server-patches.py \
  --server-source /path/to/otterio-git-checkout \
  --patch-dir /path/to/oc/buildscripts --profile all \
  --storage-extra-patch /path/to/oc/buildscripts/lifecycle-storage-hardening.patch \
  --output /path/to/console-server-hardening-results.json
```

Git checkout 中只导出固定 `--base`，不是当前 HEAD；若使用无 Git 的固定模块源码目录，
必须额外声明 `--source-revision 6f6d0835ddff68020f1491c403b958fade22841f`，
报告会保留“声明来源而非 Git 验证”的区别。可用 `--profile base` 或 `lifecycle` 独立运行，
生命周期profile必须以 `--storage-extra-patch` 指定hardening；此参数只扩展lifecycle，
base保持独立，含hardening的结果单独保存，避免覆盖原core-only报告。可再按顺序重复 `--extra-patch` 增加版本与IAM补丁；附加协议还需另跑
`go test ./cmd -run '^TestConsoleVersionAuthorization|^TestConsoleIAM' -count=1`。
`--skip-build` / `--skip-tests` 仅用于静态核对，不能作为运行时通过证据。

## 修正前历史记录

以下为 2026-10-09 本轮授权修正前的 macOS arm64 本地验收。[修正前汇总](console-server-acceptance-before-review-results.json)保存当时五个 profile 的通过状态与原始身份，不证明新版本补丁通过：

- [core静态与独立build/race](console-server-split-results.json)：固定pin的Git archive，base + storage core与冻结P3逐字等价、module不变、base对象路径边界通过；不覆盖hardening部署或current全等。
- [硬化lifecycle build/native race](console-server-hardening-results.json)：base + storage + hardening独立构建通过，cmd、完整lifecycle和storageclass包的race通过；报告保存最终profile source tree和程序身份。
- OC `docs/console-base-settings-results.json` 与 `docs/console-base-object-results.json`：三个场景各20/21组通过，后者保留性能门槛。
- OC `docs/console-base-features-results.json`：独立base + versions + IAM三个场景各18组通过，不含存储层。
- OC `docs/lifecycle-storage-integration-results.json`：硬化三层HTTP/TLS各9组通过，独立四盘源端和归档端；旧core-only报告按原身份保留。
- OC `docs/console-version-copy-results.json`：实际HEAD80 modified三个场景各12组通过；没有独立IAM，也未重复声明应用导出补丁。
- OC `docs/console-combined-features-results.json` 与 `docs/console-combined-settings-object-results.json`：五层组合三个场景各18/32组通过；OC `docs/console-combined-lifecycle-results.json` 在独立归档端HTTP/TLS各9组通过。

三份真实复制报告在各场景均记录124请求：50次确认准确复制，74次拒绝且无目标
对象/分片；真实进程采用SigV4和匿名策略。V2/V4预签名、实际已有标签与标签写锁
由[修正前 554 场景 native 记录](console-version-copy-proof.json)覆盖；这是旧程序的覆盖范围。OC
`docs/console-harness-cleanup-results.json` 的strict证明观察到console和独立服务启动，
再发送SIGTERM；子进程全部退出、临时目录移除、未强制清理，并验证preflight替换
旧passed报告。仅观察server reexec的早期证明标为superseded。

hardening导出HEAD80已合入的12个源码/测试文件，不含versions、IAM和嵌套module。
它让storageclass更新与Snapshot读取共享锁；普通旧元数据不足读quorum时仍可按
写quorum覆盖，任何可读partial tier引用阻止覆盖；RenameData按版本保留目标引用和
删除intent，允许同源恢复与pending→complete，并修复monitor取消和测试配置恢复。

历史完整程序实际按base、storage、versions、IAM、hardening构建，`serverPatches`
保持此顺序；CI采用base、storage、hardening、versions、IAM。
[版本native证明](console-version-copy-proof.json)核对两种顺序内容相同，
[历史加固 projection 比较](console-hardening-equivalence.json)证明当时组合与独立
“current + IAM”projection的1028个Go/根module路径逐字一致；实际current未启用IAM，
两个嵌套Mint module文件仍不同，不能称整个current checkout全等。
[原core比较](console-current-core-comparison.json)保留加固前的12个HEAD80路径与3个IAM路径差异。

这些历史 runtime/组合报告记录其实际补丁顺序、程序 buildInfo、SDK 和传递 helper 摘要。
模块副本来源是声明值；历史 actual current 验证 HEAD80 且 `vcs.modified=true`，
不能描述为 clean HEAD。旧 554 场景、c938 补丁及所有旧 profile 报告保留修正前身份，
不得用新的源码、补丁或程序摘要覆盖它们。最新结果使用本轮汇总和新的 OC 报告名。

OC 既有 GHCR 镜像仍不含 `oc-console`；macOS Podman Compose 控制台部署尚未交付。
本轮使用原生本机控制台，不能把这些运行结果宣称为容器部署验收。

旧P3、core-only、生命周期和五功能JSON保留原名、原补丁与二进制身份，不能重新
标为新增hardening组合的通过证据。配置工作流或应用补丁不是验收通过。外部提供者、
分布式、长期soak、磁盘耗尽、完整OtterIO套件和远程CI需另有证据。
