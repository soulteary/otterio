# Security and reliability update — October 2026

## Security

This release includes the SigV4 request-header coverage fix. Previously, an
otherwise valid delegated upload could be changed into a server-side copy by
adding an unsigned S3 operation header. The copy used the signing identity's
source-read permissions; reading the copied bytes additionally required access
to the signed destination, including destinations permitting anonymous reads.

The shared verifier now rejects unsigned S3/OtterIO operation headers with
HTTP 403 `AccessDenied` across presigned URLs, Authorization-header signatures,
and streaming seed signatures. Legitimate signed copy operations remain
supported. Payload-hash compatibility and matching signed query parameters are
preserved, and server-loaded object tags no longer mutate signed request headers.

The affected code is confirmed in `RELEASE.2026-06-07T11-32-46Z`. Upgrade to this
patched release and verify the exact image tag/digest or binary checksum; do not
assume an older locally cached `latest` image contains the fix.

Custom clients must supply S3 operation headers when signing, not append them
afterwards. Do not disable verification to bypass new rejections. No OtterIO CVE
identifier is asserted here; identifiers for similar issues in other projects
must not be represented as an OtterIO assignment.

## Other changes

- Restore Windows directory enumeration and startup, including bounded reads,
  filtering/count semantics, junction handling, and a real startup/S3 smoke test.
- Update vulnerable dependencies and require Go 1.26.6 or newer. Release builds
  select the latest available Go 1.26 patch; this is not a claim of zero risk.
- Make dynamic-timeout distribution tests deterministic with isolated seeded
  random generators, without changing production timeout behavior.
- Pin release binaries, container inputs and notes to the same tagged commit;
  require successful main CI before publication and publish GitHub Releases only
  after the image job succeeds.

## Acknowledgements

We thank the security researcher who privately reported the SigV4 header-coverage
issue and provided a detailed analysis and reproducible test case. Their work
helped improve OtterIO's security. We are withholding identifying and contact
information from this release announcement to protect their privacy.

## Upgrade notes

Back up data and configuration, test custom signing clients in staging, and
check Windows startup and normal S3 upload/download/copy behavior before broad
rollout. Review the repository's security documentation and remaining upstream
security backlog. This update does not certify that every inherited issue has
been resolved. Until upgraded, limit upload-signing identities to necessary
write operations and avoid granting them access to private source objects.

---

# 安全与可靠性更新 — 2026 年 10 月

本次版本修复 SigV4 请求头签名覆盖不完整的问题：此前，持有有效上传授权的一方，
可能通过额外的未签名 S3 操作请求头，将上传变成使用签名者源对象读取权限的服务端复制。
读取复制出的内容还需要目标对象的读取权限；允许匿名读取的目标也属于这种情况。

现在，预签名 URL、Authorization 签名与流式上传初始签名共用的校验逻辑，会拒绝未被
签名覆盖的 S3/OtterIO 操作请求头。合法签名的复制请求继续可用。自定义客户端应在
签名时提供相关请求头，不应在签名后追加，也不要通过关闭校验绕过拒绝。

同时修复 Windows 目录枚举与启动问题，更新存在安全问题的依赖，使动态超时测试
采用独立、固定种子的随机数生成器，并完善发布工作流的提交一致性和 CI 校验。

**特别感谢通过私下渠道报告此问题、提供详尽分析与可复现测试用例的安全研究者。**
这些反馈帮助 OtterIO 改进了安全性。为保护反馈者隐私，本次发布公告不披露其身份与
联系方式，也不公开邮件原文。

本说明不宣称 OtterIO 已获得专属 CVE 编号，不将其他项目的同类 CVE 作为本项目编号。
升级前请备份并在测试环境验证，使用明确的版本标签、镜像摘要或二进制校验和确认
实际运行的是修复版本。此次更新不代表所有历史安全问题均已解决。
