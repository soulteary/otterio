# Staged publication and stable promotion

The existing `Release` workflow still accepts an existing timestamp tag and
requires successful main CI for its exact commit. Publication now has separate
stages:

1. Build and checksum versioned binaries.
2. Push only fixed-version images, never `latest`; reject existing version tags
   rather than silently rebuilding over them. Authentication/transport failures
   are not evidence that a tag is absent.
3. Pull/run the GHCR image by digest and execute the existing real S3 startup,
   create/put/get/delete probe. This runtime smoke test covers Linux amd64; other
   release architectures are cross-compiled, not claimed to have been booted.
4. Record the source commit and registry digests in `release-manifest.json`, upload
   assets to a draft, download/compare every asset, then publish without moving
   the GitHub latest marker.
5. Run `Stable release promotion`, under a repository-wide concurrency group.
   It reads all published-release API pages, ignores drafts/prereleases, compares
   valid timestamp tags, checks the current tag commit and each registry digest,
   and promotes the recorded digests only when no newer stable release exists.
   The GitHub latest marker is updated after the registry aliases verify.

## Publication and promotion share one critical section

The `Release` workflow's **release job** and the entire `Stable release promotion`
workflow use the literal concurrency group `otterio-stable-promotion`. The lock
covers publication even when `promote_latest=false`. Promotion holds it before
reading the published-release list and keeps it through version-digest checks,
registry alias writes, and the GitHub latest update. A mere extra read immediately
before writing would still leave a check-to-use race; the shared lock prevents
these workflows from publishing a newer release during that interval.

If the newer release publishes first, a delayed old promotion sees it and skips.
If the older promotion acquires the lock first, its alias updates finish before
the newer release becomes published. Disabling promotion for that newer release
intentionally leaves aliases unchanged; it does not let a delayed old job write
aliases after the new publication. A failed newer promotion likewise cannot make
a later old job eligible. Builds and fixed-version image pushes remain parallel.

Do not put this global lock on the whole `Release` workflow or its `promote`
caller job: they would hold the same lock that the called workflow needs.
Publication releases its job-level lock before the caller requests promotion.
Both lock users set `cancel-in-progress: false` and `queue: max`, so up to 100
pending jobs/workflow runs can queue without replacing each other. Timestamp
validation, not queue order, decides which version may become latest.

## Retry without rebuilding

After publication, use Actions -> **Stable release promotion** -> **Run workflow**
on `main`, supplying the already published tag. It consumes that release's
manifest and never builds, uploads assets, creates tags or changes version tags.
This also repairs an interrupted alias update by repeating the same digests.
Older delayed tasks skip promotion rather than rolling latest back.

Before publication, prefer **Re-run failed jobs** in the original run so successful
binary/image jobs and their outputs are retained. Partially uploaded draft assets
can be retried while the release remains a draft. Do not delete a published
release to bypass immutability checks. A registry push that failed partway, or a
failed image smoke test after pushing, requires investigation and normally a NEW
tag: the workflow deliberately does not guess whether pre-existing version images
are safe to overwrite. Artifact expiry also requires a new release rather than an
unverified rebuild of a published version.

## Boundaries

GitHub and multiple registries do not provide a cross-service atomic transaction.
A transient failure can leave aliases temporarily inconsistent; retry promotion
for the newest verified release instead of rebuilding or rolling back. The
concurrency group serializes these workflows, not out-of-band administrator
writes. Drain workflows started with the old, publication-unlocked definition
before adopting this protocol. A full concurrency queue or a manually cancelled
job still requires an explicit retry; check the run status before proceeding.
Legacy releases without a manifest are refused, not silently adopted. Historical
non-timestamp tags are not included in the timestamp version-order comparison.
The manifest is an identity record, not a signature or supply-chain attestation.
No registry or release mutations are performed by the PR regression tests.
