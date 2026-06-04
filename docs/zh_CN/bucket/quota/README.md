# 存储桶配额配置快速入门指南

![quota](https://raw.githubusercontent.com/soulteary/OtterIO/main/docs/zh_CN/bucket/quota/bucketquota.png)

存储桶有两种配额类型可供选择，分别是FIFO和Hard。

- `Hard` 表示达到配置的配额限制后，禁止向存储桶写入数据。
- `FIFO` 会自动删除最旧的内容，直到存储桶的空间使用在限制范围内，同时允许写入。

> 注意：网关或独立单磁盘模式下不支持存储桶配额。

## 前置条件
- 安装OtterIO - [OtterIO快速入门指南](https://docs.min.io/cn/minio-quickstart-guide).
- [`mc`和OtterIO Server一起使用](https://docs.min.io/cn/minio-client-quickstart-guide)

## 设置存储桶配额

### 在OtterIO对象存储上，设置存储桶`mybucket`的额度为1GB，配额类型为hard:

```sh
$ mc admin bucket quota myotterio/mybucket --hard 1gb
```

### 将OtterIO上的存储桶"mybucket"的额度设置为5GB，配额类型为FIFO，这样就会自动删除较旧的内容，以确保存储桶的空间使用保持在5GB以内

```sh
$ mc admin bucket quota myotterio/mybucket --fifo 5gb
```

### 验证OtterIO上的存储桶`mybucket`的配额设置

```sh
$ mc admin bucket quota myotterio/mybucket
```

### 清除OtterIO上的存储桶`mybucket`的配额设置

```sh
$ mc admin bucket quota myotterio/mybucket --clear
```
