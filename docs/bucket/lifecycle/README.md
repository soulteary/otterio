# Bucket Lifecycle Configuration

OtterIO applies bucket lifecycle rules to expire current objects, remove noncurrent versions and eligible delete markers, and transition object data to a configured remote bucket. These actions run in the background; reaching a deadline makes an object eligible for processing, rather than guaranteeing completion at that instant.

## Prerequisites

- Install and configure OtterIO using the [Quick Start](../../../README.md#quick-start).
- For the command examples, install the AWS CLI and configure credentials and region for your OtterIO server. Replace `http://127.0.0.1:9000` and `testbucket` with your endpoint and an existing test bucket. Use HTTPS for remote endpoints.
- [Versioning](../versioning/README.md) is required for noncurrent-version rules. Lifecycle tier transitions currently require **native, single-pool, local erasure storage**. Filesystem mode, distributed erasure storage, multiple server pools, and gateways do not support configuring ILM targets or transition rules.

## Expire current objects

Save the following as `lifecycle.json`. It expires objects under `temp/` after seven days:

```json
{
  "Rules": [
    {
      "ID": "TempUploads",
      "Status": "Enabled",
      "Filter": { "Prefix": "temp/" },
      "Expiration": { "Days": 7 }
    }
  ]
}
```

Apply the configuration and read it back:

```sh
aws --endpoint-url http://127.0.0.1:9000 s3api put-bucket-lifecycle-configuration \
  --bucket testbucket --lifecycle-configuration file://lifecycle.json
aws --endpoint-url http://127.0.0.1:9000 s3api get-bucket-lifecycle-configuration \
  --bucket testbucket
```

Putting a lifecycle configuration replaces the existing configuration. Include all rules you intend to keep. Rule IDs must be unique, and a configuration may contain at most 1,000 rules.

`Expiration.Days` must be a positive integer. Alternatively, use `Expiration.Date` with an RFC 3339 timestamp at midnight UTC, such as `2030-01-01T00:00:00Z`; do not combine `Days` and `Date`. Day-based deadlines are rounded to the midnight UTC after the object's modification time plus the configured days. For example, an object modified on October 8 at 10:00 UTC with `Days: 7` becomes eligible on October 16 at 00:00 UTC.

In a versioning-enabled bucket, expiration of the current data version creates a delete marker. Removing the older data requires a noncurrent-version expiration rule; current-version expiration alone does not purge that history. Prefix and tag filters apply to every action in the rule.

## Remove noncurrent versions and expired delete markers

The following `lifecycle.json` removes versions under `user-uploads/` 365 days after they become noncurrent, and removes a delete marker under that prefix when it is the only remaining version:

```json
{
  "Rules": [
    {
      "ID": "RemoveOldVersions",
      "Status": "Enabled",
      "Filter": { "Prefix": "user-uploads/" },
      "NoncurrentVersionExpiration": { "NoncurrentDays": 365 }
    },
    {
      "ID": "RemoveExpiredDeleteMarkers",
      "Status": "Enabled",
      "Filter": { "Prefix": "user-uploads/" },
      "Expiration": { "ExpiredObjectDeleteMarker": true }
    }
  ]
}
```

`NoncurrentDays` counts from the successor version's modification time, when the older version became noncurrent. `ExpiredObjectDeleteMarker: true` is an eligibility condition, not an age-based delay, and cannot be combined with `Expiration.Days` or `Expiration.Date`. Object lock retention and legal hold can prevent permanent version deletion.

## Transition data to a remote bucket

Configure a bucket-scoped remote target with the OtterIO admin API before adding transition rules. [`madmin.AdminClient.SetRemoteTarget`](../../../pkg/madmin/remote-target-commands.go) accepts a `BucketTarget` with `Type: madmin.ILMService`, a nonempty `Label`, the destination endpoint and bucket, and destination credentials. The destination bucket must already exist; if the source bucket has versioning enabled, destination versioning must also be enabled. A transition cannot target the source bucket itself.

In S3 lifecycle XML, `StorageClass` names that target's label. It does not configure local Reed-Solomon parity; [local storage classes](../../erasure/storage-class/README.md) use a separate configuration. For a target labelled `ARCHIVE`, this rule transitions current objects under `logs/` after 30 days and noncurrent versions after 7 days:

```xml
<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Rule>
    <ID>ArchiveLogs</ID>
    <Status>Enabled</Status>
    <Filter><Prefix>logs/</Prefix></Filter>
    <Transition><Days>30</Days><StorageClass>ARCHIVE</StorageClass></Transition>
    <NoncurrentVersionTransition>
      <NoncurrentDays>7</NoncurrentDays>
      <StorageClass>ARCHIVE</StorageClass>
    </NoncurrentVersionTransition>
  </Rule>
</LifecycleConfiguration>
```

Send this XML through a signed S3 `PutBucketLifecycleConfiguration` request or an SDK that supports your target label. The server supports one current transition and one noncurrent transition per rule. Use a positive `Days` or a midnight-UTC `Date` for the current transition, and positive `NoncurrentDays` for the noncurrent transition. Delete markers are not transitioned. When multiple actions are due, object/version expiration takes precedence over restored-copy cleanup, which takes precedence over transition; within each action class, the earliest deadline wins.

OtterIO persists a destination reference with each source version before uploading: the target ARN, a unique remote key, the remote version ID where applicable, and the storage-class label. It commits the completed reference before removing local data. Pending transitions can resume during background scanning after a restart, even if the originating rule has been removed. Reads use the saved destination rather than choosing a target from the current rules.

Keep the destination bucket, its data, and its credentials available for as long as source versions reference it. Target removal or changes to its endpoint, bucket, or label are rejected while active rules or persisted transition references depend on it. Permanent deletion cleans up the remote object before removing the local reference; a failed remote cleanup leaves enough state for a retry. Creating a source delete marker retains the older version and its remote data. Legacy transition records without a saved destination require an unambiguous matching rule and target; missing or ambiguous configuration causes an error.

## Restore a transitioned version

Ordinary GET requests can read transitioned data through the source server from the remote bucket. `RestoreObject` creates a temporary local copy of a completed transition. It preserves the source version ID, modification time, ETag, and logical multipart layout, rather than creating another version.

```sh
aws --endpoint-url http://127.0.0.1:9000 s3api restore-object \
  --bucket testbucket --key logs/example.log --version-id VERSION_ID \
  --restore-request '{"Days":7}'
aws --endpoint-url http://127.0.0.1:9000 s3api head-object \
  --bucket testbucket --key logs/example.log --version-id VERSION_ID
```

Replace `VERSION_ID` with the version to restore, including a noncurrent data version. Omit `--version-id` for the current version of an unversioned object. A normal restore requires positive `Days`; **SELECT restore is not implemented**.

A new restore returns HTTP 202 and runs asynchronously. Inspect the `Restore` field from HEAD (`x-amz-restore`) for completion. A duplicate request while restoration is ongoing returns `RestoreAlreadyInProgress`; requesting an already-restored copy updates its expiry and returns HTTP 200. Restore request state is persisted so background scanning can retry unfinished restoration after a restart. When the local copy expires, scanning removes only that temporary copy and retains the remote data and source version, even if the transition rule has been deleted.

## References

- [OtterIO lifecycle rule evaluation](../../../pkg/bucket/lifecycle/lifecycle.go)
- [OtterIO transition and restore implementation](../../../cmd/bucket-lifecycle.go)
- [AWS CLI lifecycle request format](https://docs.aws.amazon.com/cli/latest/reference/s3api/put-bucket-lifecycle-configuration.html) and [restore command](https://docs.aws.amazon.com/cli/latest/reference/s3api/restore-object.html) (client syntax; AWS service capabilities differ from OtterIO)
