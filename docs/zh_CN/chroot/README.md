# 在Chrooted环境中运行OtterIO

Chroot允许在标准的Linux上基于用户的namespace隔离。

## 1. 前置条件
* 熟悉 [chroot](http://man7.org/linux/man-pages/man2/chroot.2.html)
* 系统上已经安装Chroot

## 2. 在Chroot中安装OtterIO
```sh
mkdir -p /mnt/export/${USER}/bin
# Download and verify a reviewed OtterIO release for your platform first:
# https://github.com/soulteary/otterio/releases
: "${OTTERIO_BINARY:?Set the path to the verified OtterIO executable}"
install -m 0755 "$OTTERIO_BINARY" "/mnt/export/${USER}/bin/otterio"
chmod +x /mnt/export/${USER}/bin/otterio
```

将你的`proc`挂载绑定到目标chroot目录
```
sudo mount --bind /proc /mnt/export/${USER}/proc
```

## 3.在Chroot中运行单节点OtterIO
### GNU/Linux
```sh
sudo chroot --userspec username:group /mnt/export/${USER} /bin/otterio --config-dir=/.otterio server /data

Endpoint:  http://192.168.1.92:9000  http://65.19.167.92:9000
AccessKey: MVPSPBW4NP2CMV1W3TXD
SecretKey: X3RKxEeFOI8InuNWoPsbG+XEVoaJVCqbvxe+PTOa
...
...
```

现在可以在主机的9000端口访问实例，在浏览器中输入http://127.0.0.1:9000/即可访问

## 进一步探索
- [Otterio纠删码快速入门](https://github.com/soulteary/otterio/blob/main/docs/erasure/README.md)
- [使用`oc`](https://github.com/soulteary/oc#readme)
- [使用`aws-cli`](https://github.com/soulteary/otterio/blob/main/docs/zh_CN/README.md)
- [使用`s3cmd`](https://github.com/soulteary/otterio/blob/main/docs/zh_CN/README.md)
- [使用`otterio-go`SDK](https://github.com/soulteary/otterio-sdk#readme)
