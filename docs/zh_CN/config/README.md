# OtterIO Server 配置指南

## 配置目录

当前服务端配置保存在 `otterio server` 指定的存储后端中，不会在程序旁生成供日常编辑的 `config.json`。使用 [OC 客户端](https://github.com/soulteary/oc)通过 OtterIO 管理接口查看和修改配置。

`--config-dir` 仍作为隐藏的旧版兼容选项保留，用于导入已有的 `config.json`。迁移成功后，旧文件会被重命名为 `config.json.deprecated`。新部署直接指定数据目录；自定义证书位置使用 `--certs-dir`。

```sh
otterio server /data
```

通过环境变量设置 root 凭据时，OtterIO 会使用这些凭据加密后端配置、IAM 数据和策略。重启或迁移部署时，需要保留对应的凭据。

### 证书目录
TLS 证书默认保存在 `${HOME}/.otterio/certs`。将 `public.crt` 和 `private.key` 放入该目录可启用 HTTPS，详见[本项目 TLS 指南](../tls/README.md)。

以下是一个具有TLS证书的OtterIO server的目录结构。

```sh
/home/user1/.otterio
└─ certs
   ├─ CAs
   ├─ private.key
   └─ public.crt
```

你可以使用`--certs-dir`命令行选项提供自定义certs目录。

#### 凭据
通过 `OTTERIO_ROOT_USER` 和 `OTTERIO_ROOT_PASSWORD` 设置 root 凭据，两个变量需要同时设置。旧名称 `OTTERIO_ACCESS_KEY` 和 `OTTERIO_SECRET_KEY` 仍为兼容保留；新部署使用 root 变量名称。

```
export OTTERIO_ROOT_USER=otterio
export OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
# 启动前将这两个值保存到你的凭据管理系统。
otterio server /data
```

已有部署应恢复保存的凭据，不能在每次重启时重新生成。初次部署参见[快速入门](../../../README_zh_CN.md)。

##### 使用新的凭据轮换加密

轮换加密后端的凭据时，在一次启动中通过 `_OLD` 变量提供原凭据，通过 root 变量提供新凭据。原凭据必须与现有配置的加密凭据一致。

```sh
# 先从凭据管理系统恢复当前真实 root 凭据。
: "${OTTERIO_ROOT_USER:?Restore the current root user first}"
: "${OTTERIO_ROOT_PASSWORD:?Restore the current root password first}"
export OTTERIO_ROOT_USER_OLD="$OTTERIO_ROOT_USER"
export OTTERIO_ROOT_PASSWORD_OLD="$OTTERIO_ROOT_PASSWORD"
export OTTERIO_ROOT_USER="otterio-$(openssl rand -hex 8)"
export OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
# 启动前将新的 root 值保存到凭据管理系统。
otterio server /data
```

服务器读取 `_OLD` 变量后会将其从自身的进程环境中移除。确认轮换成功后，在下次重启之前，从 shell 启动脚本、容器定义或服务文件中移除这些变量。

#### 区域
```
KEY:
region  服务器的物理位置标记

ARGS:
name     (string)    服务器的物理位置名字，例如 "us-west-rack2"
comment  (sentence)  为这个设置添加一个可选的注释
```

或者通过环境变量
```
KEY:
region  服务器的物理位置标记

ARGS:
OTTERIO_REGION_NAME     (string)    服务器的物理位置名字，例如  "us-west-rack2"
OTTERIO_REGION_COMMENT  (sentence)  为这个设置添加一个可选的注释
```

示例:

```sh
export OTTERIO_REGION_NAME="my_region"
otterio server /data
```

### 存储类型
纠删码集合包含 4 或 5 块盘时，STANDARD 默认校验配置为 `EC:2`；6 或 7 块盘时为 `EC:3`；8–16 块盘时为 `EC:4`。REDUCED_REDUNDANCY 默认为 `EC:2`。这些设置用于纠删码存储，详见[本项目存储类型指南](../erasure/storage-class/README.md)。

```
KEY:
storage_class  定义对象级冗余

ARGS:
standard  (string)    设置默认标准存储类型的奇偶校验计数，例如"EC:4"
rrs       (string)    设置默认低冗余存储类型的奇偶校验计数，例如"EC:2"
comment   (sentence)  为这个设置添加一个可选的注释
```

或者通过环境变量
```
KEY:
storage_class  定义对象级冗余

ARGS:
OTTERIO_STORAGE_CLASS_STANDARD  (string)    设置默认标准存储类型的奇偶校验计数，例如"EC:4"
OTTERIO_STORAGE_CLASS_RRS       (string)    设置默认低冗余存储类型的奇偶校验计数，例如"EC:2"
OTTERIO_STORAGE_CLASS_COMMENT   (sentence)  为这个设置添加一个可选的注释
```

### 缓存
OtterIO为主要的网关部署提供了缓存存储层，使您可以缓存内容以实现更快的读取速度，并节省从云中重复下载的成本。

```
KEY:
cache  添加缓存存储层

ARGS:
drives*  (csv)       逗号分隔的挂载点，例如 "/optane1,/optane2"
expiry   (number)    缓存有效期限（天），例如 "90"
quota    (number)    以百分比限制缓存驱动器的使用，例如 "90"
exclude  (csv)       逗号分隔的通配符排除模式，例如 "bucket/*.tmp,*.exe"
after    (number)    缓存对象之前的最小可访问次数
comment  (sentence)  为这个设置添加一个可选的注释
```

或者通过环境变量
```
KEY:
cache  添加缓存存储层

ARGS:
OTTERIO_CACHE_DRIVES*  (csv)       逗号分隔的挂载点，例如 "/optane1,/optane2"
OTTERIO_CACHE_EXPIRY   (number)    缓存有效期限（天），例如 "90"
OTTERIO_CACHE_QUOTA    (number)    以百分比限制缓存驱动器的使用，例如 "90"
OTTERIO_CACHE_EXCLUDE  (csv)       逗号分隔的通配符排除模式，例如 "bucket/*.tmp,*.exe"
OTTERIO_CACHE_AFTER    (number)    缓存对象之前的最小可访问次数
OTTERIO_CACHE_COMMENT  (sentence)  为这个设置添加一个可选的注释
```

#### Etcd
OtterIO支持在etcd上存储加密的IAM assets和Bucket DNS记录。

> NOTE: if *path_prefix* is set then OtterIO will not federate your buckets, namespaced IAM assets are assumed as isolated tenants, only buckets are considered globally unique but performing a lookup with a *bucket* which belongs to a different tenant will fail unlike federated setups where OtterIO would port-forward and route the request to relevant cluster accordingly. This is a special feature, federated deployments should not need to set *path_prefix*.

```
KEY:
etcd  为IAM and Bucket DNS联合多个集群

ARGS:
endpoints*       (csv)       以逗号分隔的etcd endpoint列表，例如 "http://localhost:2379"
path_prefix      (path)      为隔离租户提供的命名控件前缀,例如 "customer1/"
coredns_path     (path)      共享bucket DNS记录, 默认是 "/skydns"
client_cert      (path)      用于mTLS身份验证的客户端证书
client_cert_key  (path)      用于mTLS身份验证的客户端证书密钥
comment          (sentence)  为这个设置添加一个可选的注释
```

或者通过环境变量
```
KEY:
etcd  为IAM and Bucket DNS联合多个集群

ARGS:
OTTERIO_ETCD_ENDPOINTS*       (csv)       以逗号分隔的etcd endpoint列表，例如 "http://localhost:2379"
OTTERIO_ETCD_PATH_PREFIX      (path)      为隔离租户提供的命名控件前缀,例如 "customer1/"
OTTERIO_ETCD_COREDNS_PATH     (path)      共享bucket DNS记录, 默认是 "/skydns"
OTTERIO_ETCD_CLIENT_CERT      (path)      用于mTLS身份验证的客户端证书
OTTERIO_ETCD_CLIENT_CERT_KEY  (path)      用于mTLS身份验证的客户端证书密钥
OTTERIO_ETCD_COMMENT          (sentence)  为这个设置添加一个可选的注释
```

### API
默认 `requests_max=0` 时，服务器根据内存容量和磁盘数量计算并发请求上限。设置正数可指定部署的并发上限，分布式部署会将其分配到各个服务器节点。`requests_deadline` 控制请求等待空闲处理容量的时间，详见[本项目限流指南](../throttle/README.md)。以下列出常用 API 设置，完整参数请查询 OC 帮助。

```
KEY:
api  管理全局HTTP API调用的特定功能，例如限制，身份验证类型等.

ARGS:
requests_max       (number)    设置并发请求的最大数量，例如 "1600"
requests_deadline  (duration)  设置等待处理的API请求的期限，例如 "1m"
remote_transport_deadline (duration)  联邦实例间转发请求时远程传输的期限，例如 "2h"
cors_allow_origin  (csv)       设置CORS请求允许的来源列表,以逗号分割,例如 "https://example1.com,https://example2.com"
```

或者通过环境变量

```
OTTERIO_API_REQUESTS_MAX       (number)    设置并发请求的最大数量，例如 "1600"
OTTERIO_API_REQUESTS_DEADLINE  (duration)  设置等待处理的API请求的期限，例如 "1m"
OTTERIO_API_CORS_ALLOW_ORIGIN  (csv)       设置CORS请求允许的来源列表,以逗号分割,例如 "https://example1.com,https://example2.com"
OTTERIO_API_REMOTE_TRANSPORT_DEADLINE (duration)  联邦实例间远程传输的期限，例如 "2h"
```

#### 通知
OtterIO支持如下列表中的通知。要配置单个目标，请参阅[本项目存储桶通知指南](../bucket/notifications/README.md)的更多详细文档

```
notify_webhook        发布 bucket 通知到 webhook endpoints
notify_mysql          发布 bucket 通知到 MySQL databases
notify_postgres       发布 bucket 通知到 Postgres databases
notify_elasticsearch  发布 bucket 通知到 Elasticsearch endpoints
notify_redis          发布 bucket 通知到 Redis datastores
```

### 访问配置
使用当前 [OC 客户端](https://github.com/soulteary/oc)的 `oc admin config` get/set/reset/export/import 命令。OtterIO 管理接口使用 `/otterio/admin/v3` 路径，不应假定上游 `mc admin` 能直接兼容。

单端口部署使用 S3 地址配置别名：

```sh
oc alias set myotterio http://localhost:9000 "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" --api s3v4 --path on
```

如果服务端设置了 `--console-address ":9001"`，在别名命令中增加 `--admin-url http://localhost:9001`。这里填写管理根地址，不追加 `/otterio/` 或 `/otterio/admin/v3`；对象操作仍使用 9000 端口。管理入口的独立证书需要自定义 CA 时，使用 OC 的 `--admin-ca /path/to/admin-ca.pem`。

#### 列出所有可用的配置key
```
oc admin config set myotterio/
```

#### 获取每个key的帮助
```
oc admin config set myotterio/ <key>
```

例如: `oc admin config set myotterio/ etcd` 会返回 `etcd` 可用的配置参数

```
oc admin config set myotterio/ etcd
KEY:
etcd  federate multiple clusters for IAM and Bucket DNS

ARGS:
endpoints*       (csv)       comma separated list of etcd endpoints e.g. "http://localhost:2379"
path_prefix      (path)      namespace prefix to isolate tenants e.g. "customer1/"
coredns_path     (path)      shared bucket DNS records, default is "/skydns"
client_cert      (path)      client cert for mTLS authentication
client_cert_key  (path)      client cert key for mTLS authentication
comment          (sentence)  optionally add a comment to this setting
```

要获取每个配置参数的等效ENV，请使用`--env`标志
```
oc admin config set myotterio/ etcd --env
KEY:
etcd  federate multiple clusters for IAM and Bucket DNS

ARGS:
OTTERIO_ETCD_ENDPOINTS*       (csv)       comma separated list of etcd endpoints e.g. "http://localhost:2379"
OTTERIO_ETCD_PATH_PREFIX      (path)      namespace prefix to isolate tenants e.g. "customer1/"
OTTERIO_ETCD_COREDNS_PATH     (path)      shared bucket DNS records, default is "/skydns"
OTTERIO_ETCD_CLIENT_CERT      (path)      client cert for mTLS authentication
OTTERIO_ETCD_CLIENT_CERT_KEY  (path)      client cert key for mTLS authentication
OTTERIO_ETCD_COMMENT          (sentence)  optionally add a comment to this setting
```

此行为在所有key中都是一致的，每个key都带有可用的示例文档。

## 无需重启的动态配置

`api`、`compression`、`scanner` 和 `heal` 子系统支持运行时更新；压缩仍要求存储后端支持。环境变量优先于存储的配置。通过进程环境提供的值不能用 `oc admin config set` 改写，修改这些值需要更新环境并重启服务。

### 使用情况采集器

数据使用情况采集器默认启用。`delay` 是每次操作之间的等待倍数，默认为 `10`；值越小扫描越快，设为 `0` 会取消这种等待。`max_wait` 限制每次等待时间，默认为 `15s`；`cycle` 是两轮扫描之间的间隔，默认为 `1m`。

```sh
oc admin config set myotterio scanner
oc admin config set myotterio scanner delay=30 max_wait=15s cycle=1m
```

示例将等待倍数提高到 30，减少扫描对资源的占用，但用量和生命周期状态的更新也会更慢。对应的环境变量为 `OTTERIO_SCANNER_DELAY`、`OTTERIO_SCANNER_MAX_WAIT` 和 `OTTERIO_SCANNER_CYCLE`。

> 数据使用情况采集器不支持网关部署模式。

### 修复

`heal` 子系统的有效参数是 `bitrotscan`、`max_sleep` 和 `max_io`，默认分别为 `off`、`1s` 和 `10`。当并发请求超过 `max_io` 时，修复会通过 `max_sleep` 控制等待；`max_delay` 不是有效配置键。

```sh
oc admin config set myotterio heal
oc admin config set myotterio heal max_sleep=300ms max_io=100
```

对应环境变量为 `OTTERIO_HEAL_BITROTSCAN`、`OTTERIO_HEAL_MAX_SLEEP` 和 `OTTERIO_HEAL_MAX_IO`。

> 修复不支持网关部署模式。

## 仅通过环境变量设置的选项

### 浏览器

通过 `OTTERIO_BROWSER` 开启或关闭 Web UI，默认是 `on`。设为 `off` 不会关闭 S3 或管理接口。

示例:

```sh
export OTTERIO_BROWSER=off
otterio server /data
```

### 独立的 Web 控制台监听端口

默认情况下，Web 控制台与 S3 API 共用 ``--address`` 指定的监听器。如果希望将 Web UI 与 Admin API 单独绑定到另一个端口，可以通过 ``--console-address`` 命令行参数或 ``OTTERIO_BROWSER_ADDRESS`` 环境变量启用。开启该模式后，S3 监听端口将不再把浏览器请求重定向到 Web UI——浏览器访问 S3 端口会按常规 S3 接口返回错误响应。

控制台端口必须与 S3 端口不同，否则服务启动会直接失败。

示例:

```sh
# 命令行参数
otterio server --address ":9000" --console-address ":9001" /data

# 环境变量（等效写法）
export OTTERIO_BROWSER_ADDRESS=":9001"
otterio server --address ":9000" /data
```

### 控制台监听器使用独立 TLS 证书目录

启用 ``--console-address`` 后，可以通过 ``--console-certs-dir``（或环境变量 ``OTTERIO_BROWSER_CERTS_DIR``）让控制台监听器使用独立的 TLS 证书。该目录需要包含 ``public.crt`` 与 ``private.key``，目录结构与 ``--certs-dir`` 相同；未指定时控制台监听器复用 ``--certs-dir`` 加载的证书。

示例:

```sh
otterio server \
  --address ":9000" \
  --console-address ":9001" \
  --certs-dir /etc/otterio/certs/s3 \
  --console-certs-dir /etc/otterio/certs/console \
  /data
```

``--console-certs-dir`` 需要在 ``--console-address`` 已设置时使用，否则启动直接失败。

### 域名

默认情况下，OtterIO支持格式为 http://mydomain.com/bucket/object 的路径类型请求。
`OTTERIO_DOMAIN` 环境变量被用来启用虚拟主机类型请求。 如果请求的`Host`头信息匹配 `(.+).mydomain.com`，则匹配的模式 `$1` 被用作 bucket， 并且路径被用作object. 更多路径类型和虚拟主机类型的信息参见[这里](http://docs.aws.amazon.com/AmazonS3/latest/dev/RESTAPI.html)
示例:

```sh
export OTTERIO_DOMAIN=mydomain.com
otterio server /data
```

`OTTERIO_DOMAIN`环境变量支持逗号分隔的多域名配置
```sh
export OTTERIO_DOMAIN=sub1.mydomain.com,sub2.mydomain.com
otterio server /data
```

## 进一步探索
* [OtterIO 快速入门指南](../../../README_zh_CN.md)
* [本项目 TLS 指南](../tls/README.md)
* [OC 管理指南](https://github.com/soulteary/oc/blob/main/docs/zh_CN/administration.md)
