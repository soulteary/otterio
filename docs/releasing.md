# Releasing OtterIO / 发布指南

The supported release tag format is `RELEASE.YYYY-MM-DDTHH-MM-SSZ`, in UTC.
The runtime uses an RFC3339 build version; this workflow deliberately does not
advertise `v1.2.3`/SemVer builds. No hard-coded runtime version needs updating:
`buildscripts/gen-ldflags.go` derives it from the validated tag.

## 1. Prepare and merge

Review and update the root `RELEASE_NOTES.md` for each release. The release job
loads that file from the tagged commit; automatic PR/commit excerpts are disabled.
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
and rerun CI rather than bypassing the gate.

## 2. Fetch, validate and tag

Local requirements are only Git and Python 3, plus a signing key if using `git tag -s`.
GoReleaser, Go, Docker and GitHub CLI are not required to push a release tag.
From a clean clone, after the preparation PR is merged:

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
or claim to check remote CI; refresh refs first, and the Actions release gate
checks CI separately. Do not run preflight in an uncommitted worktree.

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
| GHCR image | `ghcr.io/soulteary/otterio:<TAG>`, Linux amd64/arm64/ppc64le |
| Docker Hub image | `soulteary/otterio:<TAG>` when both Docker Hub secrets are configured for that account |
| Release notes | Reviewed `RELEASE_NOTES.md` from the same commit |

The pipeline directly cross-compiles Go binaries; it does not invoke
`.goreleaser.yml`, publish `.deb`/`.rpm` packages, or update a Homebrew tap.
The Docker Hub namespace is taken from `DOCKERHUB_USERNAME`. Configure both
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` as Actions repository secrets to use
it; when either is missing, Docker Hub is skipped. GHCR uses `GITHUB_TOKEN` with
`packages: write`. Its package must permit this repository to publish; existing
package access settings can still deny the push. Do not put tokens in files.

A tag push updates versioned images and `latest`. The GitHub Release is published
only after binaries and configured image pushes succeed. Publication across
registries and GitHub is **not atomic**: one registry or `latest` may already have
changed if a later operation fails. Inspect all outputs before announcing.

## 4. Verify before announcing

After every Release job succeeds, confirm six binaries and the checksum file,
review the security notice and reporter-approved acknowledgement, then verify
image platforms:

```sh
# Use the exact TAG printed above, not a newly generated timestamp.
docker buildx imagetools inspect "ghcr.io/soulteary/otterio:$TAG"
docker pull "ghcr.io/soulteary/otterio:$TAG"
docker run --rm "ghcr.io/soulteary/otterio:$TAG" --version
```

Download binaries and checksums from the matching GitHub Release into one clean
directory. On Linux run `sha256sum --check "otterio-${TAG}-checksums.txt"`;
on macOS run `shasum -a 256 --check "otterio-${TAG}-checksums.txt"`. Download all
listed files to verify the full manifest, or verify the selected asset's hash
against its manifest entry. Test backups, storage startup, normal S3 operations,
custom signing clients and properly signed copies in staging before rollout.
Then announce the release and privately notify the reporter with the release
link. Do not paste their email or original report into a public announcement.

## 5. Recovery and manual runs

Prefer **Re-run failed jobs** on the original run so successful binary artifacts
are reused. CI still running is not a reason to change the tag. Once CI succeeds,
rerun the failed gate. If code/workflow changes are needed, merge them and use a
new tag; never move the old tag to the repair commit.

For an unpublished tag containing this release workflow and helper, use
**Actions → Release → Run workflow**, select `main`, and enter the existing tag.
The selected branch supplies the workflow; the `tag` input selects build content.
The workflow explicitly checks out `refs/tags/<tag>` and never creates a tag.
Manual runs default to **not** updating `latest`. Enable `promote_latest` only
when recovering the newest stable release and no newer version should remain
latest. Already-published releases are rejected, even on a manual rerun.

Optional GitHub CLI equivalent:

```sh
gh workflow run release.yml --repo soulteary/otterio --ref main \
  -f tag="$TAG" -F promote_latest=true
```

Do not mix a new tag workflow with an older tag lacking these files. If artifacts
have expired or a clean rebuild is needed, prefer a new version. Git tag signing,
checksums, image provenance and artifact signing are separate mechanisms; this
workflow does not add Cosign signatures or claim reproducible binary rebuilds.

## 中文操作摘要

合并发布准备 PR，等待合并后同一提交的 Go、Lint、Release checks 全绿；按上面的命令
同步 main、生成 UTC 时间标签、运行只读预检、签名并推送标签即可。发布由 GitHub Actions
完成，本机不需要安装 GoReleaser。手动触发只接受已存在的标签，不会代替你创建标签。

所有发布任务成功后，再检查六个平台二进制、校验和、多架构镜像及符合反馈者署名意愿的致谢，
最后发布公告并私下告知反馈者。失败时优先重跑失败任务；需要修改代码则合并修正并使用新标签。
不要移动旧标签、跳过 CI，或把其他项目的 CVE 编号当作 OtterIO 已获分配的编号。
