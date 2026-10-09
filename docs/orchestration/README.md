# OtterIO Deployment Quickstart Guide

OtterIO is a cloud-native application designed to scale in a sustainable manner in multi-tenant environments. Orchestration platforms provide perfect launchpad for OtterIO to scale. Below is the list of OtterIO deployment documents for various orchestration platforms:

| Orchestration platforms|
|:---|
| [`Docker Swarm`](https://github.com/soulteary/otterio/blob/main/docs/orchestration/docker-swarm/README.md) |
| [`Docker Compose`](https://github.com/soulteary/otterio/blob/main/docs/orchestration/docker-compose/README.md) |
| [`Kubernetes`](https://github.com/soulteary/otterio/blob/main/docs/orchestration/kubernetes/README.md) |

## Why is OtterIO cloud-native?
The term cloud-native revolves around the idea of applications deployed as micro services, that scale well. It is not about just retrofitting monolithic applications onto modern container based compute environment. A cloud-native application is portable and resilient by design, and can scale horizontally by simply replicating. Modern orchestration platforms like Swarm, Kubernetes and DC/OS make replicating and managing containers in huge clusters easier than ever.

While containers provide isolated application execution environment, orchestration platforms allow seamless scaling by helping replicate and manage containers. OtterIO extends this by adding isolated storage environment for each tenant.

OtterIO is built ground up on the cloud-native premise. With features like erasure-coding, distributed and shared setup, it focuses only on storage and does it very well. While, it can be scaled by just replicating OtterIO instances per tenant via an orchestration platform.

> In a cloud-native environment, scalability is not a function of the application but the orchestration platform.

In a typical modern infrastructure deployment, application, database, key-store, etc. already live in containers and are managed by orchestration platforms. OtterIO brings robust, scalable, AWS S3 compatible object storage to the lot.

See the [distributed deployment guide](../distributed/README.md) for topology requirements.
