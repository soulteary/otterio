# OtterIO documentation

[Project README](../README.md) · [简体中文](zh_CN/README.md)

OtterIO is the server; [OC](https://github.com/soulteary/oc) is its command-line client, and [OtterIO SDK](https://github.com/soulteary/otterio-sdk) is the Go S3 client. Start with the current quick start and credential instructions before using a feature guide.

## Get a working local deployment

1. [Start the server](../README.md#quick-start) or follow the [Docker guide](docker/README.md).
2. [Connect OC and verify an upload and download](../README.md#verify). With split listeners, the S3 endpoint and management endpoint use different ports.
3. [Configure application credentials](multi-user/README.md) and connect the [Go SDK](https://github.com/soulteary/otterio-sdk).
4. Before upgrading, review [container credential migration](../README_DOCKER_SECURITY.md), [release materials](releases/README.md), and the security guides below.

## Deployment and operations

- [Docker](docker/README.md) and the [secure Compose profile](../README_DOCKER_SECURITY.md)
- [Erasure coding](erasure/README.md), [storage classes](erasure/storage-class/README.md), and [distributed deployment](distributed/README.md)
- [Configuration](config/README.md) and [TLS certificates](tls/README.md)
- [Gateway backends](gateway/README.md): only NAS and S3 remain in this fork
- [Metrics](metrics/README.md), [Prometheus](metrics/prometheus/README.md), and [health checks](metrics/healthcheck/README.md)
- [Logging](logging/README.md), [debugging](debugging/README.md), and [service limits](otterio-limits.md)
- [Platform deployment guides](orchestration/README.md) and [Kubernetes deployment considerations](orchestration/kubernetes/README.md)

Older platform examples and feature guides retain material from the Apache-licensed upstream baseline. Review image names, credentials, supported targets and topology before applying them to the current server. Links to `docs.min.io` or `github.com/minio` describe the upstream project. For OtterIO administration use OC: the management routes are under `/otterio/admin/v3`, while upstream `mc admin` uses a different path. OC's [configuration guide](https://github.com/soulteary/oc/blob/main/docs/configuration.md) covers separate endpoints and certificate trust.

## Identity, encryption and object features

- [Users and IAM](multi-user/README.md), [administration](multi-user/admin/README.md), and [temporary credentials (STS)](sts/README.md)
- [KMS configuration](kms/README.md) and [server-side encryption](security/README.md)
- [Bucket notifications](bucket/notifications/README.md) and [replication](bucket/replication/README.md)
- [Versioning](bucket/versioning/README.md), [lifecycle](bucket/lifecycle/README.md), [retention](bucket/retention/README.md), and [quota](bucket/quota/README.md)
- [Compression](compression/README.md) and [S3 Select](select/README.md)

Feature availability depends on the running OtterIO version, backend, configuration and permissions. The [project overview](../README.md#what-is-otterio) lists differences from upstream; test the operations needed by your deployment.

## Security and upgrades

- [Private vulnerability reporting and supported versions](../SECURITY.md)
- [Upstream advisory tracking](security/upstream-cve-backlog.md)
- [Internal storage hardening and upgrade considerations](security/sn-2026-002-storage-hardening.md)
- [LDAP DN normalization migration](security/ldap-dn-normalization-migration.md)
- [Container credentials and non-root storage permissions](../README_DOCKER_SECURITY.md)

## Development and release maintenance

- [Contributor's Guide](../CONTRIBUTING.md) and [project assessment for 2026-10-08 (Chinese)](development/project-status-20261008.md)
- [Build from source](../README.md#build-from-source)
- [Server CLI migration](cli-migration.md): server CLI framework changes, separate from the OC client
- [SDK and kits dependency migration](development/sdk-kits-migration-20261008.md)
- [Maintainer release guide](releasing.md) and [release materials](releases/README.md)
- [Contributor acknowledgment records](../ACKNOWLEDGMENTS.md)

The English guides contain topics without a Chinese translation. The [Chinese index](zh_CN/README.md) links available translations and labels English fallbacks.
