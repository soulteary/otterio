#!/usr/bin/env bash
# Run the existing S3 startup/round-trip probe against an immutable image.
set -euo pipefail
[[ "${1-}" =~ ^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$ ]] || { echo 'an immutable image digest is required' >&2; exit 1; }
export IMAGE_REF="$1"
export SMOKE_ROOT
SMOKE_ROOT="$(mktemp -d)"
export SMOKE_CONTAINER="otterio-release-smoke-${RANDOM}-${RANDOM}"
cleanup() {
  docker rm -f "$SMOKE_CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$SMOKE_ROOT"
}
trap cleanup EXIT
cat > "$SMOKE_ROOT/launcher" <<'SH'
#!/bin/sh
exec docker run --rm --name "$SMOKE_CONTAINER" --network host \
  --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -e OTTERIO_ACCESS_KEY -e OTTERIO_SECRET_KEY -e OTTERIO_BROWSER \
  --mount "type=bind,src=$SMOKE_ROOT,dst=$SMOKE_ROOT" \
  "$IMAGE_REF" "$@"
SH
chmod +x "$SMOKE_ROOT/launcher"
TMPDIR="$SMOKE_ROOT" go run buildscripts/verify-s3-startup.go "$SMOKE_ROOT/launcher"
