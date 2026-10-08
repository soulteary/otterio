# OtterIO Docker 快速入门

[文档索引](../README.md) · [English](../../docker/README.md)

## 环境与镜像

安装 [Docker Engine](https://docs.docker.com/engine/install/) 或启用 Linux 容器的 Docker Desktop。OtterIO 发布的容器镜像是 Linux 镜像，在 macOS 和 Windows 上也通过 Linux 容器运行。发布流程构建 Linux amd64、arm64 与 ppc64le 镜像。

可选择 Docker Hub 或 GitHub Container Registry（GHCR）：

```sh
docker pull soulteary/otterio:latest
# 也可选择：
docker pull ghcr.io/soulteary/otterio:latest
```

下文使用 `soulteary/otterio:latest` 进行本地体验。实际部署请固定经过审核的版本标签或摘要，见[发布验证步骤](../../releasing.md#4-verify-before-announcing)。使用 GHCR 时，替换下面的镜像名称即可。

## 启动带持久存储的本地服务

首次设置非默认凭据后，请安全保存。Unix 下生成密码的命令依赖 OpenSSL；重启或连接客户端时，应继续使用同一组凭据。

### Linux 与 macOS

```sh
export OTTERIO_ROOT_USER=otterio-admin
OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
export OTTERIO_ROOT_PASSWORD

docker volume create otterio-data
docker run -d --name otterio \
  -p 127.0.0.1:9000:9000 -p 127.0.0.1:9001:9001 \
  -e OTTERIO_ROOT_USER -e OTTERIO_ROOT_PASSWORD \
  --mount source=otterio-data,target=/data \
  soulteary/otterio:latest server --console-address ":9001" /data
```

### Windows PowerShell 7.1 或更新版本

输入为本次部署保存的非默认凭据。命名卷可避免 Windows 与 Linux 主机路径的差异：

```powershell
$env:OTTERIO_ROOT_USER = Read-Host 'Root username'
$env:OTTERIO_ROOT_PASSWORD = Read-Host 'Root password' -MaskInput

docker volume create otterio-data
docker run -d --name otterio `
  -p 127.0.0.1:9000:9000 -p 127.0.0.1:9001:9001 `
  -e OTTERIO_ROOT_USER -e OTTERIO_ROOT_PASSWORD `
  --mount source=otterio-data,target=/data `
  soulteary/otterio:latest server --console-address ":9001" /data
```

S3 端点为 <http://127.0.0.1:9000>，控制台及管理端点为 <http://127.0.0.1:9001>，请使用已配置的凭据登录。通过 OC [验证上传与下载](../../../README_zh_CN.md#验证部署)，同一别名可保存两个端点。

命名卷不会随容器删除。重新创建容器时，应使用同一数据卷与凭据。临时测试可去掉 `--mount` 和 `-d`，加上 `--rm`；Docker 会在删除该容器时清理匿名数据卷。若已有实例运行，请使用不同容器名和空闲端口。

## 凭据、密钥文件与非 root 运行

镜像入口脚本会拒绝缺失、不完整或默认的凭据。`OTTERIO_ROOT_USER` 和 `OTTERIO_ROOT_PASSWORD` 作为一组优先于旧的 `OTTERIO_ACCESS_KEY` 和 `OTTERIO_SECRET_KEY`，不要各取一半混用。

使用密钥文件时，将 `OTTERIO_ROOT_USER_FILE` 和／或 `OTTERIO_ROOT_PASSWORD_FILE` 指向容器内可读、非空的普通文件。绝对路径会强制检查文件是否存在。同一凭据同时设置非空环境值和已存在的 `_FILE` 文件会报错。这些变量由容器入口脚本读取，裸金属二进制不读取它们。

非 root 本地部署请使用仓库中的[安全 Compose 配置](../../../README_DOCKER_SECURITY.md)与 [docker-compose.secure.yml](../../../docker-compose.secure.yml)，其中说明了固定 UID/GID 的权限、密码文件挂载与重启升级注意事项。若使用 `docker run --user` 配合主机目录挂载，请先确保所选数字 UID/GID 能写入该目录。Windows 容器的 Active Directory 凭据规格不适用于这些 Linux 镜像。

### Docker Swarm secrets

在已初始化的 Swarm 上，下面的 Unix shell 示例用已保存的凭据创建密钥并启动一个服务。示例不发布端口；请另行配置私有服务网络或适当的入口。

```sh
: "${OTTERIO_ROOT_USER:?请先设置已保存的用户名}"
: "${OTTERIO_ROOT_PASSWORD:?请先设置已保存的密码}"
printf '%s' "$OTTERIO_ROOT_USER" | docker secret create otterio-root-user -
printf '%s' "$OTTERIO_ROOT_PASSWORD" | docker secret create otterio-root-password -

docker service create --name otterio \
  --secret otterio-root-user --secret otterio-root-password \
  --env OTTERIO_ROOT_USER_FILE=/run/secrets/otterio-root-user \
  --env OTTERIO_ROOT_PASSWORD_FILE=/run/secrets/otterio-root-password \
  --mount type=volume,source=otterio-data,target=/data \
  soulteary/otterio:latest server --console-address ":9001" /data
```

密钥生命周期见 [Docker Swarm secrets 指南](https://docs.docker.com/engine/swarm/secrets/)。这不是分布式存储部署。本地卷属于单个节点，重新调度服务前应处理好节点与存储位置的关系。存储拓扑见[分布式部署](../distributed/README.md)和[纠删码](../erasure/README.md)。

## 查看状态与重启

```sh
docker ps -a
docker logs otterio
docker stats otterio
docker stop otterio
docker start otterio
```

`docker start` 沿用已有容器的环境与挂载。修改终端中的凭据不会改变已有容器；需要以预期环境重新创建容器，并复用原数据卷。升级或删除数据卷前应先备份。

## 从当前工作目录构建镜像

根目录的 `Dockerfile` 编译传入的构建上下文，包括本地修改。在克隆的仓库中运行：

```sh
docker build --build-arg VCS_REF="$(git rev-parse HEAD)" -t otterio:local .
```

将启动示例中的镜像替换为 `otterio:local` 即可。`make docker` 则先构建主机二进制，再通过 `Dockerfile.dev` 复制到镜像，二进制必须匹配容器的 Linux 架构。发布流程使用 `Dockerfile.ci` 与预编译发行二进制；`Dockerfile.release` 克隆远程源码，不构建本地修改。

## 继续阅读

- [容器凭据迁移与安全 Compose 配置](../../../README_DOCKER_SECURITY.md)
- [分布式部署](../distributed/README.md)与[纠删码](../erasure/README.md)
- [TLS](../tls/README.md)，包括证书挂载
- [OC 配置与独立管理端点](https://github.com/soulteary/oc/blob/main/docs/zh_CN/configuration.md)
- [历史编排示例](../orchestration/README.md)：使用前应按当前服务核对镜像、凭据和拓扑
