# Deploy OtterIO on Kubernetes

[简体中文](../../zh_CN/orchestration/kubernetes/README.md) · [Documentation index](../../README.md)

This repository publishes OtterIO container images, but does not include an OtterIO Helm chart, Kubernetes Operator or tested Kubernetes deployment manifests. The MinIO Operator and charts linked by older versions of this page belong to upstream MinIO; they are not OtterIO installation channels. The old `helm install stable/otterio` command does not identify a chart provided by this project.

Start with the [Docker guide](../../docker/README.md) to verify the image, credentials and server arguments locally. A Kubernetes deployment needs manifests adapted to your storage, network and availability requirements; the preparation below is not Kubernetes acceptance testing.

## Prepare the workload

- **Image:** use `soulteary/otterio` or `ghcr.io/soulteary/otterio`, pinned to a reviewed release tag or digest. See the [release guide](../../releases/README.md) for published artifacts.
- **Credentials:** supply `OTTERIO_ROOT_USER` and `OTTERIO_ROOT_PASSWORD` from a Kubernetes Secret, or mount Secret files and set `OTTERIO_ROOT_USER_FILE` and `OTTERIO_ROOT_PASSWORD_FILE` to their container paths. These file variables are handled by the image entrypoint; preserve that entrypoint when setting container arguments. See [Docker credential rules](../../../README_DOCKER_SECURITY.md) and the Kubernetes [Secret documentation](https://kubernetes.io/docs/concepts/configuration/secret/).
- **Storage:** mount persistent storage at the paths passed to `server`. A disposable single-node instance can use `server /data`; distributed deployments need a planned disk and endpoint layout from the [distributed guide](../../distributed/README.md) and [erasure-coding guide](../../erasure/README.md). Increasing the replica count of a single-node workload does not create a distributed OtterIO cluster. Kubernetes [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) provide stable identities and persistent-volume association, but do not configure OtterIO's storage topology for you.
- **Ports:** with `server --address :9000 --console-address :9001 /data`, S3 uses port 9000 and the console/Admin API uses port 9001. Define Services and access controls for the intended traffic, and configure OC with separate S3 and management addresses as described in the [server README](../../../README.md#run-s3-and-web-console-on-separate-ports).
- **TLS:** mount PEM certificates with the filenames and paths in the [TLS guide](../../tls/README.md). A separate console certificate directory requires a separate console listener.
- **Health:** process probes are exposed on the S3 listener at `/otterio/health/live` and `/otterio/health/ready`. They do not establish distributed read/write quorum; the [healthcheck guide](../../metrics/healthcheck/README.md) describes cluster probes. Choose timeouts and startup budgets for your deployment using the Kubernetes [probe documentation](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).

## Validate before using stored data

Verify credential loading, volume permissions, TLS trust and both client endpoints in a disposable deployment. Exercise object upload and download, Pod restart with persistent data, and the failure/recovery behavior expected from your chosen topology. A passing local Docker example does not establish Kubernetes availability or upgrade compatibility.

See [Prometheus monitoring](../../metrics/prometheus/README.md), [OC connection and transfer examples](https://github.com/soulteary/oc#connect-and-transfer-a-file), and [Kubernetes documentation](https://kubernetes.io/docs/home/) for the next steps.
