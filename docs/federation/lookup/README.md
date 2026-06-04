*Federation feature is deprecated and should be avoided for future deployments*

# Federation Quickstart Guide
This document explains how to configure OtterIO with `Bucket lookup from DNS` style federation.

## Get started

### 1. Prerequisites
Install OtterIO - [OtterIO Quickstart Guide](https://docs.min.io/docs/minio-quickstart-guide).

### 2. Run OtterIO in federated mode
Bucket lookup from DNS federation requires two dependencies

- etcd (for bucket DNS service records)
- CoreDNS (for DNS management based on populated bucket DNS service records, optional)

## Architecture

![bucket-lookup](https://github.com/minio/minio/blob/master/docs/federation/lookup/bucket-lookup.png?raw=true)

### Environment variables

#### OTTERIO_ETCD_ENDPOINTS

This is comma separated list of etcd servers that you want to use as the OtterIO federation back-end. This should
be same across the federated deployment, i.e. all the OtterIO instances within a federated deployment should use same
etcd back-end.

#### OTTERIO_DOMAIN

This is the top level domain name used for the federated setup. This domain name should ideally resolve to a load-balancer
running in front of all the federated OtterIO instances. The domain name is used to create sub domain entries to etcd. For
example, if the domain is set to `domain.com`, the buckets `bucket1`, `bucket2` will be accessible as `bucket1.domain.com`
and `bucket2.domain.com`.

#### OTTERIO_PUBLIC_IPS

This is comma separated list of IP addresses to which buckets created on this OtterIO instance will resolve to. For example,
a bucket `bucket1` created on current OtterIO instance will be accessible as `bucket1.domain.com`, and the DNS entry for
`bucket1.domain.com` will point to IP address set in `OTTERIO_PUBLIC_IPS`.

*Note*

- This field is mandatory for standalone and erasure code OtterIO server deployments, to enable federated mode.
- This field is optional for distributed deployments. If you don't set this field in a federated setup, we use the IP addresses of
hosts passed to the OtterIO server startup and use them for DNS entries.

### Run Multiple Clusters

> cluster1

```sh
export OTTERIO_ETCD_ENDPOINTS="http://remote-etcd1:2379,http://remote-etcd2:4001"
export OTTERIO_DOMAIN=domain.com
export OTTERIO_PUBLIC_IPS=44.35.2.1,44.35.2.2,44.35.2.3,44.35.2.4
otterio server http://rack{1...4}.host{1...4}.domain.com/mnt/export{1...32}
```

> cluster2

```sh
export OTTERIO_ETCD_ENDPOINTS="http://remote-etcd1:2379,http://remote-etcd2:4001"
export OTTERIO_DOMAIN=domain.com
export OTTERIO_PUBLIC_IPS=44.35.1.1,44.35.1.2,44.35.1.3,44.35.1.4
otterio server http://rack{5...8}.host{5...8}.domain.com/mnt/export{1...32}
```

In this configuration you can see `OTTERIO_ETCD_ENDPOINTS` points to the etcd backend which manages OtterIO's
`config.json` and bucket DNS SRV records. `OTTERIO_DOMAIN` indicates the domain suffix for the bucket which
will be used to resolve bucket through DNS. For example if you have a bucket such as `mybucket`, the
client can use now `mybucket.domain.com` to directly resolve itself to the right cluster. `OTTERIO_PUBLIC_IPS`
points to the public IP address where each cluster might be accessible, this is unique for each cluster.

NOTE: `mybucket` only exists on one cluster either `cluster1` or `cluster2` this is random and
is decided by how `domain.com` gets resolved, if there is a round-robin DNS on `domain.com` then
it is randomized which cluster might provision the bucket.

### 3. Upgrading to `etcdv3` API

Users running OtterIO federation from release `RELEASE.2018-06-09T03-43-35Z` to `RELEASE.2018-07-10T01-42-11Z`, should migrate the existing bucket data on etcd server to `etcdv3` API, and update CoreDNS version to `1.2.0` before updating their OtterIO server to the latest version.

Here is some background on why this is needed - OtterIO server release `RELEASE.2018-06-09T03-43-35Z` to `RELEASE.2018-07-10T01-42-11Z` used etcdv2 API to store bucket data to etcd server. This was due to `etcdv3` support not available for CoreDNS server. So, even if OtterIO used `etcdv3` API to store bucket data, CoreDNS wouldn't be able to read and serve it as DNS records.

Now that CoreDNS [supports etcdv3](https://coredns.io/2018/07/11/coredns-1.2.0-release/), OtterIO server uses `etcdv3` API to store bucket data to etcd server. As `etcdv2` and `etcdv3` APIs are not compatible, data stored using `etcdv2` API is not visible to the `etcdv3` API. So, bucket data stored by previous OtterIO version will not be visible to current OtterIO version, until a migration is done.

CoreOS team has documented the steps required to migrate existing data from `etcdv2` to `etcdv3` in [this blog post](https://coreos.com/blog/migrating-applications-etcd-v3.html). Please refer the post and migrate etcd data to `etcdv3` API.

### 4. Test your setup

To test this setup, access the OtterIO server via browser or [`mc`](https://docs.min.io/docs/minio-client-quickstart-guide). You’ll see the uploaded files are accessible from the all the OtterIO endpoints.

# Explore Further

- [Use `mc` with OtterIO Server](https://docs.min.io/docs/minio-client-quickstart-guide)
- [Use `aws-cli` with OtterIO Server](https://docs.min.io/docs/aws-cli-with-minio)
- [Use `s3cmd` with OtterIO Server](https://docs.min.io/docs/s3cmd-with-minio)
- [Use `otterio-go` SDK with OtterIO Server](https://docs.min.io/docs/golang-client-quickstart-guide)
- [The OtterIO documentation website](https://docs.min.io)
