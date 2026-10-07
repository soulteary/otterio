# OtterIO release preparation for 2026 10 07

Prepare a fresh UTC `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tag after this PR is merged
and the resulting main commit passes Go, Lint and Release checks. The date here
identifies this preparation; it does not reserve a tag or announce publication.

## Source range

Previous published version: `RELEASE.2026-10-04T22-12-15Z`, source
`be8596f0d69d530586f35366fb2d5c79bdc54399`.
Reviewed implementation cutoff: `100aa5c438c5208c5b893d87d64f09c6d37e4ec4`.
[Compare these commits](https://github.com/soulteary/otterio/compare/be8596f0d69d530586f35366fb2d5c79bdc54399...100aa5c438c5208c5b893d87d64f09c6d37e4ec4).

- `8fd3420`: validated administration query bridge with path parameter precedence.
- `cfab08a`: bounded HTTP shutdown and macOS worker supervision/restart.
- `100aa5c`: encoded paths, administration/healing streams, stream cancellation,
  accounting, listeners, TLS negotiation and portable release image naming.

The prior storage, credentials, dependency and publication changes were already
shipped in the baseline. Root `RELEASE_NOTES.md` is the body loaded by the release
workflow; the older October 5 preparation remains a historical record.
The OC companion preparation retains its pinned server SDK and compatibility
patches; this server release does not silently update that dependency.

## Release acceptance

- [ ] Reconcile main changes after the implementation cutoff, merge this PR and
  record the resulting source SHA.
- [ ] Require Go, Lint and Release checks success on that exact main SHA.
- [ ] From a clean synchronized main, run the read-only preflight and create a
  fresh annotated or signed UTC tag. Do not reuse an earlier failed tag.
- [ ] Verify Release and requested Stable release promotion results, six binaries,
  the checksum file and `release-manifest.json` (eight uploaded assets).
- [ ] Check source identity, configured registries' fixed tags and digests,
  Linux amd64 image S3 smoke results and newest-release alias promotion.
- [ ] In staging, verify encoded keys, administration and healing streams,
  cancellation, shutdown, macOS restart, existing objects and distributed healing.

Follow [the release guide](../releasing.md) for commands and failure recovery.
Only an actual publication and deployment record may mark these steps complete.
