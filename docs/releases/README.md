# Release materials / 发布材料

The reviewed bilingual body for the next release is
[RELEASE_NOTES.md](../../RELEASE_NOTES.md). The workflow reads that root file from
the exact tagged commit; it does not automatically select a date-named article
or another file in this directory.

The [maintainer release guide](../releasing.md) covers tag creation, exact-commit
main CI, staged publication, artifact verification and separate digest promotion.
Preparing or merging these documents does not create a tag or publish artifacts.

## Current announcement draft

[OtterIO 新版本：补强存储安全边界，完善容器部署与发布](2026-10-05-announcement.zh-CN.md)
is the Chinese announcement prepared on 2026-10-05. Its baseline is the published
`RELEASE.2026-10-04T09-24-10Z`; it distinguishes the new changes from retained
security fixes. Choose and verify a fresh tag before publishing the article.
The document date is not the final release timestamp. The draft now includes
#26 (internal storage boundaries) and #27 (secret-filtered manifest outputs),
which were merged after the initial #25 preparation.

## Scope review and release checklist

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
为准。中文发布文章见上方草稿链接；正式发布时再确定新标签和链接，不复用旧版标签。
本次在 #25 上继续补齐 #26/#27；完整提交映射及待执行验收见[发布核对记录](2026-10-05-release-review.md)。
64 MiB 是内部缓冲 RPC 限制，不是 S3 对象大小限制；仅当清单有效且固定标签摘要一致时，
才能单独恢复晋升。清单损坏或缺失，应修复后使用新标签完整发布。
详细操作见[维护者发布指南](../releasing.md)，包括凭据配置、产物验证、失败恢复及
单独提升 `latest` 的流程。仅合并文档不代表版本已经发布。

发布前核对[项目致谢记录](../../ACKNOWLEDGMENTS.md)中的贡献归属和反馈者批准的署名；
未经同意的私人报告继续匿名，不公开联系方式或邮件原文。
