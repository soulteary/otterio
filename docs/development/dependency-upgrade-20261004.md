# Dependency refresh: 2026-10-04

Go 1.27.1; golangci-lint 2.14.0; Node 24.21.0; Bun 1.4.2.

## Scope and compatibility

Go modules were resolved with `go get -u -t ./...`, followed by explicit
refresh of the dependency-test helper quicktest, `go mod tidy` and
`go mod verify`. All 124 retained requirements were checked against
their latest versions on the existing module paths. This does not
silently replace packages with different module paths or forks (for
example, a JWT implementation or a new major SQL-parser module).
Browser direct dependencies were refreshed across stable major versions.
Generated Go code and embedded browser production assets were rebuilt.

Babel 8 uses the explicit core-js polyfill plugin; Redux Thunk 3 uses
named middleware exports. Jest 30 tests use a navigation boundary rather
than redefining jsdom location, and provide TextEncoder/TextDecoder.
The obsolete cheerio prerelease override was removed.

## Validation before PR CI

- Go 1.27.1: package and test graph compilation, module verification, code generation.
- Browser: 59 suites / 232 tests passed; production webpack build passed.
- Complete repository CI is separate and must pass before merge.
- No release, registry alias or deployment is performed by this refresh.

## Go requirement changes

| Module | Before | After |
| --- | --- | --- |
| `github.com/apache/thrift` | `v0.23.1-0.20260429145742-d2acd3c49e58` | `v0.25.0` |
| `github.com/beevik/ntp` | `v1.5.0` | `v1.6.0` |
| `github.com/cheggaaa/pb` | `v1.0.29` | `v1.0.30` |
| `github.com/dustin/go-humanize` | `v1.0.1` | `v1.1.0` |
| `github.com/go-ldap/ldap/v3` | `v3.4.13` | `v3.4.14` |
| `github.com/go-sql-driver/mysql` | `v1.10.0` | `v1.10.1` |
| `github.com/gofiber/fiber/v3` | `v3.3.0` | `v3.5.0` |
| `github.com/klauspost/compress` | `v1.19.1` | `v1.20.1` |
| `github.com/klauspost/cpuid/v2` | `v2.3.0` | `v2.4.0` |
| `github.com/klauspost/pgzip` | `v1.2.6` | `v1.2.7` |
| `github.com/klauspost/reedsolomon` | `v1.14.0` | `v1.14.2` |
| `github.com/mattn/go-isatty` | `v0.0.22` | `v0.0.24` |
| `github.com/miekg/dns` | `v1.1.72` | `v1.1.73` |
| `github.com/minio/minio-go/v7` | `v7.2.0` | `v7.3.0` |
| `github.com/montanaflynn/stats` | `v0.9.0` | `v0.12.7` |
| `github.com/prometheus/client_model` | `v0.6.2` | `v0.6.3` |
| `github.com/prometheus/procfs` | `v0.21.1` | `v0.22.0` |
| `github.com/tinylib/msgp` | `v1.6.4` | `v1.6.5` |
| `github.com/valyala/fasthttp` | `v1.71.0` | `v1.74.0` |
| `go.etcd.io/etcd/api/v3` | `v3.6.13` | `v3.7.2` |
| `go.etcd.io/etcd/client/v3` | `v3.6.13` | `v3.7.2` |
| `golang.org/x/crypto` | `v0.54.0` | `v0.57.0` |
| `golang.org/x/net` | `v0.57.0` | `v0.59.0` |
| `golang.org/x/sys` | `v0.47.0` | `v0.48.0` |
| `github.com/bits-and-blooms/bitset` | `v1.24.5` | `v1.25.0` |
| `github.com/frankban/quicktest` | `v1.14.0` | `v1.14.6` |
| `github.com/go-asn1-ber/asn1-ber` | `v1.5.8-0.20250403174932-29230038a667` | `v1.5.8` |
| `github.com/go-jose/go-jose/v4` | `v4.1.4` | `v4.1.5` |
| `github.com/gofiber/schema` | `v1.7.2` | `v1.8.8` |
| `github.com/gofiber/utils/v2` | `v2.1.0` | `v2.6.1` |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | `v2.29.0` | `v2.31.0` |
| `github.com/lufia/plan9stats` | `v0.0.0-20260330125221-c963978e514e` | `v0.0.0-20260802145828-341c2f0c90b5` |
| `github.com/mattn/go-runewidth` | `v0.0.24` | `v0.0.30` |
| `github.com/molecule-man/go-brrr` | `new` | `v1.2.0` |
| `github.com/power-devops/perfstat` | `v0.0.0-20240221224432-82ca36839d55` | `v0.0.0-20260916203055-22a1a467d9f0` |
| `github.com/prometheus/common` | `v0.70.1` | `v0.72.0` |
| `github.com/shoenig/go-m1cpu` | `v0.2.1` | `v0.2.3` |
| `github.com/tidwall/pretty` | `v1.2.1` | `v1.2.2` |
| `go.etcd.io/etcd/client/pkg/v3` | `v3.6.13` | `v3.7.2` |
| `go.yaml.in/yaml/v3` | `v3.0.4` | `v3.0.5` |
| `golang.org/x/text` | `v0.40.0` | `v0.42.0` |
| `golang.org/x/time` | `v0.15.0` | `v0.16.0` |
| `golang.org/x/tools` | `v0.47.0` | `v0.51.0` |
| `google.golang.org/genproto/googleapis/api` | `v0.0.0-20260630182238-925bb5da69e7` | `v0.0.0-20260928230214-8a89bd6388cc` |
| `google.golang.org/genproto/googleapis/rpc` | `v0.0.0-20260803160001-6ac0973c030d` | `v0.0.0-20260928230214-8a89bd6388cc` |
| `google.golang.org/grpc` | `v1.83.1` | `v1.84.0` |
| `google.golang.org/protobuf` | `v1.36.11` | `v1.36.12` |
| `gopkg.in/ini.v1` | `v1.67.2` | `v1.67.3` |

