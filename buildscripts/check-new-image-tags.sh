#!/usr/bin/env bash
# Never treat an authentication or network failure as proof that a tag is absent.
set -euo pipefail
[ "$#" -gt 0 ] || { echo 'at least one versioned image reference is required' >&2; exit 1; }

# Buildx reports normalized Docker Hub names in errors. Normalize the lookup as
# well as the comparison, without changing release-manifest/allowlist identities.
# Buildx remains responsible for validating the complete image-reference syntax.
normalize_reference() {
  local reference="$1" first remainder
  case "$reference" in
    */*)
      first="${reference%%/*}"
      case "$first" in
        index.docker.io) reference="docker.io/${reference#*/}" ;;
        localhost|*.*|*:*) ;; # An explicit registry (including host:port).
        *) reference="docker.io/$reference" ;;
      esac
      ;;
    *) reference="docker.io/library/$reference" ;;
  esac
  case "$reference" in
    docker.io/*)
      remainder="${reference#docker.io/}"
      case "$remainder" in
        */*) ;;
        *) reference="docker.io/library/$remainder" ;;
      esac
      ;;
  esac
  printf '%s\n' "$reference"
}

error_file="$(mktemp)"
trap 'rm -f "$error_file"' EXIT
for reference in "$@"; do
  reference="$(normalize_reference "$reference")"
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
