# 在 Kubernetes 上部署 OtterIO

[English](../../../orchestration/kubernetes/README.md) · [文档目录](../../README.md)

本仓库发布 OtterIO 容器镜像，但没有提供 OtterIO Helm Chart、Kubernetes Operator 或经过 Kubernetes 验收的部署清单。本页旧版本链接的 MinIO Operator 和 Chart 属于上游 MinIO，不能作为 OtterIO 的安装渠道。旧命令 `helm install stable/otterio` 也不是本项目提供的 Chart。

先按照 [Docker 指南](../../docker/README.md)在本机验证镜像、凭据和服务端参数。Kubernetes 部署需要根据存储、网络和可用性要求编写清单；下面的准备说明不代表已经完成 Kubernetes 验收。

## 准备工作负载

- **镜像：** 使用 `soulteary/otterio` 或 `ghcr.io/soulteary/otterio`，固定经过评审的版本标签或 digest。发布产物见[发布指南](../../../releases/README.md)。
- **凭据：** 通过 Kubernetes Secret 提供 `OTTERIO_ROOT_USER` 和 `OTTERIO_ROOT_PASSWORD`，或挂载 Secret 文件，并将 `OTTERIO_ROOT_USER_FILE` 和 `OTTERIO_ROOT_PASSWORD_FILE` 设置为容器内的文件路径。文件变量由镜像入口脚本读取，设置容器参数时请保留该入口脚本。详见 [Docker 凭据规则](../../../../README_DOCKER_SECURITY.md)和 Kubernetes [Secret 文档](https://kubernetes.io/docs/concepts/configuration/secret/)。
- **存储：** 将持久化存储挂载到 `server` 参数指定的路径。临时单节点实例可使用 `server /data`；分布式部署需要按照[分布式指南](../../distributed/README.md)和[纠删码指南](../../erasure/README.md)规划磁盘及节点端点。增加单节点工作负载的副本数不会自动组成分布式 OtterIO 集群。Kubernetes [StatefulSet](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)可提供稳定身份和持久卷关联，但不会替你配置 OtterIO 的存储拓扑。
- **端口：** 使用 `server --address :9000 --console-address :9001 /data` 时，S3 监听 9000，控制台与 Admin API 监听 9001。根据流量用途配置 Service 和访问控制，并按[服务端 README](../../../../README_zh_CN.md#拆分-s3-与-web-控制台端口)为 OC 设置分别对应 S3 和管理接口的地址。
- **TLS：** 按 [TLS 指南](../../tls/README.md)挂载 PEM 格式的证书，并使用规定的文件名与路径。为控制台设置独立证书目录时，必须同时启用独立控制台监听器。
- **健康检查：** S3 监听器通过 `/otterio/health/live` 和 `/otterio/health/ready` 提供进程探针，不能据此判断分布式读写仲裁状态；[健康检查指南](../../../metrics/healthcheck/README.md)介绍集群探针。按照 Kubernetes [探针文档](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)和部署情况设置超时及启动预算。

## 使用持久数据前验证

先在可丢弃的部署中验证凭据加载、卷权限、TLS 信任和两个客户端端点，再测试对象上传下载、Pod 重启后的数据保留，以及所选拓扑的故障恢复行为。本机 Docker 示例通过，不代表已经验证 Kubernetes 的可用性或升级兼容性。

后续可参考 [Prometheus 监控](../../../metrics/prometheus/README.md)、[OC 连接与传输示例](https://github.com/soulteary/oc/blob/main/README_zh_CN.md)和 [Kubernetes 文档](https://kubernetes.io/docs/home/)。
