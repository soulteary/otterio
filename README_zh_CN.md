<div align="center">

[![OtterIO — S3 兼容对象存储](./.github/otter-io-banner.jpg)](https://github.com/soulteary/otterio)

# OtterIO

**S3 兼容对象存储** — _自由存储，无限扩展。_

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8.svg?logo=go&logoColor=white)](./go.mod)
[![GitHub](https://img.shields.io/badge/GitHub-soulteary%2Fotterio-181717.svg?logo=github)](https://github.com/soulteary/otterio)

[English](./README.md) · 简体中文

</div>

OtterIO 是一个基于 Apache License v2.0 开源协议的高性能对象存储服务。它兼容亚马逊 S3 云存储服务接口，非常适合于存储大容量非结构化的数据，例如图片、视频、日志文件、备份数据和容器/虚拟机镜像等，而一个对象文件可以是任意大小，从几 KB 到最大 5T 不等。

OtterIO 是一个非常轻量的服务，可以很简单地和其他应用结合，类似 NodeJS、Redis 或者 MySQL。

## 关于 OtterIO

OtterIO 是 MinIO 最后一个 Apache 协议版本的定制 fork，与上游存在以下差异：

- **HTTP 层**：请求路由基于 [`gofiber/fiber/v3`](https://github.com/gofiber/fiber) 实现，已不再使用 `gorilla/mux`。
- **桶通知目标（Bucket Notification）**：仅保留 `elasticsearch`、`mysql`、`postgresql`、`redis`、`webhook`，已移除消息队列类目标（Kafka、NATS、NATS Streaming、NSQ、AMQP、MQTT）。
- **网关（Gateway）**：仅保留 `nas` 与 `s3`，已移除 `azure`、`gcs`、`hdfs` 网关。
- **构建工具链**：要求 Go `1.26` 及以上版本（详见 `go.mod`）。

> **⚠️ OtterIO 是一个独立的、由社区维护的上游 MinIO Apache 协议版本的项目分支。**
>
> 本项目**未**获得 MinIO, Inc. 的关联、认可或赞助。"MinIO" 是 MinIO, Inc. 的商标，
> 这里仅用于标识本分支所派生的上游项目。Apache License 2.0 不授予任何商标权
> （详见许可证第 6 节）。
>
> OtterIO 基于 MinIO **在改用 GNU AGPLv3 之前的最后一个 Apache License 2.0 版本**，
> 并继续以 [Apache License, Version 2.0](./LICENSE) 进行分发。MinIO, Inc. 及所有第三方
> 子组件的原始版权声明均予以保留 —— 详见 [`NOTICE`](./NOTICE)。
>
> 项目主页：https://github.com/soulteary/otterio

> **🔐 安全公告 — 部署前请务必阅读。**
>
> 由于 OtterIO 派生自 **MinIO 最后一个 Apache 2.0 版本（约 `RELEASE.2021-04-22T15-44-28Z`）**，
> 上游 `minio/minio` 在该版本之后发布的 CVE / GHSA **不会**自动被本项目继承，需要逐条
> 评估并回填。
>
> **当前积压清单状态（2026-06）：14 项已修复、2 项不适用、0 项待处理。**
> 针对 2021-04 基线已纳入跟踪的全部上游公告均已在 `main` 关闭，详细
> 表格、对应 OtterIO 代码路径、上游引用与回归测试请见
> [`docs/security/upstream-cve-backlog.md`](./docs/security/upstream-cve-backlog.md)。
> 上游若有新公告，会以 `Pending` 状态滚动加入。
>
> **从早期版本升级且使用 LDAP 的运维人员**请先阅读
> [`docs/security/ldap-dn-normalization-migration.md`](./docs/security/ldap-dn-normalization-migration.md)
> 再上线：本次发布会在 LDAP DN 进入 IAM 策略表前先做规范化处理，这对于历史
> 上依赖 DN 大小写差异区分多份映射的部署是一次性的破坏性变更。
>
> **在采用 OtterIO 之前**，请结合自身部署场景做严谨的适用性评估：
> 业务负载特征、容量与吞吐目标、合规与数据驻留要求、受支持版本策略，
> 以及组织内部的变更管理规范。与任何基础设施组件一样，建议遵循
> 实验室 → 预发 → 生产的分阶段灰度路径，并在贵方自有的回归测试集中
> 验证相关代码路径。漏洞披露流程与受支持版本矩阵请参考
> [`SECURITY.md`](./SECURITY.md)。

## 安全加固亮点

相对于 2021-04-22 的 Apache 协议 MinIO 基线，OtterIO 已经回填并（在
有必要时进一步加固）下列上游公告，每一项的代码路径、上游引用与回归
测试都列在
[`docs/security/upstream-cve-backlog.md`](./docs/security/upstream-cve-backlog.md)：

- **SSE 元数据注入**（GHSA-3rh2-v3gr-35p9 等价类）—— 路由入口与
  `extractMetadata` 双层拒绝预留前缀的元数据。
- **Precondition GET / HEAD 元数据泄露**（GHSA-95fr-cm4m-q5p9 /
  CVE-2024-36107）—— 引入了一等公民的 `s3:ExistingObjectTag/*` /
  `s3:RequestObjectTag/*` 条件键，并在写出 `ETag` / `Last-Modified` 之前
  重新执行鉴权，按 tag 拒绝时不会再泄露对象状态。
- **SSE-KMS 上下文绑定**（2022 年后多个 CVE）—— 桶 / 对象 AAD 在每次
  封装与解封都从运行时 `(bucket, object)` 重建；恶意或被篡改的
  `MetaContext` 会以独立的 403 sentinel 在调用 KMS 之前被拒绝；历史
  上七处返回 `ErrNotImplemented` 的 PUT 处理桩已经替换为单一的
  `enforceSSEKMSRequest` 安全网关，覆盖单 PUT、分片上传、Copy、
  Post-Policy 路径。
- **Service Account 权限提升** —— GHSA-jjjj-jwhf-8rgr（自账户绕过创建
  SA）、RELEASE.2025-10-15 sub-policy 越权、GHSA-xx8w-mq23-29g4 /
  CVE-2024-24747（admin:UpdateServiceAccount）均已关闭，并强制 sub-policy
  必须是调用者能力的子集。
- **`AddUser` PolicyName 提权**（CVE-2021-43858）—— 在 handler 层（HTTP
  400）与 IAM 层（静默剥离）双重防御。
- **LDAP DN 规范化族**（2022–2024 公告）—— 所有 DN 出口与 IAM 边界都做
  RFC 4514 + RFC 4518 规范化，并附带一次性的持久数据迁移，详见上面的
  迁移说明。
- **桶 / IAM 策略反序列化** —— 修复了 `Principal` / `Resource` /
  `Action` 反序列化时的 panic，错误信息不再回显攻击者输入，并补充了
  fuzz 语料。
- **SigV4 签名头与 chunked 上传硬化** —— 空的 `X-Amz-Content-Sha256`
  现在被视为“未提供”，不再让规范化器误判；`SignedHeaders` 统一小写；
  aws-chunked 上传必须签 `x-amz-decoded-content-length`。
- **复制头 IAM 网关** —— 分叉私有的 `X-Otterio-Source-*` 头通过
  `enforceSourceHeaderIAM` 强制走 `s3:ReplicateObject` 鉴权，关闭了
  使用普通 `s3:PutObject` / `s3:DeleteObject` 权限即可改写对象 mtime /
  ETag、强制写入 delete-marker 的伪造路径。
- **CVE-2023-28432 bootstrap 信息泄露** —— `VerifyHandler` 现在与
  peer-rest / storage-rest / lock-rest 共用同一套节点间 JWT 校验，仅
  `HealthHandler` 保留匿名访问。
- **多值 `Host` 头走私（分叉自身引入的关注点）** —— 已审计并钉死：
  fasthttp 与 net/http 都会在任何 handler 运行之前折叠或拒绝重复
  Host 头，且 SigV4 仅读取标量 `r.Host`，因此双 Host 头走私在本分叉中
  无法构造。

另外两条上游公告（CVE-2021-41137 普通用户策略绕过、GHSA-cwq8-g58r-32hg
`ImportIAM` 提权）经审计**不适用**于 2021-04 基线 —— 审计依据与负向
钉死的回归测试参见上述 backlog。

> 提示：OtterIO 提供了自己的容器镜像，分别发布在 `soulteary/otterio`（Docker Hub）
> 和 `ghcr.io/soulteary/otterio`（GitHub 容器镜像仓库）。本指南中出现的其他上游链接
> （`docs.min.io`、`dl.min.io` 等）仍指向**原始上游项目**，而非 OtterIO。要使用上述
> 定制能力，请从源码构建 OtterIO（见[使用源码安装](#使用源码安装)）。

## Docker 容器

OtterIO 同时发布在 Docker Hub 与 GitHub 容器镜像仓库，按需任选其一拉取：

```sh
# Docker Hub
docker pull soulteary/otterio:latest

# GitHub 容器镜像仓库（GHCR）
docker pull ghcr.io/soulteary/otterio:latest
```

### 稳定版
```sh
docker run -p 9000:9000 \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" \
  soulteary/otterio:latest server /data
```

### 尝鲜版
```sh
docker run -p 9000:9000 \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" \
  soulteary/otterio:edge server /data
```

> 提示：除非你通过`-it`(TTY交互)参数启动容器，否则Docker将不会显示默认的密钥。一般情况下，并不推荐使用容器的默认密钥，更多Docker部署信息请访问 [这里](https://docs.min.io/docs/minio-docker-quickstart-guide)

## macOS
### Homebrew（推荐）
使用 [Homebrew](http://brew.sh/)安装otterio

```sh
brew install otterio/stable/otterio
otterio server /data
```

> 提示：如果你之前使用 `brew install otterio`安装过otterio, 可以用 `otterio/stable/otterio` 官方镜像进行重装. 由于golang 1.8的bug,homebrew版本不太稳定。
```sh
brew uninstall otterio
brew install otterio/stable/otterio
```

### 下载二进制文件
| 操作系统    | CPU架构      | 地址                                                        |
| ----------  | --------     | ------                                                      |
| Apple macOS | 64-bit Intel | https://dl.min.io/server/minio/release/darwin-amd64/minio |
```sh
chmod 755 otterio
./otterio server /data
```

## GNU/Linux
### 下载二进制文件
| 操作系统   | CPU架构      | 地址                                                       |
| ---------- | --------     | ------                                                     |
| GNU/Linux  | 64-bit Intel | https://dl.min.io/server/minio/release/linux-amd64/minio |
```sh
wget https://dl.min.io/server/minio/release/linux-amd64/minio
chmod +x otterio
./otterio server /data
```

| 操作系统    | CPU架构      | 地址                                                        |
| ---------- | --------     | ------                                                     |
| GNU/Linux  | ppc64le      | https://dl.min.io/server/minio/release/linux-ppc64le/minio |
```sh
wget https://dl.min.io/server/minio/release/linux-ppc64le/minio
chmod +x otterio
./otterio server /data
```

## 微软Windows系统
### 下载二进制文件
| 操作系统        | CPU架构  | 地址                                                             |
| ----------      | -------- | ------                                                           |
| 微软Windows系统 | 64位     | https://dl.min.io/server/minio/release/windows-amd64/minio.exe |
```sh
otterio.exe server D:\Photos
```

## FreeBSD
### Port
使用 [pkg](https://github.com/freebsd/pkg)进行安装，OtterIO官方并没有提供FreeBSD二进制文件， 它由FreeBSD上游维护，点击 [这里](https://www.freshports.org/www/otterio)查看。

```sh
pkg install otterio
sysrc otterio_enable=yes
sysrc otterio_disks=/home/user/Photos
service otterio start
```

## 使用源码安装

采用源码安装仅供开发人员和高级用户使用。如果你还没有 Golang 环境，请参考 [How to install Golang](https://golang.org/doc/install)。OtterIO 要求 **Go 1.26 及以上版本**（详见 `go.mod`）。

要构建 OtterIO（包含基于 Fiber 的路由及其他定制），请直接克隆并构建：

```sh
git clone https://github.com/soulteary/otterio.git
cd otterio
make build
./otterio server /data
```

## 独立监听 Web 控制台与 S3 端口

默认情况下，Web 控制台与 S3 API 监听同一个端口（由 `--address` 指定）。从最近一个版本开始，OtterIO 支持把 Web UI 与 Admin API 拆分到独立的端口，方便在反向代理、防火墙或网络策略层面分别控制 S3 流量与控制台流量。

通过 `--console-address` 命令行参数或 `OTTERIO_BROWSER_ADDRESS` 环境变量启用：

```sh
# 命令行参数
otterio server --address ":9000" --console-address ":9001" /data

# 环境变量（等效写法）
export OTTERIO_BROWSER_ADDRESS=":9001"
otterio server --address ":9000" /data
```

启用后：

- `:9000` 仅承载 S3 API、STS、HealthCheck、Metrics；浏览器请求不会再被重定向到 Web UI。
- `:9001` 承载 Web 控制台（`/otterio/`）以及 Admin API（`/otterio/admin/v3/*`）。
- 两个端口不能相同，否则启动会直接失败。
- `Ctrl+C` / `SIGTERM` 会同时优雅关停两个监听器。

> 注意：开启拆分模式后，Admin API（即 `mc admin ...` 使用的接口）位于控制台端口。使用 `mc admin` 系列命令时，需要把 mc alias 指向控制台 URL；普通 S3 操作（`mc cp`、`mc ls` 等）仍然走 S3 端口。

如未指定 `--console-address`，则保持原行为，二者共用同一个端口。

### 控制台监听器使用独立 TLS 证书

在拆分监听器的基础上，还可以通过 `--console-certs-dir`（或环境变量 `OTTERIO_BROWSER_CERTS_DIR`）让控制台使用独立的 TLS 证书，这样 S3 API 与 Web 控制台可以分别使用不同的证书（例如 `:9000` 用内部 CA 签发的证书，`:9001` 用公网证书）：

```sh
otterio server \
  --address ":9000" \
  --console-address ":9001" \
  --certs-dir /etc/otterio/certs/s3 \
  --console-certs-dir /etc/otterio/certs/console \
  /data
```

`--console-certs-dir` 指向的目录必须包含 `public.crt` 与 `private.key`，目录结构与 `--certs-dir` 相同。注意：

- `--console-certs-dir` 必须在已设置 `--console-address` 的情况下使用，否则启动会直接失败。
- 未指定 `--console-certs-dir` 时，控制台监听器复用 `--certs-dir` 加载的证书（与旧行为一致）。
- S3 监听器始终使用 `--certs-dir`，只有控制台监听器会读取 `--console-certs-dir`。
- 两套证书都由同一个证书管理器监视并热加载。

## 为防火墙设置允许访问的端口

默认情况下，OtterIO 使用端口9000来侦听传入的连接。如果你的平台默认阻止了该端口，则需要启用对该端口的访问。

### ufw

对于启用了ufw的主机（基于Debian的发行版）, 你可以通过`ufw`命令允许指定端口上的所有流量连接. 通过如下命令允许访问端口9000

```sh
ufw allow 9000
```

如下命令允许端口9000-9010上的所有传入流量。

```sh
ufw allow 9000:9010/tcp
```

### firewall-cmd

对于启用了firewall-cmd的主机（CentOS）, 你可以通过`firewall-cmd`命令允许指定端口上的所有流量连接。 通过如下命令允许访问端口9000

```sh
firewall-cmd --get-active-zones
```

这个命令获取当前正在使用的区域。 现在，就可以为以上返回的区域应用端口规则了。 假如返回的区域是 `public`, 使用如下命令

```sh
firewall-cmd --zone=public --add-port=9000/tcp --permanent
```

这里的`permanent`参数表示持久化存储规则，可用于防火墙启动、重启和重新加载。 最后，需要防火墙重新加载，让我们刚刚的修改生效。

```sh
firewall-cmd --reload
```

### iptables

对于启用了iptables的主机（RHEL, CentOS, etc）, 你可以通过`iptables`命令允许指定端口上的所有流量连接。 通过如下命令允许访问端口9000

```sh
iptables -A INPUT -p tcp --dport 9000 -j ACCEPT
service iptables restart
```

如下命令允许端口9000-9010上的所有传入流量。

```sh
iptables -A INPUT -p tcp --dport 9000:9010 -j ACCEPT
service iptables restart
```

## 使用OtterIO浏览器进行验证
OtterIO Server带有一个嵌入的Web对象浏览器，安装后使用浏览器访问[http://127.0.0.1:9000](http://127.0.0.1:9000)，如果可以访问，则表示otterio已经安装成功。

![Screenshot](https://github.com/minio/minio/blob/master/docs/screenshots/minio-browser.png?raw=true)

## 使用OtterIO客户端 `mc`进行验证
`mc` 提供了一些UNIX常用命令的替代品，像ls, cat, cp, mirror, diff这些。 它支持文件系统和亚马逊S3云存储服务。 更多信息请参考 [mc快速入门](https://docs.min.io/docs/minio-client-quickstart-guide) 。

## 已经存在的数据
当在单块磁盘上部署OtterIO server,OtterIO server允许客户端访问数据目录下已经存在的数据。比如，如果OtterIO使用`otterio server /mnt/data`启动，那么所有已经在`/mnt/data`目录下的数据都可以被客户端访问到。

上述描述对所有网关后端同样有效。

## 了解更多
- [OtterIO纠删码入门](https://docs.min.io/docs/minio-erasure-code-quickstart-guide)
- [`mc`快速入门](https://docs.min.io/docs/minio-client-quickstart-guide)
- [使用 `aws-cli`](https://docs.min.io/docs/aws-cli-with-minio)
- [使用 `s3cmd`](https://docs.min.io/docs/s3cmd-with-minio)
- [使用 `otterio-go` SDK](https://docs.min.io/docs/golang-client-quickstart-guide)
- [OtterIO文档](https://docs.min.io)

## 如何参与到 OtterIO 项目
欢迎通过仓库 https://github.com/soulteary/otterio 参与贡献。上游项目的约定请参考原始 OtterIO [贡献者指南](https://github.com/minio/minio/blob/master/CONTRIBUTING.md)。

## 授权许可

<div align="center">

<img src="./.github/otter-io-logo.jpg" alt="OtterIO logo" width="220" />

</div>

OtterIO 的使用受 Apache License, Version 2.0 约束，你可以在 [LICENSE](./LICENSE) 查看许可，归属与第三方声明见 [NOTICE](./NOTICE)。

"MinIO" 是 MinIO, Inc. 的商标。OtterIO 未获得 MinIO, Inc. 的关联、认可或赞助。
