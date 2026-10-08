# OtterIO server limits and S3 compatibility

[简体中文](zh_CN/otterio-limits.md) · [Documentation index](README.md)

These limits describe this repository's server implementation. A gateway's
upstream service, a client, a reverse proxy, available storage and configured
quotas can impose lower limits. Check the source and release used by your
deployment; S3 compatibility does not mean that every AWS S3 feature is present.

## Erasure topology and quorum

An erasure set contains **4–16 drives**. A single local drive uses the filesystem
backend without erasure redundancy. Larger deployments form multiple sets;
endpoint counts must divide into supported set sizes and satisfy the endpoint
layout checks. The drive minimum applies to a set, not to every server. For
example, four servers with one drive each can form one four-drive set.

For an object's set, let `N` be the total shards, `P` the parity shards and
`D = N - P` the data shards. Object reads require `D` valid shards. Object writes
require `D`, or `D + 1` when data and parity counts are equal. Metadata operations
also require their own quorum; drive counts alone do not guarantee availability.
Node-failure tolerance depends on the placement of each set's shards across nodes.

Default STANDARD parity is `EC:2` for 4–5 drives, `EC:3` for 6–7 drives and `EC:4`
for 8–16 drives. Changing a storage class changes parity for new objects, not the
layout of existing objects. See [storage classes](erasure/storage-class/README.md),
[distributed deployment](distributed/README.md) and the
[sizing examples](distributed/SIZING.md).

Source: [set sizes and layout](../cmd/endpoint-ellipses.go),
[object quorum](../cmd/erasure-metadata.go),
[storage-class defaults](../cmd/config/storageclass/storage-class.go).

## S3 request limits

| Item | Server limit |
| --- | --- |
| Object size and single PUT size | 5 TiB |
| Minimum object size | 0 B |
| Parts per multipart upload | 10,000 |
| Multipart part size | 5 MiB–5 GiB; the last part may be smaller, including 0 B |
| Entries per object/version listing response | 4,500 |
| Parts per list-parts response | 10,000 |
| Default page size for list-multipart-uploads | 10,000 |

TiB, GiB and MiB are binary units. Clients may request smaller pages; always
follow truncation markers or continuation tokens instead of assuming one page
contains every result. The upload-list value is the default when `max-uploads`
is omitted; explicit pagination and backend behavior may differ. These are
implementation values, not measured capacity
or throughput guarantees. Bucket and object counts remain constrained by storage,
metadata workload and operational resources.

Source: [object and part size constants](../cmd/utils.go),
[response limits](../cmd/api-response.go), [metacache block size](../cmd/metacache.go)
and [request argument parsing](../cmd/api-resources.go).

The web console uploads a file through one XMLHttpRequest rather than a multipart
S3 upload. Its practical limit depends on the browser, proxy timeouts and server
configuration; the S3 ceiling is not a verified 5 TiB browser-upload guarantee.
Use a multipart-capable S3 client for large transfers. See
[console upload code](../browser/app/js/uploads/actions.js).

## Conditional object writes

Current `main` supports atomic `If-None-Match: *` for PUT and multipart completion
on filesystem and single-pool erasure storage, subject to the running backend.
Supported configurations advertise `X-Otterio-Conditional-Writes: v1`. Existing
objects, including empty objects, return HTTP 412. Gateways, multiple pools,
write-back cache and uninitialized storage do not advertise this capability.
Unsupported conditions or backends return HTTP 501; `If-Match` is not supported
for these write operations. Preserve a conditional failure instead of retrying
with an unconditional overwrite.

This capability is newer than `RELEASE.2026-10-07T14-09-17Z`; verify that your
selected release includes it. Source: [conditional-write contract](../cmd/object-conditional-write.go)
and [release review](releases/2026-10-08-release-review.md).

## Partial or unavailable S3 features

- **Bucket/Object ACLs:** compatibility handlers accept private access and return
  a dummy owner `FULL_CONTROL` ACL. They do not implement AWS ACL grants; use
  [IAM and bucket policies](multi-user/README.md) for authorization. See
  [ACL handlers](../cmd/acl-handlers.go).
- **Per-bucket CORS configuration:** not implemented. The compatibility GET
  returns `NoSuchCORSConfiguration`; cross-origin access is governed by server
  configuration `api cors_allow_origin` or `OTTERIO_API_CORS_ALLOW_ORIGIN`
  (default `*`). See [configuration](config/README.md),
  [CORS middleware](../cmd/fiber_router.go) and [API configuration](../cmd/config/api/api.go).
- **BucketWebsite, BucketAnalytics, BucketMetrics, BucketLogging and
  BucketRequestPayment:** full AWS behavior is not implemented. Some routes
  return compatibility responses, which do not establish feature support. Use
  a web server for website hosting, [Prometheus](metrics/prometheus/README.md)
  for metrics and [audit logging](logging/README.md) for audit events.
  [Bucket notifications](bucket/notifications/README.md) serve event delivery;
  they do not reproduce S3 access-log semantics.
- **ObjectTorrent:** not implemented.
- **Lifecycle tier transition and restore:** supported only on a local,
  single-pool erasure server; see the [lifecycle guide](bucket/lifecycle/README.md)
  for target requirements and restore restrictions.

The [S3 route table](../cmd/fiber_api_router.go) and
[compatibility handlers](../cmd/dummy-handlers.go) are the source of truth for
these endpoints. Report missing functionality in
[OtterIO issues](https://github.com/soulteary/otterio/issues).

## Object names

Filesystem and NAS deployments inherit restrictions from their host filesystem.
On Windows, characters such as `^*|\\/&\";` may be unavailable in filenames; this
is not an exhaustive cross-platform list. Validate names against the backend
and clients used by your deployment.
