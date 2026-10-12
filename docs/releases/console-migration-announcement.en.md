# Accelerated migration announcement: move the Web Console to OC

**Status: accelerated migration approved; publication pending.** The selected
policy is an explicit notice plus verified older-server/snapshot rollback for
the first public Web-free OtterIO release containing this announcement. The
decision is authorized, but its revision of the published policy becomes
effective only at that actual publication. Committing or merging the notice
does not publish a release or enact the revision. Final artifact acceptance and
operator review of the publication remain required. No future tag, release date
or support deadline is assigned by this draft.

## What changes for users

The browser application moves from the OtterIO server to the independently
deployed OC Console, `oc-console`. Separating the browser assets, login handlers
and Web adapters from the storage server reduces the server's exposed routes and
keeps browser delivery independent of server maintenance. S3, Admin, STS/OpenID
validation, health, authenticated metrics, node communication, shared JWT/TLS,
terminal output and Admin logs remain server responsibilities.

The compatible OC release includes both `oc` and `oc-console` in its platform
archives and images. An OC image still starts `oc` by default; select
`--entrypoint oc-console` for the browser application. Use the exact OC/OtterIO
tags, sources, archive hashes and image digests recorded by the final paired
acceptance. The October 10 OC artifacts do not acquire newer native/OIDC features
because a newer server is installed.

Choose native IAM or OIDC for independent browser users. Requests use each user's
actual backend permissions, including explicit denies. Native login requires an
enabled native IAM user and refuses root, service-account, STS and directory
identities. OIDC uses a fixed issuer, Authorization Code with S256 PKCE, RS256
verification and WebIdentity STS. Local alias mode shares one startup identity;
it is suitable only where that sharing is intended. OC has no LDAP browser login
mode; the server's LDAP STS API remains available.

