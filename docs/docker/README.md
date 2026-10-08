# OtterIO Docker Quickstart Guide

[Documentation index](../README.md) · [简体中文](../zh_CN/docker/README.md)

## Prerequisites and images

Install [Docker Engine](https://docs.docker.com/engine/install/) or Docker Desktop with Linux containers enabled. OtterIO's published container images are Linux images, including when run from macOS or Windows. The release workflow builds Linux amd64, arm64 and ppc64le images.

Choose Docker Hub or the GitHub Container Registry (GHCR):

```sh
docker pull soulteary/otterio:latest
# Alternative registry:
docker pull ghcr.io/soulteary/otterio:latest
```

The examples use `soulteary/otterio:latest` for local evaluation. For deployments, select a reviewed release tag or digest; see [release verification](../releasing.md#4-verify-before-announcing). Substitute the GHCR name anywhere below if you use that registry.

## Start a persistent local server

Set a non-default credential pair once and save it securely. The Unix password-generation command requires OpenSSL. Reuse the same credentials when restarting the server or connecting a client.

### Linux and macOS

```sh
export OTTERIO_ROOT_USER=otterio-admin
OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
export OTTERIO_ROOT_PASSWORD

docker volume create otterio-data
docker run -d --name otterio \
  -p 127.0.0.1:9000:9000 -p 127.0.0.1:9001:9001 \
  -e OTTERIO_ROOT_USER -e OTTERIO_ROOT_PASSWORD \
  --mount source=otterio-data,target=/data \
  soulteary/otterio:latest server --console-address ":9001" /data
```

### Windows PowerShell 7.1 or newer

Enter the non-default credentials saved for this deployment. The named volume avoids host-path differences between Windows and Linux:

```powershell
$env:OTTERIO_ROOT_USER = Read-Host 'Root username'
$env:OTTERIO_ROOT_PASSWORD = Read-Host 'Root password' -MaskInput

docker volume create otterio-data
docker run -d --name otterio `
  -p 127.0.0.1:9000:9000 -p 127.0.0.1:9001:9001 `
  -e OTTERIO_ROOT_USER -e OTTERIO_ROOT_PASSWORD `
  --mount source=otterio-data,target=/data `
  soulteary/otterio:latest server --console-address ":9001" /data
```

The S3 endpoint is <http://127.0.0.1:9000> and the console/management endpoint is <http://127.0.0.1:9001>. Sign in using the configured credentials. [Verify an upload and download](../../README.md#verify) with OC; its alias can store both endpoints.

The named volume survives container removal. Recreating the container requires the same volume and credentials. For disposable testing, omit `--mount` and use `--rm` without `-d`; Docker removes the anonymous data volume when that container is removed. Use a separate container name and free ports if another instance is running.

## Credentials, secret files and non-root operation

The image entrypoint rejects missing, incomplete or default credential pairs. `OTTERIO_ROOT_USER` and `OTTERIO_ROOT_PASSWORD` take precedence as a pair over legacy `OTTERIO_ACCESS_KEY` and `OTTERIO_SECRET_KEY`. Do not mix half of each pair.

For secret files, set `OTTERIO_ROOT_USER_FILE` and/or `OTTERIO_ROOT_PASSWORD_FILE` to readable, nonempty regular files inside the container. An absolute path enforces presence. A nonempty environment value and an existing `_FILE` value for the same credential are an error. These variables are read by the container entrypoint, not by a bare-metal server binary.

For a non-root local deployment, use the repository's [secure Compose profile](../../README_DOCKER_SECURITY.md) and [docker-compose.secure.yml](../../docker-compose.secure.yml). It documents fixed UID/GID ownership, mounted password files and restart/upgrade considerations. If using `docker run --user` with a bind-mounted directory, make the directory writable by the selected numeric UID/GID first. Windows container Active Directory credential specifications do not apply to these Linux images.

### Docker Swarm secrets

On an initialized Swarm, the following Unix-shell example creates secrets from your saved credentials and starts one server. It leaves ports unpublished; configure private service networking or an appropriate ingress separately.

```sh
: "${OTTERIO_ROOT_USER:?Set your saved username first}"
: "${OTTERIO_ROOT_PASSWORD:?Set your saved password first}"
printf '%s' "$OTTERIO_ROOT_USER" | docker secret create otterio-root-user -
printf '%s' "$OTTERIO_ROOT_PASSWORD" | docker secret create otterio-root-password -

docker service create --name otterio \
  --secret otterio-root-user --secret otterio-root-password \
  --env OTTERIO_ROOT_USER_FILE=/run/secrets/otterio-root-user \
  --env OTTERIO_ROOT_PASSWORD_FILE=/run/secrets/otterio-root-password \
  --mount type=volume,source=otterio-data,target=/data \
  soulteary/otterio:latest server --console-address ":9001" /data
```

Read [Docker's Swarm secrets guide](https://docs.docker.com/engine/swarm/secrets/) for the secret lifecycle. This example is not a distributed storage deployment. A local volume belongs to one node; account for scheduling and storage placement before rescheduling a service. Follow [distributed deployment](../distributed/README.md) and [erasure coding](../erasure/README.md) for storage topology.

## Inspect and restart the container

```sh
docker ps -a
docker logs otterio
docker stats otterio
docker stop otterio
docker start otterio
```

`docker start` reuses the container's existing environment and mounts. Changing credentials in your shell does not change an existing container; recreate it with the intended environment and existing data volume. Back up data before upgrades or volume deletion.

## Build an image from this checkout

The root `Dockerfile` compiles the supplied build context, including local edits. From a cloned repository:

```sh
docker build --build-arg VCS_REF="$(git rev-parse HEAD)" -t otterio:local .
```

Replace the image name in the startup example with `otterio:local`. By comparison, `make docker` first builds a host binary and copies it through `Dockerfile.dev`; the binary must match the container's Linux architecture. The release workflow uses `Dockerfile.ci` with precompiled release binaries. `Dockerfile.release` clones remote source and does not build local edits.

## Further reading

- [Container credential migration and secure Compose profile](../../README_DOCKER_SECURITY.md)
- [Distributed deployment](../distributed/README.md) and [erasure coding](../erasure/README.md)
- [TLS](../tls/README.md), including certificate mounts
- [OC configuration and separate management endpoints](https://github.com/soulteary/oc/blob/main/docs/configuration.md)
- [Historical orchestration examples](../orchestration/README.md): review their images, credentials and topology against the current server before use
