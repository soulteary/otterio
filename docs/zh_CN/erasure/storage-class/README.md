# OtterIO 存储类型

在纠删码模式下，本地存储类型决定 OtterIO 为每个对象写入多少个数据分片和奇偶校验分片，控制的是[纠删码集合](../README.md)内的里德-所罗门冗余。生命周期转移中的 `StorageClass` 表示远端目标标签，使用[另一套配置](../../bucket/lifecycle/README.md#将数据转移到远端存储桶)。

## 默认值和允许范围

上传请求的 `x-amz-storage-class` 头接受 `STANDARD` 或 `REDUCED_REDUNDANCY`。未提供该头时使用 `STANDARD`。

一个纠删码集合包含 `N` 块盘时，配置的奇偶校验数 `P` 必须是 2 到 `floor(N/2)` 之间的整数。同时配置两种类型时，`STANDARD` 的奇偶校验数必须大于或等于 `REDUCED_REDUNDANCY`。二者相等是有效配置；4 盘集合也可以使用两种类型，此时都为 `EC:2`。

未显式配置 `STANDARD` 时，默认值为：

- 每集合 4–5 块盘：`EC:2`。
- 每集合 6–7 块盘：`EC:3`。
- 每集合 8–16 块盘：`EC:4`。

`REDUCED_REDUNDANCY` 默认使用 `EC:2`。`N` 是**单个集合**的盘数，不是所有存储池的硬盘总数。配置值必须适用于部署中的每一种集合大小。

## 容量和故障容忍

对象的数据分片数为 `D = N - P`。编码后的近似大小为 `原始大小 × N / D`，尚未包含元数据、文件系统分配和其他开销。在 16 盘集合中保存 100 MiB 对象时：

- `EC:8`：8 个数据分片 + 8 个奇偶校验分片，约占 200 MiB。
- `EC:4`（`STANDARD` 默认值）：12 个数据分片 + 4 个奇偶校验分片，约占 133.3 MiB。
- `EC:2`：14 个数据分片 + 2 个奇偶校验分片，约占 114.3 MiB。

恢复对象需要 `D` 个健康分片。写入需要 `D` 块盘；数据与奇偶校验分片数量相等时需要 `D + 1` 块盘。增加奇偶校验数会减少可用容量，同时提高对分片丢失的容忍能力；容错范围应按对象及其集合计算，详见[法定数量示例](../README.md#数据奇偶校验和法定数量)。

## 配置奇偶校验数

在启动服务之前设置以下环境变量。对至少有 6 块盘的集合，这个示例让标准上传使用 3 个奇偶校验分片，低冗余上传使用 2 个：

```sh
export OTTERIO_STORAGE_CLASS_STANDARD=EC:3
export OTTERIO_STORAGE_CLASS_RRS=EC:2
```

对应的服务配置项为 `storage_class standard` 和 `storage_class rrs`，详见[配置指南](../../config/README.md#存储类型)。环境变量优先于已保存的配置。各节点应保持相同设置。修改只影响后续写入，不会重新编码已有对象版本。

## 上传时选择类型

先为 AWS CLI 配置 OtterIO 凭据和区域，再以低冗余类型上传一个已存在的本地文件：

```sh
aws --endpoint-url http://127.0.0.1:9000 s3api put-object \
  --bucket my-bucket --key my-testfile --body ./my-testfile \
  --storage-class REDUCED_REDUNDANCY
```

将桶名和端点替换为实际值，访问远端端点时使用 HTTPS。8 盘集合配置低冗余 `EC:2` 时，该对象使用 6 个数据分片和 2 个奇偶校验分片。[OtterIO Go SDK](https://github.com/soulteary/otterio-sdk)也提供 `PutObjectOptions.StorageClass` 上传选项。

OtterIO 的当前行为以[奇偶校验数验证](../../../../cmd/config/storageclass/storage-class.go)和[默认奇偶校验数](../../../../cmd/format-erasure.go)实现为准。[AWS CLI 上传文档](https://docs.aws.amazon.com/cli/latest/reference/s3api/put-object.html)用于了解客户端语法，其中 AWS 的存储层级选项不会扩展 OtterIO 支持的本地存储类型。
