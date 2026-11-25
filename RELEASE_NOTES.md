# otterio RELEASE.2026-11-25T20-30-00Z

Release date: 2026-11-26 (Asia/Shanghai).

Rebuild the server with the current Go/network/container dependencies and deliver the Fiber routing/security regression changes since the previous stable release. The OIDC signing-algorithm retry hardening from the previous stable release is retained; it is not a newly introduced feature of this release. Storage and configuration formats are unchanged.

## Upgrade and rollback

Retain the previous exact program/image and back up configuration before rollout. Stop affected writers during replacement; validate the actual configured user path in staging. Restore the complete previous dependency lockfiles when rebuilding an older source version.

Full changes: https://github.com/soulteary/otterio/compare/RELEASE.2026-10-28T07-52-43Z...RELEASE.2026-11-25T20-30-00Z

## Packaged-pair acceptance

Draft construction does not constitute publication. Official acceptance must bind the exact archive programs, immutable platform images, manifest and release notes for both new tags; native/HTTPS/OIDC, permissions, S3/Range, readiness and documented migration/rollback boundaries remain mandatory. Only publish and promote the exact accepted bytes.
