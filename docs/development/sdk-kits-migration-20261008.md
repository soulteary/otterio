# SDK and kits compatibility / SDK 与 kits 兼容说明

This describes the dependency migration merged in
[PR #31](https://github.com/soulteary/otterio/pull/31), included in the
[October 8 release preparation](../releases/2026-10-08-release-review.md).
The root module already uses the published SDK. This preparation also repairs
the Mint installer so its actual functional-test program uses the fixed SDK
declared in `mint/run/core/minio-go/go.mod`, without local replacements.

## Published modules

- `github.com/minio/minio-go/v7` → `github.com/soulteary/otterio-sdk/v7 v7.3.1`.
- `github.com/minio/crc64nvme` → `github.com/soulteary/otterio-kits/crc64nvme v1.1.2`.
- `github.com/minio/highwayhash` → `github.com/soulteary/otterio-kits/highwayhash v1.0.5`.
- `github.com/minio/md5-simd` → `github.com/soulteary/otterio-kits/md5-simd v1.1.3`.
- `github.com/minio/sha256-simd` → `github.com/soulteary/otterio-kits/sha256-simd v1.0.2`.
- `github.com/minio/simdjson-go` → `github.com/soulteary/otterio-kits/simdjson-go v0.4.6`.
- `github.com/minio/sio` → `github.com/soulteary/otterio-kits/sio v0.5.2`.

SDK `v7.3.1` resolves to
[`c11549d350d8d1f7474bc26037616e912f344c15`](https://github.com/soulteary/otterio-sdk/commit/c11549d350d8d1f7474bc26037616e912f344c15).
The six kits tags resolve to
[`d69b21c8f7aa2098a87c47002e4e65a1c3f55f57`](https://github.com/soulteary/otterio-kits/commit/d69b21c8f7aa2098a87c47002e4e65a1c3f55f57).
In the server root `go.mod`, `crc64nvme` and `md5-simd` are indirect; the other
four kits are direct requirements. The separate
`github.com/secure-io/sio-go v0.3.1` dependency remains unchanged.
The full root module graph still contains upstream
`github.com/minio/simdjson-go v0.4.5` through CoreDNS metadata. This entry is not
used by the compiled server package list; the server uses the kits module above.
Archived CLI baselines and historical upstream links may retain old module names;
they are records of the comparison source, not current dependency instructions.

## Mint functional harness

The former installer fetched `functional_tests.go` from upstream MinIO's latest
release, so the Mint manifest's fork requirement did not establish which SDK the
test program actually used. The installer now reads the Mint module's SDK pin,
downloads that checksum-verified Go module and copies its own
`functional_tests.go` from the module cache. The Mint module remains independent;
its direct requirements and checksums cover the real program's imports.

Installation disables parent Go workspaces, rejects a replaced SDK, builds with
`-mod=readonly`, and checks the binary inventory for the exact pinned fork SDK,
no upstream MinIO modules and no replacements. Offline installer regressions
and a real temporary Mint build with unchanged manifests are part of the
existing Go CI workflow. Compilation and inventory checks do not run the full
Mint functional suite, which requires a configured live server and credentials.

## Go integration changes

Update every SDK subpackage import as well as the root import, including
`pkg/credentials`, `pkg/encrypt` and `pkg/tags`. The SDK package identifier remains
`minio`, so existing call spelling can be retained with an import alias. An alias
does not preserve the identity of named types from the old module path.

Update both sides of exported signatures that use SDK or policy types, including
custom code embedding OtterIO server APIs. Rebuild all affected Go consumers.
For SDK custom hash callbacks, update the return type to the new kits
`md5simd.Hasher` from `github.com/soulteary/otterio-kits/md5-simd`; a callback
returning the old module's interface cannot simply be passed as a function
returning the new module's interface.

The CLI migration is a separate source API change. Custom gateways register a
factory that returns a fresh `*cli.Command` and fresh flags for each command
tree. Follow [CLI migration](../cli-migration.md) for the `StartGateway` signature,
parent/backend flag scope and integer parsing behavior. Test plugins with the
updated SDK and CLI together rather than updating only the import statements.

These modules require Go 1.27.1 for source builds. SDK and kits module tags are
separate from the server's UTC timestamp release tags. Users of the prebuilt
server do not need Go import changes or a data/configuration rewrite because of
the dependency migration. Verify existing encrypted data, multipart and copy
operations in staging; this migration is not an unconditional downgrade promise.
See [the release guide](../releasing.md) before publication or rollout.

## 中文迁移要点

上述版本均为已发布模块；主模块已完成迁移，本轮还修复 Mint 安装器实际取用的测试源码：
从 Mint 清单中的固定 SDK 模块获取经过校验的 `functional_tests.go`，补齐真实程序的
依赖与校验和，禁止构建时改写依赖，并检查产物中的 fork SDK 版本与模块来源。
这项构建和身份验证不代表需要在线服务与凭据的完整 Mint 功能用例已经运行。
Go 集成需要同时调整 SDK 根包、子包和自定义函数签名，并重新构建。SDK 仍使用 `minio`
包名，但别名不能保持旧模块命名类型的身份；自定义 hash 回调也要改用新 kits 的
`md5simd.Hasher`（引用路径为 `github.com/soulteary/otterio-kits/md5-simd`）。
自定义 gateway 还需按 [CLI 迁移说明](../cli-migration.md)
改用每次返回全新命令和参数的工厂。

源码工具链要求为 Go 1.27.1。预编译服务的用户无需修改 Go 引用；依赖迁移本身不要求
重写现有数据或配置。正式部署前仍需验证已有加密对象、分片与复制，不承诺无条件降级。
SDK 的 `v7.3.1`、kits 的模块版本和服务端的 UTC 时间戳发布标记各自独立。
