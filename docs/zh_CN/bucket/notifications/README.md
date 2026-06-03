# MinIO存储桶通知指南

可以使用存储桶事件通知来监视存储桶中对象上发生的事件。 MinIO服务器支持的事件类型是

| Supported Event Types   |                                            |                          |
| :---------------------- | ------------------------------------------ | ------------------------ |
| `s3:ObjectCreated:Put`  | `s3:ObjectCreated:CompleteMultipartUpload` | `s3:ObjectAccessed:Head` |
| `s3:ObjectCreated:Post` | `s3:ObjectRemoved:Delete`                  |                          |
| `s3:ObjectCreated:Copy` | `s3:ObjectAccessed:Get`                    |                          |

使用诸如`mc`之类的客户端工具通过[`event`子命令](https://docs.min.io/cn/minio-client-complete-guide#events)设置和监听事件通知。也可以使用MinIO SDK [`BucketNotification` APIs](https://docs.min.io/cn/golang-client-api-reference#SetBucketNotification) 。MinIO发送的用于发布事件的通知消息是JSON格式的，JSON结构参考[这里](https://docs.aws.amazon.com/AmazonS3/latest/dev/notification-content-structure.html)。

存储桶事件可以发布到以下目标：

| 支持的通知目标    |                             |                                 |
| :-------------------------------- | --------------------------- | ------------------------------- |
| [`Redis`](#Redis)                 | [`MySQL`](#MySQL)           |                                 |
| [`Elasticsearch`](#Elasticsearch) | [`PostgreSQL`](#PostgreSQL) | [`Webhooks`](#webhooks)         |

## 前提条件

* 从[这里](https://docs.min.io/cn/minio-quickstart-guide)下载并安装MinIO Server。
* 从[这里](https://docs.min.io/cn/minio-client-quickstart-guide)下载并安装MinIO Client。

```
$ mc admin config get myminio | grep notify
notify_webhook        publish bucket notifications to webhook endpoints
notify_mysql          publish bucket notifications to MySQL databases
notify_postgres       publish bucket notifications to Postgres databases
notify_elasticsearch  publish bucket notifications to Elasticsearch endpoints
notify_redis          publish bucket notifications to Redis datastores
```

> 注意:
> - '\*' 结尾的参数是必填的.
> - '\*' 结尾的值，是参数的的默认值.
> - 当通过环境变量配置的时候, `:name` 可以通过这样 `MINIO_NOTIFY_WEBHOOK_ENABLE_<name>` 的格式指定.

<a name="Elasticsearch"></a>
## 使用Elasticsearch发布MinIO事件

安装 [Elasticsearch](https://www.elastic.co/downloads/elasticsearch) 。

这个通知目标支持两种格式: _namespace_ 和 _access_。

如果使用的是 _namespace_ 格式, MinIO将桶中的对象与索引中的文档进行同步。对于MinIO中的每个事件，服务器都会使用事件中的存储桶和对象名称作为文档ID创建一个文档。事件的其他细节存储在document的正文中。因此，如果一个已经存在的对象在MinIO中被覆盖，在ES中的相对应的document也会被更新。如果一个对象被删除，相对应的document也会从index中删除。

如果使用的是 _access_ 格式，MinIO将事件作为document附加到ES的index中。对于每个事件，将带有事件详细信息的文档（文档的时间戳设置为事件的时间戳）附加到索引。这个文档的ID是由ES随机生成的。在 _access_ 格式下，不会有文档被删除或者修改。

下面的步骤展示的是在`namespace`格式下，如何使用通知目标。另一种格式和这个很类似，为了不让你们说我墨迹，就不再赘述了。


### 第一步：确保至少满足最低要求

MinIO要求使用的是ES 5.X系统版本。如果使用的是低版本的ES，也没关系，ES官方支持升级迁移，详情请看[这里](https://www.elastic.co/guide/en/elasticsearch/reference/current/setup-upgrade.html)。

### 第二步：把ES集成到MinIO中

Elasticsearch的配置信息位于`notify_elasticsearch`这个顶级的key下。在这里为你的Elasticsearch实例创建配置信息键值对。key是你的Elasticsearch endpoint的名称，value是下面表格中列列的键值对集合。

```
KEY:
notify_elasticsearch[:name]  发布存储桶通知到Elasticsearch endpoints

ARGS:
url*         (url)                Elasticsearch服务器的地址，以及可选的身份验证信息
index*       (string)             存储/更新事件的Elasticsearch索引，索引是自动创建的
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
MINIO_NOTIFY_ELASTICSEARCH_ENABLE*      (on|off)             enable notify_elasticsearch target, default is 'off'
MINIO_NOTIFY_ELASTICSEARCH_URL*         (url)                Elasticsearch server's address, with optional authentication info
MINIO_NOTIFY_ELASTICSEARCH_INDEX*       (string)             Elasticsearch index to store/update events, index is auto-created
MINIO_NOTIFY_ELASTICSEARCH_FORMAT*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
MINIO_NOTIFY_ELASTICSEARCH_QUEUE_DIR    (path)               staging dir for undelivered messages e.g. '/home/events'
MINIO_NOTIFY_ELASTICSEARCH_QUEUE_LIMIT  (number)             maximum limit for undelivered messages, defaults to '100000'
MINIO_NOTIFY_ELASTICSEARCH_COMMENT      (sentence)           optionally add a comment to this setting
```

比如: `http://localhost:9200` 或者带有授权信息的 `http://elastic:MagicWord@127.0.0.1:9200`

MinIO支持持久事件存储。持久存储将在Elasticsearch broker离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

如果Elasticsearch启用了身份验证, 凭据可以通过格式为`PROTO://USERNAME:PASSWORD@ELASTICSEARCH_HOST:PORT`的`url`参数，提供给MinIO。

更新配置前，可以通过`mc admin config get`命令获取当前配置。

```sh
$ mc admin config get myminio/ notify_elasticsearch
notify_elasticsearch:1 queue_limit="0"  url="" format="namespace" index="" queue_dir=""
```

使用`mc admin config set`命令更新配置后，重启MinIO Server让配置生效。 如果一切顺利，MinIO Server会在启动时输出一行信息，类似`SQS ARNs: arn:minio:sqs::1:elasticsearch`。

请注意, 根据你的需要，你可以添加任意多个ES server endpoint，只要提供ES实例的标识符（如上例中的“ 1”）和每个实例配置参数的信息即可。

### 第三步：使用MinIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知。一旦有文件被创建或者覆盖，一个新的ES的document会被创建或者更新到之前咱配的index里。如果一个已经存在的对象被删除，这个对应的document也会从index中删除。因此，这个ES index里的行，就映射着`images`存储桶里的`.jpg`对象。

要配置这种存储桶通知，我们需要用到前面步骤MinIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`mc`这个工具，这些配置信息很容易就能添加上。假设咱们的MinIO服务别名叫`myminio`,可执行下列脚本：

```
mc mb myminio/images
mc event add  myminio/images arn:minio:sqs::1:elasticsearch --suffix .jpg
mc event list myminio/images
arn:minio:sqs::1:elasticsearch s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=”.jpg”
```

### 第四步：验证Elasticsearch

上传一张JPEG图片到`images` 存储桶。

```
mc cp myphoto.jpg myminio/images
```

使用curl查看`minio_events` index中的内容。

```
$ curl  "http://localhost:9200/minio_events/_search?pretty=true"
{
  "took" : 40,
  "timed_out" : false,
  "_shards" : {
    "total" : 5,
    "successful" : 5,
    "failed" : 0
  },
  "hits" : {
    "total" : 1,
    "max_score" : 1.0,
    "hits" : [
      {
        "_index" : "minio_events",
        "_type" : "event",
        "_id" : "images/myphoto.jpg",
        "_score" : 1.0,
        "_source" : {
          "Records" : [
            {
              "eventVersion" : "2.0",
              "eventSource" : "minio:s3",
              "awsRegion" : "",
              "eventTime" : "2017-03-30T08:00:41Z",
              "eventName" : "s3:ObjectCreated:Put",
              "userIdentity" : {
                "principalId" : "minio"
              },
              "requestParameters" : {
                "sourceIPAddress" : "127.0.0.1:38062"
              },
              "responseElements" : {
                "x-amz-request-id" : "14B09A09703FC47B",
                "x-minio-origin-endpoint" : "http://192.168.86.115:9000"
              },
              "s3" : {
                "s3SchemaVersion" : "1.0",
                "configurationId" : "Config",
                "bucket" : {
                  "name" : "images",
                  "ownerIdentity" : {
                    "principalId" : "minio"
                  },
                  "arn" : "arn:aws:s3:::images"
                },
                "object" : {
                  "key" : "myphoto.jpg",
                  "size" : 6474,
                  "eTag" : "a3410f4f8788b510d6f19c5067e60a90",
                  "sequencer" : "14B09A09703FC47B"
                }
              },
              "source" : {
                "host" : "127.0.0.1",
                "port" : "38062",
                "userAgent" : "MinIO (linux; amd64) minio-go/2.0.3 mc/2017-02-15T17:57:25Z"
              }
            }
          ]
        }
      }
    ]
  }
}
```

这个输出显示在ES中为这个事件创建了一个document。

这里我们可以看到这个document ID就是存储桶和对象的名称。如果用的是`access`格式，这个document ID就是由ES随机生成的。

<a name="Redis"></a>
## 使用Redis发布MinIO事件

安装 [Redis](http://redis.io/download)。为了演示，我们将数据库密码设为"yoursecret"。

这种通知目标支持两种格式: _namespace_ 和 _access_。

如果用的是 _namespacee_ 格式，MinIO将存储桶里的对象同步成Redis hash中的条目。对于每一个条目，对应一个存储桶里的对象，其key都被设为"存储桶名称/对象名称"，value都是一个有关这个MinIO对象的JSON格式的事件数据。如果对象更新或者删除，hash中对象的条目也会相应的更新或者删除。

如果使用的是 _access_ ,MinIO使用[RPUSH](https://redis.io/commands/rpush)将事件添加到list中。这个list中每一个元素都是一个JSON格式的list,这个list中又有两个元素，第一个元素是时间戳的字符串，第二个元素是一个含有在这个存储桶上进行操作的事件数据的JSON对象。在这种格式下，list中的元素不会更新或者删除。

下面的步骤展示如何在`namespace`和`access`格式下使用通知目标。

### 第一步：集成Redis到MinIO

The MinIO server的配置文件以json格式存储在后端。Redis的配置信息位于`notify_redis`这个顶级的key下。在这里为你的Redis实例创建配置信息键值对。key是你的Redis endpoint的名称，value是下面表格中列的键值对集合。

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
MINIO_NOTIFY_REDIS_ENABLE*      (on|off)             enable notify_redis target, default is 'off'
MINIO_NOTIFY_REDIS_KEY*         (string)             Redis key to store/update events, key is auto-created
MINIO_NOTIFY_REDIS_FORMAT*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
MINIO_NOTIFY_REDIS_PASSWORD     (string)             Redis server password
MINIO_NOTIFY_REDIS_QUEUE_DIR    (path)               staging dir for undelivered messages e.g. '/home/events'
MINIO_NOTIFY_REDIS_QUEUE_LIMIT  (number)             maximum limit for undelivered messages, defaults to '100000'
MINIO_NOTIFY_REDIS_COMMENT      (sentence)           optionally add a comment to this setting
```

MinIO支持持久事件存储。持久存储将在Redis broker离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

更新配置前，可以通过`mc admin config get`命令获取当前配置。

```sh
$ mc admin config get myminio/ notify_redis
notify_redis:1 address="" format="namespace" key="" password="" queue_dir="" queue_limit="0"
```

使用`mc admin config set`命令更新配置后，重启MinIO Server让配置生效。 如果一切顺利，MinIO Server会在启动时输出一行信息，类似`SQS ARNs: arn:minio:sqs::1:redis`。

```sh
$ mc admin config set myminio/ notify_redis:1 address="127.0.0.1:6379" format="namespace" key="bucketevents" password="yoursecret" queue_dir="" queue_limit="0"
```

请注意, 根据你的需要，你可以添加任意多个Redis server endpoint，只要提供Redis实例的标识符（如上例中的“ 1”）和每个实例配置参数的信息即可。

### 第二步: 使用MinIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知。当一个JPEG文件被创建或者覆盖，一个新的key会被创建,或者一个已经存在的key就会被更新到之前配置好的redis hash里。如果一个已经存在的对象被删除，这个对应的key也会从hash中删除。因此，这个Redis hash里的行，就映射着`images`存储桶里的`.jpg`对象。

要配置这种存储桶通知，我们需要用到前面步骤MinIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`mc`这个工具，这些配置信息很容易就能添加上。假设咱们的MinIO服务别名叫`myminio`,可执行下列脚本：

```
mc mb myminio/images
mc event add myminio/images arn:minio:sqs::1:redis --suffix .jpg
mc event list myminio/images
arn:minio:sqs::1:redis s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=”.jpg”
```

### 第三步：验证Redis

启动`redis-cli`这个Redis客户端程序来检查Redis中的内容. 运行`monitor`Redis命令将会输出在Redis上执行的每个命令的。

```
redis-cli -a yoursecret
127.0.0.1:6379> monitor
OK
```

打开一个新的terminal终端并上传一张JPEG图片到`images` 存储桶。

```
mc cp myphoto.jpg myminio/images
```

在上一个终端中，你将看到MinIO在Redis上执行的操作：

```
127.0.0.1:6379> monitor
OK
1490686879.650649 [0 172.17.0.1:44710] "PING"
1490686879.651061 [0 172.17.0.1:44710] "HSET" "minio_events" "images/myphoto.jpg" "{\"Records\":[{\"eventVersion\":\"2.0\",\"eventSource\":\"minio:s3\",\"awsRegion\":\"\",\"eventTime\":\"2017-03-28T07:41:19Z\",\"eventName\":\"s3:ObjectCreated:Put\",\"userIdentity\":{\"principalId\":\"minio\"},\"requestParameters\":{\"sourceIPAddress\":\"127.0.0.1:52234\"},\"responseElements\":{\"x-amz-request-id\":\"14AFFBD1ACE5F632\",\"x-minio-origin-endpoint\":\"http://192.168.86.115:9000\"},\"s3\":{\"s3SchemaVersion\":\"1.0\",\"configurationId\":\"Config\",\"bucket\":{\"name\":\"images\",\"ownerIdentity\":{\"principalId\":\"minio\"},\"arn\":\"arn:aws:s3:::images\"},\"object\":{\"key\":\"myphoto.jpg\",\"size\":2586,\"eTag\":\"5d284463f9da279f060f0ea4d11af098\",\"sequencer\":\"14AFFBD1ACE5F632\"}},\"source\":{\"host\":\"127.0.0.1\",\"port\":\"52234\",\"userAgent\":\"MinIO (linux; amd64) minio-go/2.0.3 mc/2017-02-15T17:57:25Z\"}}]}"
```

在这我们可以看到MinIO在`minio_events`这个key上执行了`HSET`命令。

如果用的是`access`格式，那么`minio_events`就是一个list,MinIO就会调用`RPUSH`添加到list中。这个list的消费者会使用`BLPOP`从list的最左端删除list元素。

<a name="PostgreSQL"></a>
## 使用PostgreSQL发布MinIO事件

> 注意：在版本RELEASE.2020-04-10T03-34-42Z之前的PostgreSQL通知用于支持以下选项：
>
> ```
> host                (hostname)           Postgres server hostname (used only if `connection_string` is empty)
> port                (port)               Postgres server port, defaults to `5432` (used only if `connection_string` is empty)
> username            (string)             database username (used only if `connection_string` is empty)
> password            (string)             database password (used only if `connection_string` is empty)
> database            (string)             database name (used only if `connection_string` is empty)
> ```
>
> 这些现在已经弃用, 如果你打算升级到*RELEASE.2020-04-10T03-34-42Z*之后的版本请确保
> 仅使用*connection_string*选项迁移.一旦所有服务器都升级完成，请使用以下命令更新现有的通知目标完成迁移。
>
> ```
> mc admin config set myminio/ notify_postgres[:name] connection_string="host=hostname port=2832 username=psqluser password=psqlpass database=bucketevents"
> ```
>
> 请确保执行此步骤，否则将无法执行PostgreSQL通知目标，
> 服务器升级/重启后，控制台上会显示一条错误消息，请务必遵循上述说明。
> 如有其他问题，请加入我们的 https://slack.min.io

安装 [PostgreSQL](https://www.postgresql.org/) 数据库。为了演示，我们将"postgres"用户的密码设为`password`，并且创建了一个`minio_events`数据库来存储事件信息。

这个通知目标支持两种格式: _namespace_ 和 _access_。

如果使用的是 _namespace_ 格式，MinIO将存储桶里的对象同步成数据库表中的行。每一行有两列：key和value。key是这个对象的存储桶名字加上对象名，value都是一个有关这个MinIO对象的JSON格式的事件数据。如果对象更新或者删除，表中相应的行也会相应的更新或者删除。

如果使用的是 _access_,MinIO将将事件添加到表里，行有两列：event_time 和 event_data。event_time是事件在MinIO server里发生的时间，event_data是有关这个MinIO对象的JSON格式的事件数据。在这种格式下，不会有行会被删除或者修改。

下面的步骤展示的是如何在`namespace`格式下使用通知目标，`_access_`差不多，不再赘述，我相信你可以触类旁通，举一反三，不要让我失望哦。

### 第一步：确保确保至少满足最低要求

MinIO要求PostgresSQL9.5版本及以上。 MinIO用了PostgreSQL9.5引入的[`INSERT ON CONFLICT`](https://www.postgresql.org/docs/9.5/static/sql-insert.html#SQL-ON-CONFLICT) (aka UPSERT) 特性,以及9.4引入的 [JSONB](https://www.postgresql.org/docs/9.4/static/datatype-json.html) 数据类型。

### 第二步：集成PostgreSQL到MinIO

PostgreSQL的配置信息位于`notify_postgresql`这个顶级的key下。在这里为你的PostgreSQL实例创建配置信息键值对。key是你的PostgreSQL endpoint的名称，value是下面表格中列列的键值对集合。

```
KEY:
notify_postgres[:name]  发布存储桶通知到Postgres数据库

ARGS:
connection_string*  (string)             Postgres server的连接字符串，例如 "host=localhost port=5432 dbname=minio_events user=postgres password=password sslmode=disable"
table*              (string)             存储/更新事件的数据库表名, 表会自动被创建
format*             (namespace*|access)  'namespace'或者'access', 默认是'namespace'
queue_dir           (path)               未发送消息的暂存目录 例如 '/home/events'
queue_limit         (number)             未发送消息的最大限制, 默认是'100000'
comment             (sentence)           可选的注释说明
```

或者通过环境变量（说明详见上面）
```
KEY:
notify_postgres[:name]  publish bucket notifications to Postgres databases

ARGS:
MINIO_NOTIFY_POSTGRES_ENABLE*             (on|off)             enable notify_postgres target, default is 'off'
MINIO_NOTIFY_POSTGRES_CONNECTION_STRING*  (string)             Postgres server connection-string e.g. "host=localhost port=5432 dbname=minio_events user=postgres password=password sslmode=disable"
MINIO_NOTIFY_POSTGRES_TABLE*              (string)             DB table name to store/update events, table is auto-created
MINIO_NOTIFY_POSTGRES_FORMAT*             (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
MINIO_NOTIFY_POSTGRES_QUEUE_DIR           (path)               staging dir for undelivered messages e.g. '/home/events'
MINIO_NOTIFY_POSTGRES_QUEUE_LIMIT         (number)             maximum limit for undelivered messages, defaults to '100000'
MINIO_NOTIFY_POSTGRES_COMMENT             (sentence)           optionally add a comment to this setting
```

MinIO支持持久事件存储。持久存储将在PostgreSQL连接离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

注意这里为了演示, 我们禁止了SSL. 处于安全起见, 不推荐用于生产.
更新配置前, 使用`mc admin config get`命令获取当前配置。

```sh
$ mc admin config get myminio notify_postgres
notify_postgres:1 queue_dir="" connection_string="" queue_limit="0"  table="" format="namespace"
```

Use `mc admin config set`命令更新完配置后，重启MinIO Server让配置生效。 如果一切顺利，MinIO Server会在启动时输出一行信息，类似 `SQS ARNs: arn:minio:sqs::1:postgresql`。

```sh
$ mc admin config set myminio notify_postgres:1 connection_string="host=localhost port=5432 dbname=minio_events user=postgres password=password sslmode=disable" table="bucketevents" format="namespace"
```

请注意, 根据你的需要，你可以添加任意多个PostgreSQL server endpoint，只要提供PostgreSQL实例的标识符（如上例中的“ 1”）和每个实例配置参数的信息即可。

### 第三步：使用MinIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知，一旦上有文件上传到存储桶中，PostgreSQL中会insert一条新的记录或者一条已经存在的记录会被update，如果一个存在对象被删除，一条对应的记录也会从PostgreSQL表中删除。因此，PostgreSQL表中的行，对应的就是存储桶里的一个对象。

要配置这种存储桶通知，我们需要用到前面步骤中MinIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`mc`这个工具，这些配置信息很容易就能添加上。假设MinIO服务别名叫`myminio`,可执行下列脚本：

```
# Create bucket named `images` in myminio
mc mb myminio/images
# Add notification configuration on the `images` bucket using the MySQL ARN. The --suffix argument filters events.
mc event add myminio/images arn:minio:sqs::1:postgresql --suffix .jpg
# Print out the notification configuration on the `images` bucket.
mc event list myminio/images
mc event list myminio/images
arn:minio:sqs::1:postgresql s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=”.jpg”
```

### 第四步：验证PostgreSQL

打开一个新的terminal终端并上传一张JPEG图片到``images`` 存储桶。

```
mc cp myphoto.jpg myminio/images
```

打开一个PostgreSQL终端列出表 `bucketevents` 中所有的记录。

```
$ psql -h 127.0.0.1 -U postgres -d minio_events
minio_events=# select * from bucketevents;

key                 |                      value
--------------------+----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
 images/myphoto.jpg | {"Records": [{"s3": {"bucket": {"arn": "arn:aws:s3:::images", "name": "images", "ownerIdentity": {"principalId": "minio"}}, "object": {"key": "myphoto.jpg", "eTag": "1d97bf45ecb37f7a7b699418070df08f", "size": 56060, "sequencer": "147CE57C70B31931"}, "configurationId": "Config", "s3SchemaVersion": "1.0"}, "awsRegion": "", "eventName": "s3:ObjectCreated:Put", "eventTime": "2016-10-12T21:18:20Z", "eventSource": "aws:s3", "eventVersion": "2.0", "userIdentity": {"principalId": "minio"}, "responseElements": {}, "requestParameters": {"sourceIPAddress": "[::1]:39706"}}]}
(1 row)
```

<a name="MySQL"></a>

## 使用MySQL发布MinIO事件

> 注意：在版本RELEASE.2020-04-10T03-34-42Z之前的MySQL通知用于支持以下选项：
>
> ```
> host         (hostname)           MySQL server hostname (used only if `dsn_string` is empty)
> port         (port)               MySQL server port (used only if `dsn_string` is empty)
> username     (string)             database username (used only if `dsn_string` is empty)
> password     (string)             database password (used only if `dsn_string` is empty)
> database     (string)             database name (used only if `dsn_string` is empty)
> ```
>
> 这些现在已经弃用, 如果你打算升级到*RELEASE.2020-04-10T03-34-42Z*之后的版本请确保
> 仅使用*dsn_string*选项迁移. 一旦所有服务器都升级完成，请使用以下命令更新现有的通知目标完成迁移
>
> ```
> mc admin config set myminio/ notify_mysql[:name] dsn_string="mysqluser:mysqlpass@tcp(localhost:2832)/bucketevents"
> ```
>
> 请确保执行此步骤, 否则将无法执行MySQL通知目标，
> 服务器升级/重启后，控制台上会显示一条错误消息，请务必遵循上述说明。
> 如有其他问题，请加入我们的 https://slack.min.io

安装 [MySQL](https://dev.mysql.com/downloads/mysql/). 为了演示，我们将"root"用户的密码设为`password`，并且创建了一个`miniodb`数据库来存储事件信息。

这个通知目标支持两种格式: _namespace_ 和 _access_。

如果使用的是 _namespace_ 格式，MinIO将存储桶里的对象同步成数据库表中的行。每一行有两列：key_name和value。key_name是这个对象的存储桶名字加上对象名，value都是一个有关这个MinIO对象的JSON格式的事件数据。如果对象更新或者删除，表中相应的行也会相应的更新或者删除。

如果使用的是 _access_,MinIO将将事件添加到表里，行有两列：event_time 和 event_data。event_time是事件在MinIO server里发生的时间，event_data是有关这个MinIO对象的JSON格式的事件数据。在这种格式下，不会有行会被删除或者修改。

下面的步骤展示的是如何在`namespace`格式下使用通知目标，`_access_`差不多，不再赘述。

### 第一步：确保确保至少满足最低要求

MinIO要求MySQL 版本 5.7.8及以上，MinIO使用了MySQL5.7.8版本引入的 [JSON](https://dev.mysql.com/doc/refman/5.7/en/json.html) 数据类型。我们使用的是MySQL5.7.17进行的测试。

### 第二步：集成MySQL到MinIO

MySQL配置位于 `notify_mysql`key下. 在这里为你的PostgreSQL实例创建配置信息键值对。key是你的MySQL endpoint的名称，value是下面表格中列列的键值对集合。

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
```

或者通过环境变量（说明详见上面）
```
KEY:
notify_mysql[:name]  publish bucket notifications to MySQL databases

ARGS:
MINIO_NOTIFY_MYSQL_ENABLE*      (on|off)             enable notify_mysql target, default is 'off'
MINIO_NOTIFY_MYSQL_DSN_STRING*  (string)             MySQL data-source-name connection string e.g. "<user>:<password>@tcp(<host>:<port>)/<database>"
MINIO_NOTIFY_MYSQL_TABLE*       (string)             DB table name to store/update events, table is auto-created
MINIO_NOTIFY_MYSQL_FORMAT*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
MINIO_NOTIFY_MYSQL_QUEUE_DIR    (path)               staging dir for undelivered messages e.g. '/home/events'
MINIO_NOTIFY_MYSQL_QUEUE_LIMIT  (number)             maximum limit for undelivered messages, defaults to '100000'
MINIO_NOTIFY_MYSQL_COMMENT      (sentence)           optionally add a comment to this setting
```

`dsn_string`是必须的，并且格式为 `"<user>:<password>@tcp(<host>:<port>)/<database>"`

MinIO支持持久事件存储。持久存储将在MySQL连接离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

更新配置前, 可以使用`mc admin config get`命令获取当前配置.

```sh
$ mc admin config get myminio/ notify_mysql
notify_mysql:myinstance enable=off format=namespace host= port= username= password= database= dsn_string= table= queue_dir= queue_limit=0
```

使用带有`dsn_string`参数的`mc admin config set`的命令更新MySQL的通知配置:

```sh
$ mc admin config set myminio notify_mysql:myinstance table="minio_images" dsn_string="root:xxxx@tcp(172.17.0.1:3306)/miniodb"
```

请注意, 根据你的需要，你可以添加任意多个MySQL server endpoint，只要提供MySQL实例的标识符（如上例中的"myinstance"）和每个实例配置参数的信息即可。

使用`mc admin config set`命令更新配置后，重启MinIO Server让配置生效。 如果一切顺利，MinIO Server会在启动时输出一行信息，类似 `SQS ARNs: arn:minio:sqs::myinstance:mysql`。

### 第三步：使用MinIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知，一旦上有文件上传到存储桶中，MySQL中会insert一条新的记录或者一条已经存在的记录会被update，如果一个存在对象被删除，一条对应的记录也会从MySQL表中删除。因此，MySQL表中的行，对应的就是存储桶里的一个对象。

要配置这种存储桶通知，我们需要用到前面步骤MinIO输出的ARN信息。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

有了`mc`这个工具，这些配置信息很容易就能添加上。假设咱们的MinIO服务别名叫`myminio`,可执行下列脚本：

```
# Create bucket named `images` in myminio
mc mb myminio/images
# Add notification configuration on the `images` bucket using the MySQL ARN. The --suffix argument filters events.
mc event add myminio/images arn:minio:sqs::myinstance:mysql --suffix .jpg
# Print out the notification configuration on the `images` bucket.
mc event list myminio/images
arn:minio:sqs::myinstance:mysql s3:ObjectCreated:*,s3:ObjectRemoved:*,s3:ObjectAccessed:* Filter: suffix=”.jpg”
```

### 第四步：验证MySQL

打开一个新的terminal终端并上传一张JPEG图片到`images` 存储桶。

```
mc cp myphoto.jpg myminio/images
```

打开一个MySQL终端列出表 `minio_images` 中所有的记录。

```
$ mysql -h 172.17.0.1 -P 3306 -u root -p miniodb
mysql> select * from minio_images;
+--------------------+----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| key_name           | value                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
+--------------------+----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| images/myphoto.jpg | {"Records": [{"s3": {"bucket": {"arn": "arn:aws:s3:::images", "name": "images", "ownerIdentity": {"principalId": "minio"}}, "object": {"key": "myphoto.jpg", "eTag": "467886be95c8ecfd71a2900e3f461b4f", "size": 26, "sequencer": "14AC59476F809FD3"}, "configurationId": "Config", "s3SchemaVersion": "1.0"}, "awsRegion": "", "eventName": "s3:ObjectCreated:Put", "eventTime": "2017-03-16T11:29:00Z", "eventSource": "aws:s3", "eventVersion": "2.0", "userIdentity": {"principalId": "minio"}, "responseElements": {"x-amz-request-id": "14AC59476F809FD3", "x-minio-origin-endpoint": "http://192.168.86.110:9000"}, "requestParameters": {"sourceIPAddress": "127.0.0.1:38260"}}]} |
+--------------------+----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
1 row in set (0.01 sec)

```

<a name="webhooks"></a>

## 使用Webhook发布MinIO事件

[Webhooks](https://en.wikipedia.org/wiki/Webhook) 采用推的方式获取数据，而不是一直去拉取。

### 第一步：集成Webhook到MinIO

MinIO支持持久事件存储。持久存储将在webhook离线时备份事件，并在broker恢复在线时重播事件。事件存储的目录可以通过`queue_dir`字段设置，存储的最大限制可以通过`queue_limit`设置。例如, `queue_dir`可以设置为`/home/events`, 并且`queue_limit`可以设置为`1000`. 默认情况下 `queue_limit` 是100000.

```
KEY:
notify_webhook[:name]  发布存储桶通知到webhook endpoints

ARGS:
endpoint*    (url)       webhook server endpoint,例如 http://localhost:8080/minio/events
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
MINIO_NOTIFY_WEBHOOK_ENABLE*      (on|off)    enable notify_webhook target, default is 'off'
MINIO_NOTIFY_WEBHOOK_ENDPOINT*    (url)       webhook server endpoint e.g. http://localhost:8080/minio/events
MINIO_NOTIFY_WEBHOOK_AUTH_TOKEN   (string)    opaque string or JWT authorization token
MINIO_NOTIFY_WEBHOOK_QUEUE_DIR    (path)      staging dir for undelivered messages e.g. '/home/events'
MINIO_NOTIFY_WEBHOOK_QUEUE_LIMIT  (number)    maximum limit for undelivered messages, defaults to '100000'
MINIO_NOTIFY_WEBHOOK_COMMENT      (sentence)  optionally add a comment to this setting
MINIO_NOTIFY_WEBHOOK_CLIENT_CERT  (string)    client cert for Webhook mTLS auth
MINIO_NOTIFY_WEBHOOK_CLIENT_KEY   (string)    client cert key for Webhook mTLS auth
```

```sh
$ mc admin config get myminio/ notify_webhook
notify_webhook:1 endpoint="" auth_token="" queue_limit="0" queue_dir="" client_cert="" client_key=""
```

用`mc admin config set` 命令更新配置. 在这endpoint是监听webhook通知的服务. 保存配置文件并重启MinIO服务让配配置生效. 注意一下，在重启MinIO时，这个endpoint必须是启动并且可访问到。

```sh
$ mc admin config set myminio notify_webhook:1 queue_limit="0"  endpoint="http://localhost:3000" queue_dir=""
```

### 第二步：使用MinIO客户端启用bucket通知

我们现在可以在一个叫`images`的存储桶上开启事件通知，一旦上有文件上传到存储桶中，事件将被触发。在这里，ARN的值是`arn:minio:sqs::1:webhook`。更多有关ARN的资料，请参考[这里](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)。

```
mc mb myminio/images
mc mb myminio/images-thumbnail
mc event add myminio/images arn:minio:sqs::1:webhook --event put --suffix .jpg
```

验证事件通知是否配置正确：

```
mc event list myminio/images
```

你应该可以收到如下的响应：

```
arn:minio:sqs::1:webhook   s3:ObjectCreated:*   Filter: suffix=".jpg"
```

### 第三步：采用Thumbnailer进行验证

我们使用 [Thumbnailer](https://github.com/minio/thumbnailer) 来监听MinIO通知。如果有文件上传于是MinIO服务，Thumnailer监听到该通知，生成一个缩略图并上传到MinIO服务。
安装Thumbnailer:

```
git clone https://github.com/minio/thumbnailer/
npm install
```

然后打开Thumbnailer的``config/webhook.json``配置文件，添加有关MinIO server的配置，使用下面的方式启动Thumbnailer:

```
NODE_ENV=webhook node thumbnail-webhook.js
```

Thumbnailer运行在``http://localhost:3000/``。下一步，配置MinIO server,让其发送消息到这个URL（第一步提到的），并使用 ``mc`` 来设置存储桶通知（第二步提到的）。然后上传一张图片到MinIO server:

```
mc cp ~/images.jpg myminio/images
.../images.jpg:  8.31 KB / 8.31 KB ┃▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓┃ 100.00% 59.42 KB/s 0s
```

稍等片刻，然后使用mc ls检查存储桶的内容 -，你将看到有个缩略图出现了。

```
mc ls myminio/images-thumbnail
[2017-02-08 11:39:40 IST]   992B images-thumbnail.jpg
```
