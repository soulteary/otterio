# List of metrics reported cluster wide

Each metric includes a label for the server that calculated the metric.
Each metric has a label for the server that generated the metric.

These metrics can be from any OtterIO server once per collection.

| Name                                         | Description                                                                                                         |
|:---------------------------------------------|:--------------------------------------------------------------------------------------------------------------------|
| `otterio_bucket_objects_size_distribution`     | Distribution of object sizes in the bucket, includes label for the bucket name.                                     |
| `otterio_bucket_replication_failed_bytes`      | Total number of bytes failed at least once to replicate.                                                            |
| `otterio_bucket_replication_pending_bytes`     | Total bytes pending to replicate.                                                                                   |
| `otterio_bucket_replication_received_bytes`    | Total number of bytes replicated to this bucket from another source bucket.                                         |
| `otterio_bucket_replication_sent_bytes`        | Total number of bytes replicated to the target bucket.                                                              |
| `otterio_bucket_replication_pending_count`     | Total number of replication operations pending for this bucket.                                                     |
| `otterio_bucket_replication_failed_count`      | Total number of replication foperations failed for this bucket.                                                     |
| `otterio_bucket_usage_object_total`            | Total number of objects                                                                                             |
| `otterio_bucket_usage_total_bytes`             | Total bucket size in bytes                                                                                          |
| `otterio_cache_hits_total`                     | Total number of disk cache hits                                                                                     |
| `otterio_cache_missed_total`                   | Total number of disk cache misses                                                                                   |
| `otterio_cache_sent_bytes`                     | Total number of bytes served from cache                                                                             |
| `otterio_cache_total_bytes`                    | Total size of cache disk in bytes                                                                                   |
| `otterio_cache_usage_info`                     | Total percentage cache usage, value of 1 indicates high and 0 low, label level is set as well                       |
| `otterio_cache_used_bytes`                     | Current cache usage in bytes                                                                                        |
| `otterio_cluster_capacity_raw_free_bytes`      | Total free capacity online in the cluster.                                                                          |
| `otterio_cluster_capacity_raw_total_bytes`     | Total capacity online in the cluster.                                                                               |
| `otterio_cluster_capacity_usable_free_bytes`   | Total free usable capacity online in the cluster.                                                                   |
| `otterio_cluster_capacity_usable_total_bytes`  | Total usable capacity online in the cluster.                                                                        |
| `otterio_cluster_nodes_offline_total`          | Total number of OtterIO nodes offline.                                                                                |
| `otterio_cluster_nodes_online_total`           | Total number of OtterIO nodes online.                                                                                 |
| `otterio_heal_objects_error_total`             | Objects for which healing failed in current self healing run                                                        |
| `otterio_heal_objects_heal_total`              | Objects healed in current self healing run                                                                          |
| `otterio_heal_objects_total`                   | Objects scanned in current self healing run                                                                         |
| `otterio_heal_time_last_activity_nano_seconds` | Time elapsed (in nano seconds) since last self healing activity. This is set to -1 until initial self heal activity |
| `otterio_inter_node_traffic_received_bytes`    | Total number of bytes received from other peer nodes.                                                               |
| `otterio_inter_node_traffic_sent_bytes`        | Total number of bytes sent to the other peer nodes.                                                                 |
| `otterio_node_disk_free_bytes`                 | Total storage available on a disk.                                                                                  |
| `otterio_node_disk_total_bytes`                | Total storage on a disk.                                                                                            |
| `otterio_node_disk_used_bytes`                 | Total storage used on a disk.                                                                                       |
| `otterio_node_file_descriptor_limit_total`     | Limit on total number of open file descriptors for the OtterIO Server process.                                        |
| `otterio_node_file_descriptor_open_total`      | Total number of open file descriptors by the OtterIO Server process.                                                  |
| `otterio_node_io_rchar_bytes`                  | Total bytes read by the process from the underlying storage system including cache, /proc/[pid]/io rchar            |
| `otterio_node_io_read_bytes`                   | Total bytes read by the process from the underlying storage system, /proc/[pid]/io read_bytes                       |
| `otterio_node_io_wchar_bytes`                  | Total bytes written by the process to the underlying storage system including page cache, /proc/[pid]/io wchar      |
| `otterio_node_io_write_bytes`                  | Total bytes written by the process to the underlying storage system, /proc/[pid]/io write_bytes                     |
| `otterio_node_process_starttime_seconds`       | Start time for OtterIO process per node, time in seconds since Unix epoc.                                             |
| `otterio_node_process_uptime_seconds`          | Uptime for OtterIO process per node in seconds.                                                                       |
| `otterio_node_syscall_read_total`              | Total read SysCalls to the kernel. /proc/[pid]/io syscr                                                             |
| `otterio_node_syscall_write_total`             | Total write SysCalls to the kernel. /proc/[pid]/io syscw                                                            |
| `otterio_s3_requests_error_total`              | Total number S3 requests with errors                                                                                |
| `otterio_s3_requests_inflight_total`           | Total number of S3 requests currently in flight                                                                     |
| `otterio_s3_requests_total`                    | Total number S3 requests                                                                                            |
| `otterio_s3_time_ttbf_seconds_distribution`    | Distribution of the time to first byte across API calls.                                                            |
| `otterio_s3_traffic_received_bytes`            | Total number of s3 bytes received.                                                                                  |
| `otterio_s3_traffic_sent_bytes`                | Total number of s3 bytes sent                                                                                       |
| `otterio_software_commit_info`                 | Git commit hash for the OtterIO release.                                                                              |
| `otterio_software_version_info`                | OtterIO Release tag for the server                                                                                    |
