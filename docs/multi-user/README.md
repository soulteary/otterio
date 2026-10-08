# OtterIO Multi-user Quickstart Guide

OtterIO supports long-term IAM users in addition to the root credentials supplied at startup. A new user has no S3 permissions until a policy is attached directly or through an enabled group. This guide uses [OC](https://github.com/soulteary/oc) to create users, groups and policies.

## Prerequisites

- Install [OtterIO](../../README.md) and the current [OC client](https://github.com/soulteary/oc).
- Use root credentials, or an IAM account authorized for the corresponding administration operations.
- For gateway IAM or federation, see the [etcd guide](../sts/etcd.md). Available administration operations depend on the gateway backend and its configuration.

The examples use a single listener on port 9000. Configure an administrator alias:

```sh
oc alias set myotterio http://localhost:9000 "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" --api s3v4 --path on
```

If the server has a separate console listener on port 9001, add `--admin-url http://localhost:9001` when setting the alias. The management URL is a root URL without `/otterio/` or `/otterio/admin/v3`. OC uses OtterIO's `/otterio/admin/v3` management protocol; upstream `mc admin` compatibility is not assumed.

## Create a user with a policy

The built-in S3 policies are `writeonly`, `readonly` and `readwrite`; they apply across all buckets. `diagnostics` grants selected administration diagnostics, and `consoleAdmin` grants all administration and S3 operations. Use a custom policy when access should be limited to one bucket or prefix.

Create `getonly.json`. This policy permits object downloads from `my-bucketname`; it does not permit listing the bucket or uploading objects.

```sh
cat > getonly.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": ["s3:GetObject"],
      "Effect": "Allow",
      "Resource": ["arn:aws:s3:::my-bucketname/*"]
    }
  ]
}
EOF
```

Upload the policy, create the user, and attach the policy:

```sh
oc admin policy add myotterio getonly getonly.json
export OTTERIO_TEST_USER_PASSWORD="$(openssl rand -hex 32)"
# Save this password in your secret store before creating the account.
oc admin user add myotterio newuser "$OTTERIO_TEST_USER_PASSWORD"
oc admin policy set myotterio getonly user=newuser
```

Restore `OTTERIO_TEST_USER_PASSWORD` from the saved value when using another shell. To test downloading, first create the bucket and an object using the administrator alias:

```sh
oc mb myotterio/my-bucketname
printf 'hello OtterIO\n' > my-objectname
oc cp my-objectname myotterio/my-bucketname/my-objectname
oc alias set myotterio-newuser http://localhost:9000 newuser "$OTTERIO_TEST_USER_PASSWORD" --api s3v4 --path on
oc cat myotterio-newuser/my-bucketname/my-objectname
```

## Create a group

```sh
oc admin group add myotterio newgroup newuser
oc admin policy set myotterio getonly group=newgroup
```

Policies from direct user attachments and enabled group memberships are evaluated together. Disabling a group disables access granted through that group; it does not disable its users or their directly attached policies.

## Inspect and change access

```sh
oc admin user list myotterio
oc admin user info myotterio newuser
oc admin group list myotterio
oc admin group info myotterio newgroup
```

To replace a direct policy attachment with the built-in upload policy:

```sh
oc admin policy set myotterio writeonly user=newuser
oc admin policy set myotterio writeonly group=newgroup
```

`writeonly` is a built-in policy. A policy named `putonly` must be created explicitly before it can be attached.

## Disable, enable and remove accounts

Disable and re-enable a user or group:

```sh
oc admin user disable myotterio newuser
oc admin user enable myotterio newuser
oc admin group disable myotterio newgroup
oc admin group enable myotterio newgroup
```

Remove the user from the group before removing the empty group and user:

```sh
oc admin group remove myotterio newgroup newuser
oc admin group remove myotterio newgroup
oc admin user remove myotterio newuser
```

### Policy Variables
You can use policy variables in the *Resource* element and in string comparisons in the *Condition* element.

You can use a policy variable in the Resource element, but only in the resource portion of the ARN. This portion of the ARN appears after the 5th colon (:). You can't use a variable to replace parts of the ARN before the 5th colon, such as the service or account. The following policy might be attached to a group. It gives each of the users in the group read and write access to user-specific objects (their own "home directory") in OtterIO.

```
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": ["s3:ListBucket"],
      "Effect": "Allow",
      "Resource": ["arn:aws:s3:::mybucket"],
      "Condition": {"StringLike": {"s3:prefix": ["${aws:username}/*"]}}
    },
    {
      "Action": [
        "s3:GetObject",
        "s3:PutObject"
      ],
      "Effect": "Allow",
      "Resource": ["arn:aws:s3:::mybucket/${aws:username}/*"]
    }
  ]
}
```

If the user is authenticating using an STS credential which was authorized from OpenID connect we allow all `jwt:*` variables specified in the JWT specification, custom `jwt:*` or extensions are not supported.

List of policy variables for OpenID based STS.
```
"jwt:sub"
"jwt:iss"
"jwt:aud"
"jwt:jti"
"jwt:upn"
"jwt:name"
"jwt:groups"
"jwt:given_name"
"jwt:family_name"
"jwt:middle_name"
"jwt:nickname"
"jwt:preferred_username"
"jwt:profile"
"jwt:picture"
"jwt:website"
"jwt:email"
"jwt:gender"
"jwt:birthdate"
"jwt:phone_number"
"jwt:address"
"jwt:scope"
"jwt:client_id"
```

Following example shows OpenID users with read and write access to an OpenID user-specific directory (their own "home directory") in OtterIO.
```
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": ["s3:ListBucket"],
      "Effect": "Allow",
      "Resource": ["arn:aws:s3:::mybucket"],
      "Condition": {"StringLike": {"s3:prefix": ["${jwt:preferred_username}/*"]}}
    },
    {
      "Action": [
        "s3:GetObject",
        "s3:PutObject"
      ],
      "Effect": "Allow",
      "Resource": ["arn:aws:s3:::mybucket/${jwt:preferred_username}/*"]
    }
  ]
}
```

If the user is authenticating using an STS credential which was authorized from AD/LDAP we allow `ldap:*` variables, currently only supports `ldap:user`. Following example shows LDAP users read and write access to an LDAP user-specific directory (their own "home directory") in OtterIO.
```
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": ["s3:ListBucket"],
      "Effect": "Allow",
      "Resource": ["arn:aws:s3:::mybucket"],
      "Condition": {"StringLike": {"s3:prefix": ["${ldap:user}/*"]}}
    },
    {
      "Action": [
        "s3:GetObject",
        "s3:PutObject"
      ],
      "Effect": "Allow",
      "Resource": ["arn:aws:s3:::mybucket/${ldap:user}/*"]
    }
  ]
}
```

#### Common information available in all requests

- *aws:CurrentTime* - This can be used for conditions that check the date and time.
- *aws:EpochTime* - This is the date in epoch or Unix time, for use with date/time conditions.
- *aws:PrincipalType* - This value indicates whether the principal is an account (Root credential), user (OtterIO user), or assumed role (STS)
- *aws:SecureTransport* - This is a Boolean value that represents whether the request was sent over TLS.
- *aws:SourceIp* - This is the requester's IP address, for use with IP address conditions. If running behind Nginx like proxies, OtterIO preserve's the source IP.

```
{
  "Version": "2012-10-17",
  "Statement": {
    "Effect": "Allow",
    "Action": "s3:ListBucket*",
    "Resource": "arn:aws:s3:::mybucket",
    "Condition": {"IpAddress": {"aws:SourceIp": "203.0.113.0/24"}}
  }
}
```

- *aws:UserAgent* - This value is a string that contains information about the requester's client application. This string is generated by the client and can be unreliable. You can only use this context key from `oc` or OtterIO SDKs which standardize the User-Agent string.
- *aws:username* - This is a string containing the friendly name of the current user, this value would point to STS temporary credential in `AssumeRole`ed requests, instead use `jwt:preferred_username` in case of OpenID connect and `ldap:user` in case of AD/LDAP connect. *aws:userid* is an alias to *aws:username* in OtterIO.


## Explore Further
- [OC usage guide](https://github.com/soulteary/oc/blob/main/docs/usage.md)
- [OtterIO STS Quickstart Guide](../sts/README.md)
- [OtterIO Admin Multi-user Guide](admin/README.md)
