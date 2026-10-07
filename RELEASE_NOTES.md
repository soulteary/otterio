# HTTP and administration reliability update

This release follows `RELEASE.2026-10-04T22-12-15Z`. It contains the administration,
HTTP lifecycle and macOS service fixes merged after that version. The full change
range and release checklist are in [the release review](https://github.com/soulteary/otterio/blob/main/docs/releases/2026-10-07-release-review.md).

## Changes

- Bridge validated, route-declared query parameters to legacy administration
  handlers while preserving path parameter precedence. Standard and streaming
  administration routes now receive their expected inputs.
- Decode encoded object paths once and preserve literal percent signs and encoded
  separators correctly. Fix administration query handling and healing streams.
- Correct streaming cancellation and request accounting. Tie stream cleanup to
  the request lifecycle, release consumers, and avoid closing work still in use.
- Bound HTTP shutdown by its configured timeout and close listeners reliably.
  Align TLS protocol negotiation with supported HTTP handling.
- On macOS, supervise service workers, forward signals and restart reliably.
  Workers stop when their supervisor exits unexpectedly.
- Normalize release image repository names with a portable command. The staged
  publication and digest-based stable promotion policy remains in place.

## Upgrade

Back up data and configuration. In staging, check encoded object keys, normal
S3 upload/download/delete, multipart, signed copies, administration queries and
healing streams. Check cancellation and bounded shutdown under active traffic;
macOS installations should also exercise service restart and termination.

The previous release's container credential policy, SigV4 header validation and
internal storage hardening remain applicable. Distributed deployments should
upgrade every node and test healing/replication. Historical corrupt metadata is
rejected rather than repaired automatically. Go 1.27.1 is required for source
builds; running prebuilt artifacts does not require the development toolchain.
See [the previous release notes](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-04T22-12-15Z),
[container migration](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md) and
[storage hardening](https://github.com/soulteary/otterio/blob/main/docs/security/sn-2026-002-storage-hardening.md).

No storage-format migration is introduced by this update. This is not an
unconditional downgrade guarantee or a claim that every inherited issue is fixed.
The six binary builds and Linux amd64 image smoke test do not establish runtime
acceptance for every supported architecture. Verify checksums and source/image
identity before rollout, using [the release guide](https://github.com/soulteary/otterio/blob/main/docs/releasing.md).

## Acknowledgements

Thank you **Oren Yomtov of Act Security** for the previously addressed SigV4
report, analysis and reproducer. Community contribution records remain in
[ACKNOWLEDGMENTS.md](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md); this update does not reassign credit for
these HTTP and administration changes to earlier issue reporters.

---

# HTTP 与管理接口可靠性更新

本次更新接续 `RELEASE.2026-10-04T22-12-15Z`，包含该版本之后合并的管理接口、
HTTP 生命周期及 macOS 服务修复。范围与发布验收步骤见[发布核对记录](https://github.com/soulteary/otterio/blob/main/docs/releases/2026-10-07-release-review.md)。

## 本次变化

- 将路由声明并验证过的查询参数传给旧管理处理函数，保持路径参数优先级，
  修正普通及流式管理接口的参数传递。
- 修正编码对象路径的单次解码，正确保留字面百分号与编码分隔符，并修复管理查询和修复流。
- 修正流式请求取消、请求计数与清理顺序，使消费者随请求生命周期释放。
- 按配置超时限制 HTTP 关闭过程，可靠关闭监听器，并调整 TLS 协议协商。
- macOS 使用监督进程管理服务工作进程，转发信号并可靠重启；监督进程异常退出时停止工作进程。
- 使用可移植命令规范化发布镜像仓库名，继续采用分阶段发布和按摘要串行晋升。

## 升级前检查

先备份数据和配置，在测试环境验证编码对象键、正常 S3 上传下载删除、分片上传、
合法签名复制、管理查询与修复流，并检查活跃请求下的取消和限时关闭。
macOS 部署还应检查服务重启及停止。

上一版的容器凭据策略、SigV4 请求头校验和内部存储加固继续适用；
分布式部署应升级全部节点并验证修复和复制。历史损坏元数据会被拒绝，不会自动修复。
源码构建要求 Go 1.27.1，运行预编译产物不需要安装开发工具链。
具体迁移要求见[上一正式版本](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-04T22-12-15Z)、
[容器迁移指南](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md)和[存储加固说明](https://github.com/soulteary/otterio/blob/main/docs/security/sn-2026-002-storage-hardening.md)。

本次没有引入存储格式迁移，但不构成无条件降级保证，也不宣称修复了全部历史问题。
二进制交叉编译和 Linux amd64 镜像冒烟测试不能代替所有架构的实际验收。
发布后按[发布指南](https://github.com/soulteary/otterio/blob/main/docs/releasing.md)核对校验和、源码身份及镜像摘要，再逐步部署。

感谢 **Oren Yomtov of Act Security** 对此前 SigV4 问题提供报告、分析和复现用例。
社区贡献继续按[致谢索引](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md)保留，不将本轮 HTTP 和管理接口修复归给此前问题反馈者。
