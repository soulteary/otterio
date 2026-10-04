# 发布指南 / Release guide

发布入口为 `.github/workflows/release.yml`，无需在本机安装 GoReleaser。
本次拟发布版本：`RELEASE.2026-10-04T07-00-00Z`。时间部分为 UTC，属于版本标识，不是定时任务。如果改用其他 tag，请同步重命名对应发布说明文件。

## 1. 合并与检查

合并发布准备 PR，确认 main 对应提交的 Go（含安全回归、Windows 启动、race）、Lint 等检查全部通过。检查发布说明覆盖的安全补丁已经合入。
GHCR 使用 GITHUB_TOKEN，工作流已声明 contents:write 和 packages:write；已有 GHCR package 应允许本仓库 Actions 写入。
如需发布 Docker Hub，在仓库 Settings → Secrets and variables → Actions 配置 DOCKERHUB_USERNAME 和 DOCKERHUB_TOKEN；两者缺一则只发布 GHCR。

## 2. 创建版本 tag

在干净的本地仓库执行：

```sh
git switch main
git pull --ff-only origin main
git status --short
TAG=RELEASE.2026-10-04T07-00-00Z
git tag -a "$TAG" -m "OtterIO security and compatibility release"
git push origin "$TAG"
```

确认 `git status --short` 无输出后再创建 tag。如已配置签名，可将 `git tag -a` 替换为 `git tag -s`；普通 annotated tag 不会获得 Verified 签名标记。
推送 tag 自动触发 Release。不要通过 GitHub 页面提前发布一个空 Release，也不要移动或覆盖已发布 tag。

## 3. 检查产物

在 Actions → Release 确认 Build binaries、Build and push image、Publish GitHub Release 均成功。
Release 应包含 6 个二进制与 1 个 SHA-256 文件。Linux/macOS 下载相应原始二进制后先核验、再重命名为 otterio；Windows 产物名称为 otterio-windows-amd64.exe。

```sh
# 下载所有二进制及校验文件后，在下载目录执行：
sha256sum -c otterio-RELEASE.2026-10-04T07-00-00Z-checksums.txt
# macOS 可用 shasum -a 256 -c 替代 sha256sum -c。
docker buildx imagetools inspect ghcr.io/soulteary/otterio:RELEASE.2026-10-04T07-00-00Z
docker run --rm ghcr.io/soulteary/otterio:RELEASE.2026-10-04T07-00-00Z --version
```

镜像应包含 linux/amd64、linux/arm64、linux/ppc64le。版本镜像与 latest 同时更新；生产部署建议固定版本 tag。先在测试环境验证普通 PUT/GET、预签名 PUT/GET、正常签名 CopyObject 和应用使用的 multipart 操作，再升级生产实例。

## 4. 失败后重试

先查看失败 job 日志。临时网络或 registry 故障可在原 Release run 选择 Re-run failed jobs。
也可 Actions → Release → Run workflow，选择 main，并填入已有完整 tag；工作流会检出该 tag，按同一版本构建并发布。此操作会更新 latest，避免重跑旧版本导致 latest 回退。
若需修改代码或发布流程，合入修正后使用新的版本 tag；重跑旧 tag 不会包含新提交。若同日第二次发布，可换用新的 UTC 时间戳，并复制、更新对应发布说明。

## English summary

Merge the preparation PR and require green main CI. Push an annotated (or signed) RELEASE timestamp tag. The workflow builds six binaries with checksums, publishes three Linux image architectures, then publishes the GitHub Release with curated notes plus generated changes. Docker Hub is optional and requires both secrets. Manual dispatch accepts an existing tag and checks out that tag; rerunning an older release also moves latest back to that version.
