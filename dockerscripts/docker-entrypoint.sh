#!/bin/sh
#
# MinIO Cloud Storage, (C) 2019 MinIO, Inc.
# Modifications and additions (C) 2026 soulteary, https://github.com/soulteary/otterio
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#     http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eu

fail() {
    printf 'OtterIO entrypoint: %s\n' "$*" >&2
    exit 1
}

# Names are fixed at the call sites. Values are never evaluated as shell code.
# Historical image-default filenames remain optional when absent. Every other
# nonempty _FILE setting is required; an existing default file is validated too.
file_env() {
    var="$1"
    default_file="$2"
    eval "value=\${${var}-}"
    eval "file=\${${var}_FILE-}"
    [ -n "$file" ] || return 0
    case "$file" in
        /*) path="$file" ;;
        *)
            if [ -f "$file" ]; then path="$file"; else path="/run/secrets/$file"; fi
            ;;
    esac
    if [ ! -e "$path" ] && [ "$file" = "$default_file" ]; then
        return 0
    fi
    [ -f "$path" ] && [ -r "$path" ] || fail "${var}_FILE must name a readable regular file"
    [ -z "$value" ] || fail "set either $var or ${var}_FILE, not both"
    value="$(cat -- "$path")" || fail "cannot read ${var}_FILE"
    [ -n "$value" ] || fail "${var}_FILE must not be empty"
    export "$var=$value"
    unset "${var}_FILE"
}

validate_credentials() {
    # Match cmd/common-main.go: the modern pair wins when either is set.
    if [ "${OTTERIO_ROOT_USER+x}" = x ] || [ "${OTTERIO_ROOT_PASSWORD+x}" = x ]; then
        root_user="${OTTERIO_ROOT_USER-}"
        root_password="${OTTERIO_ROOT_PASSWORD-}"
    else
        root_user="${OTTERIO_ACCESS_KEY-}"
        root_password="${OTTERIO_SECRET_KEY-}"
    fi
    if [ -z "$root_user" ] && [ -z "$root_password" ]; then
        [ "${OTTERIO_ALLOW_DEFAULT_CREDENTIALS-}" = 1 ] || fail "configure OTTERIO_ROOT_USER and OTTERIO_ROOT_PASSWORD (or _FILE); see README_DOCKER_SECURITY.md"
    elif [ -z "$root_user" ] || [ -z "$root_password" ]; then
        fail "both username and password are required; do not mix incomplete modern and legacy pairs"
    elif [ "$root_password" != otterioadmin ]; then
        return 0
    else
        [ "${OTTERIO_ALLOW_DEFAULT_CREDENTIALS-}" = 1 ] || fail "default credentials are disabled; configure a non-default password"
    fi
    printf '%s\n' 'WARNING: default credentials explicitly enabled for local development only.' >&2
}

docker_switch_user() {
    if [ -n "${OTTERIO_USERNAME-}${OTTERIO_GROUPNAME-}${OTTERIO_UID-}${OTTERIO_GID-}" ]; then
        [ -n "${OTTERIO_USERNAME-}" ] && [ -n "${OTTERIO_GROUPNAME-}" ] || fail "OTTERIO_USERNAME and OTTERIO_GROUPNAME must be set together; alternatively use docker --user"
        if [ -n "${OTTERIO_UID-}${OTTERIO_GID-}" ]; then
            [ -n "${OTTERIO_UID-}" ] && [ -n "${OTTERIO_GID-}" ] || fail "OTTERIO_UID and OTTERIO_GID must be set together"
        fi
        if ! getent group "$OTTERIO_GROUPNAME" >/dev/null; then
            if [ -n "${OTTERIO_GID-}" ]; then groupadd -g "$OTTERIO_GID" "$OTTERIO_GROUPNAME"; else groupadd "$OTTERIO_GROUPNAME"; fi
        fi
        if ! getent passwd "$OTTERIO_USERNAME" >/dev/null; then
            if [ -n "${OTTERIO_UID-}" ]; then
                useradd -M -u "$OTTERIO_UID" -g "$OTTERIO_GROUPNAME" "$OTTERIO_USERNAME"
            else
                useradd -M -g "$OTTERIO_GROUPNAME" "$OTTERIO_USERNAME"
            fi
        fi
        [ -z "${OTTERIO_UID-}" ] || [ "$(id -u "$OTTERIO_USERNAME")" = "$OTTERIO_UID" ] || fail "existing user has a different UID"
        [ -z "${OTTERIO_GID-}" ] || [ "$(getent group "$OTTERIO_GROUPNAME" | cut -d: -f3)" = "$OTTERIO_GID" ] || fail "existing group has a different GID"
        exec setpriv --reuid="$OTTERIO_USERNAME" --regid="$OTTERIO_GROUPNAME" --clear-groups "$@"
    fi
    exec "$@"
}

# Inspect only leading flags and the command's first positional argument.
# Never mistake a flag value or a later data path for a request for help.
# Unknown options remain gated; the Go CLI is authoritative for their validity.
credential_check_required() (
    shift # executable
    command=
    backend=
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --help|-h) exit 1 ;;
            --version|-v)
                [ -z "$command" ] && exit 1
                exit 0
                ;;
            --config-dir|-C|--certs-dir|-S|--address|--console-address|--console-certs-dir)
                [ "$#" -ge 2 ] || exit 0
                shift # consume the value, even if it is "help" or "--help"
                ;;
            --config-dir=*|--certs-dir=*|--address=*|--console-address=*|--console-certs-dir=*|--quiet|--anonymous|--json|--compat|--no-compat) ;;
            --)
                shift
                if [ -z "$command" ] && [ "$#" -gt 0 ]; then
                    command="$1"
                    shift
                    continue
                fi
                break
                ;;
            -*) exit 0 ;;
            *)
                if [ -z "$command" ]; then
                    command="$1"
                    case "$command" in server|gateway) ;; *) exit 1 ;; esac
                elif [ "$command" = gateway ] && [ -z "$backend" ]; then
                    backend="$1"
                    case "$backend" in help|h) exit 1 ;; nas|s3) ;; *) exit 0 ;; esac
                else
                    break
                fi
                ;;
        esac
        shift
    done
    case "$command" in
        server)
            # serverCmdArgs prefers these environment endpoints over CLI args.
            [ -z "${OTTERIO_ARGS-}" ] && [ -z "${OTTERIO_ENDPOINTS-}" ] || exit 0
            if [ "$#" -eq 0 ] || [ "$1" = help ]; then exit 1; fi
            ;;
        gateway)
            if [ -z "$backend" ] && [ "$#" -eq 0 ]; then exit 1; fi
            ;;
        *) exit 1 ;;
    esac
    exit 0
)

[ "$#" -gt 0 ] || set -- otterio
if [ "$1" != otterio ]; then set -- otterio "$@"; fi

file_env OTTERIO_ACCESS_KEY access_key
file_env OTTERIO_SECRET_KEY secret_key
file_env OTTERIO_ROOT_USER access_key
file_env OTTERIO_ROOT_PASSWORD secret_key
file_env OTTERIO_KMS_MASTER_KEY kms_master_key
file_env OTTERIO_SSE_MASTER_KEY sse_master_key

if credential_check_required "$@"; then validate_credentials; fi

docker_switch_user "$@"
