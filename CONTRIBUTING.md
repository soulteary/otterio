# Contributing to OtterIO

Start with the [project README](README.md), [documentation index](docs/README.md)
and [security reporting policy](SECURITY.md). Report suspected vulnerabilities
privately through that policy. This guide describes this fork's workflow;
upstream MinIO instructions may refer to different commands, APIs or dependencies.

## Repository layout

- `main.go` enters `cmd.Main`; `cmd/` implements the CLI, HTTP routing,
  authentication, administration, storage backends and background workers.
- `cmd/gateway/` registers the remaining NAS and S3 gateways.
- `pkg/` contains policy, lifecycle, notification, encryption and other
  supporting packages; `internal/clisupport/` maintains CLI compatibility helpers.
- The standalone Web Console is maintained in [OC](https://github.com/soulteary/oc),
  outside this server repository. See the [OC Console guide](https://github.com/soulteary/oc/blob/main/docs/console.md).
- `buildscripts/`, `.github/workflows/` and `testdata/cli/` define checks, release
  tooling and the CLI compatibility contract.
- `docs/` and `docs/zh_CN/` contain operational guides and translations;
  `mint/` contains separate client compatibility harnesses.

## Toolchain and local build

Use Go **1.27.3 or newer**, Git, Make and Bash. For race tests install a working C
compiler; ordinary server builds use `CGO_ENABLED=0`. The pinned versions in
[go.mod](go.mod), [Makefile](Makefile) and CI are the source of truth.

```sh
make build
./otterio --version
./otterio server --help
```

`make build` builds the server binary. Build and run `oc-console` separately for browser access.
Configure your own root credentials before starting a manual deployment. Follow
[Quick Start](README.md#quick-start) for the credential and listener setup.

For an isolated startup and S3 byte-round-trip check:

```sh
go run buildscripts/verify-s3-startup.go ./otterio
```

This helper creates temporary storage, chooses a random loopback port, configures
its own test credentials and stops its child process. It creates a bucket,
uploads/downloads and compares an object, then deletes it. It does not validate
distributed storage, TLS, IAM isolation or every object feature.

For a local-source Docker build, follow
[Docker development](docs/development/docker-build.md).

## Choose checks for the change

Run focused regressions while developing, then the relevant broader checks. CI
contains additional checks beyond the Makefile targets.

```sh
# Full Go verification: pinned tools, lint, generated-code check, build and tests.
make test

# Uncached race tests with CGO.
make test-race

# Container credential policy and executable documentation examples.
python3 -m unittest discover -s buildscripts -p 'test_docker_entrypoint*.py' -v

# Offline release helper regressions.
python3 -m unittest discover -s buildscripts -p 'test_release_*.py' -v
```

`make test` installs the tool versions declared in the Makefile and may download
dependencies. It does not run the Python CLI/release suites.
Container tests that need a compiled CLI use `OTTERIO_TEST_BINARY`; tests without
that input may skip their real-binary cases. Consult
[container CI](.github/workflows/container-security-checks.yml) for that setup.

For CLI behavior changes, use the archived contract and fixture checks in
[CLI compatibility CI](.github/workflows/cli-compat.yml) and read the
[CLI migration notes](docs/cli-migration.md). Do not regenerate a baseline merely
to hide a compatibility failure.

For Web Console changes, follow the contribution and test instructions in
[OC](https://github.com/soulteary/oc). Server integration changes should preserve
the S3, Admin and STS protocols, authentication, separate listener addresses and
independent S3/Admin CA configuration. Verify the affected operations using
the actual backend identity and permissions.

Linux CI runs the full Go suite and race checks. Windows runs a build, focused
directory regressions and a real server startup/S3 smoke test; it does not run
the full POSIX-dependent server suite. The CLI matrix also covers macOS.
Cross-compilation establishes build compatibility, not runtime acceptance on
all target architectures. Building the Mint SDK program in CI is not a run of
the entire Mint compatibility suite. See [Go CI](.github/workflows/go.yml).

The legacy `make verify` script uses a fixed port. Both it and
`make verify-healing` find and stop processes by the name `otterio`. Run them
only in an isolated runner or container without another OtterIO service. `make verify` currently builds
without `-race`; `make verify-healing` builds with it. The healing script checks
restart/safe-mode scenarios, not a complete object-content recovery test.
The older `verify` CI job is disabled; the healing job remains active.

## Documentation and pull requests

Verify documented flags, environment variables, limits and backend support
against the current source. Use **OC** for OtterIO administration:
`/otterio/admin/v3` differs from upstream `mc admin`. A split-listener deployment
requires a client that can configure separate S3 and admin URLs.

Keep English and Chinese versions consistent when both exist. Prefer relative
links to this repository's guides; label upstream material as background.
Distinguish changes on `main` from features in a published release. Do not turn
source constants, passing unit tests or cross-compilation into a performance,
certification or production-readiness claim.

Keep changes focused, preserve license and attribution notices, and use the
[PR template](.github/PULL_REQUEST_TEMPLATE.md). Explain the concrete behavior,
compatibility implications and checks actually run. For a documentation-only
change, validate examples and links; mark code tests as not applicable where
appropriate instead of claiming tests were added.

Publishing is a separate maintainer action. A merged PR does not create a tag,
release assets or promote `latest`. Use the [release guide](docs/releasing.md)
and update the exact tagged commit's release notes when preparing a release.