The first offered deployment is one OC instance with direct browser HTTPS and
fixed HTTPS S3/Admin endpoints. Configure browser TLS and S3, Admin and IdP CA
trust independently, keep confidential-client secrets in private files, and
persist preferences by identity. Sessions remain in memory and a restart
requires a fresh login. Writes and sharing are opt-ins subject to backend
permissions. OIDC native IAM management and Console secret rotation remain
disabled. Follow the [configuration and migration guide](../console-migration-preparation.md)
and [OC HTTPS recipes](https://github.com/soulteary/oc/blob/main/deploy/README.console-https.md).

## The selected policy and when it becomes effective

The first notice was published with
[OtterIO `RELEASE.2026-10-10T16-38-05Z`](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-10T16-38-05Z)
at `2026-10-10T16:46:29Z`. That release still defaults to the embedded Web and
retains its old login and Web routes. Those published bytes and their history
remain unchanged.

The [earlier policy](../web-console-deprecation.md#published-history) required at
least two notified releases, a separately reviewed default-off transition and a
completed opt-in fallback window before deletion. Those steps have **not** been
completed. This decision does not count source merges or acceptance runs as
additional notice cycles or as elapsed fallback time.

For the early Web-free release, the selected replacement gate is successful
acceptance of the exact packaged OC/OtterIO pair, an explicitly limited supported
scope, a tested prior-release/snapshot restore boundary, and operator approval
of publication. The accelerated policy departure has been authorized. Publishing
the first approved Web-free release containing this announcement will make that
revision effective for embedded Web removal. Until then, the original policy
remains the published commitment. This is an explicit change
to the sequence, not a claim that its previous windows were fulfilled.

There will be no embedded-Web enablement window in the new program. Leave
`OTTERIO_BROWSER` unset or set it to `off`; explicit `on` fails startup with OC
guidance. To retain or restore the old Web, use a retained, verified older server
program and a compatible data/configuration state. An environment switch cannot
restore code removed from the new program. This draft assigns no maintenance
lifetime or end date to an older release.

## Supported scope and deliberate limits

The official hosted release acceptance is intended to exercise Linux amd64
packaged programs, including object/settings/version, native IAM, permission/deny,
Range, diagnostics and real OIDC/Chromium profiles, plus the October 10 → new
pair → previous-program upgrade and rollback path. Darwin arm64 packaged
execution is separate local evidence and cannot replace the official attestation.
Linux amd64/arm64 image runtime can be claimed only after the actual digest-bound
checks pass: package-byte comparison, version/help and OC copy, plus the server's
signed S3 startup/round-trip probe. These are not the whole Console functional
suite inside each container. Hosted arm64 checks may use QEMU and do not certify
native arm64 hardware. Read the actual reports for the programs, images,
platforms and limitations accepted; this draft declares no future checks passed.

Other release targets are cross-compiled distribution targets, not native runtime
certifications. Proxy TLS termination, multiple Console replicas, gateways,
distributed/federated storage, shared etcd/external OPA, every IdP/browser and
long-running resource behavior are outside this first rollout. Conditional native
IAM writes require the initialized, single-writer native IAM object-store
protocols; unsupported capabilities refuse writes. The cross-version rehearsal
does not certify OIDC upgrades or arbitrary later IAM/configuration changes.

Three old disclosures are intentionally withdrawn: ordinary-user host/runtime
information, global storage capacity and full bucket-policy prefix summaries.
Service diagnostics provides a permission-scoped snapshot with
`admin:ServerInfo`; it does not borrow an administrator identity. It is not a
certification of the old diagnostic stream, cancellation or long-lived resource
behavior. The [28-capability mapping](https://github.com/soulteary/oc/blob/main/docs/console-migration-capabilities.md)
records replacements, withdrawals and uncovered scope; passing the source suite
does not mark all historical capability gates verified.

## Before upgrading, and how to restore

Keep the prior verified server and OC programs/images, configuration, CA material
and a consistent backup of data and backend configuration/IAM, including the
credentials needed to decrypt them. Stop writes when taking the snapshot. Preserve
the actual S3/Admin endpoints; `--console-address`, `--console-certs-dir` and
their legacy environment equivalents still configure **Admin**, while OC has its
own browser listener. Do not rotate root credentials as part of this migration.

Rehearse the published-old → new pair → published-old path on isolated storage
before changing production. The declared fixture checks seeded objects,
historical versions, tags, bucket versioning, native/group permissions and denies,
local preference compatibility and isolated native preferences across restart.
It separately checks old-program reading after one new object write and recovery
of the frozen pre-upgrade snapshot. This is a stated compatibility boundary,
not a general server downgrade promise.

Returning to old programs or restoring their Web does not undo business writes,
IAM edits, secret rotation, lifecycle/tier changes or external effects. If the
old program cannot safely read the required state, restore the tested pre-upgrade
snapshot and follow the deployment's plan for later writes and external systems.
Do not use an unverified storage downgrade as a fallback.

## How the release is verified and made public

The release process has three separate operations. A tag build prepares verified
stable **drafts** and fixed-version images. Those images may already be publicly
pullable; a GitHub draft is neither an RC nor a guarantee of private artifacts.
The [official packaged-pair acceptance](https://github.com/soulteary/oc/blob/main/docs/console-release-acceptance.md)
then binds both repositories' exact asset bytes, manifest/source identities,
programs, immutable image digests and actual runtime checks.

Manual publication consumes a successful official main-branch acceptance run and
its independently reviewed attestation SHA-256. It rechecks the accepted bytes
and publishes the same stable release without rebuilding, replacing assets or
changing `latest`. Publish OC first; the OtterIO publication verifies that its
accepted OC partner is public. A separate manual stable promotion may later move
registry aliases and GitHub's latest marker to the newest published stable
release. Neither draft preparation nor publication calls promotion.

Cross-repository draft and official artifact reads use the configured
`CONSOLE_RELEASE_TOKEN` with Contents read and Actions read on both repositories;
the actor must be able to read both drafts. Publication uses its own repository
token for the final release mutation. The reader needs no Contents write. Secret
and package access must be checked before dispatch; this draft does not assert
that they are configured.

Before publication, the operator must review the actual release notes, this
selected policy revision, the accepted support/restore boundary and the official
report. Local source reports remain development evidence and cannot authorize
publication. The policy decision does not authorize a tag build or public release
by itself; final publication approval remains separate.
