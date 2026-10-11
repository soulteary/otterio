# 旧 Web Console 物理删除依赖清单

本清单用于后续独立删除变更的审查，当前不授权删除或默认关闭。
调查基线为 OtterIO `ff6e66f3fbd7e57198ef5df016fdd79e5545fc7b`；
IAM 客户端/计划复查基线为 OC `33274003775a0684a4c93c113a470ee7e384afdd`。
代码验收不等于正式发行组合验收，当前发布周期及 fallback 门禁仍未完成。
[弃用公告及门禁](web-console-deprecation.md)继续有效；该公告中的旧 OC
单身份范围描述是历史发布背景，新的 native/OIDC 源码能力不得自动视为已发行承诺。

## 删除之前必须完成的外部依赖

- 发布包含 `oc-console` 的固定 OC/OtterIO 组合，记录 tag、源提交、二进制
  SHA-256、镜像 digest、Linux amd64/arm64 镜像和支持平台原生执行结果。
- 完成单/双监听 HTTP/TLS、独立 CA、代理/容器真实浏览器验收，保留 OIDC
  登录、刷新、退出和失败路径；真实时钟偏差不能通过静默重试掩盖。
- 验证受限身份和 explicit deny、对象/设置/IAM 操作、取消与未知结果。
  缺失原子能力必须拒绝写入，OIDC 没有权威 native 父映射时拒绝 native IAM 管理。
- 完成真实升级、偏好持久化、同版本旧 UI 回退以及 TLS/访问映射回退；
  UI 回退不能恢复旧密钥、权限、对象或生命周期数据，不能要求存储降级。
- 至少两个已通知正式发行周期，记录版本及日期，解决迁移阻塞后，单独
  审查默认关闭。发布有界 opt-in fallback 窗口的结束版本/日期；窗口结束后
  才单独审查物理删除。合并提交和经过若干天不能替代这些发布事件。

## 仅 Web 的候选删除单元

- `browser/`：`assets.go` embed、`production/` 页面/JS/CSS/字体/图标，
  `app/`、Webpack、Bun manifest/lock、UI tests 和开发说明。
- `cmd/fiber_web_router.go`：Web JSON-RPC codec/service 注册、静态资产 SPA
  回退、`/otterio/webrpc`、`/otterio/upload/{bucket}/{object}`、
  `/otterio/download/{bucket}/{object}?token=...`、`/otterio/zip?token=...`。
  包含 Web 专用压缩/asset handling 和 Web 请求 audit/trace bridge。
- `cmd/web-router.go`：`webAPIHandlers`、`assetPrefix`、`specialAssets`。
- `cmd/web-handlers.go`：Web RPC 请求/回复、旧账号登录/SetAuth、URL token、
  upload/download/ZIP、bucket/object RPC、presign、GetDiscoveryDoc/LoginSTS、
  Web error translation。删除此 adapter 不得删除其调用的 ObjectLayer、IAM、
  SigV4、bucket policy、OpenID provider 或 STS 服务。
- `cmd/web-handler-context.go` 与对应 Web tests、`cmd/web-handlers_test.go`：
  仅在调用点确认全为 Web 后删除。公共测试工具内 Web RPC/token fixtures
  应逐个清理，不能整删 `cmd/test-utils_test.go`。
- `cmd/http-tracer.go` 中 `WebTrace` 和 `pkg/rpc` import：切掉 Web trace
  分支后保留其他 HTTP/S3/Admin trace。`pkg/rpc/`、`pkg/rpc/json2/` 是后续
  候选，当前生产引用来自 Web router/handlers 与 WebTrace；删除前重新全仓
  查引用，并保留仍适用的许可证记录。
- `Dockerfile.dev.browser`、browser 开发工具链配置与 `.github/workflows/go.yml`
  的 browser job；`.github/workflows/docker-source-checks.yml` 的 browser
  toolchain job/paths、`.dockerignore` 的 embed 例外，以及 dependency/race/
  build-metadata/container-security 脚本中仅 Web 的断言/fixture。
  保留 Docker server build、source provenance、双架构与安全检查。
- README/截图/运行示例的旧网页入口可在门禁完成后改写；保留弃用和迁移
  历史，不删除 OC 的接入、TLS、Admin 地址和回退说明。

## 混合文件必须逐分支拆分

