# Deploy OtterIO on Chrooted Environment

Chroot allows user based namespace isolation on many standard Linux deployments.

## 1. Prerequisites
* Familiarity with [chroot](http://man7.org/linux/man-pages/man2/chroot.2.html)
* Chroot installed on your machine.

## 2. Install OtterIO in Chroot
```sh
mkdir -p /mnt/export/${USER}/bin
# Download and verify a reviewed OtterIO release for your platform first:
# https://github.com/soulteary/otterio/releases
: "${OTTERIO_BINARY:?Set the path to the verified OtterIO executable}"
install -m 0755 "$OTTERIO_BINARY" "/mnt/export/${USER}/bin/otterio"
chmod +x /mnt/export/${USER}/bin/otterio
```

Bind your `proc` mount to the target chroot directory
```
sudo mount --bind /proc /mnt/export/${USER}/proc
```

## 3. Run Standalone OtterIO in Chroot
### GNU/Linux
```sh
sudo chroot --userspec username:group /mnt/export/${USER} /bin/otterio --config-dir=/.otterio server /data

Endpoint:  http://192.168.1.92:9000  http://65.19.167.92:9000
AccessKey: MVPSPBW4NP2CMV1W3TXD
SecretKey: X3RKxEeFOI8InuNWoPsbG+XEVoaJVCqbvxe+PTOa
...
...
```

Instance is now accessible on the host at port 9000, proceed to access the Web browser at http://127.0.0.1:9000/

## Explore Further
- [OtterIO Erasure Code QuickStart Guide](https://github.com/soulteary/otterio/blob/main/docs/erasure/README.md)
- [Use `oc` with OtterIO Server](https://github.com/soulteary/oc#readme)
- [Use `aws-cli` with OtterIO Server](https://github.com/soulteary/otterio/blob/main/docs/README.md)
- [Use `s3cmd` with OtterIO Server](https://github.com/soulteary/otterio/blob/main/docs/README.md)
- [Use OtterIO SDK with OtterIO Server](https://github.com/soulteary/otterio-sdk#readme)
