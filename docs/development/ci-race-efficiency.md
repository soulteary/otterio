# Go CI: toolchain source, caching and race diagnostics

All six workflows using `actions/setup-go` read the checked-out `go.mod` through
`go-version-file`. Do not also set `go-version`: it takes precedence. Cache keys
use the installed version output. Release jobs must check out the validated
release SHA before installing Go, so rebuilding an older tag does not borrow the
current main branch's toolchain. The Go version, action versions and dependencies
are not upgraded by this change. Docker image pins and their consistency checks
remain in place.

`setup-go` prefers the `toolchain` directive when one exists, otherwise `go`.
The current module has only a `go` directive. Adding a different toolchain later
requires reviewing the Docker/minimum-version policy; `go-version-file` does not
automatically update Dockerfile base images. `GOTOOLCHAIN=local` is scoped to the
race job; tool-installing jobs retain their existing behavior.

## Reuse compilation, not a previous race result

The Linux/Windows test jobs and the race job cache `GOMODCACHE` separately from
`GOCACHE`. Module snapshots depend on the toolchain and `go.mod`/`go.sum`; build
snapshots also include the commit SHA. Exact cache entries are immutable, so a
source-independent build key cannot accumulate newly compiled application code.
Restore prefixes reuse compatible snapshots; Go validates each cached entry.
Plain and race snapshots have separate namespaces and include OS/architecture.

Only successful jobs save build snapshots. Commit-level snapshots consume cache
space and may be evicted; no caches are deleted by this PR. Monitor cache sizes,
restore/save overhead and hot/cold behavior before adding more writers/shards.
A cold cache after the namespace change is expected and is not a warm-run result.
Other jobs retain setup-go's built-in module AND build-output caching.

`make test-race` no longer builds an unused non-race server first, and linker
metadata is evaluated only when a build uses it. Ordinary builds, cross-compiles,
healing verification, Linux/Windows tests, security regressions and release gates
are retained. The launcher uses one `go test` invocation, the original `kqueue`
tag and 20-minute per-package timeout, `CGO_ENABLED=1`, and `-count=1`. The latter
forces tests to execute but does not discard compilation caches; honest fresh
race execution may take longer than a previous cached-result run.

Package discovery uses the same race/tag/CGO settings and must succeed before any
test starts. An empty selection is an error. Only the module's `browser` subtree
is excluded, not arbitrary package names containing the word `browser`. No test
filters, `-short`, timeout relaxation, `t.Parallel()` changes, race suppression,
KDF changes or sharding are introduced. PR and main both keep the full race run.

## Inspect a run

The **Race detector** job uses `buildscripts/race.sh -json` and preserves its exit
status through `tee` with `pipefail`. Its job summary lists package outcomes and
the slowest top-level tests without double counting subtests. Missing/incomplete
logs are explicitly marked; the summary is not a replacement for the Go exit code.
The seven-day `race-diagnostics-<attempt>` artifact contains:

- `race.jsonl`: raw Go test events, including failures and skips.
- `resources.txt`: GNU time CPU, elapsed and maximum resident-memory statistics.
- `environment.txt`: installed Go version, target, CGO policy and CPU count.

Artifacts and summaries are attempted after failures too. A cancelled runner may
not finish post-processing. Reports contain only this job's test output and no
production credentials; do not introduce real secrets into test fixtures.

For a local run with the same diagnostics (GNU time required):

```bash
set -euo pipefail
results="$(mktemp -d)"
/usr/bin/time -v -o "$results/resources.txt" \
  bash buildscripts/race.sh -json | tee "$results/race.jsonl"
python3 buildscripts/summarize_race.py "$results/race.jsonl"
```

`-json` enables verbose test behavior. In this repository `TestMain` changes
logging based on verbosity; compare baseline and candidate with identical output
flags. Measure both fresh execution with warm compilation caches and genuinely
cold compilation; do not compare a cached test result to a fresh execution.
Package/test elapsed values exclude pre-execution compilation and overlap across
packages. Wall-clock and total runner CPU time are separate acceptance metrics.

## Before a sharding follow-up

Use several comparable runs to find whether compilation, one large package,
individual tests, disk I/O or teardown dominates. No speedup is claimed before
that evidence exists. For any future partitioning, derive the test inventory
from the current source/build configuration, verify the union covers it, include
examples and fuzz seed tests, preserve top-level/subtest relationships and require
all shards. Preserve an unsharded main run initially to retain process/order
interaction coverage. Do not remove tests to make the numbers look better.

## Helper regressions

```bash
python3 -m unittest discover -s buildscripts -p test_dependency_versions.py -v
python3 -m unittest discover -s buildscripts -p 'test_race_*.py' -v
bash -n buildscripts/race.sh
```

The workflow-policy check deliberately requires the repository's block-style
setup-go YAML. Unsupported inline/merged configuration fails closed instead of
silently passing a regex that matched no versions. Launcher tests use a fake Go
executable to exercise partial enumeration, empty selections, error propagation,
Makefile wiring and pipeline failures without downloading modules. These helper
tests are not a substitute for full Go 1.27.1 repository CI.