## Browser requirement changes

| Package | Before | After |
| --- | --- | --- |
| `@fortawesome/fontawesome-free` | `^5.15.4` | `^7.3.1` |
| `@reduxjs/toolkit` | `^2.12.0` | `^2.13.0` |
| `bootstrap` | `^5` | `^5.3.8` |
| `core-js` | `^3.39.0` | `^3.50.0` |
| `dayjs` | `^1.11.21` | `^1.11.23` |
| `filesize` | `^11.0.17` | `^11.0.25` |
| `history` | `^5` | `^5.3.0` |
| `jwt-decode` | `^4` | `^4.0.0` |
| `local-storage-fallback` | `^4.1.2` | `^5.0.0` |
| `mime-db` | `^1.53.0` | `^1.54.0` |
| `mime-types` | `^2.1.35` | `^3.0.2` |
| `query-string` | `^8` | `^9.5.1` |
| `react` | `^18` | `^19.3.0` |
| `react-bootstrap` | `^2` | `^2.10.10` |
| `react-copy-to-clipboard` | `^5.1.0` | `^5.1.1` |
| `react-dom` | `^18` | `^19.3.0` |
| `react-dropzone` | `^11.7.1` | `^20.1.2` |
| `react-qr-code` | `^1.1.1` | `^2.2.0` |
| `react-redux` | `^9` | `^9.3.0` |
| `react-router-dom` | `^6` | `^7.18.4` |
| `@babel/core` | `^7.26.0` | `^8.0.6` |
| `@babel/preset-env` | `^7.26.0` | `^8.0.6` |
| `@babel/preset-react` | `^7.26.3` | `^8.0.1` |
| `@testing-library/jest-dom` | `^6.9.1` | `^7.0.1` |
| `@testing-library/react` | `^16.3.2` | `^16.3.3` |
| `@testing-library/user-event` | `^14.6.1` | `^14.6.7` |
| `babel-jest` | `^29.7.0` | `^30.5.2` |
| `babel-loader` | `^9.2.1` | `^10.1.1` |
| `babel-plugin-polyfill-corejs3` | `new` | `^1.0.0` |
| `copy-webpack-plugin` | `^11.0.0` | `^14.0.0` |
| `css-loader` | `^6.11.0` | `^7.1.5` |
| `jest` | `^29.7.0` | `^30.5.2` |
| `jest-environment-jsdom` | `^29.7.0` | `^30.5.2` |
| `less` | `^4.2.1` | `^4.9.1` |
| `less-loader` | `^11.1.4` | `^13.0.0` |
| `prettier` | `^3.8.3` | `^3.9.9` |
| `purgecss-webpack-plugin` | `^5.0.0` | `^8.0.0` |
| `redux-thunk` | `^2.4.2` | `^3.1.0` |
| `style-loader` | `^3.3.4` | `^4.0.0` |
| `terser-webpack-plugin` | `new` | `^5.6.1` |
| `webpack` | `^5.97.1` | `^5.111.1` |
| `webpack-cli` | `^5.1.4` | `^7.2.3` |
| `webpack-dev-server` | `^5.2.0` | `^6.0.0` |

## GitHub Actions

| Action | Stable release |
| --- | --- |
| `actions/cache` | `v6.1.0` |
| `actions/checkout` | `v7.0.1` |
| `actions/download-artifact` | `v8.0.1` |
| `actions/setup-go` | `v7.0.0` |
| `actions/setup-node` | `v7.0.0` |
| `actions/upload-artifact` | `v7.0.1` |
| `docker/build-push-action` | `v7.4.0` |
| `docker/login-action` | `v4.6.0` |
| `docker/setup-buildx-action` | `v4.4.1` |
| `docker/setup-qemu-action` | `v4.4.0` |
| `golangci/golangci-lint-action` | `v9.3.0` |
| `oven-sh/setup-bun` | `v2.2.0` |

## Upstream references

- https://go.dev/doc/devel/release
- https://go.dev/doc/go1.27
- https://babeljs.io/docs/v8-migration/
- https://golangci-lint.run/docs/product/changelog/
- https://bun.sh/docs/pm/cli/update
