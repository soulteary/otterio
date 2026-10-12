# OtterIO NAS Gateway

OtterIO Gateway adds Amazon S3 compatibility to NAS storage. You may run multiple otterio instances on the same shared NAS volume as a distributed object gateway.

## Run OtterIO Gateway for NAS Storage

### Using Docker

Please ensure to replace `/shared/nasvol` with actual mount path.

```
docker run -p 9000:9000 --name nas-s3 \
 -e "OTTERIO_ROOT_USER=otterio" \
 -e "OTTERIO_ROOT_PASSWORD=otterio123" \
 -v /shared/nasvol:/container/vol \
 soulteary/otterio gateway nas /container/vol
```

### Using Binary

```
export OTTERIO_ROOT_USER=otterio
export OTTERIO_ROOT_PASSWORD=otterio123
otterio gateway nas /shared/nasvol
```

## Use OC Web Console

OtterIO Gateway does not serve a Web UI. Use the `oc` commands below to verify S3 access, or run the standalone [OC Web Console](https://github.com/soulteary/oc/blob/main/docs/console.md) against the gateway's S3 endpoint.

`--console-address` configures an optional separate Admin listener, not the Web Console. When using it, configure OC with the gateway's actual S3 URL and a separate `--admin-url`; preserve the deployment's ports and S3/Admin CA settings. See [separate S3 and Admin listeners](../../README.md#run-s3-and-admin-on-separate-ports). Local alias mode uses one configured identity; multiple independent browser users require native IAM or OIDC mode and their own backend permissions. Available operations depend on the NAS gateway's supported protocols and configuration.

## Test using OtterIO Client `oc`

`oc` provides a modern alternative to UNIX commands such as ls, cat, cp, mirror, diff etc. It supports filesystems and Amazon S3 compatible cloud storage services.

### Configure `oc`

```
oc alias set mynas http://gateway-ip:9000 access_key secret_key
```

### List buckets on nas

```
oc ls mynas
[2017-02-22 01:50:43 PST]     0B ferenginar/
[2017-02-26 21:43:51 PST]     0B my-bucket/
[2017-02-26 22:10:11 PST]     0B test-bucket1/
```

## Breaking changes

There will be a breaking change after the release version 'RELEASE.2020-06-22T03-12-50Z'.

### The file-based config settings are deprecated in NAS

The support for admin config APIs will be removed. This will include getters and setters like `mc admin config get` and `mc admin config`  and any other `mc admin config` options. The reason for this change is to avoid un-necessary reloads of the config from the disk. And to comply with the Environment variable based settings like other gateways.

### Migration guide

The users who have been using the older config approach should migrate to ENV settings by setting environment variables accordingly.

For example,

Consider the following webhook target config.

```
notify_webhook:1 endpoint=http://localhost:8080/ auth_token= queue_limit=0 queue_dir=/tmp/webhk client_cert= client_key=
```

The corresponding environment variable setting can be

```
export OTTERIO_NOTIFY_WEBHOOK_ENABLE_1=on
export OTTERIO_NOTIFY_WEBHOOK_ENDPOINT_1=http://localhost:8080/
export OTTERIO_NOTIFY_WEBHOOK_QUEUE_DIR_1=/tmp/webhk
```

> NOTE: Please check the docs for the corresponding ENV setting. Alternatively, We can obtain other ENVs in the form `mc admin config set alias/ <sub-sys> --env`

## Symlink support

NAS gateway implementation allows symlinks on regular files,

### Behavior

- For reads symlink resolves to file symlink points to.
- For deletes
  - Delete of symlink deletes the symlink but not the real file to which the symlink points.
  - Delete of actual file automatically makes symlink'ed file invisible, dangling symlinks won't be visible.

#### Caveats
- Disallows follow of directory symlinks to avoid security issues, and leaving them as is on namespace makes them very inconsistent.
- Dangling symlinks are ignored automatically.

*Directory symlinks is not and will not be supported as there are no safe ways to handle them.*

## Explore Further
- [`oc` command-line interface](https://github.com/soulteary/oc#readme)
- [`aws` command-line interface](https://github.com/soulteary/otterio/blob/main/docs/README.md)
- [OtterIO Go SDK](https://github.com/soulteary/otterio-sdk#readme)
