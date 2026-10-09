# Bucket Quota Configuration Quickstart Guide

![quota](https://raw.githubusercontent.com/soulteary/OtterIO/main/docs/bucket/quota/bucketquota.png)

Buckets can be configured to have one of two types of quota configuration - FIFO and Hard quota.

- `Hard` quota disallows writes to the bucket after configured quota limit is reached.
- `FIFO` quota automatically deletes oldest content until bucket usage falls within configured limit while permitting writes.

> NOTE: Bucket quotas are not supported under gateway or standalone single disk deployments.

## Prerequisites
- Install OtterIO - [OtterIO Quickstart Guide](https://github.com/soulteary/otterio#quick-start).
- [Use `oc` with OtterIO Server](https://github.com/soulteary/oc#readme)

## Set bucket quota configuration

### Set a hard quota of 1GB for a bucket `mybucket` on OtterIO object storage:

```sh
$ oc admin bucket quota myotterio/mybucket --hard 1gb
```

### Set FIFO quota of 5GB for a bucket "mybucket" on OtterIO to allow automatic deletion of older content to ensure bucket usage remains within 5GB

```sh
$ oc admin bucket quota myotterio/mybucket --fifo 5gb
```

### Verify the quota configured on `mybucket` on OtterIO

```sh
$ oc admin bucket quota myotterio/mybucket
```

### Clear bucket quota configuration for `mybucket` on OtterIO

```sh
$ oc admin bucket quota myotterio/mybucket --clear
```
