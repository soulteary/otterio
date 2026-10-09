## `minio-go` tests
This directory serves as the location for Mint tests using the OtterIO SDK.
The output binary remains named `minio-go`; top level `mint.sh` calls `run.sh` to execute it.

## Installing the pinned harness
`mint/build/minio-go/install.sh` reads the OtterIO SDK version from this directory's
`go.mod`, downloads that checksum-verified module, and builds its `functional_tests.go`
as `main.go`. Keep this independent module and its checksums in sync with the
functional program's imports. Installation disables parent Go workspaces, builds
with read-only dependencies and checks the binary's linked SDK version and origin.

## Adding new tests
New tests are added to [the OtterIO SDK functional program](https://github.com/soulteary/otterio-sdk/blob/v7.3.2/functional_tests.go).
Publish an SDK version and update this module's pin/checksums to include them.

## Running tests manually
- Set environment variables `MINT_DATA_DIR`, `MINT_MODE`, `SERVER_ENDPOINT`, `ACCESS_KEY`, `SECRET_KEY`, `SERVER_REGION` and `ENABLE_HTTPS`
- Call `run.sh` with output log file and error log file. for example
```bash
export MINT_DATA_DIR=~/my-mint-dir
export MINT_MODE=core
export SERVER_ENDPOINT="play.minio.io:9000"
export ACCESS_KEY="Q3AM3UQ867SPQQA43P2F"
export SECRET_KEY="zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG"
export ENABLE_HTTPS=1
export SERVER_REGION=us-east-1
./run.sh /tmp/output.log /tmp/error.log
```
