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

控制台通过单个 XMLHttpRequest 上传文件，没有使用 S3 分片上传。实际限制还取决于
浏览器、代理超时与服务端配置，不能把 S3 上限写成已验证的浏览器 5 TiB 上传能力。
大文件请使用支持分片上传的客户端。见[控制台上传实现](../../browser/app/js/uploads/actions.js)。

## 条件写入

当前 `main` 在文件系统及单池纠删码后端支持 PUT、完成分片上传时的原子
`If-None-Match: *`，具体能力取决于运行配置。支持的配置声明
`X-Otterio-Conditional-Writes: v1`。已有对象，包括空对象，返回 HTTP 412。
gateway、多池、write-back 缓存与尚未初始化的存储不声明该能力。不支持的条件或
后端返回 HTTP 501；这些写入操作暂不支持 `If-Match`。遇到条件失败时应保留失败，
不能改成无条件覆盖重试。

该能力晚于 `RELEASE.2026-10-07T14-09-17Z`，使用前确认所选版本包含此实现。
详见[条件写入源码](../../cmd/object-conditional-write.go)与[发布核对记录](../releases/2026-10-08-release-review.md)。

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
