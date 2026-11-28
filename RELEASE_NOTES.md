# OtterIO conditional object operations

Since [RELEASE.2026-11-25T20-30-00Z](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-11-25T20-30-00Z),
this release adds atomic current-object matching for PUT and multipart completion
([#54](https://github.com/soulteary/otterio/pull/54)), plus conditional copy and
current-object deletion ([#55](https://github.com/soulteary/otterio/pull/55)).
Storage and configuration formats are unchanged.

## Conditions and supported storage

Each new capability has its own response header. Clients must check the running
server's advertised capability and preserve the condition on the final request.

| Operation | Supported destination/current-object condition | Capability response header |
| --- | --- | --- |
| `PutObject`, `CompleteMultipartUpload` | One strong `If-Match` ETag, or `If-Match: *` | `X-Otterio-Conditional-Match: v1` |
| `CopyObject` | One `If-None-Match: *` for the destination | `X-Otterio-Conditional-Copy: v1` |
| `DeleteObject` | One strong `If-Match` ETag; no wildcard | `X-Otterio-Conditional-Delete: v1` |

These new operations support filesystem storage and **local single-pool erasure**
storage. Gateways, distributed erasure and multiple pools are unsupported.
Matching PUT/completion supports the existing write-through cache path through
origin storage; write-back cache is unsupported. Conditional copy and deletion
reject **all object caches and bucket federation**. Conditional deletion also
rejects buckets with replication rules. A capability header does not override
these object- or bucket-specific restrictions.

The existing `X-Otterio-Conditional-Writes: v1` contract remains create-only PUT
and multipart completion with `If-None-Match: *`; it does not advertise any of
the three new capabilities. Ordinary unconditional operations and GET/HEAD
conditions retain their existing behavior. Destination conditions on unsupported
operations, including multipart initiation/parts/abort, bulk deletion, metadata
mutations, browser POST and Snowball extraction, are explicitly rejected.

### Matching writes

The server compares the current object and commits the replacement under the
same destination write lock. A different ETag returns `412 PreconditionFailed`;
an authorized missing object or current delete marker returns `404 NoSuchKey`.
`If-Match: *` requires a supported current object to exist. The request requires
SigV4 with `if-match` in `SignedHeaders`, including presigned and streaming PUT,
and both `s3:GetObject` and `s3:PutObject` permission. Authorization uses the
current object's stored tags and is repeated under the commit lock.

ETags are case-sensitive opaque values; one quoted or customary bare S3 ETag is
accepted, including multipart validators. Plain, SSE-S3, SSE-KMS and SSE-C
validators are supported; encrypted validators use the existing object's
metadata. Key-unsealing failures remain errors. Weak tags and tag lists are
unsupported. Compressed current objects, directories and absent or placeholder
metadata validators are unsupported even with `If-Match: *`. Explicit destination
versions and replication/import source headers are unsupported. With erasure
bucket versioning enabled, a successful write creates a new version; older
history does not satisfy a current delete marker. See the
[conditional-write contract](docs/otterio-limits.md#conditional-object-writes).

### Conditional copy and deletion

Conditional copy requires SigV4 with `if-none-match` in `SignedHeaders`. The
destination existence check and commit share the destination write lock. An
existing target, including an empty object, returns `412` and is preserved.
Source-copy conditions, source-read and destination-write authorization,
metadata/tag directives and encryption retain their ordinary copy semantics.
Destination `If-Match`, other `If-None-Match` values, multiple condition values,
explicit destination versions and replication/import headers are unsupported.

Conditional deletion requires a signed or presigned SigV4 request with
`if-match` in `SignedHeaders`, and both `s3:GetObject` and `s3:DeleteObject`
permission. Current-tag authorization and the ETag comparison are repeated
under the deletion commit lock. A changed ETag returns `412`; an authorized
missing current object or delete marker returns `404`. Neither outcome deletes
an object. The current-object validator restrictions above also apply.

Deletion rejects `If-Match: *`, `If-None-Match`, explicit `versionId` (including
`null`), replication/import, force-deletion and retention-bypass headers. With
bucket versioning enabled it creates a delete marker according to ordinary
current-key deletion semantics; it does not purge a historical version or bypass
retention. See [conditional copy](docs/otterio-limits.md#conditional-copy) and
[conditional deletion](docs/otterio-limits.md#conditional-object-deletion).

## Client compatibility and rename limits

For multipart uploads, put the condition on **CompleteMultipartUpload**, never
on initiation or individual parts. A failed completion condition leaves the
upload available for an explicit retry or abort. Clients must parse the completion
body: the existing keepalive protocol can return an XML error after HTTP `200`.
OtterIO SDK v7.3.4 automatic multipart uploads send the condition at initiation
and omit it at completion. Use an SDK containing the
[forwarding fix](https://github.com/soulteary/otterio-sdk/pull/13), direct
conditional completion, or a single PUT where appropriate. The fixed SDK rejects
conditional RDMA uploads because that interface cannot forward the condition.
Do not remove a condition to work around a rejection.

OC rename requires a compatible OC build that checks both new copy and deletion
capabilities and signs the corresponding conditions. The destination HEAD must
advertise `X-Otterio-Conditional-Copy: v1`; the source HEAD must advertise
`X-Otterio-Conditional-Delete: v1`. The older create-only header or a server
upgrade alone does not make an older client's rename safe. The compatible OC
path supports same-bucket renames up to 5 GiB: it conditionally copies to an
absent destination with `X-Amz-Copy-Source-If-Match`, then conditionally deletes
the source with that **source ETag**.

These are two separate commits, not an atomic two-key rename. If copying
succeeds but the source ETag changes, deletion fails and the copied destination
remains. A deletion failure or lost response requires checking both names before
retrying; there is no automatic destination rollback or unconditional retry.

An ETag is **not a unique mutation revision**. Same-content writes,
metadata-only changes or an A-to-B-to-A sequence can retain or restore it. These
conditions do not distinguish such changes. Applications requiring a distinct
generation for every state transition must maintain one in the object contents
and use conditional writes consistently across their writers.

## Upgrade and rollback

Retain the previous exact server and OC programs/images and back up configuration
before rollout. Stop affected writers during replacement, and finish or abort
owned multipart uploads. Validate the selected server/OC pair with the actual
storage backend, identity permissions, encryption and versioning configuration
before enabling the new operations. Confirm that intermediaries preserve both
the capability response headers and the signed request conditions.

Before rolling back, stop workflows that depend on the new match/copy/delete
contracts. Older releases do not acquire these guarantees from the client, and
clients must not fall back to unconditional mutations. Replacing programs does
not undo committed writes, delete markers or a partially completed rename;
inspect actual object state and use the verified backup/restore boundary when
needed. Restore the complete previous dependency lockfiles when rebuilding an
older source version. The [Console migration and rollback guide](docs/console-migration-preparation.md)
continues to apply.

The [release procedure](docs/releasing.md) requires acceptance of the exact
server/OC archives, immutable images, manifests and rendered release notes,
including the existing native/HTTPS/OIDC, permissions, S3/Range, readiness and
migration/rollback scope. Publish and promote only those accepted bytes.
