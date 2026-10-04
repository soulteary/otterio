# Acknowledgment records / 致谢记录

The [project index](../../ACKNOWLEDGMENTS.md) lists contributors once; monthly
records preserve the individual reports, proposals, and patches.

[项目总索引](../../ACKNOWLEDGMENTS.md)集中展示贡献者，月度记录保留每次有效反馈和改动。

## Recording contributions / 记录方式

- Describe the concrete help: reporting, reproducing, investigating, proposing,
  implementing, reviewing, or validating. Do not infer these roles from authorship alone.
- Distinguish an issue reporter from a PR author and from the author of a follow-up implementation.
- Retain credit when a useful proposal is superseded by another implementation;
  record that outcome without claiming the original PR was merged.
- Group duplicate issue/PR descriptions of one contribution. Keep all useful links.
- Use the month of merge or final disposition for monthly records. Preserve the
  original report/proposal dates where they help explain a longer history.
- Record the first released version only after checking the actual release tag
  and commit history. Until then use “not yet confirmed,” not a planned tag.
- Use public GitHub handles for public contributions. For private reports,
  retain anonymous acknowledgment unless the reporter agrees to attribution.
- Do not publish email addresses, private correspondence, attachments from
  private reports, or inferred real names.
- Credit contributions by people. Keep automated review/CI results and routine
  maintainer implementation history as supporting evidence rather than extra people.
- Testing-only issues with no substantive contribution do not require a permanent entry.

说明具体帮助，区分报告者、提案者和最终实现者。未合并的有效提案也可以致谢，
但必须准确记录结果。同一反馈的 Issue/PR 合并归档；发布版本必须以真实标签核实。
公开记录使用公开账号，私人反馈遵循署名意愿，不公开联系方式和邮件原文。

## Audit scope / 本次核查范围

Checked on 2026-10-04 against main commit
`7d3db27` and GitHub's public issue/PR/release records:

- 6 issues: #1, #2, #4, #6, #8, #10.
- 10 PRs: #3, #5, #7, #9, #11, #12, #13, #14, #15, #16.
- Issue comments, PR conversation comments, reviews and inline review comments.
- The files changed by the five community-authored PRs (#3, #5, #7, #9, #11).
- Current security and release notes, which retain anonymous security acknowledgment.

All 16 records were closed at the time of this check. Nine PRs were merged;
#11 was closed in favor of #12. The public history identifies three external
GitHub contributors. The private security report is acknowledged anonymously.

Issue #1 contains only “test,” followed by the author's explanation that it was
created by an AI process. It has no substantive contribution to record.
PRs #12–#16 were opened by the maintainer; their relevant outcomes are linked
to the originating contributions rather than treated as new community authors.

No additional human reviewer was found beyond the maintainer in the checked
review and comment records. Automated comments are not listed as people.

At the time of this check, the published GitHub releases were
`RELEASE.2026-06-04T03-00-56Z` and `RELEASE.2026-06-07T11-32-46Z`.
Both predate the merges recorded here. Therefore, a first published GitHub
release containing these changes has not been confirmed. This statement does
not establish what an independently built binary or container includes.

核查涵盖全部历史 Issue/PR 及其讨论、审查和社区补丁。#1 为测试记录；#12–#16
为维护者提交，作为处理结果引用。本次未发现维护者以外的其他人工审查贡献者。
已发布的 GitHub 版本均早于这些合并，故首次包含改动的发布版本暂未确认。

## Keeping records current / 后续维护

After a new contribution is accepted, update its monthly record and the root
index. Thank the reporter in the issue and the patch author in the PR, explaining
the outcome and linking the relevant change. For superseded proposals, acknowledge
the retained idea and link the replacement. After publication, add the verified
release version to each affected record.

Private reporter attribution preferences take precedence over earlier public
mentions. Existing historical disclosures are not permission to repeat an identity.
