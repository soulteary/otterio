<div align="center">

[![OtterIO — S3-Compatible Object Storage](./.github/otter-io-banner.jpg)](https://github.com/soulteary/otterio)

# OtterIO

**S3-Compatible Object Storage** — _Store freely. Scale endlessly._

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8.svg?logo=go&logoColor=white)](./go.mod)
[![GitHub](https://img.shields.io/badge/GitHub-soulteary%2Fotterio-181717.svg?logo=github)](https://github.com/soulteary/otterio)

English · [简体中文](./README_zh_CN.md)

</div>

OtterIO is a high-performance, S3-compatible object storage server. It is suitable for building infrastructure for machine learning, analytics, backup, and general application data workloads.

This README covers running OtterIO on bare metal, Docker, and from source. For deeper topics (erasure coding, distributed mode, KMS, replication, etc.) see the [`docs/`](./docs) folder.

> [!IMPORTANT]
> OtterIO is an independent, community-maintained fork of the upstream Apache-licensed MinIO codebase. It is **not** affiliated with, endorsed by, or sponsored by MinIO, Inc. See [Trademark & Upstream Notice](#trademark--upstream-notice) and the [Security Advisory](#security-advisory) before deploying.

---

## What is OtterIO

OtterIO is a customized fork of the **last Apache License 2.0 release of MinIO** (≈ `RELEASE.2021-04-22T15-44-28Z`). It differs from upstream in the following ways:

- **HTTP layer** — request router built on [`gofiber/fiber/v3`](https://github.com/gofiber/fiber) instead of `gorilla/mux`.
- **Bucket notification targets** — only `elasticsearch`, `mysql`, `postgresql`, `redis`, and `webhook` are supported. The message-queue targets (Kafka, NATS, NATS Streaming, NSQ, AMQP, MQTT) have been removed.
- **Gateways** — only `nas` and `s3` remain. The `azure`, `gcs`, and `hdfs` gateways have been removed.
- **Toolchain** — requires Go `1.26` or newer (see [`go.mod`](./go.mod)).
- **Container images** — published at `soulteary/otterio` (Docker Hub) and `ghcr.io/soulteary/otterio` (GitHub Container Registry).

OtterIO continues to be distributed under the [Apache License, Version 2.0](./LICENSE). All original copyright notices are retained — see [`NOTICE`](./NOTICE).

---

## Quick Start

Run a single-node OtterIO instance with Docker:

```sh
docker run -p 9000:9000 -p 9001:9001 \
  -v /mnt/data:/data \
  soulteary/otterio:latest server /data --console-address ":9001"
```

Default root credentials are `otterioadmin:otterioadmin`. Once running, see [Verify](#verify) to connect via the web console or `mc`.

> [!NOTE]
> Standalone OtterIO servers are best suited for development and evaluation. Production deployments should run **distributed mode with Erasure Coding enabled** — at least **4 drives per server**. See [`docs/erasure/README.md`](./docs/erasure/README.md) and [`docs/distributed/README.md`](./docs/distributed/README.md).

---

## Installation

### Docker

OtterIO publishes container images to both Docker Hub and the GitHub Container Registry:

```sh
# Docker Hub
docker pull soulteary/otterio:latest

# GitHub Container Registry
docker pull ghcr.io/soulteary/otterio:latest
```

| Tag      | Description                                              |
| -------- | -------------------------------------------------------- |
| `latest` | Latest stable release.                                   |
| `edge`   | Bleeding-edge build from `main` — for testing only.      |

Run a standalone server with an ephemeral volume:

```sh
docker run -p 9000:9000 soulteary/otterio:latest server /data
```

For persistent storage, map a host directory to `/data`:

```sh
docker run -p 9000:9000 -v /mnt/data:/data soulteary/otterio:latest server /data
```

### macOS

#### Homebrew (recommended)

```sh
brew install otterio/stable/otterio
otterio server /data
```

If you previously installed OtterIO from a different tap, reinstall from the official tap:

```sh
brew uninstall otterio
brew install otterio/stable/otterio
```

#### Binary

Pre-built macOS binaries are published on [GitHub Releases](https://github.com/soulteary/otterio/releases). Download the appropriate archive, then:

```sh
chmod +x otterio
./otterio server /data
```

### Linux

Pre-built Linux binaries are published on [GitHub Releases](https://github.com/soulteary/otterio/releases). Download the asset that matches your architecture and run it as `otterio`:

```sh
chmod +x otterio
./otterio server /data
```

The release pipeline ([`.goreleaser.yml`](./.goreleaser.yml)) currently produces Linux binaries for the following architectures:

| Architecture                | `goarch`   |
| --------------------------- | ---------- |
| 64-bit Intel/AMD            | `amd64`    |
| 64-bit ARM                  | `arm64`    |
| 64-bit PowerPC LE           | `ppc64le`  |

`.deb` and `.rpm` packages are also produced for the supported architectures.

### Windows

Pre-built Windows binaries (`amd64`) are published on [GitHub Releases](https://github.com/soulteary/otterio/releases). After downloading `otterio.exe`, run it from the directory where it lives, or add that directory to the system `PATH`:

```powershell
otterio.exe server D:\
```

### FreeBSD

OtterIO does not currently provide an official FreeBSD package. Build from source on FreeBSD using the [Build from Source](#build-from-source) instructions below.

### Build from Source

Source builds are intended for developers and advanced users. Make sure you have a working Go toolchain (Go 1.26 or newer — see [How to install Go](https://go.dev/doc/install)).

```sh
git clone https://github.com/soulteary/otterio.git
cd otterio
make build
./otterio server /data
```

> [!WARNING]
> We strongly recommend **against** running compiled-from-source binaries in production. Use a tagged release for production deployments.

---

## Configuration

### Run S3 and Web Console on separate ports

By default the web console and the S3 API share the listener bound to `--address`. OtterIO can serve the web UI and admin API on a dedicated port so that reverse proxies, firewalls, and network policies can govern S3 traffic and console traffic independently.

Enable the dedicated console listener via the `--console-address` flag or the `OTTERIO_BROWSER_ADDRESS` environment variable:

```sh
# CLI flag
otterio server --address ":9000" --console-address ":9001" /data

# Environment variable (equivalent)
export OTTERIO_BROWSER_ADDRESS=":9001"
otterio server --address ":9000" /data
```

When the dedicated console listener is enabled:

- `:9000` only serves the S3 API, STS, health, and metrics. Browser requests are no longer redirected to the web UI.
- `:9001` serves the web console (`/otterio/`) and the admin API (`/otterio/admin/v3/*`).
- The console port must differ from the S3 port; otherwise startup fails fast.
- `Ctrl+C` / `SIGTERM` shuts down both listeners gracefully.

> [!NOTE]
> The admin API (used by `mc admin ...`) is served from the console port in this mode. Configure your `mc` alias to point at the console URL when issuing admin commands. Regular S3 operations (`mc cp`, `mc ls`, etc.) continue to use the S3 port.

If `--console-address` is not provided, both surfaces share a single port (the original behaviour).

### Dedicated TLS certificates for the console listener

When you split listeners, you can also point the console at its own TLS keypair so the S3 API and the web console can use different certificates (e.g. an internal CA-signed cert for `:9000` and a public cert for `:9001`). Use `--console-certs-dir` or `OTTERIO_BROWSER_CERTS_DIR`:

```sh
otterio server \
  --address ":9000" \
  --console-address ":9001" \
  --certs-dir /etc/otterio/certs/s3 \
  --console-certs-dir /etc/otterio/certs/console \
  /data
```

The directory pointed to by `--console-certs-dir` must contain `public.crt` and `private.key`, the same layout used by `--certs-dir`. Notes:

- `--console-certs-dir` requires `--console-address`; otherwise startup fails fast.
- If `--console-certs-dir` is not set, the console listener reuses the certificates loaded from `--certs-dir` (the legacy behaviour).
- The S3 listener always uses `--certs-dir`; only the console listener honours `--console-certs-dir`.
- Both keypairs are watched and hot-reloaded by the same certificate manager used for `--certs-dir`.

### Firewall

By default OtterIO listens on port `9000` for S3 traffic (and `9001` if the console listener is split out). Some platforms block these ports until you explicitly open them.

<details>
<summary><strong>ufw</strong> (Debian / Ubuntu)</summary>

```sh
ufw allow 9000

# Range
ufw allow 9000:9010/tcp
```

</details>

<details>
<summary><strong>firewall-cmd</strong> (CentOS / RHEL)</summary>

```sh
firewall-cmd --get-active-zones
firewall-cmd --zone=public --add-port=9000/tcp --permanent
firewall-cmd --reload
```

`--permanent` makes the rule persist across reboots and reloads.

</details>

<details>
<summary><strong>iptables</strong> (RHEL, CentOS, etc.)</summary>

```sh
iptables -A INPUT -p tcp --dport 9000 -j ACCEPT
service iptables restart

# Range
iptables -A INPUT -p tcp --dport 9000:9010 -j ACCEPT
service iptables restart
```

</details>

### Pre-existing data

When deployed on a single drive, OtterIO server lets clients access any pre-existing data in the data directory. For example, if OtterIO is started with `otterio server /mnt/data`, any pre-existing data in `/mnt/data` is accessible to clients. The same applies to all gateway backends.

---

## Verify

Once OtterIO is running, the deployment uses default root credentials `otterioadmin:otterioadmin` (override via `OTTERIO_ROOT_USER` / `OTTERIO_ROOT_PASSWORD` environment variables in production).

### Web console

Point a browser at <http://127.0.0.1:9000> (or the console port if you split listeners). Log in with the root credentials to create buckets, upload objects, and browse contents.

### `mc` client

`mc` is a modern command-line client that speaks S3 and local filesystem URIs (similar to `ls`, `cp`, `mirror`, `diff`, etc.). Configure an alias against your OtterIO endpoint:

```sh
mc alias set local http://127.0.0.1:9000 otterioadmin otterioadmin
mc mb local/test-bucket
mc cp ./somefile local/test-bucket/
mc ls local/test-bucket/
```

OtterIO is wire-compatible with the AWS S3 API, so `aws-cli`, `s3cmd`, and the various AWS SDKs all work out of the box — point them at your OtterIO endpoint with the root credentials (or an IAM-issued access key).

---

## Further Reading

### Project documentation (this repository)

- [Erasure Coding](./docs/erasure/README.md)
- [Distributed mode](./docs/distributed/README.md)
- [Multi-user / IAM](./docs/multi-user/README.md)
- [STS — temporary credentials](./docs/sts/README.md)
- [TLS](./docs/tls/README.md)
- [KMS](./docs/kms/README.md)
- [Bucket notifications](./docs/bucket/notifications/README.md)
- [Bucket replication](./docs/bucket/replication/README.md)
- [Bucket lifecycle / retention / versioning / quota](./docs/bucket)
- [Metrics & Prometheus](./docs/metrics/README.md)
- [Logging](./docs/logging/README.md)
- [Docker](./docs/docker/README.md) · [Orchestration](./docs/orchestration/README.md)
- [Security advisories backlog](./docs/security/upstream-cve-backlog.md)
- [LDAP DN normalisation migration](./docs/security/ldap-dn-normalization-migration.md)
- [Limits](./docs/otterio-limits.md)

### Upstream references (third-party)

The links below point to the **original upstream MinIO** project's documentation. They remain useful as background reading on S3-compatible workflows, but they describe upstream MinIO behaviour and are **not** maintained by OtterIO:

- [Erasure Code Quickstart Guide](https://docs.min.io/docs/minio-erasure-code-quickstart-guide) (upstream)
- [`mc` Client Quickstart](https://docs.min.io/docs/minio-client-quickstart-guide) (upstream)
- [`aws-cli` with MinIO](https://docs.min.io/docs/aws-cli-with-minio) (upstream)
- [`s3cmd` with MinIO](https://docs.min.io/docs/s3cmd-with-minio) (upstream)
- [Go SDK Quickstart](https://docs.min.io/docs/golang-client-quickstart-guide) (upstream)

---

## Contributing

Contributions are welcome via the project repository at <https://github.com/soulteary/otterio>. For coding conventions inherited from the upstream baseline, see the original [Contributor's Guide](https://github.com/minio/minio/blob/master/CONTRIBUTING.md).

---

## Security Advisory

<details>
<summary><strong>Click to expand — please read before deploying.</strong></summary>

Because OtterIO is forked from the **last Apache 2.0 release of MinIO (≈ `RELEASE.2021-04-22T15-44-28Z`)**, every CVE / GHSA published against upstream `minio/minio` after that date must be evaluated and back-ported separately. OtterIO does **not** automatically inherit those fixes.

**Backlog status (as of 2026-06): 14 closed, 2 not-applicable, 0 open.** Every advisory currently triaged against the post-2021-04 baseline has been resolved on `main` — see [`docs/security/upstream-cve-backlog.md`](./docs/security/upstream-cve-backlog.md) for the per-item table with the OtterIO codepath, the upstream reference, and the regression tests pinning each fix. New upstream advisories will be added with status `Pending` and tracked from there.

**Operators upgrading from a previous OtterIO build that used LDAP** should consult [`docs/security/ldap-dn-normalization-migration.md`](./docs/security/ldap-dn-normalization-migration.md) before rolling out: the new release canonicalises every LDAP DN before it touches the IAM policy map, which is a one-shot breaking change for deployments that happened to rely on case-only DN differences.

**Before adopting OtterIO**, please evaluate fitness against your own deployment context — workload profile, capacity / throughput targets, compliance and data-residency requirements, supported-version policy, and your organisation's change-management expectations. As with any infrastructure component, we recommend a staged rollout (lab → staging → production) and validating the relevant codepaths against your own regression suite. See [`SECURITY.md`](./SECURITY.md) for the disclosure policy and the supported-versions matrix.

### Hardening highlights

Relative to the 2021-04-22 Apache-licensed MinIO baseline OtterIO has back-ported and (where applicable) hardened the following upstream advisories — full per-item context, codepaths, and regression tests live in [`docs/security/upstream-cve-backlog.md`](./docs/security/upstream-cve-backlog.md):

- **SSE metadata injection** (GHSA-3rh2-v3gr-35p9 class) — reserved-prefix metadata is rejected at both the router edge and `extractMetadata`.
- **Precondition GET / HEAD metadata disclosure** (GHSA-95fr-cm4m-q5p9 / CVE-2024-36107) — `s3:ExistingObjectTag/*` and `s3:RequestObjectTag/*` are now first-class condition keys, and the precondition path re-runs auth before writing `ETag` / `Last-Modified` so a tag-gated deny no longer leaks object state.
- **SSE-KMS context binding** (multiple post-2022 CVEs) — bucket / object AAD is reconstructed from the runtime `(bucket, object)` on every seal and unseal, hostile or tampered `MetaContext` blobs are rejected with a dedicated 403 sentinel before the KMS is ever called, and the seven legacy `ErrNotImplemented` PUT-handler stubs have been replaced with a single `enforceSSEKMSRequest` security gate covering single-PUT, multipart, copy and post-policy paths.
- **Service-account privilege escalation** — `GHSA-jjjj-jwhf-8rgr` (own-account create-SA bypass), the RELEASE.2025-10-15 sub-policy escalation, and `GHSA-xx8w-mq23-29g4 / CVE-2024-24747` (admin:UpdateServiceAccount) are all closed; sub-policies must be a subset of the caller's capability.
- **`AddUser` PolicyName privilege escalation** (CVE-2021-43858) — defence-in-depth at both the handler (HTTP 400) and IAM-layer (silent strip).
- **LDAP DN normalisation family** (2022–2024 advisories) — RFC 4514 + RFC 4518 canonicalisation at every DN egress and at the IAM boundary, with a one-shot persisted-data migration; see the migration note linked above.
- **Bucket / IAM policy parsing** — `Principal` / `Resource` / `Action` unmarshal panics fixed, attacker-controlled input no longer reflected into error messages, fuzz corpus added.
- **SigV4 signed-headers and chunked-upload hardening** — empty `X-Amz-Content-Sha256` is treated as "not provided" instead of coercing the canonicaliser, `SignedHeaders` is case-folded, and aws-chunked uploads must sign `x-amz-decoded-content-length`.
- **Replication-header IAM gate** — fork-private `X-Otterio-Source-*` headers now require the `s3:ReplicateObject` action via `enforceSourceHeaderIAM`, closing a forge path that could rewrite object mtime / ETag or force delete-markers under ordinary `s3:PutObject` / `s3:DeleteObject` permissions.
- **CVE-2023-28432 bootstrap info disclosure** — `VerifyHandler` is now gated on the same inter-node JWT validator used by peer-rest / storage-rest / lock-rest; only `HealthHandler` remains anonymous.
- **Multi-value `Host` header smuggle (fork-introduced concern)** — audited and pinned: both fasthttp and net/http collapse or reject duplicate Host headers before any handler runs, and SigV4 reads only the scalar `r.Host`, so the two-header smuggle is not constructible.

Two further upstream advisories (CVE-2021-41137 regular-user policy bypass and GHSA-cwq8-g58r-32hg `ImportIAM` privilege escalation) are **not applicable** to the 2021-04 baseline — see the backlog for the audit trail and the negative-pinning regression tests.

</details>

---

## Trademark & Upstream Notice

<details>
<summary><strong>Click to expand</strong></summary>

OtterIO is an independent, community-maintained fork of the upstream Apache-licensed MinIO codebase. This project is **not** affiliated with, endorsed by, or sponsored by MinIO, Inc.

"MinIO" is a trademark of MinIO, Inc., used here solely to identify the upstream project from which this fork is derived. No trademark rights are granted by the Apache License 2.0 (see Section 6 of the license).

OtterIO is based on the **last Apache License 2.0 release of MinIO**, prior to MinIO's relicensing to the GNU AGPLv3, and remains distributed under the [Apache License, Version 2.0](./LICENSE). The original copyright notices of MinIO, Inc. and all third-party subcomponents are retained — see [`NOTICE`](./NOTICE).

OtterIO publishes its own container images at `soulteary/otterio` (Docker Hub) and `ghcr.io/soulteary/otterio` (GitHub Container Registry). Other links in this guide pointing to `docs.min.io`, `dl.min.io`, etc. still refer to the **original upstream project**, not to OtterIO. Build OtterIO from source (see [Build from Source](#build-from-source)) to use the OtterIO customizations.

Project home: <https://github.com/soulteary/otterio>

</details>

---

## License

<div align="center">

<img src="./.github/otter-io-logo.jpg" alt="OtterIO logo" width="220" />

</div>

OtterIO is governed by the Apache License, Version 2.0, found at [LICENSE](./LICENSE). Attribution and third-party notices are listed in [NOTICE](./NOTICE).

"MinIO" is a trademark of MinIO, Inc. OtterIO is not affiliated with, endorsed by, or sponsored by MinIO, Inc.
