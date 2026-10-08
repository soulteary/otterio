# Releasing OtterIO / 发布指南

The supported release tag format is `RELEASE.YYYY-MM-DDTHH-MM-SSZ`, in UTC.
The runtime uses an RFC3339 build version; this workflow deliberately does not
advertise `v1.2.3`/SemVer builds. No hard-coded runtime version needs updating:
`buildscripts/gen-ldflags.go` derives it from the validated tag.

## 1. Prepare and merge

Review and update the root `RELEASE_NOTES.md` for each release. The release job
loads that file from the tagged commit; automatic PR/commit excerpts are disabled.
An article or date-named file under `docs/releases/` is supporting material,
not an automatically selected release body or proof that its version exists.
Choose a fresh UTC tag after merge and verification, not while drafting notes.
For the current preparation, review the [baseline-to-main change inventory and
release checklist](releases/2026-10-08-release-review.md), including the SDK/kits,
CLI, account information and conditional-write changes after
`RELEASE.2026-10-07T14-09-17Z`. Reconcile any later main commits
before tagging; a fixed review cutoff is not permission to omit later changes.

Keep reporter acknowledgements anonymous unless public attribution has been
approved. Never copy private email addresses, mail headers or correspondence into
release notes, PR descriptions, commit messages, or screenshots. A related CVE
in another product is not an assigned OtterIO CVE. Older Git history is not
anonymized by changing current release notes.

Merge the preparation PR, then require the **Go**, **Lint**, and **Release checks**
workflows to succeed on the exact resulting `main` commit. A green PR run alone
is not the release gate. The Go workflow includes full Linux tests, race checks,
Windows checks, the SigV4 regressions and vulnerability scanning. Any missing,
running, cancelled or failed required workflow blocks publication; fix the issue
and rerun CI rather than bypassing the gate. Review other applicable PR checks,
including Docker source and container security checks, before merging as well;
the release workflow's automated exact-commit gate currently names only those
three workflows.

Go setup reads the tagged checkout's `go.mod` (currently Go 1.27.1), rather than
selecting an arbitrary latest Go 1.26 patch. Recheck the module and dependency
records when preparing a later release.

## 2. Fetch, validate and tag

Local requirements are Git and Python 3, plus a signing key if using `git tag -s`.
GoReleaser, Go, Docker and GitHub CLI are not required to push a release tag.
From a clean clone, after the preparation PR is merged and main CI passes:

```sh
# The && chain stops immediately if any prerequisite or signature check fails.
# Recommended: use your already configured signing key.
git switch main &&
git fetch origin main --tags &&
git pull --ff-only origin main &&
TAG="RELEASE.$(date -u +%Y-%m-%dT%H-%M-%SZ)" &&
python3 buildscripts/release-preflight.py "$TAG" &&
git tag -s "$TAG" -m "OtterIO $TAG" &&
git verify-tag "$TAG" &&
git push origin "refs/tags/$TAG"
```

The preflight is read-only: it checks tag/date syntax, a clean tree, `main` at
`origin/main`, no reused tag, and nonempty release notes. It does **not** fetch
or check remote CI, GitHub Release history or registry tags; refresh refs first,
and the Actions gates check CI and publication separately. Do not run preflight
in an uncommitted worktree. A deleted GitHub Release does not establish that its
Git tag or versioned images are available for reuse. Use a new UTC tag, never a
previously published or partially used release identity.

Without a signing key, use `git tag -a "$TAG" -m "OtterIO $TAG"` instead of the
sign/verify pair. An annotated tag is not a cryptographically signed tag, and a
verified commit does not automatically make its tag verified. Never force-push,
move or delete a published release tag. Do not also create a release manually:
pushing the tag triggers the publication workflow.

## 3. What the workflow publishes

The workflow validates and peels the tag to a commit, requires that commit to be
on `origin/main` with passing main CI, and refuses to overwrite an already
published GitHub Release. Every build checkout uses that fixed commit SHA.

