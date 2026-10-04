# Acknowledgments / 致谢

Thank you to everyone who helps improve OtterIO by reporting problems,
providing reproducible examples, investigating causes, proposing changes,
and submitting patches. We credit the specific contribution even when a
proposal is incorporated into a different implementation.

感谢通过问题报告、可复现示例、原因分析、改进建议和补丁帮助 OtterIO 的贡献者。
提案被后续实现采用时，即使原 PR 未直接合并，我们也保留其贡献记录。

## Community contributions / 社区贡献

| Contributor / 贡献者 | Contribution / 具体贡献 | References / 关联记录 |
| --- | --- | --- |
| [@luojiyin1987](https://github.com/luojiyin1987) | Submitted spelling corrections across command code, documentation, packages, and Mint compatibility tests. / 提交命令代码、文档、基础包及 Mint 兼容测试中的拼写改进。 | Issues [#2](https://github.com/soulteary/otterio/issues/2), [#4](https://github.com/soulteary/otterio/issues/4), [#6](https://github.com/soulteary/otterio/issues/6), [#8](https://github.com/soulteary/otterio/issues/8); PRs [#3](https://github.com/soulteary/otterio/pull/3), [#5](https://github.com/soulteary/otterio/pull/5), [#7](https://github.com/soulteary/otterio/pull/7), [#9](https://github.com/soulteary/otterio/pull/9) |
| [@929496959](https://github.com/929496959) | Reported Windows startup failures, investigated directory enumeration, supplied a proposed fix, and reported successful startup with that proposal. / 报告 Windows 启动故障，分析目录枚举原因，提供修复方案及该方案的启动验证结果。 | Issue [#10](https://github.com/soulteary/otterio/issues/10); proposal [comment](https://github.com/soulteary/otterio/issues/10#issuecomment-4951331988); fix [#12](https://github.com/soulteary/otterio/pull/12) |
| [@MikhailIzvekov](https://github.com/MikhailIzvekov) | Submitted a Windows directory-enumeration patch based on the proposal in #10. Its approach was retained and extended in #12. / 根据 #10 的方案提交 Windows 目录枚举补丁，其思路在 #12 中保留并完善。 | PR [#11](https://github.com/soulteary/otterio/pull/11), superseded by / 由后续实现取代 [#12](https://github.com/soulteary/otterio/pull/12) |
| An anonymous security researcher / 一位匿名安全研究者 | Privately reported the SigV4 request-header coverage flaw with detailed analysis and a reproducible test case. / 私下报告 SigV4 请求头签名覆盖缺陷，并提供详尽分析及可复现测试。 | Fix / 修复 [#13](https://github.com/soulteary/otterio/pull/13); privacy handling / 隐私处理 [#16](https://github.com/soulteary/otterio/pull/16); [security notes](docs/security/sigv4-header-coverage.md) |

## Detailed records / 详细记录

- [June 2026 / 2026 年 6 月](docs/acknowledgments/2026-06.md)
- [October 2026 / 2026 年 10 月](docs/acknowledgments/2026-10.md)
- [Record policy / 记录规则](docs/acknowledgments/README.md)

These records describe contributions and repository outcomes. A merged PR does
not by itself identify the first released version containing a change.

本记录说明贡献及仓库中的处理结果。PR 合并不等于已发布，首次包含改动的版本需另行核实。
