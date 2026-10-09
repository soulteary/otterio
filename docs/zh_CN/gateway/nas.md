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
## 使用浏览器进行验证
使用你的浏览器访问`http://127.0.0.1:9000`,如果能访问，恭喜你，启动成功了。

![Screenshot](https://raw.githubusercontent.com/soulteary/OtterIO/main/docs/screenshots/otterio-browser-gateway.png)

## 使用`oc`进行验证
`oc`为ls，cat，cp，mirror，diff，find等UNIX命令提供了一种替代方案。它支持文件系统和兼容Amazon S3的云存储服务（AWS Signature v2和v4）。

### 设置`oc`
```
mc alias set mynas http://gateway-ip:9000 access_key secret_key
```

### 列举nas上的存储桶
```
mc ls mynas
[2017-02-22 01:50:43 PST]     0B ferenginar/
[2017-02-26 21:43:51 PST]     0B my-bucket/
[2017-02-26 22:10:11 PST]     0B test-bucket1/
```

## 了解更多
- [`oc`快速入门](https://github.com/soulteary/oc#readme)
- [使用 aws-cli](https://github.com/soulteary/otterio/blob/main/docs/README.md)
- [使用 otterio-go SDK](https://github.com/soulteary/otterio-sdk#readme)
