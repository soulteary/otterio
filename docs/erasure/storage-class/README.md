# OtterIO Storage Classes

In erasure mode, local storage classes choose how many data and parity shards OtterIO writes for each object. They apply to Reed-Solomon redundancy within an [erasure set](../README.md). Lifecycle transition `StorageClass` values name remote target labels and use a [different configuration](../../bucket/lifecycle/README.md#transition-data-to-a-remote-bucket).

## Defaults and accepted values

The upload request header `x-amz-storage-class` accepts `STANDARD` or `REDUCED_REDUNDANCY`. If the header is absent, OtterIO uses `STANDARD`.

For an erasure set containing `N` drives, a configured parity count `P` must be an integer from 2 through `floor(N/2)`. When both classes are configured, `STANDARD` parity must be greater than or equal to `REDUCED_REDUNDANCY` parity. Equal parity is valid, including on a 4-drive set, where both classes use `EC:2`.

When `STANDARD` is not explicitly configured, its default is:

- 4–5 drives per set: `EC:2`.
- 6–7 drives per set: `EC:3`.
- 8–16 drives per set: `EC:4`.

`REDUCED_REDUNDANCY` defaults to `EC:2`. `N` is the number of drives **in one set**, rather than the total number of drives in all pools. Choose values valid for every set size in your deployment.

## Capacity and failure tolerance

An object has `D = N - P` data shards. Its approximate encoded size is `original size × N / D`, before metadata, filesystem allocation, and other overhead. For a 100 MiB object in a 16-drive set:

- `EC:8`: 8 data + 8 parity, approximately 200 MiB.
- `EC:4` (the `STANDARD` default): 12 data + 4 parity, approximately 133.3 MiB.
- `EC:2`: 14 data + 2 parity, approximately 114.3 MiB.

Recovering an object requires `D` healthy shards. Writing requires `D` drives, or `D + 1` when data and parity counts are equal. Increasing parity trades usable capacity for greater tolerance of missing shards; failure tolerance applies per object and per set. See [quorum examples](../README.md#data-parity-and-quorum).

## Configure parity

Set these environment variables before starting the server. For a set with at least six drives, this example uses 3 parity shards for standard uploads and 2 for reduced redundancy:

```sh
export OTTERIO_STORAGE_CLASS_STANDARD=EC:3
export OTTERIO_STORAGE_CLASS_RRS=EC:2
```

The corresponding server configuration keys are `storage_class standard` and `storage_class rrs`; see the [configuration guide](../../config/README.md#storage-class). Environment variables override saved configuration. Keep settings consistent across participating nodes. Changes affect subsequent writes and do not recode existing object versions.

## Choose the class on upload

With the AWS CLI configured for your OtterIO credentials and region, upload an existing local file with reduced redundancy:

```sh
aws --endpoint-url http://127.0.0.1:9000 s3api put-object \
  --bucket my-bucket --key my-testfile --body ./my-testfile \
  --storage-class REDUCED_REDUNDANCY
```

Replace the bucket and endpoint with your deployment values; use HTTPS for remote endpoints. For an 8-drive set with `EC:2` reduced redundancy, the object uses 6 data and 2 parity shards. The [OtterIO Go SDK](https://github.com/soulteary/otterio-sdk) also exposes `PutObjectOptions.StorageClass` for uploads.

See the implementation of [parity validation](../../../cmd/config/storageclass/storage-class.go) and [default parity](../../../cmd/format-erasure.go) for OtterIO's current behavior. The [AWS CLI upload reference](https://docs.aws.amazon.com/cli/latest/reference/s3api/put-object.html) describes client syntax; its AWS storage-tier options do not expand OtterIO's supported local classes.
