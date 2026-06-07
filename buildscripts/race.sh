#!/usr/bin/env bash

set -e

# Single invocation lets `go test` parallelise across packages and reuse the
# race-instrumented standard library, instead of relinking 95 times.
# shellcheck disable=SC2046
CGO_ENABLED=1 go test -tags kqueue -race -timeout 20m \
    $(go list ./... | grep -v browser)