- `cmd/fiber_router.go`：server/gateway 单/双监听都含 Web 注册。
  删除 `registerWebRouterFiber` 调用后，保留 Admin/health 在专用管理监听器
  的注册，S3/STS/metrics/节点路由和 graceful shutdown。
- `cmd/fiber_middleware.go`、`cmd/generic-handlers.go`、`cmd/fiber_ctx.go`：
  browser UA 猜测、重定向、asset cache、Web federation upload/download 分支
  是候选；reserved bucket、防伪元数据、TLS/auth、CORS、限额、stats、
  validity、bucket forwarding、proxy 与通用安全头不能按名称或文件整体删除。
  `setAuthHandlerFiber` 的 JWT 分支仍是 shared auth，必须保留。
- `cmd/api-response.go`、`cmd/fiber_response.go`：只审查 browser 格式化
  分支，保留标准 S3 XML/JSON 状态、错误映射和 streaming bridge。
- `cmd/common-main.go`、`cmd/globals.go`、`cmd/config/constants.go`、
  server/gateway startup：未来 `OTTERIO_BROWSER` 的弃用/兼容行为需明确发布；
  不能将 `globalBrowserEnabled` 当前默认 `true` 改掉作为本清单的副作用。
  不得顺便删除管理监听器配置、TLS 加载与启动日志。
- `cmd/fiber_test_router.go` 和泛化 HTTP tests：仅拆 Web fixture/register，
  保留 S3/Admin/STS/health/metrics 和 split-listener 测试基础设施。

## 明确保留的共享认证与管理能力

- `cmd/jwt.go` 整体不能删除：`authenticateNode`/`newAuthToken` 被 peer、
  lock、storage、bootstrap REST client 使用；共享 `cmd/jwt/`、`pkg/auth/`
  及 `jwt-go` 依赖也不能跟 browser manifest 一起裁掉。
- `webRequestAuthenticate`/`webTokenCallback` 的名称含 Web，但
  `cmd/metrics.go:AuthMiddleware` 仍用于 Prometheus bearer token 并强制
  issuer=`prometheus`；`cmd/fiber_metrics_router.go` legacy/cluster/node
  metrics 路由仍依赖它。`cmd/auth-handler.go`、`cmd/fiber_middleware.go`、
  `cmd/handler-utils.go` 也有共享认证调用。若重命名/抽离应先保持等价行为，
  不得把 metrics 改为匿名访问或删除 issuer/signature/expiry 校验。
- `authenticateWeb`、`authenticateURL`、`webTokenAuthenticate` 与 Web
  token validity helper 可在全仓零生产调用证明后单独清理；其底层 shared
  JWT claim/parser/credential lookup、STS 临时用户处理不能连带删除。
- 保留 `--console-address`、`--console-certs-dir`，以及
  `OTTERIO_BROWSER_ADDRESS`、`OTTERIO_BROWSER_CERTS_DIR` 的现有管理监听
  兼容语义。CLI 命名虽然含 console/browser，端口同时承载 Admin；不得因
  页面删除让 OC `--admin-url` 或已有自动化失去服务。改名是另一个兼容变更。
- 保留 S3/API router、Admin router（含 conditional IAM/relationships、
  self-credentials、配置、日志流）、STS/OpenID validator/provider、IAM
  parent/group/service/temporary credential 与 policy 校验、health、metrics。
- 保留 `pkg/console/`、`cmd/consolelogger.go`、Admin ConsoleLog 和 logger
  console target：它们是终端打印、后台存储任务/运维日志，不是内置网页。
- 保留 `pkg/env/web_env.go`：它实现 `env://`/`env+tls://` 远程环境值获取，
  不是 Web Console。`pkg/event/target/webhook.go` 与 JWT policy conditions
  同样不能因为关键词包含 web/jwt 而删除。
- `Version`/release ldflags 是 server 公共发行身份；旧 Web 的 `uiVersion`
  回复字段可以消失，server source/binary provenance 不能消失。

## 最终 IAM 协议复查边界

- mutation 的明确拒绝只认严格解析、状态匹配的服务端错误。签名/权限拒绝
  和原子条件 `PreconditionFailed` 可保留会话；超时、失联、无法解析/不一致
  ack、5xx（除规范明确的 501 unsupported）进入 `outcome_unknown`。
  不自动重试写，不用 postflight 读取替代结果证明。