| Output | Platforms / behavior |
| --- | --- |
| Standalone binaries | Linux amd64/arm64/ppc64le; macOS amd64/arm64; Windows amd64 |
| Checksums | `otterio-<TAG>-checksums.txt`, SHA-256 for all six binaries |
| Release manifest | `release-manifest.json`, containing the release tag, source SHA and configured image repository/digest pairs |
| GHCR image | `ghcr.io/soulteary/otterio:<TAG>`, Linux amd64/arm64/ppc64le |
| Docker Hub image | `<DOCKERHUB_USERNAME>/otterio:<TAG>` when both Docker Hub secrets are configured; `soulteary/otterio:<TAG>` for the project's usual namespace |
| Release notes | Reviewed root `RELEASE_NOTES.md` from the same commit |

The pipeline directly cross-compiles Go binaries; it does not invoke
`.goreleaser.yml`, publish `.deb`/`.rpm` packages, or update a Homebrew tap.
Configure both `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` as Actions repository
secrets to use Docker Hub; when either is missing, Docker Hub is skipped.
GHCR uses `GITHUB_TOKEN` with `packages: write`. Its package must permit this
repository to publish; existing package settings can still deny the push.
Do not put tokens in files or release notes.

### Manifest generation and secret-filtered outputs

The image job exports the real build digest and an explicit
`dockerhub_published=true|false` selection, not repository-name job outputs.
GitHub may [omit outputs containing secrets](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idoutputs),
including public names that contain a username stored as a secret. Publication
reconstructs GHCR from `GITHUB_REPOSITORY` and takes the Docker Hub namespace from
its configured username; the two owners need not be identical.

`buildscripts/release-promotion.py --write-manifest` validates the tag, source
SHA, digest, selection and allowed image identities before opening the output
file. It shares identity checks with promotion. Missing build metadata is an
error, not a reason to omit an intended registry. The allowlist remains strict;
never disable masking, encode secrets to bypass it, or relax validation to make
a failed release green. This prevents new malformed manifests; it does not
repair an already uploaded one. The workflow does not sign release assets with
GPG; optional Git tag signing is a separate mechanism.

### Publication order

1. Validate source, notes and exact-commit main CI; reject an already published
   release or a prerelease draft.
2. Build the six binaries and checksums and smoke-test the Linux amd64 binary.
3. Refuse existing versioned image tags, then push fixed-version images without
   changing `latest`. Authentication/network errors are not evidence of absence.
   Run the real S3 startup/create/put/get/delete probe against the pushed GHCR
   digest on Linux amd64; this does not exercise every image architecture.
4. Generate and validate `release-manifest.json`, then upload the eight expected files to a stable
   draft, download them again and compare filenames and every file's bytes.
   Publish only after verification, with GitHub's latest marker unchanged.
5. For tag pushes, automatically call **Stable release promotion**. Manual
   Release runs do this only with `promote_latest=true`. Promotion validates the
   published release, source identity, allowed repositories and recorded digests;
   only the newest published stable timestamp release may move `latest`.
   Update and verify registry aliases first, then GitHub's latest marker.

Publication and promotion share the repository-wide `otterio-stable-promotion`
concurrency group. Promotion uses the recorded digest without rebuilding or
changing version tags. The policy prevents older delayed jobs from rolling
aliases back; it does not protect against out-of-band administrator changes.

Publication across GitHub and registries is **not atomic**. A fixed-version image
may exist before the GitHub Release, and a promotion failure may leave aliases
temporarily inconsistent. Inspect all outputs before announcing.

## 4. Verify before announcing

