# OtterIO Server Config Guide

## Configuration Directory

Current server configuration is stored in the storage backend passed to `otterio server`, rather than in a local `config.json` beside the executable. Use [OC](https://github.com/soulteary/oc) to inspect and update it through the OtterIO management API.

`--config-dir` remains a hidden, deprecated compatibility option for importing a legacy `config.json`. A successful migration renames the old file to `config.json.deprecated`. For a new deployment, specify the data directory and use `--certs-dir` when a custom certificate location is needed.

```sh
otterio server /data
```

When root credentials are supplied through environment variables, OtterIO uses them to encrypt backend configuration, IAM data and policies. Keep these credentials available when restarting or migrating the deployment.

### Certificate Directory

TLS certificates by default are stored under ``${HOME}/.otterio/certs`` directory. You need to place certificates here to enable `HTTPS` based access. See the local [TLS guide](../tls/README.md).

Following is the directory structure for OtterIO server with TLS certificates.

```sh
/home/user1/.otterio
└─ certs
   ├─ CAs
   ├─ private.key
   └─ public.crt
```

You can provide a custom certs directory using `--certs-dir` command line option.

#### Credentials
Set the root credentials with `OTTERIO_ROOT_USER` and `OTTERIO_ROOT_PASSWORD`. Set both variables together. The legacy `OTTERIO_ACCESS_KEY` and `OTTERIO_SECRET_KEY` names remain accepted for compatibility; use the root variable names for new deployments.

```sh
export OTTERIO_ROOT_USER=otterio
export OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
# Save both values in your secret store before starting the server.
otterio server /data
```

For an existing deployment, restore the saved values instead of generating new credentials at each restart. See the [quickstart](../../README.md) for the initial setup.

##### Rotating encryption with new credentials

To rotate credentials for an encrypted backend, supply the current credentials through the `_OLD` variables and the new credentials through the root variables for one startup. The old values must match the credentials that encrypted the existing configuration.

```sh
# Restore the current root credentials from your secret store first.
: "${OTTERIO_ROOT_USER:?Restore the current root user first}"
: "${OTTERIO_ROOT_PASSWORD:?Restore the current root password first}"
export OTTERIO_ROOT_USER_OLD="$OTTERIO_ROOT_USER"
export OTTERIO_ROOT_PASSWORD_OLD="$OTTERIO_ROOT_PASSWORD"
export OTTERIO_ROOT_USER="otterio-$(openssl rand -hex 8)"
export OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
# Save the new root values in your secret store before starting the server.
otterio server /data
```

The server removes the `_OLD` variables from its process environment after reading them. After a successful rotation, remove them from shell startup scripts, container definitions or service files before restarting again.

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
For an erasure set with 4 or 5 drives, default STANDARD parity is `EC:2`; with 6 or 7 drives it is `EC:3`; with 8–16 drives it is `EC:4`. The default REDUCED_REDUNDANCY parity is `EC:2`. These settings apply to erasure storage. See the local [storage class guide](../erasure/storage-class/README.md).

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
The default `requests_max=0` lets the server calculate a concurrent request limit from available memory and the number of drives. Set a positive value to choose an explicit deployment limit; distributed deployments divide it across server hosts. `requests_deadline` controls how long a request can wait for capacity. See the local [throttling guide](../throttle/README.md). The following are common API settings; query OC help for the full set.

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
Notification targets supported by OtterIO are in the following list. To configure individual targets please refer to more detailed documentation [the bucket notification guide](../bucket/notifications/README.md)

```
notify_webhook        publish bucket notifications to webhook endpoints
notify_mysql          publish bucket notifications to MySQL databases
notify_postgres       publish bucket notifications to Postgres databases
notify_elasticsearch  publish bucket notifications to Elasticsearch endpoints
notify_redis          publish bucket notifications to Redis datastores
```

### Accessing configuration
Use the current [OC client](https://github.com/soulteary/oc) and its `oc admin config` get/set/reset/export/import commands. OtterIO exposes management operations under `/otterio/admin/v3`; upstream `mc admin` compatibility is not assumed.

For a single-port server, configure the alias using the S3 endpoint:

```sh
oc alias set myotterio http://localhost:9000 "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" --api s3v4 --path on
```

If the server uses `--console-address ":9001"`, add `--admin-url http://localhost:9001` to the alias command. This URL is the management root; do not append `/otterio/` or `/otterio/admin/v3`. Object operations still use port 9000. For separate TLS certificates, use OC's `--admin-ca /path/to/admin-ca.pem` when the management certificate needs a custom CA.

#### List all config keys available
```
oc admin config set myotterio/
```

#### Obtain help for each key
```
oc admin config set myotterio/ <key>
```

e.g: `oc admin config set myotterio/ etcd` returns available `etcd` config args

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

To get ENV equivalent for each config args use `--env` flag
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

This behavior is consistent across all keys, each key self documents itself with valid examples.

## Dynamic systems without restarting server

The following sub-systems are dynamic i.e., configuration parameters for each sub-systems can be changed while the server is running without any restarts.

```
api                   manage global HTTP API call specific features, such as throttling, authentication types, etc.
compression           configure compression where supported by the storage backend
heal                  manage object healing frequency and bitrot verification checks
scanner               manage namespace scanning for usage calculation, lifecycle, healing and more
```

> Environment variables take precedence over stored settings. A value supplied through the process environment cannot be changed with `oc admin config set`; update the environment and restart the server to change that value.

### Usage scanner

Data usage scanner is enabled by default. The following configuration settings allow for more staggered delay in terms of usage calculation. The scanner adapts to the system speed and completely pauses when the system is under load. It is possible to adjust the speed of the scanner and thereby the latency of updates being reflected. The delays between each operation of the scanner can be adjusted by the `oc admin config set alias/ scanner delay=15.0`. By default the value is `10.0`. This means the scanner will sleep *10x* the time each operation takes.

In most setups this will keep the scanner slow enough to not impact overall system performance. Setting the `delay` key to a *lower* value will make the scanner faster and setting it to 0 will make the scanner run at full speed (not recommended in production). Setting it to a higher value will make the scanner slower, consuming less resources with the trade off of not collecting metrics for operations like healing and disk usage as fast.

```
oc admin config set alias/ scanner
KEY:
scanner  manage namespace scanning for usage calculation, lifecycle, healing and more

ARGS:
delay     (float)     scanner delay multiplier, defaults to '10.0'
max_wait  (duration)  maximum wait time between operations, defaults to '15s'
cycle     (duration)  time between scanner cycles, defaults to '1m'
```

Example: Following setting will decrease the scanner speed by a factor of 3, reducing the system resource use, but increasing the latency of updates being reflected.

```sh
oc admin config set alias/ scanner delay=30.0
```

Once set the scanner settings are automatically applied without the need for server restarts.

> NOTE: Data usage scanner is not supported under Gateway deployments.

### Healing

Healing is enabled by default. The following configuration settings allow for more staggered delay in terms of healing. The healing system by default adapts to the system speed and pauses up to '1sec' per object when the system has `max_io` number of concurrent requests. It is possible to adjust the `max_sleep` and `max_io` values thereby increasing the healing speed. The delays between each operation of the healer can be adjusted by the `oc admin config set alias/ heal max_sleep=1s` and maximum concurrent requests allowed before we start slowing things down can be configured with `oc admin config set alias/ heal max_io=30` . By default the wait delay is `1sec` beyond 10 concurrent operations. This means the healer will sleep *1 second* at max for each heal operation if there are more than *10* concurrent client requests.

In most setups this is sufficient to heal the content after drive replacements. Setting `max_sleep` to a *lower* value and setting `max_io` to a *higher* value would make heal go faster.

```
oc admin config set alias/ heal
KEY:
heal  manage object healing frequency and bitrot verification checks

ARGS:
bitrotscan  (on|off)    perform bitrot scan on disks when checking objects during scanner
max_sleep   (duration)  maximum sleep duration between objects to slow down heal operation. eg. 2s
max_io      (int)       maximum IO requests allowed between objects to slow down heal operation. eg. 3
```

Example: The following settings will increase the heal operation speed by allowing healing operation to run without delay up to `100` concurrent requests, and the maximum delay between each heal operation is set to `300ms`.

```sh
oc admin config set alias/ heal max_sleep=300ms max_io=100
```

Once set the healer settings are automatically applied without the need for server restarts.

> NOTE: Healing is not supported under Gateway deployments.


## Environment only settings (not in config)

### Browser

Enable or disable access to the web UI with `OTTERIO_BROWSER`; the default is `on`. Setting it to `off` does not disable the S3 or management APIs.

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
* [OtterIO Quickstart Guide](../../README.md)
* [Configure OtterIO Server with TLS](../tls/README.md)
* [OC configuration and administration](https://github.com/soulteary/oc/blob/main/docs/administration.md)
