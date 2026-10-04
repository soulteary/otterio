# SigV4 request-header coverage

OtterIO must authenticate the headers that select and modify an S3 operation,
not just the subset that the client lists in `SignedHeaders`. Earlier code
allowed additional unsigned S3 headers to change an otherwise valid request.
For example, a delegated upload could become a server-side copy using the
signer's source-read permissions. The destination remains constrained by the
signed URL; reading copied bytes additionally requires access to that destination.

Reported by **Oren Yomtov**. The affected extraction logic is present in
`RELEASE.2026-06-07T11-32-46Z` and in main at
`a8f27bf7532928343becf38eacee155e6198601d`. Do not assume an existing image tag
contains this fix: deploy a build that includes the header-coverage change.
The equivalent Ceph vulnerability, CVE-2026-54330, is a reference for the bug
class, **not an assigned OtterIO CVE**.

## Enforcement and compatibility

| Request input | Required behavior |
| --- | --- |
| `x-amz-*` and `x-otterio-*` headers | Must be covered by the signature; an uncovered header returns HTTP 403 `AccessDenied`. Empty values are still headers. |
| Presigned query parameters repeated as headers | Must have exactly matching values, including value count and order. Conflicting representations return `InvalidRequest`. |
| `x-amz-content-sha256` | May be omitted from `SignedHeaders` because its effective value is already bound through `HashedPayload`. A conflicting presigned query/header value is rejected. |
| Ordinary transport/client headers | `User-Agent`, `Accept-Encoding`, and unsigned `Content-Type` remain compatible. |
| Legitimate copy requests | Sign the copy source, range, directives and other S3 headers before sending. Source IAM checks remain mandatory. |
| Tags loaded by the server during GET/HEAD | Carried in internal request context for policy evaluation; signed request headers are not rewritten. |

The shared extraction guard covers presigned URLs, Authorization-header SigV4,
and the initial signature of streaming uploads. It also protects the production
router's CopyObjectPart and archive-extraction dispatch from unsigned header
injection. It does not disable legitimate copy operations or change SigV2 and
POST-policy authentication.

Before upgrading, update custom clients that append S3/replication headers after
signing: include those headers at signing time. Do not work around a rejection by
disabling verification. Until a patched build is deployed, issue upload URLs with
a dedicated identity limited to the required upload objects and without private
source-object read permissions. Shorter expiry alone does not remove the flaw.

## Regression coverage

`cmd/signature-v4-coverage_test.go` covers unsigned headers, casing, empty and
multiple values, signed query/header conflicts, valid signatures through all
three entry points, SDK compatibility, and server-loaded tags.

`cmd/signature-v4-copy_security_test.go` uses an independent S3 SDK signer, the
production Fiber routing table, and both FS and erasure storage. It verifies
ordinary uploads, rejected injected copies, unchanged anonymous read-back,
legitimate signed copies, tampered signed sources, unchanged multipart data,
source IAM denial for a write-only signer, and authenticated tagged GET/HEAD.
The existing copy tests now sign their copy headers before sending requests.

References:

- [AWS presigned request authentication](https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sigv4-query-string-auth.html)
- [AWS canonical headers and payload hash](https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sig-v4-header-based-auth.html)
- [Ceph's related advisory](https://docs.ceph.com/en/latest/security/CVE-2026-54330/)
