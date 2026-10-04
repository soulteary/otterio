#!/usr/bin/env bash
# Never treat an authentication or network failure as proof that a tag is absent.
set -euo pipefail
[ "$#" -gt 0 ] || { echo 'at least one versioned image reference is required' >&2; exit 1; }
error_file="$(mktemp)"
trap 'rm -f "$error_file"' EXIT
for reference in "$@"; do
  if docker buildx imagetools inspect "$reference" >/dev/null 2>"$error_file"; then
    echo "Refusing to overwrite existing image tag: $reference" >&2
    exit 1
  fi
  if ! grep -Eq 'manifest unknown|MANIFEST_UNKNOWN' "$error_file" && \
     ! grep -Fxq -- "ERROR: $reference: not found" "$error_file"; then
    cat "$error_file" >&2
    echo "Could not prove image tag is absent: $reference" >&2
    exit 1
  fi
done
