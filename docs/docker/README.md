# OtterIO Docker Quickstart Guide

## Prerequisites
Docker installed on your machine. Download the relevant installer from [here](https://www.docker.com/community-edition#/download).

## Pull the OtterIO Image
OtterIO publishes official container images to both Docker Hub and the GitHub Container Registry (GHCR). Pull whichever registry is most convenient:

```sh
# Docker Hub
docker pull soulteary/otterio:latest

# GitHub Container Registry (GHCR)
docker pull ghcr.io/soulteary/otterio:latest
```

The examples below use `soulteary/otterio:latest`; you can substitute `ghcr.io/soulteary/otterio:latest` anywhere the image name appears.

## Run Standalone OtterIO on Docker.
OtterIO needs a persistent volume to store configuration and application data. However, for testing purposes, you can launch OtterIO by simply passing a directory (`/data` in the example below). This directory gets created in the container filesystem at the time of container start. But all the data is lost after container exits.

```sh
docker run -p 9000:9000 \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" \
  soulteary/otterio:latest server /data
```

To create a OtterIO container with persistent storage, you need to map local persistent directories from the host OS to virtual config `~/.otterio` and export `/data` directories. To do this, run the below commands

#### GNU/Linux and macOS
```sh
docker run -p 9000:9000 \
  --name otterio1 \
  -v /mnt/data:/data \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" \
  soulteary/otterio:latest server /data
```

#### Windows
```sh
docker run -p 9000:9000 \
  --name otterio1 \
  -v D:\data:/data \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" \
  soulteary/otterio:latest server /data
```

## Run Distributed OtterIO on Docker
Distributed OtterIO can be deployed via [Docker Compose](https://docs.min.io/docs/deploy-minio-on-docker-compose) or [Swarm mode](https://docs.min.io/docs/deploy-minio-on-docker-swarm). The major difference between these two being, Docker Compose creates a single host, multi-container deployment, while Swarm mode creates a multi-host, multi-container deployment.

This means Docker Compose lets you quickly get started with Distributed OtterIO on your computer - ideal for development, testing, staging environments. While deploying Distributed OtterIO on Swarm offers a more robust, production level deployment.

## OtterIO Docker Tips

### OtterIO Custom Access and Secret Keys
To override OtterIO's auto-generated keys, you may pass secret and access keys explicitly as environment variables. OtterIO server also allows regular strings as access and secret keys.

#### GNU/Linux and macOS
```sh
docker run -p 9000:9000 --name otterio1 \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" \
  -v /mnt/data:/data \
  soulteary/otterio:latest server /data
```

#### Windows
```powershell
docker run -p 9000:9000 --name otterio1 \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" \
  -v D:\data:/data \
  soulteary/otterio:latest server /data
```

### Run OtterIO Docker as a regular user
Docker provides standardized mechanisms to run docker containers as non-root users.

#### GNU/Linux and macOS
On Linux and macOS you can use `--user` to run the container as regular user.

> NOTE: make sure --user has write permission to *${HOME}/data* prior to using `--user`.
```sh
mkdir -p ${HOME}/data
docker run -p 9000:9000 \
  --user $(id -u):$(id -g) \
  --name otterio1 \
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY" \
  -v ${HOME}/data:/data \
  soulteary/otterio:latest server /data
```

#### Windows
On windows you would need to use [Docker integrated windows authentication](https://success.docker.com/article/modernizing-traditional-dot-net-applications#integratedwindowsauthentication) and [Create a container with Active Directory Support](https://blogs.msdn.microsoft.com/containerstuff/2017/01/30/create-a-container-with-active-directory-support/)

> NOTE: make sure your AD/Windows user has write permissions to *D:\data* prior to using `credentialspec=`.

```powershell
docker run -p 9000:9000 \
  --name otterio1 \
  --security-opt "credentialspec=file://myuser.json"
  -e "OTTERIO_ROOT_USER=AKIAIOSFODNN7EXAMPLE" \
  -e "OTTERIO_ROOT_PASSWORD=wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY" \
  -v D:\data:/data \
  soulteary/otterio:latest server /data
```

### OtterIO Custom Access and Secret Keys using Docker secrets
To override OtterIO's auto-generated keys, you may pass secret and access keys explicitly by creating access and secret keys as [Docker secrets](https://docs.docker.com/engine/swarm/secrets/). OtterIO server also allows regular strings as access and secret keys.

```
echo "AKIAIOSFODNN7EXAMPLE" | docker secret create access_key -
echo "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" | docker secret create secret_key -
```

Create a OtterIO service using `docker service` to read from Docker secrets.
```
docker service create --name="otterio-service" --secret="access_key" --secret="secret_key" soulteary/otterio:latest server /data
```

Read more about `docker service` [here](https://docs.docker.com/engine/swarm/how-swarm-mode-works/services/)

#### OtterIO Custom Access and Secret Key files
To use other secret names follow the instructions above and replace `access_key` and `secret_key` with your custom names (e.g. `my_secret_key`,`my_custom_key`). Run your service with
```
docker service create --name="otterio-service" \
  --secret="my_access_key" \
  --secret="my_secret_key" \
  --env="OTTERIO_ROOT_USER_FILE=my_access_key" \
  --env="OTTERIO_ROOT_PASSWORD_FILE=my_secret_key" \
  soulteary/otterio:latest server /data
```
`OTTERIO_ROOT_USER_FILE` and `OTTERIO_ROOT_PASSWORD_FILE` also support custom absolute paths, in case Docker secrets are mounted to custom locations or other tools are used to mount secrets into the container. For example, HashCorp Vault injects secrets to `/vault/secrets`. With the custom names above, set the environment variables to
```
OTTERIO_ROOT_USER_FILE=/vault/secrets/my_access_key
OTTERIO_ROOT_PASSWORD_FILE=/vault/secrets/my_secret_key
```

### Retrieving Container ID
To use Docker commands on a specific container, you need to know the `Container ID` for that container. To get the `Container ID`, run

```sh
docker ps -a
```

`-a` flag makes sure you get all the containers (Created, Running, Exited). Then identify the `Container ID` from the output.

### Starting and Stopping Containers
To start a stopped container, you can use the [`docker start`](https://docs.docker.com/engine/reference/commandline/start/) command.

```sh
docker start <container_id>
```

To stop a running container, you can use the [`docker stop`](https://docs.docker.com/engine/reference/commandline/stop/) command.
```sh
docker stop <container_id>
```

### OtterIO container logs
To access OtterIO logs, you can use the [`docker logs`](https://docs.docker.com/engine/reference/commandline/logs/) command.

```sh
docker logs <container_id>
```

### Monitor OtterIO Docker Container
To monitor the resources used by OtterIO container, you can use the [`docker stats`](https://docs.docker.com/engine/reference/commandline/stats/) command.

```sh
docker stats <container_id>
```

## Explore Further

* [Deploy OtterIO on Docker Compose](https://docs.min.io/docs/deploy-minio-on-docker-compose)
* [Deploy OtterIO on Docker Swarm](https://docs.min.io/docs/deploy-minio-on-docker-swarm)
* [Distributed OtterIO Quickstart Guide](https://docs.min.io/docs/distributed-minio-quickstart-guide)
* [OtterIO Erasure Code QuickStart Guide](https://docs.min.io/docs/minio-erasure-code-quickstart-guide)
