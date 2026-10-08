# Release materials / 发布材料

The reviewed bilingual body for the next release is
[RELEASE_NOTES.md](../../RELEASE_NOTES.md). The workflow reads that root file from
the exact tagged commit; it does not automatically select a date-named article
or another file in this directory.

The [maintainer release guide](../releasing.md) covers tag creation, exact-commit
main CI, staged publication, artifact verification and separate digest promotion.
Preparing or merging these documents does not create a tag or publish artifacts.

## Current preparation

[2026-10-08 release review](2026-10-08-release-review.md) covers all 12 commits
after `RELEASE.2026-10-07T14-09-17Z` through
`458b54c9bc155780c546696a12581450adfee32b`: durable lifecycle transition and
version restore, protected tier metadata and overwrite quorum, bucket
configuration revisions and IAM self-rotation, storage-class synchronization,
account information and conditional writes, CLI v3, published SDK/kits, the
fixed/checksum-verified Mint installer, image runtime refresh and reconciled
guides. It distinguishes current regression results from historical `6f6d083`
compatibility checks and keeps publication/deployment gates pending.
Go integrations should also read [SDK and kits compatibility](../development/sdk-kits-migration-20261008.md)
and [CLI migration](../cli-migration.md). Reconcile any later main changes before
selecting a fresh UTC tag.

The [project assessment for 2026-10-08](../development/project-status-20261008.md)
is a historical snapshot at `8ead9fb`, an ancestor of the current `458b54c` cutoff.
Its implementation range is included in the preparation above and root release
notes. Publication and deployment acceptance remain pending as recorded in the
release review.

## Historical announcement draft

[OtterIO 新版本：补强存储安全边界，完善容器部署与发布](2026-10-05-announcement.zh-CN.md)
is the historical Chinese announcement prepared on 2026-10-05. Its baseline is the published
`RELEASE.2026-10-04T09-24-10Z`; it distinguishes the new changes from retained
security fixes. Choose and verify a fresh tag before publishing the article.
The document date is not the final release timestamp. The draft now includes
#26 (internal storage boundaries) and #27 (secret-filtered manifest outputs),
which were merged after the initial #25 preparation.

## Historical scope reviews

[2026-10-07 release review](2026-10-07-release-review.md) records the HTTP,
administration and macOS service preparation following the October 4 release.

[2026-10-05 release review](2026-10-05-release-review.md) maps all ten merged
commits after the baseline through `c5ecf86801011576468737d2a1d349be7a5e3999`.
It records new versus retained fixes, the failed prior attempt, upgrade checks
and uncompleted publication gates. It is a preparation record, not evidence
that a new tag, verified artifact or successful promotion already exists.

Community contributions and approved security-reporter credits are recorded in
[ACKNOWLEDGMENTS.md](../../ACKNOWLEDGMENTS.md). Keep private reports anonymous
unless attribution is approved; do not publish contact details or correspondence.

## 中文说明

下一版本的中英双语 Release 正文以根目录 [RELEASE_NOTES.md](../../RELEASE_NOTES.md)
为准。本次范围、兼容检查与待执行验收见[10 月 8 日发布核对记录](2026-10-08-release-review.md)，
范围已补齐至 `458b54c`：生命周期迁移与版本恢复、分层元数据与覆盖写入保护、
桶配置修订与 IAM 自助轮换、存储类型同步，以及此前账户信息、对象条件写入、CLI、
SDK/kits、Mint 安装器、镜像刷新与文档更新。当前回归与 `6f6d083` 历史兼容检查分开记录。
[10 月 8 日项目现状分析](../development/project-status-20261008.md)是 `8ead9fb` 的历史快照，
其实现范围已纳入当前 `458b54c` 发布准备；正式发布与部署验收仍以核对记录中的待办为准。
上方 10 月 5 日文章及 10 月 5 日、7 日核对记录作为历史材料保留，不能代替当前正文。
正式发布时再确定新标签和链接，不复用旧版标签。
64 MiB 是内部缓冲 RPC 限制，不是 S3 对象大小限制；仅当清单有效且固定标签摘要一致时，
才能单独恢复晋升。清单损坏或缺失，应修复后使用新标签完整发布。
详细操作见[维护者发布指南](../releasing.md)，包括凭据配置、产物验证、失败恢复及
单独提升 `latest` 的流程。仅合并文档不代表版本已经发布。

发布前核对[项目致谢记录](../../ACKNOWLEDGMENTS.md)中的贡献归属和反馈者批准的署名；
未经同意的私人报告继续匿名，不公开联系方式或邮件原文。
