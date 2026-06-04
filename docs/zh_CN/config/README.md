# OtterIO Server 配置指南

## 配置目录

默认的配置目录是 `${HOME}/.otterio`，你可以使用`--config-dir`命令行选项重写之。OtterIO server在首次启动时会生成一个新的`config.json`，里面带有自动生成的访问凭据。

```sh
otterio server --config-dir /etc/otterio /data
```

截止到 OtterIO `RELEASE.2018-08-02T23-11-36Z` 版本, OtterIO server 的配置文件(`config.json`) 被存储在通过 `--config-dir` 指定的目录或者默认的 `${HOME}/.otterio` 目录。 但是从 `RELEASE.2018-08-18T03-49-57Z` 版本之后, 配置文件 (仅仅), 已经被迁移到存储后端 (存储后端指的是启动一个服务器的时候，传递给OtterIO server的目录)。

您可以使用`--config-dir`指定现有配置的位置, OtterIO 会迁移 `config.json` 配置到你的存储后端。 迁移成功后，你当前 `--config-dir` 目录中的 `config.json` 将被重命名为 `config.json.deprecated`。 迁移后，所有现有配置都将得到保留。

此外，`--config-dir`现在是一个旧配置，计划在将来删除，因此请相应地更新本地startup和ansible脚本。

```sh
otterio server /data
```

OtterIO还使用管理员凭据对所有配置，IAM和策略内容进行加密。

