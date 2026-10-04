# Safe container startup and credentials

## Migration notice

Container `server` and `gateway` commands now require an explicit non-default
credential pair. Old quick-start commands that omit credentials intentionally
stop with an actionable error. Existing deployments with custom credentials
continue to work. This entrypoint policy does not change bare-metal binaries.
The image's default UID is not changed, so existing data ownership is preserved;
use the non-root profile below rather than silently changing existing volumes.

Modern `OTTERIO_ROOT_USER` / `OTTERIO_ROOT_PASSWORD` take precedence as a pair over
legacy `OTTERIO_ACCESS_KEY` / `OTTERIO_SECRET_KEY`, matching the server. Do not
combine half of each pair. Both modern and legacy pairs remain supported.

Each value can independently come from its environment variable or `_FILE`.
For example, a username environment variable and a password file work together.
An existing file and a nonempty environment value for the same variable are an
error, not an implicit override. Explicit file paths must be readable, regular
and nonempty. Errors do not print secret values. Trailing newlines are removed,
matching the previous entrypoint behavior. KMS/SSE files follow the same rules.

The historical image defaults (`access_key`, `secret_key`, `kms_master_key`,
`sse_master_key`) remain optional auto-discovery names when absent. Use an
absolute `_FILE` path when its presence must be enforced. Set `_FILE` to an empty
string to disable auto-discovery for that value.

## Safe local quick start

Use a reviewed release tag or digest that contains these changes:

```sh
export OTTERIO_IMAGE='ghcr.io/soulteary/otterio:YOUR_REVIEWED_RELEASE'
export OTTERIO_ROOT_USER='your-admin-name'
export OTTERIO_ROOT_PASSWORD_PATH="$PWD/secrets/root-password"
export OTTERIO_DATA_DIR="$PWD/data"
export OTTERIO_UID=10001 OTTERIO_GID=10001
install -d -m 0700 secrets
mkdir -p data
# Create a strong password in secrets/root-password without committing it.
# Make that file readable, and data writable, by the selected container UID/GID.
# On Linux, for a newly created dedicated directory/file only:
sudo chown 10001:10001 data secrets/root-password
sudo chmod 0700 data
sudo chmod 0400 secrets/root-password
docker compose -f docker-compose.secure.yml up -d
```

The profile runs as a fixed numeric non-root user, drops capabilities, prevents
privilege escalation, uses a read-only root filesystem plus a bounded temporary
filesystem, and binds S3/console to loopback only. A host reverse proxy can use
127.0.0.1:9000. A containerized Traefik should share a private Docker network with
OtterIO and connect to its service port; its own 127.0.0.1 is not the host. Do not
publish the console on the public Internet. Network controls do not replace IAM.

Local Compose secrets use bind mounts: ensure the file is actually readable by
the numeric container user; do not rely on Compose `uid`/`gid` remapping. Never
recursively chown an existing production volume without an ownership/migration
plan. UID/GID configuration here is Compose interpolation, not the legacy
username-creation entrypoint mechanism. Test backup, restore and upgrades before
using this profile for irreplaceable data. It is not a high-availability setup.

Only for an isolated local demo, `OTTERIO_ALLOW_DEFAULT_CREDENTIALS=1` explicitly
allows the default pair and prints a warning. Bind such a demo to loopback and
never use this option in production. An incomplete credential pair is still an
error even with the demo opt-in. `--help` and `--version` need no credentials.