- OC backend 查询并捕获 immutable scope，验证独立固定 S3+Admin target、
  actor/session、backend instance/lifetime、能力、expiry 和 single use。
  confirmed 与 unknown 都退休 pre-submit 集合；返回额外/缺失影响集合视为
  unknown，不能相信浏览器关系字段。自身操作先响应再退休。
- user/group binding plan 保留浏览器已审阅 binding revision；fresh read 不匹配
  立即拒绝，不能用 fresh revision 覆盖旧 review。user secret 同时使用 binding
  和 graph revision。group/service 修改使用 signed graph revision+expiry。
- OtterIO 同一 persistence write lock 下重验 actor 当前 secret/status/native
  类别、当前 user+group allow/explicit deny，并检查条件后持久化。过时签名
  不能因先前 HTTP 授权而继续写。拒绝 distributed/etcd/OPA 场景的本机原子假设。
- graph HMAC 覆盖 credentials（含 secret、JWT、parent、groups、expiry/status）、
  groups、user/group mappings、policy documents 和 effective membership index。
  token 绑定 kind/target/server expiry；signed expiry 不可重新延长，锁内
  `now < expiry <= now+30s` 不变。语义不变的 no-op 重放不能新增权限，
  Console plan 本身仍单次消费。process restart 会使 token 失效。
- group 删除成功 policy 步而 group 持久化失败时反映成功步的内存变化，返回
  503 `IAMPropagationIncomplete`；客户端必须 unknown 并退休捕获集合。
- service-account.policy 只允许 root 或权威 nativeParent 本人；父用户仍需
  当前启用、既有 Update/GetUser/ListUsers/ListServiceAccounts 权限以及策略
  校验。其他 manager 即使有 Update 权限也不能委派父数据权限；停用父拒绝。
  legacy action/resource probe 不能证明复杂 wildcard/condition 的数学 subset，
  不作为绕过此限制的依据。runtime 权限保持 parent ∩ session restriction，
  不修改 parent policy/mapping。status/secret/delete 的旧 guard 保持，impact
  只含目标服务账号；OIDC/external/unknown parent 不推断 native 关系。
- 新 mutation body 在 header receipt 建立 4096（user secret）或32KiB
  （binding/group/service）及15秒读取上限；固定 ContentLength、完整 payload
  hash、EOF/加密 envelope 校验先于 persistence。通用 object upload 不继承小限额。

## 仍须单独判断的旧 native IAM 路径

本复查的原子关系协议结论仅覆盖 binding、user secret、group 修改和已有
service-account 修改。OC 基线 allowlist 仍有 user.disable/delete，走
`native_iam_retirement.go:retireNativeUser` 的直接目标 retirement 而非关系
snapshot/CAS；该路径不能证明父用户的所有 service/STS 子身份会话已捕获。
user.enable 和 service-account.create 也不是本次新增 coupled mutation。
这些范围已报告协调 agent，删除旧 Web 前应采用已验证的保守拒绝或完成独立
原子协议/会话影响证据，不能把新增协议测试推广成全部 IAM 关系安全保证。

## 删除变更的提交顺序与验收

1. 先完成以上发布/fallback 门禁；单独评审删除范围和 CLI 兼容承诺。
2. 先抽离/保留 shared JWT 和 metrics auth，并验证现有凭据、缺失/伪造/过期
   token、错误 issuer、权限拒绝，以及 node/bootstrap/lock/storage 认证。
3. 删除 Web 注册、RPC/专属 handler/assets；保留 S3/Admin/STS/metrics/health
   监听与认证。Browser UA 不再触发 SPA，旧 route 必须不再被任何入口挂载。
4. 删除零引用 Web 库/测试/工具链，重生成 dependency/SBOM/license inventory；
   仅实际零引用依赖可移除，不能按 browser/Web/console 关键词批量删除。
5. 跑支持平台 build、S3/Admin/STS/metrics/health/节点回归和真实浏览器 OC
   replacement；验证双监听、自定义端口、独立证书/CA、代理、升级与回退。
   记录 exact source/tree/dirty、binary hash、镜像 digest 和可重放验收报告。

本次只提交依赖清单。没有关闭/删除旧 Web、改运行默认、裁剪认证或承诺删除版本。
