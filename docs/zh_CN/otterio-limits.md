# OtterIO 服务限制与 S3 兼容边界

[English](../otterio-limits.md) · [文档索引](README.md)

下列限制依据本仓库服务端实现。gateway 上游服务、客户端、反向代理、可用空间和
已配置配额可能施加更低的限制。请核对实际部署的源码与版本；兼容 S3 并不意味着
实现了 AWS S3 的全部功能。

## 纠删拓扑与读写仲裁

每个纠删集包含 **4–16 块磁盘**。单块本地磁盘使用文件系统后端，没有纠删冗余。
更大的部署组成多个纠删集；端点数量必须能按受支持的集合大小分组，并满足端点布局
检查。最低盘数针对纠删集，不是每台服务器。例如，四台服务器各使用一块盘，可以
组成一个四盘纠删集。

对某个对象，设纠删集有 `N` 个分片、`P` 个校验分片，数据分片数为 `D = N - P`。
读取对象需要 `D` 个有效分片；写入需要 `D` 个，数据与校验分片数量相等时需要
`D + 1` 个。元数据操作还需满足自身仲裁，不能仅凭在线盘数保证可用性。节点故障
容忍能力取决于每个纠删集在各节点上的分片分布。

STANDARD 默认校验数为：4–5 盘 `EC:2`，6–7 盘 `EC:3`，8–16 盘 `EC:4`。
修改存储类型影响新对象，不会重写已有对象的布局。详见[存储类型](erasure/storage-class/README.md)、
[分布式部署](distributed/README.md)和[容量配置示例（英文）](../distributed/SIZING.md)。

源码依据：[集合大小与布局](../../cmd/endpoint-ellipses.go)、
[对象仲裁](../../cmd/erasure-metadata.go)、[存储类型默认值](../../cmd/config/storageclass/storage-class.go)。

## S3 请求限制

| 条目 | 服务端限制 |
| --- | --- |
| 对象大小与单次 PUT 大小 | 5 TiB |
| 最小对象大小 | 0 B |
| 每次分片上传的分片数 | 10,000 |
| 分片大小 | 5 MiB–5 GiB；最后一片可以更小，包括 0 B |
| 对象／版本列表每页条目数 | 4,500 |
| 分片列表每页条目数 | 10,000 |
| 未完成分片上传列表默认每页条目数 | 10,000 |

TiB、GiB、MiB 是二进制单位。客户端可以请求更小的页面；请始终处理截断标记或
续传令牌，不能假定一次返回全部结果。未完成上传列表的数值是在未指定 `max-uploads`
时的默认值，显式分页与后端行为可能不同。这些是实现数值，不是经过实测的容量或吞吐
保证。桶与对象总数仍受空间、元数据负载与运维资源约束。

源码依据：[对象与分片大小常量](../../cmd/utils.go)、[响应限制](../../cmd/api-response.go)、
[metacache 块大小](../../cmd/metacache.go)与[请求参数解析](../../cmd/api-resources.go)。

