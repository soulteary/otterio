# Build the checked-out source with Docker

`docker build -t otterio-local .` compiles this directory, including local edits
and committed browser assets. It does not clone a repository or checkout remote
`main`. Git metadata is excluded from the build context.

For a clean, identified checkout:

```sh
docker build --build-arg VCS_REF="$(git rev-parse HEAD)" -t otterio-local .
docker run --rm otterio-local --version
```

`VCS_REF` is metadata, not a source selector. Its default is `unknown`, which also
supports downloaded source archives. Do not attach an unqualified commit identity
to dirty production builds. `VERSION` optionally supplies the RFC3339 build time;
`GO_VERSION` selects the builder image. This development Dockerfile continues to
use the shared `buildscripts/gen-ldflags.go` and the release build's `kqueue` and
`trimpath` settings. Published releases still use the existing exact-commit binary
workflow and `Dockerfile.ci`; this change does not publish anything.

The Docker source CI injects an uncommitted version marker and checks that the
built container reports it. The metadata tests run without a Git checkout and
reject malformed commit overrides. Dependency and base-image downloads still
require network access; local-source correctness is not a claim of bit-for-bit
reproducibility.

## Runtime package refresh in CI images

`Dockerfile.ci` installs runtime packages before the version labels and binary
copy, so changing a binary does not rerun the system package installer under
QEMU. The `RUNTIME_REFRESH` build argument controls when that layer expires:

- Edge pushes share the layer within one UTC day. The first build on the next
  day updates it; a manual Docker (edge) workflow run forces another update
  immediately, including when a security fix arrives on the same day.
- Every stable Release run and rerun updates runtime packages independently.
- Edge and Release use separate GitHub Actions cache scopes to avoid overwriting
  each other's build snapshots. Their architecture lists and image verification
  remain the same.

For a local `Dockerfile.ci` build, pass
`--build-arg RUNTIME_REFRESH="$(date -u +%Y-%m-%d)"` to refresh at least daily,
or use a new value or `--no-cache` to force an immediate package refresh. Without
an override, local builds may reuse the `local` runtime layer. Changing only
`RELEASE` intentionally keeps the cached runtime packages.
