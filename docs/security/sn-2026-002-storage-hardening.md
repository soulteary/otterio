# SN-2026-002-related internal storage hardening

This is an OtterIO-specific remediation of the inherited defect classes discussed
in [SILO SN-2026-002](https://silo.pgsty.com/about/security-advisories/).
It is not an assertion that SILO's twelve findings or CVE-2026-42600's exact
ReadMultiple exploit apply unchanged to OtterIO. This tree has neither the
ReadMultiple endpoint nor the newer grid transport. The implementation here is
independent and retains OtterIO's Apache-2.0 licensing.

## Trust boundary and changes

Internal storage REST requests authenticate with the cluster's node/root JWT and
are registered for distributed erasure deployments. An ordinary anonymous S3
request or a limited S3 user's key is not sufficient. A compromised peer must
nevertheless not be able to escape the storage root or crash a node with malformed
metadata. Single-node deployment does not replace fixing the shared storage layer.

The storage implementation now validates raw volume/object paths before joining
or cleaning them, including request-body `FileInfo.Name` and `DataDir`, scanner
cache paths, directory-walk base paths, and both sides of renames. Dot/dot-dot
components, absolute paths, NULs, backslashes and drive-prefixed components are
rejected. The deletion helper uses path-segment containment instead of a string
prefix. Batch volume creation and version deletion validate the entire batch
before the first mutation. Nested system volumes, directory-healing volumes,
empty root listings and the existing empty-path Delete no-op remain supported.

Erasure block sizes must be positive and at most the legacy 10 MiB block size;
data/parity counts and part sizes are checked before arithmetic or persistence.
The same checks apply when consuming V1/V2 metadata. V2's parallel part arrays
must agree in length before indexing. Invalid sizes cannot turn a truncated shard
into a healthy one. Shard/bitrot size calculations reject invalid or overflowing
inputs, and verification rejects unsupported algorithms before constructing a
hash. Streaming verification uses a fixed 32 KiB copy buffer rather than a buffer
sized from peer metadata.

## Resource limits and compatibility

| Internal operation | Limit |
| --- | --- |
| Buffered AppendFile / WriteAll / ReadFile | 64 MiB per request |
| FileInfo metadata body, including a version-delete batch | 64 MiB actual bytes |
| DeleteVersions | 1,000 versions per request |
| Metadata MessagePack array/map | 10,000 entries per collection |
| Metadata MessagePack nesting / total values | 64 levels / 1,000,000 values |

These are **internal buffered RPC limits, not a 64 MiB S3 object-size limit**.
Larger data uses the existing CreateFile/ReadFileStream paths. A custom internal
client must split large buffered operations and version batches. Normal peers
already use the streaming data paths. Metadata is preflighted without allocating
collections before the generated decoder runs: a tiny array32/map32 declaration
must not cause a huge allocation before the body limit can take effect.

Upgrade all distributed nodes; a rolling mixed-version cluster still contains
unprotected old nodes. Back up metadata before upgrading. Malformed historical
metadata is rejected as corrupt, not silently rewritten or deleted; investigate
and recover it from a known-good copy rather than disabling validation.

## Limits of the remediation

The path fence is lexical. It is not an OS sandbox and does not defend against an
administrator creating symlinks/reparse points or racing filesystem changes in
the data directory. It also does not impose a global concurrent-memory budget,
redesign every peer protocol decoder, or claim protection after unrestricted
host/root compromise. Keep data directories under service ownership, isolate
internode traffic, and rotate exposed cluster credentials. No OIDC or unrelated
signature-authentication change is included in this patch.

## Regression coverage

`cmd/storage-security_test.go` covers direct storage boundaries and outside-root
sentinels, sibling-prefix deletion, batch preflight, body-only traversal, valid
nested/root operations, zero/negative/overflowing metadata, truncated shards,
unknown bitrot algorithms, and allocating REST endpoints with valid node JWTs.
A successful authenticated write ensures negative handler tests are not merely
failing at the authentication gate. MessagePack tests include legitimate
roundtrips, truncated/oversized declarations, nesting, and a fuzz target.

```sh
go test -count=1 -timeout 12m ./cmd -run '^TestStorageSecurity'
go test -count=1 -timeout 20m ./cmd -run '^(TestXLStorage.*|TestStorageREST.*|TestErasure.*|TestBitrot.*|TestXLV2.*|TestIsXLMeta.*|TestGetXLMeta.*)$'
go test -race -count=1 -timeout 12m ./cmd -run '^TestStorageSecurity'
go test -run '^$' -fuzz '^FuzzStorageSecurityMsgpack$' -fuzztime 30s ./cmd
```
