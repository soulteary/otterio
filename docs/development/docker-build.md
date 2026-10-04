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
