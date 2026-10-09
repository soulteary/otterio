# Object version and tag authorization update

This release follows `RELEASE.2026-10-08T16-08-32Z` and includes main changes through `5995377f0ca859e521b9b7e860bbe85d457e7266` plus this release preparation.

## Changes

- Authorize GET/HEAD and ordinary/multipart copy against the actual selected source version. Current-object and historical-version read permissions are independent, including explicit `null` versions. Version listing requires `s3:ListBucketVersions`; `s3:ListBucket` no longer serves as a fallback.
- Derive existing-object tag policy conditions from stored tags on the selected object/version, filtering injected reserved condition values. Tagging XML and effective COPY/REPLACE tags supply the request-tag conditions for the data actually written. Recheck tag updates under the object write lock.
- Check authoritative backend metadata before cached GET authorization and reuse. HEAD, explicit versions, multipart and Range reads bypass the cache. Backend metadata failures do not fall back to offline cached reads. Guard Range, deletion-marker, tier-error and replica-fallback paths before exposing data or metadata.
- Advertise `X-Otterio-Version-Authorization: v1` only for supported initialized FS/erasure implementations and known cache configurations. See the [authorization acceptance record](https://github.com/soulteary/otterio/blob/main/docs/console-server-acceptance.md) for coverage and limits.
- Upgrade source builds and build images to Go 1.27.2, OtterIO SDK to v7.3.2, and five kits modules to their published maintenance versions. Keep sha256-simd at v1.0.2. Refresh server, browser and Mint test dependencies, modernize Mint package installation, and align SDK dependency and stringer checks.
- Allow more time for the complete race suite while retaining its required checks.

## Upgrade

Back up data and configuration, then verify normal S3 operations and restricted credentials in staging. Review policies that relied on current/history read permission equivalence or ListBucket fallback for version listing: grant each intended action explicitly. Test GET/HEAD, CopyObject and UploadPartCopy with current and historical versions, explicit null versions, denied versions and stored-tag conditions. Verify tagging changes and cached reads with restricted identities.

Cached GET now performs an authoritative metadata lookup; HEAD, version, multipart and Range reads bypass cache. Account for the additional backend requests. Authorization uses the metadata snapshot checked for that read; later tag changes do not revoke a response already in progress.

Source builds require Go 1.27.2; prebuilt binaries do not require a Go installation. Existing lifecycle/restore topology limits, credential migration and storage recovery requirements remain applicable. Verify published checksums, manifest source SHA and image digests using the [release guide](https://github.com/soulteary/otterio/blob/main/docs/releasing.md). Linux amd64 release smoke tests do not establish runtime acceptance on every cross-compiled platform.

---

# 对象版本与标签授权更新

本次更新接续 `RELEASE.2026-10-08T16-08-32Z`，包含主分支截至 `5995377f0ca859e521b9b7e860bbe85d457e7266` 的改动及本次发布准备。

## 本次变化

- GET/HEAD、普通复制及分片复制按实际选中的源版本授权。当前对象与历史版本读取权限相互独立，显式 `null` 版本也按历史版本检查。版本列表必须具有 `s3:ListBucketVersions`，不再回退到 `s3:ListBucket`。
- 已有对象标签条件取自所选对象／版本的实际存储标签，过滤保留条件值注入。请求标签条件使用解析后的标签 XML，以及 COPY／REPLACE 最终实际写入的标签；标签更新在对象写锁内再次检查权限。
- 缓存 GET 在授权及复用数据前查询后端权威元数据。HEAD、显式版本、分片及 Range 读取绕过缓存；后端元数据读取失败时不回退到离线缓存。修正 Range、删除标记、远端分层错误及副本回退路径的授权顺序，避免在授权前输出数据或元数据。
- 仅受支持且已初始化的 FS／纠删码实现与已知缓存配置声明 `X-Otterio-Version-Authorization: v1`。覆盖范围及限制见[授权验收记录](https://github.com/soulteary/otterio/blob/main/docs/console-server-acceptance.md)。
- 源码构建和构建镜像升级到 Go 1.27.2，OtterIO SDK 升级到 v7.3.2，五个 kits 模块使用已发布的维护版本，sha256-simd 保持 v1.0.2。更新服务端、浏览器与 Mint 测试依赖，调整 Mint 软件包安装流程，统一 SDK 依赖及 stringer 检查。
- 延长完整 race 测试的执行时间，保留必需检查。

## 升级前检查

先备份数据和配置，在测试环境验证普通 S3 操作及受限身份。检查依赖当前／历史版本权限隐式互通或 ListBucket 回退的策略，显式授予实际需要的动作。验证当前版本、历史版本、显式 null 版本、被拒绝版本及存储标签条件下的 GET/HEAD、CopyObject 和 UploadPartCopy，并以受限身份检查标签更新与缓存读取。

缓存 GET 增加权威元数据查询，HEAD、版本、分片和 Range 读取绕过缓存，应评估新增后端请求。读取授权以本次元数据快照为边界，后续标签变化不会撤销已经开始的响应。

源码构建要求 Go 1.27.2，运行预编译程序无需安装 Go。既有生命周期／恢复拓扑限制、凭据迁移与存储恢复要求继续适用。按[发布指南](https://github.com/soulteary/otterio/blob/main/docs/releasing.md)核对校验和、清单源码 SHA 和镜像摘要。Linux amd64 发布冒烟测试不能代替其他交叉编译平台的运行验收。
