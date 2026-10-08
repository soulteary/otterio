# OtterIO 文档索引

[项目 README](../../README_zh_CN.md) · [English](../README.md)

OtterIO 是服务端，[OC](https://github.com/soulteary/oc) 是配套命令行客户端，[OtterIO SDK](https://github.com/soulteary/otterio-sdk) 是 Go S3 客户端。使用专题指南前，请先阅读当前快速开始与凭据说明。没有中文译文的主题在下文标记为英文。

## 先跑通本地部署

1. 按[快速开始](../../README_zh_CN.md#快速开始)启动服务，或阅读 [Docker 指南](docker/README.md)。
2. [连接 OC 并验证上传与下载](../../README_zh_CN.md#验证部署)。拆分监听器时，S3 端点和管理端点使用不同端口。
3. 配置应用所需的[用户与 IAM 凭据（英文）](../multi-user/README.md)，再连接 [Go SDK](https://github.com/soulteary/otterio-sdk)。
4. 升级前核对[容器凭据迁移（英文）](../../README_DOCKER_SECURITY.md)、[发布材料](../releases/README.md)与下方安全文档。

## 部署与运维

- [Docker](docker/README.md)与[安全 Compose 配置（英文）](../../README_DOCKER_SECURITY.md)
- [纠删码](erasure/README.md)、[存储类型](erasure/storage-class/README.md)与[分布式部署](distributed/README.md)
- [配置](config/README.md)与 [TLS 证书](tls/README.md)
- [网关后端](gateway/README.md)：本分支仅保留 NAS 与 S3
- [指标（英文）](../metrics/README.md)、[Prometheus（英文）](../metrics/prometheus/README.md)与[健康检查（英文）](../metrics/healthcheck/README.md)
- [日志（英文）](../logging/README.md)、[故障排查](debugging/README.md)与[服务限制](otterio-limits.md)
- [平台部署指南](orchestration/README.md)与 [Kubernetes 部署准备](orchestration/kubernetes/README.md)

部分旧平台示例与专题指南保留了 Apache 协议上游基线的资料。使用前应按当前服务核对镜像名称、凭据、支持的目标与存储拓扑。`docs.min.io` 或 `github.com/minio` 链接描述的是上游项目。OtterIO 管理接口位于 `/otterio/admin/v3`，上游 `mc admin` 使用不同路径，因此管理操作请使用 OC。独立端点与证书信任配置见 [OC 配置指南](https://github.com/soulteary/oc/blob/main/docs/zh_CN/configuration.md)。

## 身份、加密与对象功能

- [用户与 IAM（英文）](../multi-user/README.md)、[管理操作（英文）](../multi-user/admin/README.md)与 [STS 临时凭据（英文）](../sts/README.md)
- [KMS 配置（英文）](../kms/README.md)与[服务端加密（英文）](../security/README.md)
- [桶通知](bucket/notifications/README.md)与[桶复制](bucket/replication/README.md)
- [版本控制](bucket/versioning/README.md)、[生命周期](bucket/lifecycle/README.md)、[保留策略](bucket/retention/README.md)与[配额](bucket/quota/README.md)
- [压缩](compression/README.md)与 [S3 Select（英文）](../select/README.md)

具体功能取决于运行版本、后端、配置与权限。[项目概述](../../README_zh_CN.md#关于-otterio)列出了与上游的差异，请验证实际部署所需的操作。

## 安全与升级

- [私密漏洞报告与受支持版本（英文）](../../SECURITY.md)
- [上游安全公告跟踪（英文）](../security/upstream-cve-backlog.md)
- [内部存储加固与升级说明（英文）](../security/sn-2026-002-storage-hardening.md)
- [LDAP DN 规范化迁移（英文）](../security/ldap-dn-normalization-migration.md)
- [容器凭据与非 root 存储权限（英文）](../../README_DOCKER_SECURITY.md)

## 开发与发布维护

- [贡献指南（英文）](../../CONTRIBUTING.md)与[2026-10-08 项目现状分析](../development/project-status-20261008.md)
- [源码构建](../../README_zh_CN.md#源码构建)
- [服务端 CLI 迁移（英文）](../cli-migration.md)：服务端 CLI 框架变更，与 OC 客户端是不同主题
- [SDK 与 kits 依赖迁移（英文）](../development/sdk-kits-migration-20261008.md)
- [维护者发布指南](../releasing.md)与[发布材料](../releases/README.md)
- [贡献者致谢记录](../../ACKNOWLEDGMENTS.md)

更多主题可参见[英文索引](../README.md)。翻译中的命令与路径若与当前服务不同，请以本仓库代码、当前 README 和对应版本的验收结果为准。
