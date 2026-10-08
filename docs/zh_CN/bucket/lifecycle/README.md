# 存储桶生命周期配置

OtterIO 通过生命周期规则让当前对象过期、删除非当前版本和符合条件的删除标记，也可以将对象数据转移到配置好的远端存储桶。这些操作由后台执行；达到规则期限表示对象可以被处理，并不保证在该时刻立即完成。

## 前提条件

- 按照[快速入门](../../../../README.md#quick-start)安装和配置 OtterIO。
- 本页命令使用 AWS CLI，需要先配置访问 OtterIO 的凭据和区域。将 `http://127.0.0.1:9000` 和 `testbucket` 替换为实际端点及已存在的测试桶。访问远端端点时使用 HTTPS。
- 非当前版本规则要求启用[版本控制](../versioning/README.md)。生命周期分层转移目前仅支持**原生、单存储池、本地纠删码后端**。文件系统模式、分布式纠删码、多存储池和网关不支持配置 ILM 目标或转移规则。

## 让当前对象过期

将以下内容保存为 `lifecycle.json`，它让 `temp/` 前缀下的对象在七天后过期：

```json
{
  "Rules": [
    {
      "ID": "TempUploads",
      "Status": "Enabled",
      "Filter": { "Prefix": "temp/" },
      "Expiration": { "Days": 7 }
    }
  ]
}
```

写入配置并读回确认：

```sh
aws --endpoint-url http://127.0.0.1:9000 s3api put-bucket-lifecycle-configuration \
  --bucket testbucket --lifecycle-configuration file://lifecycle.json
aws --endpoint-url http://127.0.0.1:9000 s3api get-bucket-lifecycle-configuration \
  --bucket testbucket
```

写入生命周期配置会替换现有配置，需要包含所有要保留的规则。规则 ID 必须唯一，一份配置最多可以包含 1,000 条规则。

`Expiration.Days` 必须为正整数。也可以使用 `Expiration.Date` 指定 UTC 零点的 RFC 3339 时间，例如 `2030-01-01T00:00:00Z`，但不能同时设置 `Days` 和 `Date`。按天计算时，截止时间会取对象修改时间加上指定天数之后的下一个 UTC 零点。例如，对象在 10 月 8 日 10:00 UTC 修改，配置 `Days: 7`，则从 10 月 16 日 00:00 UTC 开始符合过期条件。

对于启用了版本控制的桶，当前数据版本过期会创建删除标记；清理历史数据还需要非当前版本过期规则，单独配置当前版本过期不会清空版本历史。规则中的前缀和标签过滤条件适用于该规则的每一项操作。

## 删除非当前版本和过期删除标记

以下 `lifecycle.json` 会删除 `user-uploads/` 下成为非当前版本已满 365 天的数据版本，并在该前缀下只剩一个删除标记时清理这个标记：

```json
{
  "Rules": [
    {
      "ID": "RemoveOldVersions",
      "Status": "Enabled",
      "Filter": { "Prefix": "user-uploads/" },
      "NoncurrentVersionExpiration": { "NoncurrentDays": 365 }
    },
    {
      "ID": "RemoveExpiredDeleteMarkers",
      "Status": "Enabled",
      "Filter": { "Prefix": "user-uploads/" },
      "Expiration": { "ExpiredObjectDeleteMarker": true }
    }
  ]
}
```

`NoncurrentDays` 从后继版本的修改时间开始计算，也就是旧版本成为非当前版本的时间。`ExpiredObjectDeleteMarker: true` 是清理条件，不是按天等待的期限，不能与 `Expiration.Days` 或 `Expiration.Date` 同时使用。对象锁定的保留期限和法律保留可能阻止永久删除版本。

## 将数据转移到远端存储桶

添加转移规则之前，先使用 OtterIO 管理 API 配置该桶的远端目标。[`madmin.AdminClient.SetRemoteTarget`](../../../../pkg/madmin/remote-target-commands.go) 接收 `BucketTarget`，需要设置 `Type: madmin.ILMService`、非空 `Label`、目标端点和桶名，以及目标凭据。目标桶必须已存在；源桶启用版本控制时，目标桶也必须启用版本控制。转移目标不能是源桶本身。

S3 生命周期 XML 中的 `StorageClass` 填写该目标的标签。它与本地里德-所罗门奇偶校验配置不同；[本地存储类型](../../erasure/storage-class/README.md)有独立的配置。假设目标标签为 `ARCHIVE`，以下规则会将 `logs/` 下的当前对象在 30 天后转移，将非当前版本在成为非当前版本 7 天后转移：

```xml
<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Rule>
    <ID>ArchiveLogs</ID>
    <Status>Enabled</Status>
    <Filter><Prefix>logs/</Prefix></Filter>
    <Transition><Days>30</Days><StorageClass>ARCHIVE</StorageClass></Transition>
    <NoncurrentVersionTransition>
      <NoncurrentDays>7</NoncurrentDays>
      <StorageClass>ARCHIVE</StorageClass>
    </NoncurrentVersionTransition>
  </Rule>
</LifecycleConfiguration>
```

通过签名的 S3 `PutBucketLifecycleConfiguration` 请求，或支持目标标签的 SDK 发送该 XML。服务端每条规则支持一个当前版本转移和一个非当前版本转移。当前版本使用正数 `Days` 或 UTC 零点的 `Date`，非当前版本使用正数 `NoncurrentDays`。删除标记不会被转移。同一对象有多个操作到期时，对象或版本过期优先于恢复副本清理，恢复副本清理优先于转移；同类操作选择最早的期限。

上传前，OtterIO 会在源版本元数据中持久化目标 ARN、唯一远端键、适用时的远端版本 ID 和存储类型标签；提交转移完成的引用后才移除本地数据。服务重启后，后台扫描可以继续处理待完成的转移，即使原来的规则已经被删除。读取使用保存的目标，而不是根据当前规则重新选择目标。

只要源版本仍然引用远端数据，就需要保持目标桶、数据和凭据可用。活动规则或持久化引用仍依赖某个目标时，服务端会拒绝删除该目标，或修改其端点、桶名和标签。永久删除会先清理远端对象再移除本地引用；远端清理失败时保留状态以便重试。为源对象创建删除标记会保留旧版本及其远端数据。没有保存目标的旧版转移记录，需要唯一匹配的规则和目标；配置缺失或有歧义时操作会报错。

## 恢复已转移的版本

普通 GET 请求可以通过源服务从远端桶读取已转移的数据。`RestoreObject` 为已完成转移的数据建立临时本地副本，保留源版本 ID、修改时间、ETag 和逻辑分片信息，不会创建新版本。

```sh
aws --endpoint-url http://127.0.0.1:9000 s3api restore-object \
  --bucket testbucket --key logs/example.log --version-id VERSION_ID \
  --restore-request '{"Days":7}'
aws --endpoint-url http://127.0.0.1:9000 s3api head-object \
  --bucket testbucket --key logs/example.log --version-id VERSION_ID
```

将 `VERSION_ID` 替换为要恢复的版本，也可以恢复非当前数据版本。操作未启用版本控制的对象时省略 `--version-id`。普通恢复要求正数 `Days`；**SELECT 恢复尚未实现**。

新恢复任务返回 HTTP 202，并在后台执行。通过 HEAD 的 `Restore` 字段（`x-amz-restore`）查看完成状态。恢复期间重复提交会返回 `RestoreAlreadyInProgress`；对尚未到期的已恢复副本再次提交会更新到期时间，并返回 HTTP 200。恢复请求状态会持久化，服务重启后后台扫描可以重试未完成的恢复。本地副本到期时只清理临时副本，保留远端数据和源版本，即使转移规则已被删除也会执行这项清理。

## 参考

- [OtterIO 生命周期规则计算](../../../../pkg/bucket/lifecycle/lifecycle.go)
- [OtterIO 转移和恢复实现](../../../../cmd/bucket-lifecycle.go)
- [AWS CLI 生命周期请求格式](https://docs.aws.amazon.com/cli/latest/reference/s3api/put-bucket-lifecycle-configuration.html)与[恢复命令](https://docs.aws.amazon.com/cli/latest/reference/s3api/restore-object.html)（用于了解客户端语法，AWS 服务能力与 OtterIO 不同）