Confirm the Release workflow and its requested promotion completed successfully.
Check the six binaries, checksum file and `release-manifest.json` (eight uploaded
assets, excluding GitHub's automatically generated source archives), the release
body and reporter-approved credit. Confirm the manifest's tag/source SHA match
the intended Git tag and the image repositories/digests match registry output:

```sh
# Use the exact TAG printed above, not a newly generated timestamp.
docker buildx imagetools inspect "ghcr.io/soulteary/otterio:$TAG"
docker pull "ghcr.io/soulteary/otterio:$TAG"
docker run --rm "ghcr.io/soulteary/otterio:$TAG" --version
```

The image's `--version` command does not require server credentials. For startup
and S3 tests, use non-default credentials and follow the
[container migration guide](../README_DOCKER_SECURITY.md).

Download binaries and checksums from the matching GitHub Release into one clean
directory. On Linux run `sha256sum --check "otterio-${TAG}-checksums.txt"`;
on macOS run `shasum -a 256 --check "otterio-${TAG}-checksums.txt"`. Download all
listed binaries to verify the full checksum file, or verify a selected binary
against its entry. This checksum file covers the six binaries, not the later
created `release-manifest.json`. The manifest records identity; it is not a
signature or attestation.

### Read-only manifest and fixed-tag verification

From a reviewed checkout containing the current release helper, set `TAG` to
the actual published version. If that build included Docker Hub, also set
`DOCKERHUB_USERNAME` to its published namespace (not its token). Leave it unset
for a GHCR-only build. The following Bash subshell downloads the manifest,
validates it against the tag commit and stable-release listing, and compares
every fixed image tag to the recorded digest. It does not create/replace assets,
change aliases, or generate a replacement manifest. Git/temporary-file writes
are local; registries may require read authentication.

```bash
(
  set -euo pipefail
  : "${TAG:?Set TAG to the actual published release}"
  REPO=soulteary/otterio
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  python3 buildscripts/release-promotion.py "$TAG" --validate-tag
  git fetch --no-tags origin "refs/tags/$TAG"
  source_sha="$(git rev-parse 'FETCH_HEAD^{commit}')"
  gh release download "$TAG" --repo "$REPO" \
    --pattern release-manifest.json --dir "$work"
  gh api --paginate --slurp "repos/$REPO/releases?per_page=100" > "$work/releases.json"
  python3 buildscripts/release-promotion.py "$TAG" \
    --manifest "$work/release-manifest.json" --published "$work/releases.json" \
    --source-sha "$source_sha" --repository "$REPO" \
    --dockerhub-user "${DOCKERHUB_USERNAME:-}" > "$work/plan.json"
  if [ -n "${DOCKERHUB_USERNAME:-}" ]; then
    jq -e '.has_dockerhub == true' "$work/plan.json" >/dev/null
  fi
  jq -r '.images[] | [.repository, .digest] | @tsv' "$work/plan.json" > "$work/images.tsv"
  while IFS=$'\t' read -r repository digest; do
    actual="$(docker buildx imagetools inspect "$repository:$TAG" --format '{{json .Manifest}}' | jq -r .digest)"
    test "$actual" = "$digest" || { echo 'Fixed tag digest mismatch' >&2; exit 1; }
  done < "$work/images.tsv"
  jq '{tag, source_sha, images, promote, reason}' "$work/plan.json"
)
```

A successful read-only check does not run promotion or prove the S3 smoke tests
passed. `promote: false` means a newer stable release exists, not corrupt data.
Review the original workflow's build/smoke evidence and separately inspect
`latest`. A missing/invalid manifest, omitted expected registry or mismatched
fixed-tag digest is a stop condition, not something to repair with a permissive
fallback or a manually invented identity record.

When promotion was requested, inspect every configured registry's `latest`
digest and GitHub's latest release. If a newer stable version has appeared,
an older release's promotion is expected to skip rather than replace it.
Test backup/restore, storage permissions, credential loading, normal S3
operations, custom signing clients and properly signed copies in staging.
For storage hardening, also verify existing-object reads, multipart and large
streaming uploads, and distributed healing/replication. Plan upgrades for every
node, back up metadata, and handle newly rejected corrupt metadata using a
known-good copy. See the [storage compatibility and limits](security/sn-2026-002-storage-hardening.md);
internal 64 MiB buffering limits are not S3 object-size limits.
Then announce the release and privately notify the reporter with the release
link. Do not paste their email or original report into a public announcement.

## 5. Recovery and manual runs

Choose recovery based on what has already been published, not only the final
workflow status:

| State | Safe next step |
| --- | --- |
| Required CI missing/running/failed; no artifacts published | Finish or repair CI and rerun the failed gate for the same unchanged tag. |
| Failure before image tags were pushed | Prefer **Re-run failed jobs** on the original run, preserving successful binary outputs. Inspect registry state if a push was attempted. |
| Versioned image push partly succeeded, or pushed-image smoke failed | Investigate; normally repair on main and use a **new tag**. Do not delete or overwrite image tags just to bypass the guard. |
| Images succeeded; draft upload/download verification failed | Prefer **Re-run failed jobs** on the original run, reusing successful image/binary outputs. Only stable draft assets may be replaced by the workflow. |
| GitHub Release published; manifest is valid, all intended repositories are present, and fixed tags match recorded digests; only promotion failed or was omitted | Run **Stable release promotion** for the newest published stable tag after resolving its operational failure. Do not rerun Release to rebuild or re-upload it. |
| Manifest is malformed, empty, missing an expected registry, or does not match the source/fixed-tag digest | Stop. Investigate and fix on main, then publish a **new tag** through the full pipeline. Re-running promotion does not repair metadata. Do not overwrite published assets or relax the allowlist. |
| Older release has no `release-manifest.json` | The new promotion workflow refuses it. Do not fabricate a manifest; publish a new release through the complete verified pipeline. |

[Re-running a workflow](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs)
keeps the original event's `GITHUB_SHA` and `GITHUB_REF`; it does not retag newer
source. Some steps explicitly check out `main` (the promotion helper does), but
that cannot repair old assets or include later storage fixes in an old binary.
A workflow fix on main is not proof that a retry of an old release is corrected.
Use a new tag when the release source or manifest-generation workflow must change.

For an unpublished, existing tag containing the current release helpers, use
**Actions → Release → Run workflow**, select `main`, and enter that tag. The
selected branch supplies the workflow; the `tag` input selects build content.
The workflow checks out `refs/tags/<tag>` and never creates a tag. A fresh manual
run can encounter existing-image guards; it is not a substitute for resuming
only the failed jobs of a partially completed publication.

Manual Release runs default to **not** promoting `latest`. Enable
`promote_latest` only for the intended newest stable release. Optional CLI:

```sh
gh workflow run release.yml --repo soulteary/otterio --ref main \
  -f tag="$TAG" -F promote_latest=true
```

For an **already published** stable release with a verified manifest, use
**Actions → Stable release promotion → Run workflow** on `main`, or:

```sh
gh workflow run promote-release.yml --repo soulteary/otterio --ref main \
  -f tag="$TAG"
```

Promotion checks release ordering again; an older stable release is not eligible
to replace a newer one. It does not create tags, build images or replace assets.
If artifacts expired, source/workflow changes are required or a clean rebuild
is necessary, merge the changes and use a fresh tag. Never move the old tag.

Do not mix a new release workflow with an older tag lacking its helpers. Git tag
signing, binary checksums, image provenance and artifact signing are separate
mechanisms; this workflow does not add Cosign signatures or claim reproducible
binary rebuilds.

## 中文操作摘要

先更新根目录 `RELEASE_NOTES.md` 并合并发布准备 PR，等待合并后同一提交的 Go、Lint、
Release checks 全绿。发布文章或日期命名的文档不是实际版本号；发布时再同步 main、
生成全新的 UTC 标签、运行只读预检、签名并推送，由 Actions 完成发布。
不要另外在网页手动创建同名 Release，也不要复用已删除 Release 的旧标签或镜像版本。

顺序是：校验源码和 CI → 构建二进制 → 推送固定版本镜像并验证摘要 → 上传并下载比对
草稿附件 → 发布 Release → 串行提升 `latest`。不是在镜像构建阶段就更新 `latest`。
六个二进制、一个校验文件及一个身份清单共八个上传附件；校验文件只覆盖六个二进制，
身份清单不是签名。发布后还应核对镜像平台、摘要、凭据迁移和反馈者确认的致谢。

失败时先确认已经产生哪些产物。草稿附件失败可优先重跑原任务的失败步骤；
已有版本镜像或镜像冒烟失败不能靠删除覆盖标签解决，通常需要调查修复后使用新标签。
正式 Release 已发布、清单有效、应有仓库均已记录且固定标签摘要一致，只是 `latest`
未完成时，才单独运行 **Stable release promotion**。清单为空、损坏、缺失或身份不匹配，
重跑晋升不会修复；应修复主分支后使用全新标签，不能覆盖正式附件或放宽校验。
重跑保留原事件 SHA/ref，显式检出 main 的步骤也不会把后续修复加入旧二进制。
旧版没有身份清单时不能套用新晋升流程；上面的只读检查不会发布或更新 `latest`。

本轮包含内部存储加固：分布式节点应全部升级，提前备份元数据并验证已有对象、分片上传、
大对象流式读写及修复/复制。64 MiB 限制不针对 S3 对象大小；历史损坏元数据需调查恢复，
不能以关闭校验绕过。所有产物与部署验证完成后再公告。
