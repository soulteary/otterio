# 使用Docker Compose部署OtterIO

Docker Compose允许定义和运行单主机，多容器Docker应用程序。

使用Compose，您可以使用Compose文件来配置OtterIO服务。 然后，使用单个命令，您可以通过你的配置创建并启动所有分布式OtterIO实例。 分布式OtterIO实例将部署在同一主机上的多个容器中。 这是建立基于分布式OtterIO的开发，测试和预发布环境的好方法。

## 1. 前提条件

* 熟悉 [Docker Compose](https://docs.docker.com/compose/overview/).
* Docker已经在本机安装，从[这里](https://www.docker.com/community-edition#/download)下载相关的安装器。

## 2. 在Docker Compose上运行分布式OtterIO

在Docker Compose上部署分布式OtterIO,请下载[docker-compose.yaml](https://github.com/soulteary/otterio/blob/main/docs/orchestration/docker-compose/docker-compose.yaml?raw=true)到你的当前工作目录。Docker Compose会pull OtterIO Docker Image,所以你不需要手动去下载OtterIO binary。然后运行下面的命令

### GNU/Linux and macOS

```sh
docker-compose pull
docker-compose up
```

### Windows

```sh
docker-compose.exe pull
docker-compose.exe up
```

示例通过 Nginx 代理四个 OtterIO 实例，在 `http://127.0.0.1:9000` 提供 S3 端点。使用 [OC](https://github.com/soulteary/oc#readme)验证访问。浏览器入口由独立的 [OC Web Console](https://github.com/soulteary/oc/blob/main/docs/zh_CN/console.md)提供，不是这个 S3 端口。如果部署使用 `--console-address` 拆分 Admin 流量，应保留其代理映射，为 OC 配置实际的 S3/Admin URL 和各自的 CA 设置。

### 注意事项

* 默认情况下Docker Compose file使用的是最新版的OtterIO server的Docker镜像，你可以修改image tag来拉取指定版本的[OtterIO Docker image](https://hub.docker.com/r/soulteary/otterio/).

* 默认情况下会创建4个otterio实例，你可以添加更多的OtterIO服务（最多总共16个）到你的OtterIO Compose deployment。添加一个服务
  * 复制服务定义并适当地更改新服务的名称。
  * 更新每个服务中的命令部分。
  * 更新 Nginx 配置中的 upstream，加入新实例。

  关于分布式OtterIO的更多资料，请访问[这里](https://github.com/soulteary/otterio/blob/main/docs/zh_CN/README.md).

* 当前 Compose 示例中的 OtterIO 服务在容器网络使用 9000，由 Nginx 映射到主机 9000。已有部署应保留自己的端口与代理配置。

### 了解更多
- [Docker Compose概述](https://docs.docker.com/compose/overview/)
- [OtterIO Docker快速入门](https://github.com/soulteary/otterio/blob/main/docs/zh_CN/README.md)
- [使用Docker Swarm部署OtterIO](https://github.com/soulteary/otterio/blob/main/docs/zh_CN/README.md)
- [OtterIO纠删码快速入门](https://github.com/soulteary/otterio/blob/main/docs/erasure/README.md)
