# OtterIO Bucket Notification Guide

Bucket notifications publish events for matching object operations. Gateway mode does not support bucket notifications, except the NAS gateway.

## Events and targets

The current source recognizes the following event names. In each line, names after the first share its prefix:

- `s3:ObjectCreated:Put`, `Post`, `Copy`, `CompleteMultipartUpload`, `PutTagging`, `DeleteTagging`, `PutRetention`, `PutLegalHold`.
- `s3:ObjectRemoved:Delete`, `DeleteMarkerCreated`.
- `s3:ObjectAccessed:Get`, `Head`, `GetRetention`, `GetLegalHold`.
- `s3:Replication:OperationFailedReplication`, `OperationCompletedReplication`, `OperationNotTracked`, `OperationMissedThreshold`, `OperationReplicatedAfterThreshold`.
- `s3:ObjectRestore:Post`, `Completed`; `s3:ObjectTransition:Failed`, `Complete`.

Global events `s3:BucketCreated:*` and `s3:BucketRemoved:*` are available through the listen-notification API, rather than per-bucket target configuration. The event name definitions are in [pkg/event/name.go](../../../pkg/event/name.go).

Use [OC](https://github.com/soulteary/oc) with `oc event add`, `oc event list` and `oc event listen`, or the [OtterIO Go SDK](https://github.com/soulteary/otterio-sdk) bucket notification APIs. Notification messages are JSON objects with a `Records` array following the [S3 notification structure](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-content-structure.html); OtterIO sets `eventSource` to `otterio:s3`.

The supported targets are Redis, MySQL, PostgreSQL, Elasticsearch and Webhooks. Kafka, NATS, NATS Streaming, NSQ, AMQP and MQTT targets have been removed.

## Prerequisites

- Install [OtterIO](../../../README.md) and the current [OC client](https://github.com/soulteary/oc).
- Configure an alias with credentials authorized for configuration and bucket notification operations:

```sh
oc alias set myotterio http://localhost:9000 "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" --api s3v4 --path on
oc admin config set myotterio
```

For a server started with `--console-address ":9001"`, add `--admin-url http://localhost:9001` to the alias command. Object and bucket notification operations use port 9000; `oc admin config` uses the management listener on port 9001. The management URL is a root URL without `/otterio/` or `/otterio/admin/v3`. Upstream `mc admin` compatibility is not assumed.

Query the target's live help before editing it, for example:

```sh
oc admin config set myotterio notify_webhook
oc admin config set myotterio notify_webhook --env
```

The five configuration subsystems are `notify_webhook`, `notify_mysql`, `notify_postgres`, `notify_elasticsearch` and `notify_redis`. Examples below show illustrative output; use the values returned by your server.

- `*` after a parameter marks a required parameter; `*` after a value marks the default value.
- Enable a target with `enable=on`, or its corresponding `OTTERIO_NOTIFY_<TARGET>_ENABLE=on` environment variable. Restart the server after changing notification target configuration.
- A named target uses the same suffix on every environment variable, for example `OTTERIO_NOTIFY_WEBHOOK_ENABLE_photos` and `OTTERIO_NOTIFY_WEBHOOK_ENDPOINT_photos`. The unsuffixed variables configure the default target.
- ARN format is `arn:otterio:sqs:<region>:<target-id>:<type>`. Copy the ARN printed by your server. The examples use an empty region; `OTTERIO_REGION_NAME` changes that component. The PostgreSQL configuration key is `notify_postgres`, while its ARN type is `postgresql`.
- Persistent delivery requires a nonempty, absolute `queue_dir` writable by the server, on persistent storage for each server instance. An empty directory setting disables the disk queue. `queue_limit=0` selects the default queue limit of 100,000 entries, capped by the process file limit; it does not mean unlimited delivery. A full queue returns an error, and consumers should tolerate duplicate events during retries.
- Notifications apply to matching operations after the configuration is enabled. `namespace` targets do not backfill objects already present in the bucket.
- Endpoint hostnames are resolved from the OtterIO server's network environment. When running in a container, `localhost` refers to that container.

Supported target guides: [Elasticsearch](#Elasticsearch), [Redis](#Redis), [PostgreSQL](#PostgreSQL), [MySQL](#MySQL), [Webhooks](#webhooks).

<a name="Elasticsearch"></a>

## Publish OtterIO events via Elasticsearch

Install [Elasticsearch](https://www.elastic.co/downloads/elasticsearch) server.

This notification target supports two formats: _namespace_ and _access_.

When the _namespace_ format is used, OtterIO synchronizes objects in the bucket with documents in the index. For each event in the OtterIO, the server creates a document with the bucket and object name from the event as the document ID. Other details of the event are stored in the body of the document. Thus if an existing object is over-written in OtterIO, the corresponding document in the Elasticsearch index is updated. If an object is deleted, the corresponding document is deleted from the index.

When the _access_ format is used, OtterIO appends events as documents in an Elasticsearch index. For each event, a document with the event details, with the timestamp of document set to the event's timestamp is appended to an index. The ID of the documented is randomly generated by Elasticsearch. No documents are deleted or modified in this format.

The steps below show how to use this notification target in `namespace` format. The other format is very similar and is omitted for brevity.

### Step 1: Ensure minimum requirements are met

The bundled target uses [`github.com/olivere/elastic/v7`](../../../pkg/event/target/elasticsearch.go). Validate compatibility with your deployed Elasticsearch API; a client dependency alone does not establish compatibility with every server major release.

### Step 2: Add Elasticsearch endpoint to OtterIO

The Elasticsearch configuration is located in the `notify_elasticsearch` key. Create a configuration key-value pair here for your Elasticsearch instance. The key is a name for your Elasticsearch endpoint, and the value is a collection of key-value parameters described in the table below.

```
KEY:
notify_elasticsearch[:name]  publish bucket notifications to Elasticsearch endpoints

ARGS:
url*         (url)                Elasticsearch server's address, with optional authentication info
index*       (string)             Elasticsearch index to store/update events, index is auto-created
format*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
queue_dir    (path)               staging dir for undelivered messages e.g. '/home/events'
queue_limit  (number)             maximum limit for undelivered messages, defaults to '100000'
username     (string)             username for Elasticsearch basic-auth
password     (string)             password for Elasticsearch basic-auth
comment      (sentence)           optionally add a comment to this setting
```

or environment variables

```
KEY:
notify_elasticsearch[:name]  publish bucket notifications to Elasticsearch endpoints

ARGS:
OTTERIO_NOTIFY_ELASTICSEARCH_ENABLE*      (on|off)             enable notify_elasticsearch target, default is 'off'
OTTERIO_NOTIFY_ELASTICSEARCH_URL*         (url)                Elasticsearch server's address, with optional authentication info
OTTERIO_NOTIFY_ELASTICSEARCH_INDEX*       (string)             Elasticsearch index to store/update events, index is auto-created
OTTERIO_NOTIFY_ELASTICSEARCH_FORMAT*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
OTTERIO_NOTIFY_ELASTICSEARCH_QUEUE_DIR    (path)               staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_ELASTICSEARCH_QUEUE_LIMIT  (number)             maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_ELASTICSEARCH_USERNAME     (string)             username for Elasticsearch basic-auth
OTTERIO_NOTIFY_ELASTICSEARCH_PASSWORD     (string)             password for Elasticsearch basic-auth
OTTERIO_NOTIFY_ELASTICSEARCH_COMMENT      (sentence)           optionally add a comment to this setting
```

For example: `http://localhost:9200` or with authentication info `http://<username>:<password>@127.0.0.1:9200`.

OtterIO supports persistent event store. The persistent store will backup events when the Elasticsearch broker goes offline and replays it when the broker comes back online. The event store can be configured by setting the directory path in `queue_dir` field and the maximum limit of events in the queue_dir in `queue_limit` field. For eg, the `queue_dir` can be `/home/events` and `queue_limit` can be `1000`. By default, the `queue_limit` is set to 100000.

If Elasticsearch has authentication enabled, the credentials can be supplied to OtterIO via the `url` parameter formatted as `PROTO://USERNAME:PASSWORD@ELASTICSEARCH_HOST:PORT`.

To update the configuration, use `oc admin config get` command to get the current configuration.

```sh
$ oc admin config get myotterio/ notify_elasticsearch
notify_elasticsearch:1 queue_limit="0"  url="" format="namespace" index="" queue_dir=""
```

Use `oc admin config set` command to update the configuration for the deployment. Restart the OtterIO server to put the changes into effect. The server will print a line like `SQS ARNs: arn:otterio:sqs::1:elasticsearch` at start-up if there were no errors.

```sh
$ oc admin config set myotterio notify_elasticsearch:1 enable=on queue_limit="0"  url="http://127.0.0.1:9200" format="namespace" index="otterio_events" queue_dir="" username="" password=""
```

Note that, you can add as many Elasticsearch server endpoint configurations as needed by providing an identifier (like "1" in the example above) for the Elasticsearch instance and an object of per-server configuration parameters.

### Step 3: Enable bucket notification using OtterIO client

We will now enable bucket event notifications on a bucket named `images`. Whenever a JPEG image is created/overwritten, a new document is added or an existing document is updated in the Elasticsearch index configured above. When an existing object is deleted, the corresponding document is deleted from the index. Thus, the rows in the Elasticsearch index, reflect the `.jpg` objects in the `images` bucket.

To configure this bucket notification, we need the ARN printed by OtterIO in the previous step. Additional information about ARN is available [here](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html).

With the `oc` tool, the configuration is very simple to add. Let us say that the OtterIO server is aliased as `myotterio` in the OC configuration. Execute the following:

```
oc mb myotterio/images
oc event add  myotterio/images arn:otterio:sqs::1:elasticsearch --suffix .jpg --event put,delete
oc event list myotterio/images
arn:otterio:sqs::1:elasticsearch s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### Step 4: Test on Elasticsearch

Upload a JPEG and query the configured index:

```sh
oc cp myphoto.jpg myotterio/images
curl "http://localhost:9200/otterio_events/_search?pretty=true"
```

Look for a hit whose `_id` is `images/myphoto.jpg` and whose `_source.Records[0].eventName` is `s3:ObjectCreated:Put`. Request IDs, timestamps and event metadata vary by deployment.

<a name="Redis"></a>

## Publish OtterIO events via Redis

Install [Redis](https://redis.io/downloads/) and load its actual password into `OTTERIO_REDIS_PASSWORD` from your secret store before using the configuration example.

This notification target supports two formats: _namespace_ and _access_.

When the _namespace_ format is used, OtterIO synchronizes objects in the bucket with entries in a hash. For each entry, the key is formatted as "bucketName/objectName" for an object that exists in the bucket, and the value is the JSON-encoded event data about the operation that created/replaced the object in OtterIO. When objects are updated or deleted, the corresponding entry in the hash is also updated or deleted.

When the _access_ format is used, OtterIO appends events to a list using [RPUSH](https://redis.io/commands/rpush). Each item in the list is a JSON encoded list with two items, where the first item is a timestamp string, and the second item is a JSON object containing event data about the operation that happened in the bucket. No entries appended to the list are updated or deleted by OtterIO in this format.

The steps below show how to use this notification target in `namespace` and `access` format.

### Step 1: Add Redis endpoint to OtterIO

The Redis configuration is located in the `notify_redis` subsystem. Create a configuration key-value pair here for your Redis instance. The key is a name for your Redis endpoint, and the value is a collection of key-value parameters described in the table below.

```
KEY:
notify_redis[:name]  publish bucket notifications to Redis datastores

ARGS:
address*     (address)            Redis server's address. For example: `localhost:6379`
key*         (string)             Redis key to store/update events, key is auto-created
format*      (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
password     (string)             Redis server password
queue_dir    (path)               staging dir for undelivered messages e.g. '/home/events'
queue_limit  (number)             maximum limit for undelivered messages, defaults to '100000'
comment      (sentence)           optionally add a comment to this setting
```

or environment variables

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

OtterIO supports persistent event store. The persistent store will backup events when the Redis broker goes offline and replays it when the broker comes back online. The event store can be configured by setting the directory path in `queue_dir` field and the maximum limit of events in the queue_dir in `queue_limit` field. For eg, the `queue_dir` can be `/home/events` and `queue_limit` can be `1000`. By default, the `queue_limit` is set to 100000.

To update the configuration, use `oc admin config get` command to get the current configuration.

```sh
$ oc admin config get myotterio/ notify_redis
notify_redis:1 address="" format="namespace" key="" password="" queue_dir="" queue_limit="0"
```

Use `oc admin config set` command to update the configuration for the deployment.Restart the OtterIO server to put the changes into effect. The server will print a line like `SQS ARNs: arn:otterio:sqs::1:redis` at start-up if there were no errors.

```sh
$ oc admin config set myotterio/ notify_redis:1 enable=on address="127.0.0.1:6379" format="namespace" key="bucketevents" password="$OTTERIO_REDIS_PASSWORD" queue_dir="" queue_limit="0"
```

Note that, you can add as many Redis server endpoint configurations as needed by providing an identifier (like "1" in the example above) for the Redis instance and an object of per-server configuration parameters.

### Step 2: Enable bucket notification using OtterIO client

We will now enable bucket event notifications on a bucket named `images`. Whenever a JPEG image is created/overwritten, a new key is added or an existing key is updated in the Redis hash configured above. When an existing object is deleted, the corresponding key is deleted from the Redis hash. Thus, the rows in the Redis hash, reflect the `.jpg` objects in the `images` bucket.

To configure this bucket notification, we need the ARN printed by OtterIO in the previous step. Additional information about ARN is available [here](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html).

With the `oc` tool, the configuration is very simple to add. Let us say that the OtterIO server is aliased as `myotterio` in the OC configuration. Execute the following:

```
oc mb myotterio/images
oc event add myotterio/images arn:otterio:sqs::1:redis --suffix .jpg --event put,delete
oc event list myotterio/images
arn:otterio:sqs::1:redis s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### Step 3: Test on Redis

Upload a JPEG, then read the corresponding entry from the configured `bucketevents` hash:

```sh
oc cp myphoto.jpg myotterio/images
REDISCLI_AUTH="$OTTERIO_REDIS_PASSWORD" redis-cli HGET bucketevents images/myphoto.jpg
```

The returned JSON should contain a `Records` entry with `eventName` equal to `s3:ObjectCreated:Put` and object key `myphoto.jpg`. For `access` format, inspect the Redis list with `LRANGE bucketevents 0 -1` instead.

<a name="PostgreSQL"></a>

## Publish OtterIO events via PostgreSQL

Use `connection_string` to configure the connection. The legacy `host`, `port`, `username`, `password` and `database` configuration keys are not accepted by the current notification subsystem.

Install [PostgreSQL](https://www.postgresql.org/), create the database `otterio_events` and a notification account named `otterio_events_user` with permission to create the event table and insert, update and delete rows. Load its actual password into `OTTERIO_POSTGRES_PASSWORD` from your secret store. The example uses a local database connection.

This notification target supports two formats: _namespace_ and _access_.

When the _namespace_ format is used, OtterIO synchronizes objects in the bucket with rows in the table. It creates rows with two columns: key and value. The key is the bucket and object name of an object that exists in OtterIO. The value is JSON encoded event data about the operation that created/replaced the object in OtterIO. When objects are updated or deleted, the corresponding row from this table is updated or deleted respectively.

When the _access_ format is used, OtterIO appends events to a table. It creates rows with two columns: event_time and event_data. The event_time is the time at which the event occurred in the OtterIO server. The event_data is the JSON encoded event data about the operation on an object. No rows are deleted or modified in this format.

The steps below show how to use this notification target in `namespace` format. The other format is very similar and is omitted for brevity.

### Step 1: Ensure minimum requirements are met

OtterIO requires PostgreSQL version 9.5 or above. OtterIO uses the [`INSERT ON CONFLICT`](https://www.postgresql.org/docs/9.5/static/sql-insert.html#SQL-ON-CONFLICT) (aka UPSERT) feature, introduced in version 9.5 and the [JSONB](https://www.postgresql.org/docs/9.4/static/datatype-json.html) data-type introduced in version 9.4.

### Step 2: Add PostgreSQL endpoint to OtterIO

The PostgreSQL configuration is located in the `notify_postgres` key. Create a configuration key-value pair here for your PostgreSQL instance. The key is a name for your PostgreSQL endpoint, and the value is a collection of key-value parameters described in the table below.

```
KEY:
notify_postgres[:name]  publish bucket notifications to Postgres databases

ARGS:
connection_string*   (string)             Postgres server connection-string e.g. "host=localhost port=5432 dbname=otterio_events user=postgres password=<password> sslmode=disable"
table*               (string)             DB table name to store/update events, table is auto-created
format*              (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
queue_dir            (path)               staging dir for undelivered messages e.g. '/home/events'
queue_limit          (number)             maximum limit for undelivered messages, defaults to '100000'
max_open_connections (number)             maximum number of open connections to the database, defaults to '2'
comment              (sentence)           optionally add a comment to this setting
```

or environment variables
```
KEY:
notify_postgres[:name]  publish bucket notifications to Postgres databases

ARGS:
OTTERIO_NOTIFY_POSTGRES_ENABLE*              (on|off)             enable notify_postgres target, default is 'off'
OTTERIO_NOTIFY_POSTGRES_CONNECTION_STRING*   (string)             Postgres server connection-string e.g. "host=localhost port=5432 dbname=otterio_events user=postgres password=<password> sslmode=disable"
OTTERIO_NOTIFY_POSTGRES_TABLE*               (string)             DB table name to store/update events, table is auto-created
OTTERIO_NOTIFY_POSTGRES_FORMAT*              (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
OTTERIO_NOTIFY_POSTGRES_QUEUE_DIR            (path)               staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_POSTGRES_QUEUE_LIMIT          (number)             maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_POSTGRES_COMMENT              (sentence)           optionally add a comment to this setting
OTTERIO_NOTIFY_POSTGRES_MAX_OPEN_CONNECTIONS (number)             maximum number of open connections to the database, defaults to '2'
```

> NOTE: If the `max_open_connections` key or the environment variable `OTTERIO_NOTIFY_POSTGRES_MAX_OPEN_CONNECTIONS` is set to `0`, There will be no limit set on the number of
> open connections to the database. This setting is generally NOT recommended as the behavior may be inconsistent during recursive deletes in `namespace` format.

OtterIO supports persistent event store. The persistent store will backup events when the PostgreSQL connection goes offline and replays it when the broker comes back online. The event store can be configured by setting the directory path in `queue_dir` field and the maximum limit of events in the queue_dir in `queue_limit` field. For eg, the `queue_dir` can be `/home/events` and `queue_limit` can be `1000`. By default, the `queue_limit` is set to 100000.

Note that for illustration here, we have disabled SSL. In the interest of security, for production this is not recommended.
To update the configuration, use `oc admin config get` command to get the current configuration.

```sh
$ oc admin config get myotterio notify_postgres
notify_postgres:1 queue_dir="" connection_string="" queue_limit="0"  table="" format="namespace"
```

Use `oc admin config set` command to update the configuration for the deployment. Restart the OtterIO server to put the changes into effect. The server will print a line like `SQS ARNs: arn:otterio:sqs::1:postgresql` at start-up if there were no errors.

```sh
$ oc admin config set myotterio notify_postgres:1 enable=on connection_string="host=localhost port=5432 dbname=otterio_events user=otterio_events_user password=${OTTERIO_POSTGRES_PASSWORD} sslmode=disable" table="bucketevents" format="namespace"
```

Note that, you can add as many PostgreSQL server endpoint configurations as needed by providing an identifier (like "1" in the example above) for the PostgreSQL instance and an object of per-server configuration parameters.

### Step 3: Enable bucket notification using OtterIO client

We will now enable bucket event notifications on a bucket named `images`. Whenever a JPEG image is created/overwritten, a new row is added or an existing row is updated in the PostgreSQL configured above. When an existing object is deleted, the corresponding row is deleted from the PostgreSQL table. Thus, the rows in the PostgreSQL table, reflect the `.jpg` objects in the `images` bucket.

To configure this bucket notification, we need the ARN printed by OtterIO in the previous step. Additional information about ARN is available [here](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html).

With the `oc` tool, the configuration is very simple to add. Let us say that the OtterIO server is aliased as `myotterio` in the OC configuration. Execute the following:

```
# Create bucket named `images` in myotterio
oc mb myotterio/images
# Add notification configuration on the `images` bucket using the PostgreSQL ARN. The --suffix argument filters events.
oc event add myotterio/images arn:otterio:sqs::1:postgresql --suffix .jpg --event put,delete
# Print out the notification configuration on the `images` bucket.
oc event list myotterio/images
arn:otterio:sqs::1:postgresql s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### Step 4: Test on PostgreSQL

Upload a JPEG, connect as the notification account, and inspect the recorded event:

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

## Publish OtterIO events via MySQL

Use `dsn_string` to configure the connection. The legacy `host`, `port`, `username`, `password` and `database` configuration keys are not accepted by the current notification subsystem.

Install [MySQL](https://dev.mysql.com/downloads/mysql/), create the database `otteriodb` and a notification account named `otterio_events_user` with permission to create the event table and insert, update and delete rows. Load its actual password into `OTTERIO_MYSQL_PASSWORD` from your secret store.

This notification target supports two formats: _namespace_ and _access_.

When the _namespace_ format is used, OtterIO synchronizes objects in the bucket with rows in the table. It creates rows with two columns: key_name and value. The key_name is the bucket and object name of an object that exists in OtterIO. The value is JSON encoded event data about the operation that created/replaced the object in OtterIO. When objects are updated or deleted, the corresponding row from this table is updated or deleted respectively.

When the _access_ format is used, OtterIO appends events to a table. It creates rows with two columns: event_time and event_data. The event_time is the time at which the event occurred in the OtterIO server. The event_data is the JSON encoded event data about the operation on an object. No rows are deleted or modified in this format.

The steps below show how to use this notification target in `namespace` format. The other format is very similar and is omitted for brevity.

### Step 1: Ensure minimum requirements are met

OtterIO requires MySQL version 5.7.8 or above. OtterIO uses the [JSON](https://dev.mysql.com/doc/refman/5.7/en/json.html) data-type introduced in version 5.7.8. We tested this setup on MySQL 5.7.17.

### Step 2: Add MySQL server endpoint configuration to OtterIO

The MySQL configuration is located in the `notify_mysql` key. Create a configuration key-value pair here for your MySQL instance. The key is a name for your MySQL endpoint, and the value is a collection of key-value parameters described in the table below.

```
KEY:
notify_mysql[:name]  publish bucket notifications to MySQL databases. When multiple MySQL server endpoints are needed, a user specified "name" can be added for each configuration, (e.g."notify_mysql:myinstance").

ARGS:
dsn_string*          (string)             MySQL data-source-name connection string e.g. "<user>:<password>@tcp(<host>:<port>)/<database>"
table*               (string)             DB table name to store/update events, table is auto-created
format*              (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
queue_dir            (path)               staging dir for undelivered messages e.g. '/home/events'
queue_limit          (number)             maximum limit for undelivered messages, defaults to '100000'
max_open_connections (number)             maximum number of open connections to the database, defaults to '2'
comment              (sentence)           optionally add a comment to this setting
```

or environment variables
```
KEY:
notify_mysql[:name]  publish bucket notifications to MySQL databases

ARGS:
OTTERIO_NOTIFY_MYSQL_ENABLE*              (on|off)             enable notify_mysql target, default is 'off'
OTTERIO_NOTIFY_MYSQL_DSN_STRING*          (string)             MySQL data-source-name connection string e.g. "<user>:<password>@tcp(<host>:<port>)/<database>"
OTTERIO_NOTIFY_MYSQL_TABLE*               (string)             DB table name to store/update events, table is auto-created
OTTERIO_NOTIFY_MYSQL_FORMAT*              (namespace*|access)  'namespace' reflects current bucket/object list and 'access' reflects a journal of object operations, defaults to 'namespace'
OTTERIO_NOTIFY_MYSQL_QUEUE_DIR            (path)               staging dir for undelivered messages e.g. '/home/events'
OTTERIO_NOTIFY_MYSQL_QUEUE_LIMIT          (number)             maximum limit for undelivered messages, defaults to '100000'
OTTERIO_NOTIFY_MYSQL_MAX_OPEN_CONNECTIONS (number)             maximum number of open connections to the database, defaults to '2'
OTTERIO_NOTIFY_MYSQL_COMMENT              (sentence)           optionally add a comment to this setting
```

> NOTE: If the `max_open_connections` key or the environment variable `OTTERIO_NOTIFY_MYSQL_MAX_OPEN_CONNECTIONS` is set to `0`, There will be no limit set on the number of
> open connections to the database. This setting is generally NOT recommended as the behavior may be inconsistent during recursive deletes in `namespace` format.

`dsn_string` is required and is of form `"<user>:<password>@tcp(<host>:<port>)/<database>"`

OtterIO supports persistent event store. The persistent store will backup events if MySQL connection goes offline and then replays the stored events when the broken connection comes back up. The event store can be configured by setting a directory path in `queue_dir` field, and the maximum number of events, which can be stored in a `queue_dir`, in `queue_limit` field. For example, `queue_dir` can be set to `/home/events` and `queue_limit` can be set to `1000`. By default, the `queue_limit` is set to `100000`.

Before updating the configuration, let's start with `oc admin config get` command to get the current configuration.

```sh
$ oc admin config get myotterio/ notify_mysql
notify_mysql:myinstance enable=off format=namespace dsn_string= table= queue_dir= queue_limit=0 max_open_connections=2
```

Use `oc admin config set` command to update MySQL notification configuration for the deployment with `dsn_string` parameter:

```sh
$ oc admin config set myotterio notify_mysql:myinstance enable=on table="otterio_images" dsn_string="otterio_events_user:${OTTERIO_MYSQL_PASSWORD}@tcp(127.0.0.1:3306)/otteriodb"
```

Note that, you can add as many MySQL server endpoint configurations as needed by providing an identifier (like "myinstance" in the example above) for each MySQL instance desired.

Restart the OtterIO server to put the changes into effect. The server will print a line like `SQS ARNs: arn:otterio:sqs::myinstance:mysql` at start-up, if there are no errors.

### Step 3: Enable bucket notification using OtterIO client

We will now setup bucket notifications on a bucket named `images`. Whenever a JPEG image object is created/overwritten, a new row is added or an existing row is updated in the MySQL table configured above. When an existing object is deleted, the corresponding row is deleted from the MySQL table. Thus, the rows in the MySQL table, reflect the `.jpg` objects in the `images` bucket.

To configure this bucket notification, we need the ARN printed by OtterIO in the previous step. Additional information about ARN is available [here](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html).

With the `oc` tool, the configuration is very simple to add. Let us say that the OtterIO server is aliased as `myotterio` in the OC configuration. Execute the following:

```
# Create bucket named `images` in myotterio
oc mb myotterio/images
# Add notification configuration on the `images` bucket using the MySQL ARN. The --suffix argument filters events.
oc event add myotterio/images arn:otterio:sqs::myinstance:mysql --suffix .jpg --event put,delete
# Print out the notification configuration on the `images` bucket.
oc event list myotterio/images
arn:otterio:sqs::myinstance:mysql s3:ObjectCreated:*,s3:ObjectRemoved:* Filter: suffix=".jpg"
```

### Step 4: Test on MySQL

Upload a JPEG, connect as the notification account, and inspect the recorded event:

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

## Publish OtterIO events via Webhooks

[Webhooks](https://en.wikipedia.org/wiki/Webhook) are a way to receive information when it happens, rather than continually polling for that data.

### Step 1: Add Webhook endpoint to OtterIO

OtterIO supports persistent event store. The persistent store will backup events when the webhook goes offline and replays it when the broker comes back online. The event store can be configured by setting the directory path in `queue_dir` field and the maximum limit of events in the queue_dir in `queue_limit` field. For eg, the `queue_dir` can be `/home/events` and `queue_limit` can be `1000`. By default, the `queue_limit` is set to 100000.

```
KEY:
notify_webhook[:name]  publish bucket notifications to webhook endpoints

ARGS:
endpoint*    (url)       webhook server endpoint e.g. http://localhost:8080/otterio/events
auth_token   (string)    opaque string or JWT authorization token
queue_dir    (path)      staging dir for undelivered messages e.g. '/home/events'
queue_limit  (number)    maximum limit for undelivered messages, defaults to '100000'
client_cert  (string)    client cert for Webhook mTLS auth
client_key   (string)    client cert key for Webhook mTLS auth
comment      (sentence)  optionally add a comment to this setting
```

or environment variables
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

Use `oc admin config set` command to update the configuration for the deployment. Here the endpoint is the server listening for webhook notifications. Save the settings and restart the OtterIO server for changes to take effect. Note that the endpoint needs to be live and reachable when you restart your OtterIO server.

```sh
$ oc admin config set myotterio notify_webhook:1 enable=on queue_limit="0"  endpoint="http://localhost:3000" queue_dir=""
```

### Step 2: Enable bucket notification using OtterIO client

We will enable bucket event notification to trigger whenever a JPEG image is uploaded to `images` bucket on `myotterio` server. Here ARN value is `arn:otterio:sqs::1:webhook`. To learn more about ARN please follow [AWS ARN](http://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html) documentation.

```
oc mb myotterio/images
oc event add myotterio/images arn:otterio:sqs::1:webhook --event put --suffix .jpg
```

Check if event notification is successfully configured by

```
oc event list myotterio/images
```

You should get a response like this

```
arn:otterio:sqs::1:webhook   s3:ObjectCreated:*   Filter: suffix=".jpg"
```

### Step 3: Test with a local webhook receiver

Save the following as `webhook_receiver.py` and run `python3 webhook_receiver.py` in another terminal before configuring the target in Step 1. This example assumes the receiver and OtterIO run on the same host; adjust the listen address and endpoint for a container or another machine.

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

Upload a JPEG to trigger the configured `put` event:

```sh
oc cp ~/images.jpg myotterio/images
```

The receiver should print a JSON object whose `Records` entry contains `eventName: s3:ObjectCreated:Put`, bucket `images` and the uploaded object key. Confirm delivery using the received payload; the upload progress alone does not confirm notification delivery.
