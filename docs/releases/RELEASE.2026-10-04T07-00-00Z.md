# RELEASE.2026-10-04T07-00-00Z

Security and compatibility release. Operators using presigned URLs should upgrade.

## Security

- Reject unsigned SigV4 operation headers, including injected `x-amz-copy-source`, so a presigned PUT cannot be repurposed into a server-side copy under the signer's identity (#13).
- Cover signed and presigned requests, query/header collisions, multipart copy, and legitimate signed copy operations with regressions. Custom clients must sign operation-affecting headers or use supported signed query parameters; adding such headers after signing is rejected.
- Raise the Go security baseline to 1.26.6 and update CoreDNS, gRPC, x/text and related dependencies (#12).

## Compatibility and reliability

- Restore Windows directory handling and server startup, with native directory regressions and an S3 startup smoke check (#12).
- Make dynamic-timeout test samples deterministic (#14).
- Include community spelling corrections across command code, documentation, packages and Mint compatibility tests (#3, #5, #7, #9).

## Acknowledgements / 致谢

- Thanks to [@luojiyin1987](https://github.com/luojiyin1987) for the command,
  documentation, package and Mint spelling improvements (#3, #5, #7, #9).
  感谢其提交命令代码、文档、基础包及 Mint 测试的拼写改进。
- Thanks to [@929496959](https://github.com/929496959) for the Windows startup
  report, diagnosis, proposed fix and reported startup validation (#10).
  感谢其提供 Windows 启动问题报告、诊断、方案及方案验证。
- Thanks to [@MikhailIzvekov](https://github.com/MikhailIzvekov) for submitting
  the Windows proposal as #11. Its approach was retained and extended in #12;
  #11 itself was closed without merge.
  感谢其提交 #11，相关思路在 #12 中保留并完善，原 PR 未直接合并。
- Special thanks to the security researcher who privately reported the SigV4
  issue with detailed analysis and a reproducible test case. We retain anonymous
  attribution and do not publish identifying or contact information.
  特别感谢私下报告 SigV4 问题、提供详尽分析及可复现测试的安全研究者；
  保留匿名致谢，不公开身份、联系方式或邮件原文。

See the [project acknowledgment records](https://github.com/soulteary/otterio/blob/main/ACKNOWLEDGMENTS.md)
for contribution details. No OtterIO-specific CVE assignment is claimed.
详细贡献见项目致谢记录；本说明不宣称 OtterIO 已获得专属 CVE 编号。

## Distribution

- Linux: amd64, arm64, ppc64le. macOS: amd64, arm64. Windows: amd64.
- Raw binaries plus SHA-256 checksums; no deb/rpm packages.
- Multi-architecture container images on GHCR; Docker Hub publication requires both configured credentials.
- Manual releases now check out the requested existing tag. GitHub Release publication waits for the container image job.

## 升级说明

本次修复预签名 PUT 被注入未签名操作头后转为复制操作的问题，建议使用预签名 URL 的部署升级。自定义客户端应在签名时包含会影响操作的请求头，不能在签名后追加这些头。

同时修复 Windows 目录读取及启动问题，更新 Go 安全基线与依赖。升级前备份配置和数据，在测试环境验证上传、下载、预签名 URL，以及正常签名的对象复制；LDAP 部署从早期版本升级时仍需阅读 [DN 迁移说明](../security/ldap-dn-normalization-migration.md)。

[Full changelog](https://github.com/soulteary/otterio/compare/RELEASE.2026-06-07T11-32-46Z...RELEASE.2026-10-04T07-00-00Z)