### 证书目录
TLS证书存在``${HOME}/.otterio/certs``目录下，你需要将证书放在该目录下来启用`HTTPS` 。如果你是一个乐学上进的好青年，这里有一本免费的秘籍传授一你: [如何使用TLS安全的访问otterio](https://docs.min.io/cn/how-to-secure-access-to-minio-server-with-tls).

以下是一个具有TLS证书的OtterIO server的目录结构。

```sh
$ mc tree --files ~/.otterio
/home/user1/.otterio
└─ certs
   ├─ CAs
   ├─ private.key
   └─ public.crt
```

你可以使用`--certs-dir`命令行选项提供自定义certs目录。

#### 凭据
只能通过环境变量`OTTERIO_ROOT_USER` 和 `OTTERIO_ROOT_PASSWORD` 更改OtterIO的admin凭据和root凭据。使用这两个值的组合，OtterIO加密存储在后端的配置

```
export OTTERIO_ROOT_USER=otterio
export OTTERIO_ROOT_PASSWORD=otterio13
otterio server /data
```

##### 使用新的凭据轮换加密

另外，如果您想更改管理员凭据，则OtterIO将自动检测到该凭据，并使用新凭据重新加密，如下所示。一次只需要设置如下所示的环境变量即可轮换加密配置。

> 旧的环境变量永远不会在内存中被记住，并且在使用新凭据迁移现有内容后立即销毁。在服务器再次成功重启后，你可以安全的删除它们。

```
export OTTERIO_ROOT_USER=newotterio
export OTTERIO_ROOT_PASSWORD=newotterio123
export OTTERIO_ROOT_USER_OLD=otterio
export OTTERIO_ROOT_PASSWORD_OLD=otterio123
otterio server /data
```

迁移完成后, 服务器会自动的取消进程空间中的`OTTERIO_ROOT_USER_OLD` and `OTTERIO_ROOT_PASSWORD_OLD`设置。

> **注意: 在下一次服务重新启动前，要确保移除脚本或者服务文件中的 `OTTERIO_ROOT_USER_OLD` and `OTTERIO_ROOT_PASSWORD_OLD`， 避免现有的内容被双重加密**

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
默认情况下，标准存储类型的奇偶校验值设置为N/2，低冗余的存储类型奇偶校验值设置为2。在[此处](https://github.com/minio/minio/blob/master/docs/zh_CN/erasure/storage-class/README.md)了解有关OtterIO服务器存储类型的更多信息。

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
默认情况下，服务器/集群同时处理的并发请求数没有限制。 但是，可以使用API子系统强加这种限制。 在[此处](https://github.com/minio/minio/blob/master/docs/zh_CN/throttle/README.md)阅读有关OtterIO服务器中限制限制的更多信息。

```
KEY:
api  管理全局HTTP API调用的特定功能，例如限制，身份验证类型等.

ARGS:
requests_max       (number)    设置并发请求的最大数量，例如 "1600"
requests_deadline  (duration)  设置等待处理的API请求的期限，例如 "1m"
ready_deadline     (duration)  设置健康检查API /otterio/health/ready的期限，例如 "1m"
cors_allow_origin  (csv)       设置CORS请求允许的来源列表,以逗号分割,例如 "https://example1.com,https://example2.com"
```

或者通过环境变量

```
OTTERIO_API_REQUESTS_MAX       (number)    设置并发请求的最大数量，例如 "1600"
OTTERIO_API_REQUESTS_DEADLINE  (duration)  设置等待处理的API请求的期限，例如 "1m"
OTTERIO_API_CORS_ALLOW_ORIGIN  (csv)       设置CORS请求允许的来源列表,以逗号分割,例如 "https://example1.com,https://example2.com"
```

#### 通知
OtterIO支持如下列表中的通知。要配置单个目标，请参阅[此处](https://docs.min.io/cn/minio-bucket-notification-guide.html)的更多详细文档

```
notify_webhook        发布 bucket 通知到 webhook endpoints
notify_mysql          发布 bucket 通知到 MySQL databases
notify_postgres       发布 bucket 通知到 Postgres databases
notify_elasticsearch  发布 bucket 通知到 Elasticsearch endpoints
notify_redis          发布 bucket 通知到 Redis datastores
```

### 访问配置
可以使用[`mc admin config` get/set/reset/export/import commands](https://github.com/minio/mc/blob/master/docs/minio-admin-complete-guide.md)命令应用所有配置的更改.

#### 列出所有可用的配置key
```
~ mc admin config set myotterio/
```

#### 获取每个key的帮助
```
~ mc admin config set myotterio/ <key>
```

例如: `mc admin config set myotterio/ etcd` 会返回 `etcd` 可用的配置参数

```
~ mc admin config set play/ etcd
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
~ mc admin config set play/ etcd --env
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

## 环境变量仅有的配置 (配置文件中没有)

#### 使用情况采集器
> 注意: 数据使用情况采集器不支持网关部署模式。

数据使用情况采集器默认是启用的，通过Envs可以设置更多的交错延迟。

采集器能适应系统速度，并在系统负载时完全暂停。 可以调整采集器的速度，从而达到延迟更新的效果。 每次采集操作之间的延迟都可以通过环境变量`OTTERIO_SCANNER_DELAY`来调整。 默认情况下，该值为10。 这意味着采集每次操作都将休眠*10x*的时间。

大多数设置要让采集器足够慢，这样不会影响整体的系统性能。
设置 `OTTERIO_SCANNER_DELAY` 为一个 *较低* 的值可以让采集器更快，并且设置为0的时候，可以让采集器全速运行（不推荐）。 设置一个较高的值可以让采集器变慢，进一步减少资源的消耗。

示例: 如下设置将使采集器的速度降低三倍, 减少了系统资源的使用，但是反映到更新的延迟会增加。

```sh
export OTTERIO_SCANNER_DELAY=30
otterio server /data
```

### 浏览器

开启或关闭浏览器访问，默认是开启的，你可以通过``OTTERIO_BROWSER``环境变量进行修改。

示例:

```sh
export OTTERIO_BROWSER=off
otterio server /data
```

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
* [OtterIO快速入门指南](https://docs.min.io/cn/minio-quickstart-guide)
* [使用TLS安全的访问Otterio服务](https://docs.min.io/cn/how-to-secure-access-to-minio-server-with-tls.html)
