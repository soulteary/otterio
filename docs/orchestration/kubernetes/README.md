# Deploy OtterIO on Kubernetes

OtterIO is a high performance distributed object storage server, designed for large-scale private cloud infrastructure. OtterIO is designed in a cloud-native manner to scale sustainably in multi-tenant environments. Orchestration platforms like Kubernetes provide perfect cloud-native environment to deploy and scale OtterIO.

## OtterIO Deployment on Kubernetes

There are multiple options to deploy OtterIO on Kubernetes:

- OtterIO-Operator: Operator offers seamless way to create and update highly available distributed OtterIO clusters. Refer [OtterIO Operator documentation](https://github.com/minio/minio-operator/blob/master/README.md) for more details.

- Helm Chart: OtterIO Helm Chart offers customizable and easy OtterIO deployment with a single command. Refer [OtterIO Helm Chart documentation](https://github.com/minio/charts) for more details.

## Monitoring OtterIO in Kubernetes

OtterIO server exposes un-authenticated liveness endpoints so Kubernetes can natively identify unhealthy OtterIO containers. OtterIO also exposes Prometheus compatible data on a different endpoint to enable Prometheus users to natively monitor their OtterIO deployments.

## Explore Further

- [OtterIO Erasure Code QuickStart Guide](https://docs.min.io/docs/minio-erasure-code-quickstart-guide)
- [Kubernetes Documentation](https://kubernetes.io/docs/home/)
- [Helm package manager for kubernetes](https://helm.sh/)
