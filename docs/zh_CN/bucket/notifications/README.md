# OtterIO 存储桶通知指南

存储桶通知会发布符合过滤条件的对象操作事件。网关模式中，只有 NAS 网关支持存储桶通知。

## 事件与通知目标

当前源码识别以下事件名称。每一行中，首个名称之后的名称使用相同前缀：

- `s3:ObjectCreated:Put`, `Post`, `Copy`, `CompleteMultipartUpload`, `PutTagging`, `DeleteTagging`, `PutRetention`, `PutLegalHold`.
- `s3:ObjectRemoved:Delete`, `DeleteMarkerCreated`.
- `s3:ObjectAccessed:Get`, `Head`, `GetRetention`, `GetLegalHold`.
- `s3:Replication:OperationFailedReplication`, `OperationCompletedReplication`, `OperationNotTracked`, `OperationMissedThreshold`, `OperationReplicatedAfterThreshold`.
- `s3:ObjectRestore:Post`, `Completed`; `s3:ObjectTransition:Failed`, `Complete`.

全局事件 `s3:BucketCreated:*` 和 `s3:BucketRemoved:*` 通过监听通知接口提供，不用于单个存储桶的目标配置。事件名称定义见 [pkg/event/name.go](../../../../pkg/event/name.go)。

使用 [OC 客户端](https://github.com/soulteary/oc)的 `oc event add`、`oc event list` 和 `oc event listen`，或者使用 [OtterIO Go SDK](https://github.com/soulteary/otterio-sdk)的存储桶通知接口。通知为包含 `Records` 数组的 JSON 对象，结构参考 [S3 通知格式](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-content-structure.html)；OtterIO 的 `eventSource` 为 `otterio:s3`。

目前支持 Redis、MySQL、PostgreSQL、Elasticsearch 和 Webhook。Kafka、NATS、NATS Streaming、NSQ、AMQP 和 MQTT 通知目标已经移除。

## 前提条件

- 安装 [OtterIO](../../../../README_zh_CN.md)和当前 [OC 客户端](https://github.com/soulteary/oc)。
- 使用有权修改配置和存储桶通知的凭据设置别名：

```sh
oc alias set myotterio http://localhost:9000 "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" --api s3v4 --path on
oc admin config set myotterio
```

如果服务端设置了 `--console-address ":9001"`，在别名命令中增加 `--admin-url http://localhost:9001`。对象和存储桶通知操作使用 9000 端口，`oc admin config` 使用 9001 管理入口。管理地址填写根 URL，不追加 `/otterio/` 或 `/otterio/admin/v3`；不应假定上游 `mc admin` 能直接兼容。

修改目标之前，先查询服务端提供的帮助：

```sh
oc admin config set myotterio notify_webhook
oc admin config set myotterio notify_webhook --env
```

五个通知配置子系统分别为 `notify_webhook`、`notify_mysql`、`notify_postgres`、`notify_elasticsearch` 和 `notify_redis`。下文输出为示例，实际值以你的服务端返回结果为准。

- 参数后的 `*` 表示必填，值后的 `*` 表示默认值。
- 使用 `enable=on` 或对应的 `OTTERIO_NOTIFY_<TARGET>_ENABLE=on` 环境变量启用目标；修改通知目标后需要重启服务。
- 命名目标的所有环境变量都使用相同后缀，例如 `OTTERIO_NOTIFY_WEBHOOK_ENABLE_photos` 和 `OTTERIO_NOTIFY_WEBHOOK_ENDPOINT_photos`。不带后缀的变量配置默认目标。
- ARN 格式为 `arn:otterio:sqs:<region>:<target-id>:<type>`。复制服务器输出的 ARN；示例的区域为空，设置 `OTTERIO_REGION_NAME` 后该部分会改变。PostgreSQL 的配置键是 `notify_postgres`，ARN 类型则是 `postgresql`。
- 持久化投递需要设置非空、绝对路径的 `queue_dir`，确保每个服务器实例的目录可写且存储可持久化。空目录配置会关闭磁盘队列。`queue_limit=0` 表示使用默认 100,000 条队列上限，并受进程文件上限约束，不表示无限容量。队列满时会返回错误，消费者需要处理重试产生的重复事件。
- 通知只覆盖配置启用之后发生的匹配操作。`namespace` 目标不会自动补入存储桶中已有的对象。
- 目标地址由 OtterIO 服务器所在的网络环境解析；在容器中，`localhost` 指向容器自身。

目标配置指南：[Elasticsearch](#Elasticsearch)、[Redis](#Redis)、[PostgreSQL](#PostgreSQL)、[MySQL](#MySQL)、[Webhook](#webhooks)。

<a name="Elasticsearch"></a>
## 使用Elasticsearch发布OtterIO事件

安装 [Elasticsearch](https://www.elastic.co/downloads/elasticsearch) 。

这个通知目标支持两种格式: _namespace_ 和 _access_。

如果使用的是 _namespace_ 格式, OtterIO将桶中的对象与索引中的文档进行同步。对于OtterIO中的每个事件，服务器都会使用事件中的存储桶和对象名称作为文档ID创建一个文档。事件的其他细节存储在document的正文中。因此，如果一个已经存在的对象在OtterIO中被覆盖，在ES中的相对应的document也会被更新。如果一个对象被删除，相对应的document也会从index中删除。

如果使用的是 _access_ 格式，OtterIO将事件作为document附加到ES的index中。对于每个事件，将带有事件详细信息的文档（文档的时间戳设置为事件的时间戳）附加到索引。这个文档的ID是由ES随机生成的。在 _access_ 格式下，不会有文档被删除或者修改。

以下步骤演示 `namespace` 格式，`access` 格式的配置过程类似。


### 第一步：确保至少满足最低要求

内置目标使用 [`github.com/olivere/elastic/v7`](../../../../pkg/event/target/elasticsearch.go)。接入前需要验证与实际 Elasticsearch API 的兼容性，客户端依赖本身不能证明与所有服务端主版本兼容。

### 第二步：把ES集成到OtterIO中

Elasticsearch的配置信息位于`notify_elasticsearch`这个顶级的key下。在这里为你的Elasticsearch实例创建配置信息键值对。key是你的Elasticsearch endpoint的名称，value是下面表格中列列的键值对集合。

```
KEY:
notify_elasticsearch[:name]  发布存储桶通知到Elasticsearch endpoints

ARGS:
url*         (url)                Elasticsearch服务器的地址，以及可选的身份验证信息
index*       (string)             存储/更新事件的Elasticsearch索引，索引是自动创建的
username     (string)             Elasticsearch basic-auth 用户名
password     (string)             Elasticsearch basic-auth 密码
format*      (namespace*|access)  是`namespace` 还是 `access`，默认是 'namespace'
queue_dir    (path)               未发送消息的暂存目录 例如 '/home/events'
queue_limit  (number)             未发送消息的最大限制, 默认是'100000'
comment      (sentence)           可选的注释
```

或者通过环境变量(配置说明参考上面)

```
KEY:
notify_elasticsearch[:name]  publish bucket notifications to Elasticsearch endpoints

ARGS:
OTTERIO_NOTIFY_ELASTICSEARCH_ENABLE*      (on|off)             enable notify_elasticsearch target, default is 'off'
OTTERIO_NOTIFY_ELASTICSEARCH_URL*         (url)                Elasticsearch server's address, with optional authentication info
OTTERIO_NOTIFY_ELASTICSEARCH_INDEX*       (string)             Elasticsearch index to store/update events, index is auto-created
OTTERIO_NOTIFY_ELASTICSEARCH_USERNAME     (string)             username for Elasticsearch basic-auth
OTTERIO_NOTIFY_ELASTICSEARCH_PASSWORD     (string)             password for Elasticsearch basic-auth
OTTERIO_NOTIFY_ELASTICSEARCH_FORMAT*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
OTTERIO_NOTIFY_ELASTICSEARCH_QUEUE_DIR    (path)               staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_ELASTICSEARCH_QUEUE_LIMIT  (number)             maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_ELASTICSEARCH_COMMENT      (sentence)           optionally add a comment to this setting
```

比如: `http://localhost:9200` 或者带有授权信息的 `http://<username>:<password>@127.0.0.1:9200`

OtterIO支持持久事件存储。持久存储将在Elasticsearch broker离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

如果Elasticsearch启用了身份验证, 凭据可以通过格式为`PROTO://USERNAME:PASSWORD@ELASTICSEARCH_HOST:PORT`的`url`参数，提供给OtterIO。

更新配置前，可以通过`oc admin config get`命令获取当前配置。

```sh
$ oc admin config get myotterio/ notify_elasticsearch
notify_elasticsearch:1 queue_limit="0"  url="" format="namespace" index="" queue_dir=""
```

使用`oc admin config set`命令更新配置后，重启OtterIO Server让配置生效。 如果一切顺利，OtterIO Server会在启动时输出一行信息，类似`SQS ARNs: arn:otterio:sqs::1:elasticsearch`。

```sh
oc admin config set myotterio notify_elasticsearch:1 enable=on url="http://127.0.0.1:9200" format="namespace" index="otterio_events"
```

请注意, 根据你的需要，你可以添加任意多个ES server endpoint，只要提供ES实例的标识符（如上例中的“ 1”）和每个实例配置参数的信息即可。

### 第三步：使用OtterIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知。一旦有文件被创建或者覆盖，一个新的ES的document会被创建或者更新到之前咱配的index里。如果一个已经存在的对象被删除，这个对应的document也会从index中删除。因此，这个ES index里的行，就映射着`images`存储桶里的`.jpg`对象。

要配置这种存储桶通知，我们需要用到前面步骤OtterIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`oc`这个工具，这些配置信息很容易就能添加上。假设咱们的OtterIO服务别名叫`myotterio`,可执行下列脚本：

```
oc mb myotterio/images
oc event add  myotterio/images arn:otterio:sqs::1:elasticsearch --suffix .jpg --event put,delete
oc event list myotterio/images
arn:otterio:sqs::1:elasticsearch s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### 第四步：验证 Elasticsearch

上传一张 JPEG 图片，然后查询已配置的索引：

```sh
oc cp myphoto.jpg myotterio/images
curl "http://localhost:9200/otterio_events/_search?pretty=true"
```

结果中应包含 `_id` 为 `images/myphoto.jpg` 的文档，`_source.Records[0].eventName` 为 `s3:ObjectCreated:Put`。请求 ID、时间戳和事件元数据会因部署而不同。

<a name="Redis"></a>
## 使用Redis发布OtterIO事件

安装 [Redis](https://redis.io/downloads/)，在执行配置示例之前，从凭据管理系统中将它的真实密码恢复到 `OTTERIO_REDIS_PASSWORD`。

这种通知目标支持两种格式: _namespace_ 和 _access_。

如果用的是 _namespace_ 格式，OtterIO将存储桶里的对象同步成Redis hash中的条目。对于每一个条目，对应一个存储桶里的对象，其key都被设为"存储桶名称/对象名称"，value都是一个有关这个OtterIO对象的JSON格式的事件数据。如果对象更新或者删除，hash中对象的条目也会相应的更新或者删除。

如果使用的是 _access_ ,OtterIO使用[RPUSH](https://redis.io/commands/rpush)将事件添加到list中。这个list中每一个元素都是一个JSON格式的list,这个list中又有两个元素，第一个元素是时间戳的字符串，第二个元素是一个含有在这个存储桶上进行操作的事件数据的JSON对象。在这种格式下，list中的元素不会更新或者删除。

下面的步骤展示如何在`namespace`和`access`格式下使用通知目标。

### 第一步：集成Redis到OtterIO

Redis 的配置位于 `notify_redis` 子系统中。在这里为你的Redis实例创建配置信息键值对。key是你的Redis endpoint的名称，value是下面表格中列的键值对集合。

```
KEY:
notify_redis[:name]  发布存储桶通知到Redis

ARGS:
address*     (address)            Redis服务器的地址. 例如: `localhost:6379`
key*         (string)             存储/更新事件的Redis key, key会自动创建
format*      (namespace*|access)  是`namespace` 还是 `access`，默认是 'namespace'
password     (string)             Redis服务器的密码
queue_dir    (path)               未发送消息的暂存目录 例如 '/home/events'
queue_limit  (number)             未发送消息的最大限制, 默认是'100000'
comment      (sentence)           可选的注释说明
```

或者通过环境变量(配置说明参考上面)

```
KEY:
notify_redis[:name]  publish bucket notifications to Redis datastores

ARGS:
OTTERIO_NOTIFY_REDIS_ENABLE*      (on|off)             enable notify_redis target, default is 'off'
OTTERIO_NOTIFY_REDIS_ADDRESS*     (address)            Redis server address, e.g. 'localhost:6379'
OTTERIO_NOTIFY_REDIS_KEY*         (string)             Redis key to store/update events, key is auto-created
OTTERIO_NOTIFY_REDIS_FORMAT*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
OTTERIO_NOTIFY_REDIS_PASSWORD     (string)             Redis server password
OTTERIO_NOTIFY_REDIS_QUEUE_DIR    (path)               staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_REDIS_QUEUE_LIMIT  (number)             maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_REDIS_COMMENT      (sentence)           optionally add a comment to this setting
```

OtterIO支持持久事件存储。持久存储将在Redis broker离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

更新配置前，可以通过`oc admin config get`命令获取当前配置。

```sh
$ oc admin config get myotterio/ notify_redis
notify_redis:1 address="" format="namespace" key="" password="" queue_dir="" queue_limit="0"
```

使用`oc admin config set`命令更新配置后，重启OtterIO Server让配置生效。 如果一切顺利，OtterIO Server会在启动时输出一行信息，类似`SQS ARNs: arn:otterio:sqs::1:redis`。

```sh
$ oc admin config set myotterio/ notify_redis:1 enable=on address="127.0.0.1:6379" format="namespace" key="bucketevents" password="$OTTERIO_REDIS_PASSWORD" queue_dir="" queue_limit="0"
```

请注意, 根据你的需要，你可以添加任意多个Redis server endpoint，只要提供Redis实例的标识符（如上例中的“ 1”）和每个实例配置参数的信息即可。

### 第二步: 使用OtterIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知。当一个JPEG文件被创建或者覆盖，一个新的key会被创建,或者一个已经存在的key就会被更新到之前配置好的redis hash里。如果一个已经存在的对象被删除，这个对应的key也会从hash中删除。因此，这个Redis hash里的行，就映射着`images`存储桶里的`.jpg`对象。

要配置这种存储桶通知，我们需要用到前面步骤OtterIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`oc`这个工具，这些配置信息很容易就能添加上。假设咱们的OtterIO服务别名叫`myotterio`,可执行下列脚本：

```
oc mb myotterio/images
oc event add myotterio/images arn:otterio:sqs::1:redis --suffix .jpg --event put,delete
oc event list myotterio/images
arn:otterio:sqs::1:redis s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### 第三步：验证 Redis

上传一张 JPEG 图片，然后从已配置的 `bucketevents` hash 读取对应条目：

```sh
oc cp myphoto.jpg myotterio/images
REDISCLI_AUTH="$OTTERIO_REDIS_PASSWORD" redis-cli HGET bucketevents images/myphoto.jpg
```

返回的 JSON 应包含 `Records`，其中 `eventName` 为 `s3:ObjectCreated:Put`，对象 key 为 `myphoto.jpg`。使用 `access` 格式时，改用 `LRANGE bucketevents 0 -1` 查看 Redis list。

<a name="PostgreSQL"></a>
## 使用PostgreSQL发布OtterIO事件

使用 `connection_string` 配置连接。当前通知子系统不接受旧版 `host`、`port`、`username`、`password` 和 `database` 配置键。

安装 [PostgreSQL](https://www.postgresql.org/)，创建 `otterio_events` 数据库和通知专用账户 `otterio_events_user`，允许该账户创建事件表并新增、更新和删除行。从凭据管理系统中将它的真实密码恢复到 `OTTERIO_POSTGRES_PASSWORD`。示例使用本地数据库连接。

这个通知目标支持两种格式: _namespace_ 和 _access_。

如果使用的是 _namespace_ 格式，OtterIO将存储桶里的对象同步成数据库表中的行。每一行有两列：key和value。key是这个对象的存储桶名字加上对象名，value都是一个有关这个OtterIO对象的JSON格式的事件数据。如果对象更新或者删除，表中相应的行也会相应的更新或者删除。

如果使用的是 _access_,OtterIO将将事件添加到表里，行有两列：event_time 和 event_data。event_time是事件在OtterIO server里发生的时间，event_data是有关这个OtterIO对象的JSON格式的事件数据。在这种格式下，不会有行会被删除或者修改。

下面的步骤展示的是如何在`namespace`格式下使用通知目标，`_access_`差不多，不再赘述，我相信你可以触类旁通，举一反三，不要让我失望哦。

### 第一步：确保确保至少满足最低要求

OtterIO要求PostgresSQL9.5版本及以上。 OtterIO用了PostgreSQL9.5引入的[`INSERT ON CONFLICT`](https://www.postgresql.org/docs/9.5/static/sql-insert.html#SQL-ON-CONFLICT) (aka UPSERT) 特性,以及9.4引入的 [JSONB](https://www.postgresql.org/docs/9.4/static/datatype-json.html) 数据类型。

### 第二步：集成PostgreSQL到OtterIO

PostgreSQL的配置信息位于`notify_postgres`这个顶级的key下。在这里为你的PostgreSQL实例创建配置信息键值对。key是你的PostgreSQL endpoint的名称，value是下面表格中列列的键值对集合。

```
KEY:
notify_postgres[:name]  发布存储桶通知到Postgres数据库

ARGS:
connection_string*  (string)             Postgres server的连接字符串，例如 "host=localhost port=5432 dbname=otterio_events user=postgres password=<password> sslmode=disable"
table*              (string)             存储/更新事件的数据库表名, 表会自动被创建
format*             (namespace*|access)  'namespace'或者'access', 默认是'namespace'
queue_dir           (path)               未发送消息的暂存目录 例如 '/home/events'
queue_limit         (number)             未发送消息的最大限制, 默认是'100000'
comment             (sentence)           可选的注释说明
max_open_connections (number)           数据库最大连接数，默认 2；0 表示不限制
```

或者通过环境变量（说明详见上面）
```
KEY:
notify_postgres[:name]  publish bucket notifications to Postgres databases

ARGS:
OTTERIO_NOTIFY_POSTGRES_ENABLE*             (on|off)             enable notify_postgres target, default is 'off'
OTTERIO_NOTIFY_POSTGRES_CONNECTION_STRING*  (string)             Postgres server connection-string e.g. "host=localhost port=5432 dbname=otterio_events user=postgres password=<password> sslmode=disable"
OTTERIO_NOTIFY_POSTGRES_TABLE*              (string)             DB table name to store/update events, table is auto-created
OTTERIO_NOTIFY_POSTGRES_FORMAT*             (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
OTTERIO_NOTIFY_POSTGRES_QUEUE_DIR           (path)               staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_POSTGRES_QUEUE_LIMIT         (number)             maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_POSTGRES_COMMENT             (sentence)           optionally add a comment to this setting
OTTERIO_NOTIFY_POSTGRES_MAX_OPEN_CONNECTIONS (number)  maximum number of open database connections, defaults to 2
```

OtterIO支持持久事件存储。持久存储将在PostgreSQL连接离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

注意这里为了演示, 我们禁止了SSL. 处于安全起见, 不推荐用于生产.
更新配置前, 使用`oc admin config get`命令获取当前配置。

```sh
$ oc admin config get myotterio notify_postgres
notify_postgres:1 queue_dir="" connection_string="" queue_limit="0"  table="" format="namespace"
```

使用 `oc admin config set` 命令更新配置后，重启OtterIO Server让配置生效。 如果一切顺利，OtterIO Server会在启动时输出一行信息，类似 `SQS ARNs: arn:otterio:sqs::1:postgresql`。

```sh
$ oc admin config set myotterio notify_postgres:1 enable=on connection_string="host=localhost port=5432 dbname=otterio_events user=otterio_events_user password=${OTTERIO_POSTGRES_PASSWORD} sslmode=disable" table="bucketevents" format="namespace"
```

请注意, 根据你的需要，你可以添加任意多个PostgreSQL server endpoint，只要提供PostgreSQL实例的标识符（如上例中的“ 1”）和每个实例配置参数的信息即可。

### 第三步：使用OtterIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知，一旦上有文件上传到存储桶中，PostgreSQL中会insert一条新的记录或者一条已经存在的记录会被update，如果一个存在对象被删除，一条对应的记录也会从PostgreSQL表中删除。因此，PostgreSQL表中的行，对应的就是存储桶里的一个对象。

要配置这种存储桶通知，我们需要用到前面步骤中OtterIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`oc`这个工具，这些配置信息很容易就能添加上。假设OtterIO服务别名叫`myotterio`,可执行下列脚本：

```
# Create bucket named `images` in myotterio
oc mb myotterio/images
# Add notification configuration on the `images` bucket using the PostgreSQL ARN. The --suffix argument filters events.
oc event add myotterio/images arn:otterio:sqs::1:postgresql --suffix .jpg --event put,delete
# Print out the notification configuration on the `images` bucket.
oc event list myotterio/images
arn:otterio:sqs::1:postgresql s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### 第四步：验证 PostgreSQL

上传一张 JPEG 图片，用通知账户连接数据库并查询事件：

```sh
oc cp myphoto.jpg myotterio/images
psql -h 127.0.0.1 -U otterio_events_user -d otterio_events
```

```sql
SELECT key, value->'Records'->0->>'eventName' AS event_name FROM bucketevents;
```

```text
key                | event_name
-------------------+---------------------
images/myphoto.jpg | s3:ObjectCreated:Put
```

<a name="MySQL"></a>

## 使用MySQL发布OtterIO事件

使用 `dsn_string` 配置连接。当前通知子系统不接受旧版 `host`、`port`、`username`、`password` 和 `database` 配置键。

安装 [MySQL](https://dev.mysql.com/downloads/mysql/)，创建 `otteriodb` 数据库和通知专用账户 `otterio_events_user`，允许该账户创建事件表并新增、更新和删除行。从凭据管理系统中将它的真实密码恢复到 `OTTERIO_MYSQL_PASSWORD`。

这个通知目标支持两种格式: _namespace_ 和 _access_。

如果使用的是 _namespace_ 格式，OtterIO将存储桶里的对象同步成数据库表中的行。每一行有两列：key_name和value。key_name是这个对象的存储桶名字加上对象名，value都是一个有关这个OtterIO对象的JSON格式的事件数据。如果对象更新或者删除，表中相应的行也会相应的更新或者删除。

如果使用的是 _access_,OtterIO将将事件添加到表里，行有两列：event_time 和 event_data。event_time是事件在OtterIO server里发生的时间，event_data是有关这个OtterIO对象的JSON格式的事件数据。在这种格式下，不会有行会被删除或者修改。

下面的步骤展示的是如何在`namespace`格式下使用通知目标，`_access_`差不多，不再赘述。

### 第一步：确保确保至少满足最低要求

OtterIO要求MySQL 版本 5.7.8及以上，OtterIO使用了MySQL5.7.8版本引入的 [JSON](https://dev.mysql.com/doc/refman/5.7/en/json.html) 数据类型。我们使用的是MySQL5.7.17进行的测试。

### 第二步：集成MySQL到OtterIO

MySQL配置位于 `notify_mysql`key下. 在这里为你的 MySQL 实例创建配置信息键值对。key 是你的 MySQL endpoint的名称，value是下面表格中列列的键值对集合。

```
KEY:
notify_mysql[:name]  发布存储桶通知到MySQL数据库. 当需要多个MySQL server endpoint时，可以为每个配置添加用户指定的“name”（例如"notify_mysql:myinstance"）.

ARGS:
dsn_string*  (string)             MySQL数据源名称连接字符串，例如 "<user>:<password>@tcp(<host>:<port>)/<database>"
table*       (string)             存储/更新事件的数据库表名, 表会自动被创建
format*      (namespace*|access)  'namespace'或者'access', 默认是'namespace'
queue_dir    (path)               未发送消息的暂存目录 例如 '/home/events'
queue_limit  (number)             未发送消息的最大限制, 默认是'100000'
comment      (sentence)           可选的注释说明
max_open_connections (number)           数据库最大连接数，默认 2；0 表示不限制
```

或者通过环境变量（说明详见上面）
```
KEY:
notify_mysql[:name]  publish bucket notifications to MySQL databases

ARGS:
OTTERIO_NOTIFY_MYSQL_ENABLE*      (on|off)             enable notify_mysql target, default is 'off'
OTTERIO_NOTIFY_MYSQL_DSN_STRING*  (string)             MySQL data-source-name connection string e.g. "<user>:<password>@tcp(<host>:<port>)/<database>"
OTTERIO_NOTIFY_MYSQL_TABLE*       (string)             DB table name to store/update events, table is auto-created
OTTERIO_NOTIFY_MYSQL_FORMAT*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
OTTERIO_NOTIFY_MYSQL_QUEUE_DIR    (path)               staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_MYSQL_QUEUE_LIMIT  (number)             maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_MYSQL_COMMENT      (sentence)           optionally add a comment to this setting
OTTERIO_NOTIFY_MYSQL_MAX_OPEN_CONNECTIONS (number)  maximum number of open database connections, defaults to 2
```

`dsn_string`是必须的，并且格式为 `"<user>:<password>@tcp(<host>:<port>)/<database>"`

OtterIO支持持久事件存储。持久存储将在MySQL连接离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

更新配置前, 可以使用`oc admin config get`命令获取当前配置.

```sh
$ oc admin config get myotterio/ notify_mysql
notify_mysql:myinstance enable=off format=namespace dsn_string= table= queue_dir= queue_limit=0 max_open_connections=2
```

使用带有`dsn_string`参数的`oc admin config set`的命令更新MySQL的通知配置:

```sh
$ oc admin config set myotterio notify_mysql:myinstance enable=on table="otterio_images" dsn_string="otterio_events_user:${OTTERIO_MYSQL_PASSWORD}@tcp(127.0.0.1:3306)/otteriodb"
```

请注意, 根据你的需要，你可以添加任意多个MySQL server endpoint，只要提供MySQL实例的标识符（如上例中的"myinstance"）和每个实例配置参数的信息即可。

使用`oc admin config set`命令更新配置后，重启OtterIO Server让配置生效。 如果一切顺利，OtterIO Server会在启动时输出一行信息，类似 `SQS ARNs: arn:otterio:sqs::myinstance:mysql`。

### 第三步：使用OtterIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知，一旦上有文件上传到存储桶中，MySQL中会insert一条新的记录或者一条已经存在的记录会被update，如果一个存在对象被删除，一条对应的记录也会从MySQL表中删除。因此，MySQL表中的行，对应的就是存储桶里的一个对象。

要配置这种存储桶通知，我们需要用到前面步骤OtterIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`oc`这个工具，这些配置信息很容易就能添加上。假设咱们的OtterIO服务别名叫`myotterio`,可执行下列脚本：

```
# Create bucket named `images` in myotterio
oc mb myotterio/images
# Add notification configuration on the `images` bucket using the MySQL ARN. The --suffix argument filters events.
oc event add myotterio/images arn:otterio:sqs::myinstance:mysql --suffix .jpg --event put,delete
# Print out the notification configuration on the `images` bucket.
oc event list myotterio/images
arn:otterio:sqs::myinstance:mysql s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### 第四步：验证 MySQL

上传一张 JPEG 图片，用通知账户连接数据库并查询事件：

```sh
oc cp myphoto.jpg myotterio/images
mysql -h 127.0.0.1 -P 3306 -u otterio_events_user -p otteriodb
```

```sql
SELECT key_name, JSON_UNQUOTE(JSON_EXTRACT(value, '$.Records[0].eventName')) AS event_name
FROM otterio_images;
```

```text
key_name           | event_name
-------------------+---------------------
images/myphoto.jpg | s3:ObjectCreated:Put
```

<a name="webhooks"></a>

## 使用Webhook发布OtterIO事件

[Webhooks](https://en.wikipedia.org/wiki/Webhook) 采用推的方式获取数据，而不是一直去拉取。

### 第一步：集成Webhook到OtterIO

OtterIO支持持久事件存储。持久存储将在webhook离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

```
KEY:
notify_webhook[:name]  发布存储桶通知到webhook endpoints

ARGS:
endpoint*    (url)       webhook server endpoint,例如 http://localhost:8080/otterio/events
auth_token   (string)    opaque token或者JWT authorization token
queue_dir    (path)      未发送消息的暂存目录 例如 '/home/events'
queue_limit  (number)    未发送消息的最大限制, 默认是'100000'
client_cert  (string)    Webhook的mTLS身份验证的客户端证书
client_key   (string)    Webhook的mTLS身份验证的客户端证书密钥
comment      (sentence)  可选的注释说明
```

或者通过环境变量（说明参见上面）
```
KEY:
notify_webhook[:name]  publish bucket notifications to webhook endpoints

ARGS:
OTTERIO_NOTIFY_WEBHOOK_ENABLE*      (on|off)    enable notify_webhook target, default is 'off'
OTTERIO_NOTIFY_WEBHOOK_ENDPOINT*    (url)       webhook server endpoint e.g. http://localhost:8080/otterio/events
OTTERIO_NOTIFY_WEBHOOK_AUTH_TOKEN   (string)    opaque string or JWT authorization token
OTTERIO_NOTIFY_WEBHOOK_QUEUE_DIR    (path)      staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_WEBHOOK_QUEUE_LIMIT  (number)    maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_WEBHOOK_COMMENT      (sentence)  optionally add a comment to this setting
OTTERIO_NOTIFY_WEBHOOK_CLIENT_CERT  (string)    client cert for Webhook mTLS auth
OTTERIO_NOTIFY_WEBHOOK_CLIENT_KEY   (string)    client cert key for Webhook mTLS auth
```

```sh
$ oc admin config get myotterio/ notify_webhook
notify_webhook:1 endpoint="" auth_token="" queue_limit="0" queue_dir="" client_cert="" client_key=""
```

用`oc admin config set` 命令更新配置. 在这endpoint是监听webhook通知的服务. 保存配置文件并重启OtterIO服务让配配置生效. 注意一下，在重启OtterIO时，这个endpoint必须是启动并且可访问到。

```sh
$ oc admin config set myotterio notify_webhook:1 enable=on queue_limit="0"  endpoint="http://localhost:3000" queue_dir=""
```

### 第二步：使用OtterIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知，一旦上有文件上传到存储桶中，事件将被触发。在这里，ARN的值是`arn:otterio:sqs::1:webhook`。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

```
oc mb myotterio/images
oc event add myotterio/images arn:otterio:sqs::1:webhook --event put --suffix .jpg
```

验证事件通知是否配置正确：

```
oc event list myotterio/images
```

你应该可以收到如下的响应：

```
arn:otterio:sqs::1:webhook   s3:ObjectCreated:*   Filter: suffix=".jpg"
```

### 第三步：使用本地 Webhook 接收器验证

将以下内容保存为 `webhook_receiver.py`，在另一个终端运行 `python3 webhook_receiver.py`，然后执行第一步的目标配置。示例假定接收器与 OtterIO 位于同一台主机；容器或远程部署需要调整监听地址和目标地址。

```python
from http.server import BaseHTTPRequestHandler, HTTPServer
import json

class Receiver(BaseHTTPRequestHandler):
    def do_HEAD(self):
        self.send_response(200)
        self.end_headers()

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        print(json.dumps(json.loads(body), ensure_ascii=False, indent=2), flush=True)
        self.send_response(200)
        self.end_headers()

HTTPServer(("127.0.0.1", 3000), Receiver).serve_forever()
```

上传一张 JPEG 图片，触发已配置的 `put` 事件：

```sh
oc cp ~/images.jpg myotterio/images
```

接收器应打印 JSON，`Records` 中包含 `eventName: s3:ObjectCreated:Put`、存储桶 `images` 和上传对象的 key。接收到通知内容后才能确认投递成功，上传进度本身不能证明通知已送达。