浏览器上传由独立的 [OC Web Console](https://github.com/soulteary/oc/blob/main/docs/zh_CN/console.md)处理，
它有自己的上传限制和分片传输实现。限制还取决于所选 OC 版本、浏览器、代理及后端配置，
不能把服务端 S3 上限写成已验证的浏览器 5 TiB 上传能力。
超过 Console 配置限制的文件请使用支持分片上传的 S3 客户端。

## 条件写入

条件写入适用于普通 `PutObject` 与 `CompleteMultipartUpload`。目标检查与最终提交
共用目标对象的写锁。请读取运行中服务返回的能力头；源码包含某项能力不代表旧版本已支持。

| 条件 | 含义 | 能力响应头 | 支持的存储 |
| --- | --- | --- | --- |
| `If-None-Match: *` | 当前对象不存在时才创建 | `X-Otterio-Conditional-Writes: v1` | 文件系统或单池纠删码 |
| `If-Match: "<etag>"` | 当前对象 ETag 匹配时才覆盖 | `X-Otterio-Conditional-Match: v1` | 文件系统或**本地**单池纠删码 |
| `If-Match: *` | 存在受支持的当前对象时才覆盖 | `X-Otterio-Conditional-Match: v1` | 与 ETag 匹配条件相同 |

gateway、多池、write-back 缓存与尚未初始化的存储不声明上述能力。分布式纠删码不声明
新增的匹配能力，已有的条件创建能力保持原样。受支持的 write-through 缓存由源存储完成
条件检查与提交，条件 PUT 成功后使缓存条目失效。

### 匹配当前对象

使用 GET 或 HEAD 返回的 ETag，发送一个区分大小写的强实体标签；标签是不透明字符串。
支持带双引号与 S3 客户端常用的不带引号形式，包括分片 ETag。弱标签和标签列表尚未实现。
检查始终针对当前对象，不支持同时指定目标 `versionId` 或复制/导入源元数据头。
在启用版本控制的纠删码存储上，成功写入按桶配置创建新版本；当前版本是删除标记时，
更早的存活版本不能满足条件。

新增匹配能力要求 Signature V4，支持预签名和流式 PUT，且必须将 `if-match` 纳入
`SignedHeaders`。调用者必须同时拥有 `s3:PutObject` 和 `s3:GetObject` 权限。
服务端在提交锁内使用当前对象的已存储标签重新鉴权，再返回匹配或不存在结果。
Signature V2、匿名请求和未签名条件会被拒绝。这些要求不改变已有条件创建的契约。

支持普通及分片对象的标签，包括 SSE-S3、SSE-KMS、SSE-C 对外返回的 ETag。
旧对象的加密元数据决定用于比较的标签，新上传携带的加密头不能决定旧 ETag。
密钥解封失败保持为错误，不会被当作标签不匹配。当前对象已压缩、文件系统目录对象、
缺失或回退的占位标签均返回 `501`，因为它们不具备本版本支持的标签契约；
`If-Match: *` 也适用这一限制。

| 结果 | 响应 |
| --- | --- |
| 当前对象存在，但 ETag 不匹配 | `412 PreconditionFailed` |
| 当前对象不存在或为删除标记 | `404 NoSuchKey` |
| 缺少读写权限或条件签名 | `403 AccessDenied` |
| 匹配头为空、重复、格式错误，或同时携带两种条件头 | `400 InvalidRequest` |
| 不支持的条件形式、操作、存储配置或当前对象形态 | `501 NotImplemented` |

ETag 不是唯一版本 ID。相同内容重写、仅修改元数据或 A→B→A 的变化都可能保持或恢复
同一标签。需要识别每一次状态变化的应用应在对象内容中包含递增代次等字段。
无条件写入仍然可用；同一乐观并发控制流程中的所有写入方都应使用合适的条件。
条件失败后应保留失败，不能改成无条件覆盖重试。

### 分片上传与不支持的操作

目标条件必须放在 **CompleteMultipartUpload** 上，而不是初始化或上传分片请求上。
条件失败不会消费上传会话；应用可以明确获取新 ETag 后重新完成，也可以中止上传。
耗时较长的分片完成可能先发送保活空白，再以 HTTP `200` 返回 XML `<Error>`；
客户端必须按照已有完成协议检查响应体。

分片初始化/上传分片/中止、批量删除、对象元数据修改、浏览器 POST 和 Snowball 解包
会明确拒绝目标 `If-Match` 与 `If-None-Match`。已有的 `x-amz-copy-source-if-match`
检查复制源，作为独立请求头继续受支持。GET/HEAD 的条件处理也保持原样。

SDK 自动分片上传必须把条件保留到最终完成。OtterIO SDK v7.3.4 会在初始化时
发送这些条件，却在自动完成时丢弃；服务端现在对这种初始化返回 `501`，避免接受无条件结果。
请使用包含[分片条件透传修复](https://github.com/soulteary/otterio-sdk/pull/13)的 SDK、
直接发起带条件的完成请求，或在适用时使用普通 PUT。
包含该修复的 SDK 在 HTTP 普通上传和 HTTP 自动分片上传路径支持这些条件；
RDMA 原生接口无法透传条件，携带 `If-Match` 或 `If-None-Match` 的上传会明确返回
`InvalidArgument`。
不要通过删除条件绕过这个错误。

条件创建能力晚于 `RELEASE.2026-10-07T14-09-17Z`，匹配能力晚于
`RELEASE.2026-11-25T20-30-00Z`，使用前请确认所选版本包含实现。
详见[条件创建契约](../../cmd/object-conditional-write.go)、
[匹配契约](../../cmd/object-conditional-match.go)、
[不支持操作的防护](../../cmd/object-write-condition-routes.go)与
[条件创建发布核对记录](../releases/2026-10-08-release-review.md)。

### 条件复制

`X-Otterio-Conditional-Copy: v1` 单独声明 `CopyObject` 支持一个目标
`If-None-Match: *`。请求须使用 SigV4，并将 `if-none-match` 纳入
`SignedHeaders`；后端须为文件系统或**本地**单池纠删码，且未启用对象缓存或桶联合。
网关、分布式和多池后端不受支持。原有条件创建 PUT/分片完成能力标记不代表支持条件复制。

目标存在性检查与最终提交共用该对象的命名空间写锁。已有目标（包括零字节对象）
会返回 `412`，原对象保持不变。复制源条件、源授权、元数据/标签指令和加密仍沿用
普通复制的语义。该操作不删除复制源，也不使后续重命名成为原子操作。
目标 `If-Match`、非通配符或多个 `If-None-Match`、显式目标版本和复制/导入请求头
均返回 `501`。

## 条件删除对象

`X-Otterio-Conditional-Delete: v1` 表示独立的有限 `DeleteObject` 契约：只接受一个
强 `If-Match` ETag（带引号或不带引号），且条件必须包含在 SigV4 或 SigV4 预签名中。
调用者必须同时拥有针对当前对象已存储标签的 `s3:GetObject` 与 `s3:DeleteObject`
权限。后端在同一对象命名空间写锁内重新鉴权、比较 ETag 并完成删除。
ETag 不匹配返回 `412`；有权限的调用者访问不存在的当前对象或当前删除标记时返回
`404`。两种失败都不会删除对象。无条件 DELETE 对缺失对象仍按原行为返回 `204`。

支持文件系统和本地单池纠删码。不支持任何缓存、分布式或多池纠删码、网关、联邦部署、
配置复制规则的桶、显式 `versionId`（包括 `null`）、复制/导入请求头、强制删除或绕过保留策略的请求头、通配符
`If-Match: *`、`If-None-Match`、弱 ETag 与 ETag 列表。重复或冲突的条件属于请求错误。
前述当前对象 ETag 限制同样适用：目录、缺失或占位元数据、压缩对象均不能条件删除。
该能力不会改变已有条件写入能力，也不会为批量删除、标签、元数据或分片中止添加条件。

在启用版本的桶中，条件删除沿用普通当前对象删除行为，创建删除标记；不会永久删除指定
历史版本，也不会绕过保留策略。ETag 沿用既有 S3 内容校验标识契约，不是唯一的修改序号；
再次写入相同 ETag 的对象无法通过这一条件区分。

客户端可以使用条件复制后接条件删除来重命名。它们仍然是两个独立操作：复制后源对象的 ETag
发生变化时，删除失败，已复制的目标保留。该流程不是两键原子重命名，也不会自动回滚目标；
客户端应报告部分完成，在重新读取状态后再决定下一步。
参见[条件删除实现](../../cmd/object-conditional-delete.go)。

## 部分支持或未实现的 S3 功能

- **Bucket/Object ACL：**兼容处理器接受 private 访问，返回占位的 owner
  `FULL_CONTROL` ACL，没有实现 AWS ACL 授权模型。权限管理请使用
  [IAM 与桶策略（英文）](../multi-user/README.md)。见 [ACL 处理器](../../cmd/acl-handlers.go)。
- **逐桶 CORS 配置：**未实现。兼容 GET 返回 `NoSuchCORSConfiguration`。
  跨域访问由服务端 `api cors_allow_origin` 或 `OTTERIO_API_CORS_ALLOW_ORIGIN`
  控制，默认 `*`。见[配置指南](config/README.md)、[CORS 中间件](../../cmd/fiber_router.go)
  与 [API 配置](../../cmd/config/api/api.go)。
- **BucketWebsite、BucketAnalytics、BucketMetrics、BucketLogging、BucketRequestPayment：**
  未完整实现 AWS 行为。部分路由的兼容响应不代表功能已经实现。静态网站可使用
  Web 服务器，指标见 [Prometheus（英文）](../metrics/prometheus/README.md)，审计事件见
  [审计日志（英文）](../logging/README.md)。[桶通知](bucket/notifications/README.md)提供事件
  投递，不等同于 S3 访问日志。
- **ObjectTorrent：**未实现。
- **生命周期分层迁移与恢复：**仅支持本地、单池纠删码服务，目标要求与恢复限制见
  [生命周期指南](bucket/lifecycle/README.md)。

路由与兼容行为以 [S3 路由表](../../cmd/fiber_api_router.go)和
[兼容处理器](../../cmd/dummy-handlers.go)为准。功能需求可提交到
[OtterIO issues](https://github.com/soulteary/otterio/issues)。

## 对象名称

文件系统与 NAS 部署受宿主文件系统的命名规则限制。Windows 文件名可能无法使用
`^*|\\/&\";` 等字符，这不是跨平台的完整限制列表。请按实际后端与客户端验证对象名称。
