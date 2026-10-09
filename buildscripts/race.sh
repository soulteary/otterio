#!/usr/bin/env bash

set -euo pipefail

# Resolve paths independently of the caller's working directory.
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
export CGO_ENABLED=1

# Keep enumeration separate: a failed/partial go list must never turn into a
# successful test of only the packages printed before the error.
module_path=$(go list -m -f '{{.Path}}')
if [[ -z "$module_path" || "$module_path" == *$'\n'* ]]; then
    echo "Cannot determine a single main module for race tests" >&2
    exit 1
fi
package_list=$(go list -tags kqueue -race ./...)
packages=()
while IFS= read -r package; do
    case "$package" in
        ""|"$module_path/browser"|"$module_path/browser/"*) continue ;;
    esac
    packages+=("$package")
done <<< "$package_list"
if [[ ${#packages[@]} -eq 0 ]]; then
    echo "No packages selected for race tests" >&2
    exit 1
fi

# One invocation preserves package parallelism and reuse of build outputs.
# Reuse compilation, not previous successful test results. Optional arguments
# are for local diagnostics (e.g. -json); CI never supplies test filters.
printf 'Race testing %s packages\n' "${#packages[@]}" >&2
# The timeout covers an entire package, not each individual test. The cmd
# suite exercises large multipart/encrypted objects and exceeded 20 minutes
# on the two-core hosted runner while tests were still making progress.
exec go test -tags kqueue -race -timeout 90m -count=1 "$@" "${packages[@]}"
