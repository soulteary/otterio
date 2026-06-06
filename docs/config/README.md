# OtterIO Server Config Guide

## Configuration Directory

Till OtterIO release `RELEASE.2018-08-02T23-11-36Z`, OtterIO server configuration file (`config.json`) was stored in the configuration directory specified by `--config-dir` or defaulted to `${HOME}/.otterio`. However from releases after `RELEASE.2018-08-18T03-49-57Z`, the configuration file (only), has been migrated to the storage backend (storage backend is the directory passed to OtterIO server while starting the server).

You can specify the location of your existing config using `--config-dir`, OtterIO will migrate the `config.json` to your backend storage. Your current `config.json` will be renamed upon successful migration as `config.json.deprecated` in your current `--config-dir`. All your existing configurations are honored after this migration.

Additionally `--config-dir` is now a legacy option which will is scheduled for removal in future, so please update your local startup, ansible scripts accordingly.

```sh
otterio server /data
```

OtterIO also encrypts all the config, IAM and policies content with admin credentials.

### Certificate Directory

TLS certificates by default are stored under ``${HOME}/.otterio/certs`` directory. You need to place certificates here to enable `HTTPS` based access. Read more about [How to secure access to OtterIO server with TLS](https://docs.min.io/docs/how-to-secure-access-to-minio-server-with-tls).

Following is the directory structure for OtterIO server with TLS certificates.

```sh
$ mc tree --files ~/.otterio
/home/user1/.otterio
└─ certs
   ├─ CAs
   ├─ private.key
   └─ public.crt
```

You can provide a custom certs directory using `--certs-dir` command line option.

#### Credentials
On OtterIO admin credentials or root credentials are only allowed to be changed using ENVs namely `OTTERIO_ROOT_USER` and `OTTERIO_ROOT_PASSWORD`. Using the combination of these two values OtterIO encrypts the config stored at the backend.

```sh
export OTTERIO_ROOT_USER=otterio
export OTTERIO_ROOT_PASSWORD=otterio13
otterio server /data
```

##### Rotating encryption with new credentials

Additionally if you wish to change the admin credentials, then OtterIO will automatically detect this and re-encrypt with new credentials as shown below. For one time only special ENVs as shown below needs to be set for rotating the encryption config.

> Old ENVs are never remembered in memory and are destroyed right after they are used to migrate your existing content with new credentials. You are safe to remove them after the server as successfully started, by restarting the services once again.

```sh
export OTTERIO_ROOT_USER=newotterio
export OTTERIO_ROOT_PASSWORD=newotterio123
export OTTERIO_ROOT_USER_OLD=otterio
export OTTERIO_ROOT_PASSWORD_OLD=otterio123
otterio server /data
```

Once the migration is complete, server will automatically unset the `OTTERIO_ROOT_USER_OLD` and `OTTERIO_ROOT_PASSWORD_OLD` with in the process namespace.

> **NOTE: Make sure to remove `OTTERIO_ROOT_USER_OLD` and `OTTERIO_ROOT_PASSWORD_OLD` in scripts or service files before next service restarts of the server to avoid double encryption of your existing contents.**

#### Region
```
KEY:
region  label the location of the server

ARGS:
name     (string)    name of the location of the server e.g. "us-west-rack2"
comment  (sentence)  optionally add a comment to this setting
```

or environment variables
```
KEY:
region  label the location of the server

ARGS:
OTTERIO_REGION_NAME     (string)    name of the location of the server e.g. "us-west-rack2"
OTTERIO_REGION_COMMENT  (sentence)  optionally add a comment to this setting
```

Example:

```sh
export OTTERIO_REGION_NAME="my_region"
otterio server /data
```

### Storage Class
By default, parity for objects with standard storage class is set to `N/2`, and parity for objects with reduced redundancy storage class objects is set to `2`. Read more about storage class support in OtterIO server [here](https://github.com/minio/minio/blob/master/docs/erasure/storage-class/README.md).

```
KEY:
storage_class  define object level redundancy

ARGS:
standard  (string)    set the parity count for default standard storage class e.g. "EC:4"
rrs       (string)    set the parity count for reduced redundancy storage class e.g. "EC:2"
comment   (sentence)  optionally add a comment to this setting
```

or environment variables
```
KEY:
storage_class  define object level redundancy

ARGS:
OTTERIO_STORAGE_CLASS_STANDARD  (string)    set the parity count for default standard storage class e.g. "EC:4"
OTTERIO_STORAGE_CLASS_RRS       (string)    set the parity count for reduced redundancy storage class e.g. "EC:2"
OTTERIO_STORAGE_CLASS_COMMENT   (sentence)  optionally add a comment to this setting
```

### Cache
OtterIO provides caching storage tier for primarily gateway deployments, allowing you to cache content for faster reads, cost savings on repeated downloads from the cloud.

```
KEY:
cache  add caching storage tier

ARGS:
drives*  (csv)       comma separated mountpoints e.g. "/optane1,/optane2"
expiry   (number)    cache expiry duration in days e.g. "90"
quota    (number)    limit cache drive usage in percentage e.g. "90"
exclude  (csv)       comma separated wildcard exclusion patterns e.g. "bucket/*.tmp,*.exe"
after    (number)    minimum number of access before caching an object
comment  (sentence)  optionally add a comment to this setting
```

or environment variables
```
KEY:
cache  add caching storage tier

ARGS:
OTTERIO_CACHE_DRIVES*  (csv)       comma separated mountpoints e.g. "/optane1,/optane2"
OTTERIO_CACHE_EXPIRY   (number)    cache expiry duration in days e.g. "90"
OTTERIO_CACHE_QUOTA    (number)    limit cache drive usage in percentage e.g. "90"
OTTERIO_CACHE_EXCLUDE  (csv)       comma separated wildcard exclusion patterns e.g. "bucket/*.tmp,*.exe"
OTTERIO_CACHE_AFTER    (number)    minimum number of access before caching an object
OTTERIO_CACHE_COMMENT  (sentence)  optionally add a comment to this setting
```

#### Etcd
OtterIO supports storing encrypted IAM assets and bucket DNS records on etcd.

> NOTE: if *path_prefix* is set then OtterIO will not federate your buckets, namespaced IAM assets are assumed as isolated tenants, only buckets are considered globally unique but performing a lookup with a *bucket* which belongs to a different tenant will fail unlike federated setups where OtterIO would port-forward and route the request to relevant cluster accordingly. This is a special feature, federated deployments should not need to set *path_prefix*.

```
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

or environment variables
```
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

### API
By default, there is no limitation on the number of concurrent requests that a server/cluster processes at the same time. However, it is possible to impose such limitation using the API subsystem. Read more about throttling limitation in OtterIO server [here](https://github.com/minio/minio/blob/master/docs/throttle/README.md).

```
KEY:
api  manage global HTTP API call specific features, such as throttling, authentication types, etc.

ARGS:
requests_max               (number)    set the maximum number of concurrent requests, e.g. "1600"
requests_deadline          (duration)  set the deadline for API requests waiting to be processed e.g. "1m"
cors_allow_origin          (csv)       set comma separated list of origins allowed for CORS requests e.g. "https://example1.com,https://example2.com"
remote_transport_deadline  (duration)  set the deadline for API requests on remote transports while proxying between federated instances e.g. "2h"
```

or environment variables

```
OTTERIO_API_REQUESTS_MAX               (number)    set the maximum number of concurrent requests, e.g. "1600"
OTTERIO_API_REQUESTS_DEADLINE          (duration)  set the deadline for API requests waiting to be processed e.g. "1m"
OTTERIO_API_CORS_ALLOW_ORIGIN          (csv)       set comma separated list of origins allowed for CORS requests e.g. "https://example1.com,https://example2.com"
OTTERIO_API_REMOTE_TRANSPORT_DEADLINE  (duration)  set the deadline for API requests on remote transports while proxying between federated instances e.g. "2h"
```

#### Notifications
Notification targets supported by OtterIO are in the following list. To configure individual targets please refer to more detailed documentation [here](https://docs.min.io/docs/minio-bucket-notification-guide.html)

```
notify_webhook        publish bucket notifications to webhook endpoints
notify_mysql          publish bucket notifications to MySQL databases
notify_postgres       publish bucket notifications to Postgres databases
notify_elasticsearch  publish bucket notifications to Elasticsearch endpoints
notify_redis          publish bucket notifications to Redis datastores
```

### Accessing configuration
All configuration changes can be made using [`mc admin config` get/set/reset/export/import commands](https://github.com/minio/mc/blob/master/docs/minio-admin-complete-guide.md).

#### List all config keys available
```
~ mc admin config set myotterio/
```

#### Obtain help for each key
```
~ mc admin config set myotterio/ <key>
```

e.g: `mc admin config set myotterio/ etcd` returns available `etcd` config args

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

To get ENV equivalent for each config args use `--env` flag
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

This behavior is consistent across all keys, each key self documents itself with valid examples.

## Dynamic systems without restarting server

The following sub-systems are dynamic i.e., configuration parameters for each sub-systems can be changed while the server is running without any restarts.

```
api                   manage global HTTP API call specific features, such as throttling, authentication types, etc.
heal                  manage object healing frequency and bitrot verification checks
scanner               manage namespace scanning for usage calculation, lifecycle, healing and more
```

> NOTE: if you set any of the following sub-system configuration using ENVs, dynamic behavior is not supported.

### Usage scanner

Data usage scanner is enabled by default. The following configuration settings allow for more staggered delay in terms of usage calculation. The scanner adapts to the system speed and completely pauses when the system is under load. It is possible to adjust the speed of the scanner and thereby the latency of updates being reflected. The delays between each operation of the scanner can be adjusted by the `mc admin config set alias/ delay=15.0`. By default the value is `10.0`. This means the scanner will sleep *10x* the time each operation takes.

In most setups this will keep the scanner slow enough to not impact overall system performance. Setting the `delay` key to a *lower* value will make the scanner faster and setting it to 0 will make the scanner run at full speed (not recommended in production). Setting it to a higher value will make the scanner slower, consuming less resources with the trade off of not collecting metrics for operations like healing and disk usage as fast.

```
~ mc admin config set alias/ scanner
KEY:
scanner  manage namespace scanning for usage calculation, lifecycle, healing and more

ARGS:
delay     (float)     scanner delay multiplier, defaults to '10.0'
max_wait  (duration)  maximum wait time between operations, defaults to '15s'
```

Example: Following setting will decrease the scanner speed by a factor of 3, reducing the system resource use, but increasing the latency of updates being reflected.

```sh
~ mc admin config set alias/ scanner delay=30.0
```

Once set the scanner settings are automatically applied without the need for server restarts.

> NOTE: Data usage scanner is not supported under Gateway deployments.

### Healing

Healing is enabled by default. The following configuration settings allow for more staggered delay in terms of healing. The healing system by default adapts to the system speed and pauses up to '1sec' per object when the system has `max_io` number of concurrent requests. It is possible to adjust the `max_delay` and `max_io` values thereby increasing the healing speed. The delays between each operation of the healer can be adjusted by the `mc admin config set alias/ max_delay=1s` and maximum concurrent requests allowed before we start slowing things down can be configured with `mc admin config set alias/ max_io=30` . By default the wait delay is `1sec` beyond 10 concurrent operations. This means the healer will sleep *1 second* at max for each heal operation if there are more than *10* concurrent client requests.

In most setups this is sufficient to heal the content after drive replacements. Setting `max_delay` to a *lower* value and setting `max_io` to a *higher* value would make heal go faster.

```
~ mc admin config set alias/ heal
KEY:
heal  manage object healing frequency and bitrot verification checks

ARGS:
bitrotscan  (on|off)    perform bitrot scan on disks when checking objects during scanner
max_sleep   (duration)  maximum sleep duration between objects to slow down heal operation. eg. 2s
max_io      (int)       maximum IO requests allowed between objects to slow down heal operation. eg. 3
```

Example: The following settings will increase the heal operation speed by allowing healing operation to run without delay up to `100` concurrent requests, and the maximum delay between each heal operation is set to `300ms`.

```sh
~ mc admin config set alias/ heal max_delay=300ms max_io=100
```

Once set the healer settings are automatically applied without the need for server restarts.

> NOTE: Healing is not supported under Gateway deployments.


## Environment only settings (not in config)

### Browser

Enable or disable access to web UI. By default it is set to `on`. You may override this field with `OTTERIO_BROWSER` environment variable.

Example:

```sh
export OTTERIO_BROWSER=off
otterio server /data
```

### Browser Address (separate console listener)

By default the web console and S3 API share the listener bound to `--address`. To serve the web UI and the admin API on a dedicated port, pass `--console-address` to `server`/`gateway`, or set the `OTTERIO_BROWSER_ADDRESS` environment variable. The S3 listener stops redirecting browser requests to the web UI in this mode; clients hitting the S3 port with a browser receive standard S3 error responses.

The console port must be different from the S3 port, otherwise the server refuses to start.

Example:

```sh
# CLI flag
otterio server --address ":9000" --console-address ":9001" /data

# environment variable (equivalent)
export OTTERIO_BROWSER_ADDRESS=":9001"
otterio server --address ":9000" /data
```

### Browser Certs Dir (separate TLS for the console listener)

When using `--console-address`, you can also point the console listener at its own TLS keypair via `--console-certs-dir` (or the `OTTERIO_BROWSER_CERTS_DIR` environment variable). The directory must contain `public.crt` and `private.key`, mirroring the layout of `--certs-dir`. Without this flag, the console listener reuses the certificates loaded from `--certs-dir`.

Example:

```sh
otterio server \
  --address ":9000" \
  --console-address ":9001" \
  --certs-dir /etc/otterio/certs/s3 \
  --console-certs-dir /etc/otterio/certs/console \
  /data
```

`--console-certs-dir` requires `--console-address`; otherwise startup fails fast.

### Domain

By default, OtterIO supports path-style requests that are of the format http://mydomain.com/bucket/object. `OTTERIO_DOMAIN` environment variable is used to enable virtual-host-style requests. If the request `Host` header matches with `(.+).mydomain.com` then the matched pattern `$1` is used as bucket and the path is used as object. More information on path-style and virtual-host-style [here](http://docs.aws.amazon.com/AmazonS3/latest/dev/RESTAPI.html)
Example:

```sh
export OTTERIO_DOMAIN=mydomain.com
otterio server /data
```

For advanced use cases `OTTERIO_DOMAIN` environment variable supports multiple-domains with comma separated values.
```sh
export OTTERIO_DOMAIN=sub1.mydomain.com,sub2.mydomain.com
otterio server /data
```

## Explore Further
* [OtterIO Quickstart Guide](https://docs.min.io/docs/minio-quickstart-guide)
* [Configure OtterIO Server with TLS](https://docs.min.io/docs/how-to-secure-access-to-minio-server-with-tls)
