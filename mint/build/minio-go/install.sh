#!/bin/bash
#
#  Mint (C) 2017 Minio, Inc.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

set -euo pipefail

test_run_dir="${MINT_RUN_CORE_DIR:?}/minio-go"
cd "$test_run_dir"
export GO111MODULE=on GOWORK=off CGO_ENABLED=0

sdk_module=github.com/soulteary/otterio-sdk/v7
sdk_version=$(go list -mod=readonly -m -f '{{if .Replace}}replaced{{else}}{{.Version}}{{end}}' "$sdk_module")
if [[ ! "$sdk_version" =~ ^v7\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
    echo "Mint SDK must be pinned to an unreplaced v7 version" >&2
    exit 1
fi

# Go verifies the published module against go.sum before exposing its source.
go mod download "$sdk_module@$sdk_version"
sdk_dir=$(go list -mod=readonly -m -f '{{.Dir}}' "$sdk_module")
cp "$sdk_dir/functional_tests.go" main.go
go build -mod=readonly -o minio-go main.go

# The required SDK must actually be linked, with no upstream SDK or replacements.
build_info=$(go version -m minio-go)
if ! awk -v sdk="$sdk_module" -v version="$sdk_version" '
    $1 == "dep" && $2 == sdk && $3 == version { found = 1 }
    $1 == "dep" && $2 ~ /^github.com\/minio\// { upstream = 1 }
    $1 == "=>" { replaced = 1 }
    END { exit !(found && !upstream && !replaced) }
' <<< "$build_info"; then
    echo "Mint binary must link the pinned OtterIO SDK without upstream MinIO modules or replacements" >&2
    exit 1
fi
