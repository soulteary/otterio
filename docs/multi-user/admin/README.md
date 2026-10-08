# OtterIO Admin Multi-user Quickstart Guide

An IAM user can receive administration permissions through a policy. Administration actions use the `admin:` namespace; S3 access uses separate `s3:` actions. This guide creates an account that can manage users and their policies.

## Prerequisites

Install [OtterIO](../../../README.md) and the current [OC client](https://github.com/soulteary/oc). Configure a root administrator alias for a single-port deployment:

```sh
oc alias set myotterio http://localhost:9000 "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" --api s3v4 --path on
```

If the server has a separate console listener on port 9001, add `--admin-url http://localhost:9001` to this alias command and the `admin1` alias command below. Management URLs are root URLs without `/otterio/` or `/otterio/admin/v3`. OC uses the OtterIO `/otterio/admin/v3` API; upstream `mc admin` compatibility is not assumed.

## Create the administration policy and user

Create `adminManageUser.json`:

```sh
cat > adminManageUser.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "admin:CreateUser",
        "admin:DeleteUser",
        "admin:CreatePolicy",
        "admin:AttachUserOrGroupPolicy"
      ],
      "Effect": "Allow"
    }
  ]
}
EOF
```

`CreatePolicy` permits uploading policies and `AttachUserOrGroupPolicy` permits attaching them. Both are required for the policy commands in the next section. An account allowed to create and attach arbitrary policies can grant broad permissions; only delegate these capabilities to trusted administrators. This example does not grant S3 object access.

Use the root alias to create the policy and user, then attach the policy:

```sh
oc admin policy add myotterio userManager adminManageUser.json
export OTTERIO_TEST_ADMIN_PASSWORD="$(openssl rand -hex 32)"
# Save this password in your secret store before creating the account.
oc admin user add myotterio admin1 "$OTTERIO_TEST_ADMIN_PASSWORD"
oc admin policy set myotterio userManager user=admin1
```

Restore `OTTERIO_TEST_ADMIN_PASSWORD` from the saved value when using another shell.

## Use the new administrator account

Create a second alias and a user:

```sh
oc alias set myotterio-admin1 http://localhost:9000 admin1 "$OTTERIO_TEST_ADMIN_PASSWORD" --api s3v4 --path on
export OTTERIO_TEST_USER_PASSWORD="$(openssl rand -hex 32)"
# Save this password in your secret store before creating the account.
oc admin user add myotterio-admin1 user1 "$OTTERIO_TEST_USER_PASSWORD"
```

Create a policy file using the [multi-user guide](../README.md#create-a-user-with-a-policy), then upload and attach it. For example, using the `getonly.json` file from that guide:

```sh
oc admin policy add myotterio-admin1 user1policy getonly.json
oc admin policy set myotterio-admin1 user1policy user=user1
```

`admin1` can delete this user with `oc admin user remove myotterio-admin1 user1`. Listing users, managing groups and changing server configuration require additional permissions from the list below.

## Supported administration permissions

The authoritative list is the `supportedAdminActions` map in [admin-action.go](../../../pkg/iam/policy/admin-action.go). The following actions can be used in policies in the current source tree.

### Configuration

- `admin:ConfigUpdate`

### Users and service accounts

- `admin:CreateUser`
- `admin:DeleteUser`
- `admin:ListUsers`
- `admin:EnableUser`
- `admin:DisableUser`
- `admin:GetUser`
- `admin:CreateServiceAccount`
- `admin:UpdateServiceAccount`
- `admin:RemoveServiceAccount`
- `admin:ListServiceAccounts`

### Groups

- `admin:AddUserToGroup`
- `admin:RemoveUserFromGroup`
- `admin:GetGroup`
- `admin:ListGroups`
- `admin:EnableGroup`
- `admin:DisableGroup`

### Policies

- `admin:CreatePolicy`
- `admin:DeletePolicy`
- `admin:GetPolicy`
- `admin:AttachUserOrGroupPolicy`
- `admin:ListUserPolicies`

### Diagnostics, healing and service control

- `admin:ServerInfo`
- `admin:StorageInfo`
- `admin:DataUsageInfo`
- `admin:TopLocksInfo`
- `admin:OBDInfo`
- `admin:Profiling`
- `admin:ServerTrace`
- `admin:ConsoleLog`
- `admin:BandwidthMonitor`
- `admin:KMSKeyStatus`
- `admin:Heal`
- `admin:ServiceRestart`
- `admin:ServiceStop`

### Bucket quotas and remote targets

- `admin:SetBucketQuota`
- `admin:GetBucketQuota`
- `admin:SetBucketTarget`
- `admin:GetBucketTarget`

### Full administration access

`admin:*` grants all administration operations, without independently granting S3 object access. The built-in `consoleAdmin` policy combines `admin:*` with `s3:*` on all buckets and objects. Attach it only when both kinds of access are intended.

## External identity providers

Administration permissions can also be granted through policies selected for STS credentials issued by an external identity provider. See the [STS guide](../../sts/README.md) and the provider-specific configuration before assigning administration actions.

## Explore Further

- [OtterIO multi-user guide](../README.md)
- [OC administration guide](https://github.com/soulteary/oc/blob/main/docs/administration.md)
- [OtterIO configuration guide](../../config/README.md)
