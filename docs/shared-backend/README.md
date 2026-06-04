# Shared Backend OtterIO Quickstart Guide

OtterIO shared mode lets you use single [NAS](https://en.wikipedia.org/wiki/Network-attached_storage) (like NFS, GlusterFS, and other
distributed filesystems) as the storage backend for multiple OtterIO servers. Synchronization among OtterIO servers is taken care by design.
Read more about the OtterIO shared mode design [here](https://github.com/minio/minio/blob/master/docs/shared-backend/DESIGN.md).

OtterIO shared mode is developed to solve several real world use cases, without any special configuration changes. Some of these are

- You have already invested in NAS and would like to use OtterIO to add S3 compatibility to your storage tier.
- You need to use NAS with an S3 interface due to your application architecture requirements.
- You expect huge traffic and need a load balanced S3 compatible server, serving files from a single NAS backend.

With a proxy running in front of multiple, shared mode OtterIO servers, it is very easy to create a Highly Available, load balanced, AWS S3 compatible storage system.

# Get started

If you're aware of stand-alone OtterIO set up, the installation and running remains the same.

## 1. Prerequisites

Install OtterIO - [OtterIO Quickstart Guide](https://docs.min.io/docs/minio-quickstart-guide).

## 2. Run OtterIO on Shared Backend

To run OtterIO shared backend instances, you need to start multiple OtterIO servers pointing to the same backend storage. We'll see examples on how to do this in the following sections.

*Note*

- All the nodes running shared OtterIO need to have same access key and secret key. To achieve this, we export access key and secret key as environment variables on all the nodes before executing OtterIO server command.
- The drive paths below are for demonstration purposes only, you need to replace these with the actual drive paths/folders.

#### OtterIO shared mode on Ubuntu 16.04 LTS.

You'll need the path to the shared volume, e.g. `/path/to/nfs-volume`. Then run the following commands on all the nodes you'd like to launch OtterIO.

```sh
export OTTERIO_ROOT_USER=<ACCESS_KEY>
export OTTERIO_ROOT_PASSWORD=<SECRET_KEY>
otterio gateway nas /path/to/nfs-volume
```

#### OtterIO shared mode on Windows 2012 Server

You'll need the path to the shared volume, e.g. `\\remote-server\smb`. Then run the following commands on all the nodes you'd like to launch OtterIO.

```cmd
set OTTERIO_ROOT_USER=my-username
set OTTERIO_ROOT_PASSWORD=my-password
otterio.exe gateway nas \\remote-server\smb\export
```

*Windows Tip*

If a remote volume, e.g. `\\remote-server\smb` is mounted as a drive, e.g. `M:\`. You can use [`net use`](https://technet.microsoft.com/en-us/library/bb490717.aspx) command to map the drive to a folder.

```cmd
set OTTERIO_ROOT_USER=my-username
set OTTERIO_ROOT_PASSWORD=my-password
net use m: \\remote-server\smb\export /P:Yes
otterio.exe gateway nas M:\export
```

## 3. Test your setup

To test this setup, access the OtterIO server via browser or [`mc`](https://docs.min.io/docs/minio-client-quickstart-guide). You’ll see the uploaded files are accessible from the all the OtterIO shared backend endpoints.

## Explore Further
- [Use `mc` with OtterIO Server](https://docs.min.io/docs/minio-client-quickstart-guide)
- [Use `aws-cli` with OtterIO Server](https://docs.min.io/docs/aws-cli-with-minio)
- [Use `s3cmd` with OtterIO Server](https://docs.min.io/docs/s3cmd-with-minio)
- [Use `otterio-go` SDK with OtterIO Server](https://docs.min.io/docs/golang-client-quickstart-guide)
- [The OtterIO documentation website](https://docs.min.io)
