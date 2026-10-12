# OtterIO NAS网关
OtterIO网关使用NAS存储支持Amazon S3。你可以在同一个共享NAS卷上运行多个otterio实例，作为一个分布式的对象网关。

## 为NAS存储运行OtterIO网关
### 使用Docker
```
docker run -p 9000:9000 --name nas-s3 \
 -e "OTTERIO_ROOT_USER=otterio" \
 -e "OTTERIO_ROOT_PASSWORD=otterio123" \
 soulteary/otterio gateway nas /shared/nasvol
```

### 使用二进制
```
export OTTERIO_ROOT_USER=otterioaccesskey
export OTTERIO_ROOT_PASSWORD=otteriosecretkey
otterio gateway nas /shared/nasvol
```
## 使用 OC Web Console

OtterIO 网关不再提供 Web UI。可以使用下面的 `oc` 命令验证 S3 访问，或让独立的 [OC Web Console](https://github.com/soulteary/oc/blob/main/docs/zh_CN/console.md)连接网关的 S3 端点。

`--console-address` 用于配置独立的 Admin 监听器，不是 Web Console 地址。启用后，为 OC 配置实际的 S3 URL 和单独的 `--admin-url`，并保留部署现有的端口及 S3/Admin CA 设置，详见[拆分 S3 与 Admin 端口](../../../README_zh_CN.md#拆分-s3-与-admin-端口)。本机别名模式使用一个已配置的身份；多个独立浏览器用户需要 native IAM 或 OIDC 模式，以及各自真实的后端权限。可用操作取决于 NAS 网关支持的协议和配置。

## 使用`oc`进行验证
`oc`为ls，cat，cp，mirror，diff，find等UNIX命令提供了一种替代方案。它支持文件系统和兼容Amazon S3的云存储服务（AWS Signature v2和v4）。

### 设置`oc`
```
oc alias set mynas http://gateway-ip:9000 access_key secret_key
```

### 列举nas上的存储桶
```
oc ls mynas
[2017-02-22 01:50:43 PST]     0B ferenginar/
[2017-02-26 21:43:51 PST]     0B my-bucket/
[2017-02-26 22:10:11 PST]     0B test-bucket1/
```

## 了解更多
- [`oc`快速入门](https://github.com/soulteary/oc#readme)
- [使用 aws-cli](https://github.com/soulteary/otterio/blob/main/docs/README.md)
- [使用 otterio-go SDK](https://github.com/soulteary/otterio-sdk#readme)
