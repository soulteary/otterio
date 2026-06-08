# KMS Guide

OtterIO uses a key-management-system (KMS) to support SSE-S3. If a client requests SSE-S3, or auto-encryption is enabled, the OtterIO server encrypts each object with an unique object key which is protected by a master key managed by the KMS.

## Quick Start

OtterIO supports multiple KMS implementations via our [KES](https://github.com/minio/kes#kes) project. We run a KES instance at `https://play.min.io:7373` for you to experiment and quickly get started. To run OtterIO with a KMS just fetch the root identity, set the following environment variables and then start your OtterIO server. If you haven't installed OtterIO, yet, then follow the OtterIO [install instructions](https://docs.min.io/docs/minio-quickstart-guide) first.

#### 1. Fetch the root identity
As the initial step, fetch the private key and certificate of the root identity:

```sh
curl -sSL --tlsv1.2 \
     -O 'https://raw.githubusercontent.com/otterio/kes/master/root.key' \
     -O 'https://raw.githubusercontent.com/otterio/kes/master/root.cert'
```

#### 2. Set the OtterIO-KES configuration

```sh
export OTTERIO_KMS_KES_ENDPOINT=https://play.min.io:7373
export OTTERIO_KMS_KES_KEY_FILE=root.key
export OTTERIO_KMS_KES_CERT_FILE=root.cert
export OTTERIO_KMS_KES_KEY_NAME=my-otterio-key
```

#### 3. Start the OtterIO Server

```sh
export OTTERIO_ROOT_USER=otterio
export OTTERIO_ROOT_PASSWORD=otterio123
otterio server ~/export
```

> The KES instance at `https://play.min.io:7373` is meant to experiment and provides a way to get started quickly.
> Note that anyone can access or delete master keys at `https://play.min.io:7373`. You should run your own KES
> instance in production.

## Configuration Guides

A typical OtterIO deployment that uses a KMS for SSE-S3 looks like this:
```
    ┌────────────┐
    │ ┌──────────┴─┬─────╮          ┌────────────┐
    └─┤ ┌──────────┴─┬───┴──────────┤ ┌──────────┴─┬─────────────────╮
      └─┤ ┌──────────┴─┬─────┬──────┴─┤ KES Server ├─────────────────┤
        └─┤   OtterIO    ├─────╯        └────────────┘            ┌────┴────┐
          └────────────┘                                        │   KMS   │
                                                                └─────────┘
```

In a given setup, there are `n` OtterIO instances talking to `m` KES servers but only `1` central KMS. The most simple setup consists of `1` OtterIO server or cluster talking to `1` KMS via `1` KES server.

The main difference between various OtterIO-KMS deployments is the KMS implementation. The following table helps you select the right option for your use case:

| KMS                                                                                          | Purpose                                                           |
|:---------------------------------------------------------------------------------------------|:------------------------------------------------------------------|
| [Hashicorp Vault](https://github.com/minio/kes/wiki/Hashicorp-Vault-Keystore)                | Local KMS. OtterIO and KMS on-prem (**Recommended**)                |
| [AWS-KMS + SecretsManager](https://github.com/minio/kes/wiki/AWS-SecretsManager)             | Cloud KMS. OtterIO in combination with a managed KMS installation   |
| [Gemalto KeySecure /Thales CipherTrust](https://github.com/minio/kes/wiki/Gemalto-KeySecure) | Local KMS. OtterIO and KMS On-Premises.                             |
| [Google Cloud Platform SecretManager](https://github.com/minio/kes/wiki/GCP-SecretManager)   | Cloud KMS. OtterIO in combination with a managed KMS installation   |
| [FS](https://github.com/minio/kes/wiki/Filesystem-Keystore)                                  | Local testing or development (**Not recommended for production**) |


The OtterIO-KES configuration is always the same - regardless of the underlying KMS implementation. Checkout the OtterIO-KES [configuration example](https://github.com/minio/kes/wiki/MinIO-Object-Storage).

### Further references

- [Run OtterIO with TLS / HTTPS](https://docs.min.io/docs/how-to-secure-access-to-minio-server-with-tls.html)
- [Tweak the KES server configuration](https://github.com/minio/kes/wiki/Configuration)
- [Run a load balancer infront of KES](https://github.com/minio/kes/wiki/TLS-Proxy)
- [Understand the KES server concepts](https://github.com/minio/kes/wiki/Concepts)

## Auto Encryption
Auto-Encryption is useful when OtterIO administrator wants to ensure that all data stored on OtterIO is encrypted at rest.

### Using `mc encrypt` (recommended)
OtterIO automatically encrypts all objects on buckets if KMS is successfully configured and bucket encryption configuration is enabled for each bucket as shown below:
```
mc encrypt set sse-s3 myotterio/bucket/
```

Verify if OtterIO has `sse-s3` enabled
```
mc encrypt info myotterio/bucket/
Auto encryption 'sse-s3' is enabled
```

### Using environment (deprecated)
> NOTE: The following ENV might be removed in future, you are advised to move to the previously recommended approach using `mc encrypt`. S3 gateway supports encryption at gateway layer which may  be dropped in favor of simplicity at a later time. It is advised that S3 gateway users migrate to OtterIO server mode or enable encryption at REST at the backend.

OtterIO automatically encrypts all objects on buckets if KMS is successfully configured and following ENV is enabled:
```
export OTTERIO_KMS_AUTO_ENCRYPTION=on
```

### Verify auto-encryption
> Note that auto-encryption only affects requests without S3 encryption headers. So, if a S3 client sends
> e.g. SSE-C headers, OtterIO will encrypt the object with the key sent by the client and won't reach out to
> the configured KMS.

To verify auto-encryption, use the following `mc` command:

```
mc cp test.file myotterio/bucket/
test.file:              5 B / 5 B  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓  100.00% 337 B/s 0s
```

```
mc stat myotterio/bucket/test.file
Name      : test.file
...
Encrypted :
  X-Amz-Server-Side-Encryption: AES256
```

## Explore Further

- [Use `mc` with OtterIO Server](https://docs.min.io/docs/minio-client-quickstart-guide)
- [Use `aws-cli` with OtterIO Server](https://docs.min.io/docs/aws-cli-with-minio)
- [Use `s3cmd` with OtterIO Server](https://docs.min.io/docs/s3cmd-with-minio)
- [Use `otterio-go` SDK with OtterIO Server](https://docs.min.io/docs/golang-client-quickstart-guide)
- [The OtterIO documentation website](https://docs.min.io)
