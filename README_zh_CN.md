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

## 升级 OtterIO
OtterIO 服务端支持滚动升级, 也就是说你可以一次更新分布式集群中的一个OtterIO实例。 这样可以在不停机的情况下进行升级。可以通过将二进制文件替换为最新版本并以滚动方式重新启动所有服务器来手动完成升级。但是, 我们建议所有用户从客户端使用 [`mc admin update`](https://docs.min.io/docs/minio-admin-complete-guide.html#update) 命令升级。 这将同时更新集群中的所有节点并重新启动它们, 如下命令所示:

```
mc admin update <otterio alias, e.g., myotterio>
```

> 注意: 有些发行版可能不允许滚动升级，这通常在发行说明中提到，所以建议在升级之前阅读发行说明。在这种情况下，建议使用`mc admin update`升级机制来一次升级所有服务器。

### OtterIO升级时要记住的重要事项

- `mc admin update` 命令仅当运行OtterIO的用户对二进制文件所在的父目录具有写权限时才工作, 比如当前二进制文件位于`/usr/local/bin/otterio`, 你需要具备`/usr/local/bin`目录的写权限.
- `mc admin update` 命令同时更新并重新启动所有服务器，应用程序将在升级后重试并继续各自的操作。
- `mc admin update` 命令在 kubernetes/container 环境下是不能用的, 容器环境提供了它自己的更新机制来更新。
- 对于联盟部署模式，应分别针对每个群集运行`mc admin update`。 在成功更新所有群集之前，不要将`mc`更新为任何新版本。
- 如果将`kes`用作OtterIO的KMS，只需替换二进制文件并重新启动`kes`，可以在 [这里](https://github.com/minio/kes/wiki) 找到有关`kes`的更多信息。
- 如果将Vault作为OtterIO的KMS，请确保已遵循如下Vault升级过程的概述：https://www.vaultproject.io/docs/upgrading/index.html
- 如果将OtterIO与etcd配合使用, 请确保已遵循如下etcd升级过程的概述: https://github.com/etcd-io/etcd/blob/master/Documentation/upgrades/upgrading-etcd.md

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
