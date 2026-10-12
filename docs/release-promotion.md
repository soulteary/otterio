# Staged publication and stable promotion

The tag build, `release.yml`, accepts an existing timestamp tag and requires
successful main CI for its exact commit. Draft preparation, accepted publication
and stable promotion are separate operations:

1. Build and checksum versioned binaries.
2. Push only fixed-version images, never `latest`; reject existing version tags
   rather than silently rebuilding over them. Authentication/transport failures
   are not evidence that a tag is absent.
3. Pull/run the GHCR image by digest and execute the existing real S3 startup,
   create/put/get/delete probe. This runtime smoke test covers Linux amd64; other
   release architectures are cross-compiled, not claimed to have been booted.
4. Record the source commit and registry digests in `release-manifest.json`, upload
   assets to a stable draft and download/compare every asset. Stop at the draft.
   Fixed-version images may already be public; this is not an RC or a guarantee
   of private distribution.
5. Run official `soulteary/oc` main `console-release-acceptance.yml` for the actual
   packaged pair. Review the successful run, unchanged asset/program/image
   identities, accepted runtime/upgrade scope and attestation SHA-256. Approve
   the explicit [selected migration policy](releases/console-migration-announcement.en.md)
   and its supported restore boundary before publication. Local execution
   reports cannot substitute for that official run.
6. Manually run `publish-release.yml` on `main` with the existing `tag`,
   `acceptance_run_id` and `attestation_sha256`. It checks the current bytes
   against the accepted pair, requires the accepted OC partner to be public,
   then publishes without rebuilding, replacing assets or moving GitHub latest.
7. Separately run `Stable release promotion`, under a repository-wide concurrency group.
   It reads all published-release API pages, ignores drafts/prereleases, compares
   valid timestamp tags, checks the current tag commit and each registry digest,
   and promotes the recorded digests only when no newer stable release exists.
   The GitHub latest marker is updated after the registry aliases verify.

## Publication and promotion share one critical section

The draft workflow's **release job**, manual publication and the entire
`Stable release promotion` workflow use the literal concurrency group
`otterio-stable-promotion`. Promotion holds it before
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

The draft and publication workflows never call promotion. Preserve the same
literal lock around draft mutation and public publication, and on the whole
manual promotion workflow. All three lock users set `cancel-in-progress: false`
and `queue: max`, so up to 100
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
are safe to overwrite. Expired build-job artifacts do not authorize rebuilding
over partially published version tags. If a verified draft is unchanged,
acceptance or publication can be retried without rebuilding. Changed accepted
bytes require a new release identity and fresh acceptance; never edit the
attestation or relax validation to match the changed draft.
For failed or expired acceptance, preserve its reports and dispatch a fresh
official manual run with a new run ID; do not overwrite an old attestation.

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
The manifest is an identity record, not a signature. The official acceptance
JSON and its independently reviewed hash bind the runtime evidence to the
accepted bytes; they are not a new Cosign/Sigstore signature or a guarantee for
every cross-compiled platform. Review the
[packaged-pair gate](https://github.com/soulteary/oc/blob/main/docs/console-release-acceptance.md)
and the [release guide](releasing.md) for access requirements and manual inputs.
No registry or release mutations are performed by the PR regression tests.
