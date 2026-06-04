#!/bin/sh
#
# MinIO Cloud Storage, (C) 2019 MinIO, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# If command starts with an option, prepend otterio.
if [ "${1}" != "otterio" ]; then
    if [ -n "${1}" ]; then
        set -- otterio "$@"
    fi
fi

## Look for docker secrets at given absolute path or in default documented location.
docker_secrets_env_old() {
    if [ -f "$OTTERIO_ACCESS_KEY_FILE" ]; then
        ACCESS_KEY_FILE="$OTTERIO_ACCESS_KEY_FILE"
    else
        ACCESS_KEY_FILE="/run/secrets/$OTTERIO_ACCESS_KEY_FILE"
    fi
    if [ -f "$OTTERIO_SECRET_KEY_FILE" ]; then
        SECRET_KEY_FILE="$OTTERIO_SECRET_KEY_FILE"
    else
        SECRET_KEY_FILE="/run/secrets/$OTTERIO_SECRET_KEY_FILE"
    fi

    if [ -f "$ACCESS_KEY_FILE" ] && [ -f "$SECRET_KEY_FILE" ]; then
        if [ -f "$ACCESS_KEY_FILE" ]; then
            OTTERIO_ACCESS_KEY="$(cat "$ACCESS_KEY_FILE")"
            export OTTERIO_ACCESS_KEY
        fi
        if [ -f "$SECRET_KEY_FILE" ]; then
            OTTERIO_SECRET_KEY="$(cat "$SECRET_KEY_FILE")"
            export OTTERIO_SECRET_KEY
        fi
    fi
}

docker_secrets_env() {
    if [ -f "$OTTERIO_ROOT_USER_FILE" ]; then
        ROOT_USER_FILE="$OTTERIO_ROOT_USER_FILE"
    else
        ROOT_USER_FILE="/run/secrets/$OTTERIO_ROOT_USER_FILE"
    fi
    if [ -f "$OTTERIO_ROOT_PASSWORD_FILE" ]; then
        SECRET_KEY_FILE="$OTTERIO_ROOT_PASSWORD_FILE"
    else
        SECRET_KEY_FILE="/run/secrets/$OTTERIO_ROOT_PASSWORD_FILE"
    fi

    if [ -f "$ROOT_USER_FILE" ] && [ -f "$SECRET_KEY_FILE" ]; then
        if [ -f "$ROOT_USER_FILE" ]; then
            OTTERIO_ROOT_USER="$(cat "$ROOT_USER_FILE")"
            export OTTERIO_ROOT_USER
        fi
        if [ -f "$SECRET_KEY_FILE" ]; then
            OTTERIO_ROOT_PASSWORD="$(cat "$SECRET_KEY_FILE")"
            export OTTERIO_ROOT_PASSWORD
        fi
    fi
}

## Set KMS_MASTER_KEY from docker secrets if provided
docker_kms_encryption_env() {
    if [ -f "$OTTERIO_KMS_MASTER_KEY_FILE" ]; then
        KMS_MASTER_KEY_FILE="$OTTERIO_KMS_MASTER_KEY_FILE"
    else
        KMS_MASTER_KEY_FILE="/run/secrets/$OTTERIO_KMS_MASTER_KEY_FILE"
    fi

    if [ -f "$KMS_MASTER_KEY_FILE" ]; then
        OTTERIO_KMS_MASTER_KEY="$(cat "$KMS_MASTER_KEY_FILE")"
        export OTTERIO_KMS_MASTER_KEY
    fi
}

## Legacy
## Set SSE_MASTER_KEY from docker secrets if provided
docker_sse_encryption_env() {
    SSE_MASTER_KEY_FILE="/run/secrets/$OTTERIO_SSE_MASTER_KEY_FILE"

    if [ -f "$SSE_MASTER_KEY_FILE" ]; then
        OTTERIO_SSE_MASTER_KEY="$(cat "$SSE_MASTER_KEY_FILE")"
        export OTTERIO_SSE_MASTER_KEY
    fi
}

# su-exec to requested user, if service cannot run exec will fail.
docker_switch_user() {
    if [ ! -z "${OTTERIO_USERNAME}" ] && [ ! -z "${OTTERIO_GROUPNAME}" ]; then
        if [ ! -z "${OTTERIO_UID}" ] && [ ! -z "${OTTERIO_GID}" ]; then
            groupadd -g "$OTTERIO_GID" "$OTTERIO_GROUPNAME" && \
                useradd -u "$OTTERIO_UID" -g "$OTTERIO_GROUPNAME" "$OTTERIO_USERNAME"
        else
            groupadd "$OTTERIO_GROUPNAME" && \
                useradd -g "$OTTERIO_GROUPNAME" "$OTTERIO_USERNAME"
        fi
        exec setpriv --reuid="${OTTERIO_USERNAME}" --regid="${OTTERIO_GROUPNAME}" --keep-groups "$@"
    else
        exec "$@"
    fi
}

## Set access env from secrets if necessary.
docker_secrets_env_old

## Set access env from secrets if necessary.
docker_secrets_env

## Set kms encryption from secrets if necessary.
docker_kms_encryption_env

## Set sse encryption from secrets if necessary. Legacy
docker_sse_encryption_env

## Switch to user if applicable.
docker_switch_user "$@"
