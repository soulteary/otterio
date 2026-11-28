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

Browser uploads are handled by the standalone [OC Web Console](https://github.com/soulteary/oc/blob/main/docs/console.md#upload-semantics),
which has its own upload limits and multipart transfer implementation. Its limits
also depend on the selected OC version, browser, proxy and backend configuration;
the server's S3 ceiling is not a verified 5 TiB browser-upload guarantee.
Use a multipart-capable S3 client for transfers beyond the Console's configured limit.

## Conditional object writes

Conditional writes apply to ordinary `PutObject` and `CompleteMultipartUpload`.
The destination check and final commit share the destination write lock. Read
the capabilities advertised by the running server; source support does not mean
an older release includes it.

| Condition | Meaning | Capability response header | Supported storage |
| --- | --- | --- | --- |
| `If-None-Match: *` | Create only if the current object does not exist | `X-Otterio-Conditional-Writes: v1` | Filesystem or single-pool erasure |
| `If-Match: "<etag>"` | Replace only if the current object's ETag matches | `X-Otterio-Conditional-Match: v1` | Filesystem or **local** single-pool erasure |
| `If-Match: *` | Replace only if a supported current object exists | `X-Otterio-Conditional-Match: v1` | Same as the ETag match condition |

Neither capability is advertised for gateways, multiple pools, write-back
cache, or uninitialized storage. Distributed erasure does not advertise the new
match capability; the existing create-only capability is unchanged. Supported
write-through caches delegate matching and commit to origin storage and
invalidate the cached entry after a successful conditional PUT.

### Matching the current object

Send the ETag returned by GET or HEAD as one strong, case-sensitive, opaque tag.
Quoted tags and the customary bare S3 ETag form are accepted, including multipart
ETags. Weak tags and tag lists are not implemented. The check always selects the
current object; explicit destination `versionId` and replication/import source
headers are not supported with `If-Match`. On versioned erasure storage, a
successful write creates a new version according to the bucket configuration;
an older live version does not satisfy the condition when the current version
is a delete marker.

The new match capability requires Signature V4, including presigned or streaming
PUT, with `if-match` in `SignedHeaders`. The principal must have both
`s3:PutObject` and `s3:GetObject` permission. Authorization is rechecked with the
current object's stored tags under the commit lock, before returning a match or
absence result. Signature V2, anonymous requests and unsigned conditions are
rejected. These requirements do not change the existing create-only contract.

Plain and multipart validators, including the validators exposed for SSE-S3,
SSE-KMS and SSE-C objects, are supported. The old object's encryption metadata
determines its validator; encryption headers on the new upload do not determine
the old ETag. Key-unsealing errors remain errors rather than being treated as a
mismatch. Compressed current objects, filesystem directory objects and missing
or fallback validators are rejected with `501`; they do not provide a supported
validator contract, even with `If-Match: *`.

| Result | Response |
| --- | --- |
| Existing object has a different ETag | `412 PreconditionFailed` |
| Current object is absent or a delete marker | `404 NoSuchKey` |
| Read/write authorization or condition signature is missing | `403 AccessDenied` |
| Empty, duplicate or malformed match header; both condition headers present | `400 InvalidRequest` |
| Unsupported condition form, operation, storage configuration or current-object shape | `501 NotImplemented` |

An ETag is not a unique version ID. Rewriting the same content, metadata-only
changes or an A-to-B-to-A sequence can retain or restore the same validator.
Applications requiring a unique state transition should include a generation
value in the object contents. Ordinary unconditional writes remain available;
all writers in an optimistic concurrency workflow must use the appropriate
condition. Preserve a conditional failure instead of retrying with an
unconditional overwrite.

### Multipart and unsupported operations

Attach the destination condition to **CompleteMultipartUpload**, not to
initiation or individual parts. A failed condition does not consume the upload;
the application can explicitly retry completion with a newly obtained ETag or
abort it. Slow multipart completion can have an HTTP `200` response containing
an XML `<Error>` after keepalive whitespace; clients must inspect the response
body as required by the existing completion protocol.

Destination `If-Match` and `If-None-Match` are explicitly rejected on copy,
multipart initiation/parts/abort, delete, object metadata mutations, browser
POST and Snowball extraction. The existing `x-amz-copy-source-if-match` checks
the copy source and remains a separate supported header. GET/HEAD conditions
are also unchanged.

SDK automatic multipart uploads must preserve the condition through completion.
OtterIO SDK v7.3.4 sends these conditions at initiation and discards
them during automatic completion; this server rejects that initiation with
`501` rather than accepting an unconditional result. Use an SDK containing the
[multipart-condition forwarding fix](https://github.com/soulteary/otterio-sdk/pull/13), direct conditional completion, or a single
PUT where applicable. An SDK containing that fix supports these conditions for
ordinary HTTP uploads and HTTP automatic multipart uploads; RDMA uploads return
`InvalidArgument` for `If-Match` or `If-None-Match` because the native interface
cannot forward them. Do not remove the condition to work around this error.

The create-only capability is newer than `RELEASE.2026-10-07T14-09-17Z`; the match
capability is newer than `RELEASE.2026-11-25T20-30-00Z`. Verify the selected release.
Sources: [create-only contract](../cmd/object-conditional-write.go),
[match contract](../cmd/object-conditional-match.go),
[unsupported operation guards](../cmd/object-write-condition-routes.go) and
[create-only release review](releases/2026-10-08-release-review.md).

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
