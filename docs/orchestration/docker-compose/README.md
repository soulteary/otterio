# Deploy OtterIO on Docker Compose

Docker Compose allows defining and running single host, multi-container Docker applications.

With Compose, you use a Compose file to configure OtterIO services. Then, using a single command, you can create and launch all the Distributed OtterIO instances from your configuration. Distributed OtterIO instances will be deployed in multiple containers on the same host. This is a great way to set up development, testing, and staging environments, based on Distributed OtterIO.

## 1. Prerequisites

* Familiarity with [Docker Compose](https://docs.docker.com/compose/overview/).
* Docker installed on your machine. Download the relevant installer from [here](https://www.docker.com/community-edition#/download).

## 2. Run Distributed OtterIO on Docker Compose

To deploy Distributed OtterIO on Docker Compose, please download [docker-compose.yaml](https://github.com/minio/minio/blob/master/docs/orchestration/docker-compose/docker-compose.yaml?raw=true) and [nginx.conf](https://github.com/minio/minio/blob/master/docs/orchestration/docker-compose/nginx.conf?raw=true) to your current working directory. Note that Docker Compose pulls the OtterIO Docker image, so there is no need to explicitly download OtterIO binary. Then run one of the below commands

### GNU/Linux and macOS

```sh
docker-compose pull
docker-compose up
```

### Windows

```sh
docker-compose.exe pull
docker-compose.exe up
```

Distributed instances are now accessible on the host at ports 9000, proceed to access the Web browser at http://127.0.0.1:9000/. Here 4 OtterIO server instances are reverse proxied through Nginx load balancing.

### Notes

* By default the Docker Compose file uses the Docker image for latest OtterIO server release. You can change the image tag to pull a specific [OtterIO Docker image](https://hub.docker.com/r/minio/minio/).

* There are 4 otterio distributed instances created by default. You can add more OtterIO services (up to total 16) to your OtterIO Compose deployment. To add a service
  * Replicate a service definition and change the name of the new service appropriately.
  * Update the command section in each service.
  * Add a new OtterIO server instance to the upstream directive in the Nginx configuration file.

  Read more about distributed OtterIO [here](https://docs.min.io/docs/distributed-minio-quickstart-guide).

### Explore Further
- [Overview of Docker Compose](https://docs.docker.com/compose/overview/)
- [OtterIO Docker Quickstart Guide](https://docs.min.io/docs/minio-docker-quickstart-guide)
- [Deploy OtterIO on Docker Swarm](https://docs.min.io/docs/deploy-minio-on-docker-swarm)
- [OtterIO Erasure Code QuickStart Guide](https://docs.min.io/docs/minio-erasure-code-quickstart-guide)
