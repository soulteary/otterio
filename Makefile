PWD := $(shell pwd)
GOPATH := $(shell go env GOPATH)
LDFLAGS := $(shell go run buildscripts/gen-ldflags.go)

GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)

VERSION ?= $(shell git describe --tags)
TAG ?= "soulteary/otterio:$(VERSION)"

# Versions of code generation / lint tools.
# IMPORTANT: keep MSGP_VERSION in sync with the github.com/tinylib/msgp version
# pinned in go.mod, otherwise `make check-gen` will regenerate the *_gen.go
# files in a slightly different style and CI will fail with
# "Non-committed changes in auto-generated code is detected".
MSGP_VERSION ?= v1.6.4
STRINGER_VERSION ?= v0.45.0

all: build

checks:
	@echo "Checking dependencies"
	@(env bash $(PWD)/buildscripts/checkdeps.sh)

getdeps:
	@mkdir -p ${GOPATH}/bin
	@which golangci-lint 1>/dev/null || (echo "Installing golangci-lint" && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)
	@echo "Installing msgp@$(MSGP_VERSION)" && go install github.com/tinylib/msgp@$(MSGP_VERSION)
	@echo "Installing stringer@$(STRINGER_VERSION)" && go install golang.org/x/tools/cmd/stringer@$(STRINGER_VERSION)

crosscompile:
	@(env bash $(PWD)/buildscripts/cross-compile.sh)

verifiers: getdeps lint check-gen

check-gen:
	@go generate ./... >/dev/null
	@(! git diff --name-only | grep '_gen.go$$') || (echo "Non-committed changes in auto-generated code is detected, please commit them to proceed." && false)

lint:
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint cache clean
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint run --config ./.golangci.yml

# Builds otterio, runs the verifiers then runs the tests.
check: test
test: verifiers build
	@echo "Running unit tests"
	@GOGC=25 GO111MODULE=on CGO_ENABLED=0 go test -tags kqueue ./... 1>/dev/null

test-race: verifiers build
	@echo "Running unit tests under -race"
	@(env bash $(PWD)/buildscripts/race.sh)

# Verify otterio binary
verify:
	@echo "Verifying build with race"
	@GO111MODULE=on CGO_ENABLED=1 go build -tags kqueue -trimpath --ldflags "$(LDFLAGS)" -o $(PWD)/otterio 1>/dev/null
	@(env bash $(PWD)/buildscripts/verify-build.sh)

# Verify healing of disks with otterio binary
verify-healing:
	@echo "Verify healing build with race"
	@GO111MODULE=on CGO_ENABLED=1 go build -race -tags kqueue -trimpath --ldflags "$(LDFLAGS)" -o $(PWD)/otterio 1>/dev/null
	@(env bash $(PWD)/buildscripts/verify-healing.sh)

# Builds otterio locally.
build: checks
	@echo "Building otterio binary to './otterio'"
	@GO111MODULE=on CGO_ENABLED=0 go build -tags kqueue -trimpath --ldflags "$(LDFLAGS)" -o $(PWD)/otterio 1>/dev/null

hotfix-vars:
	$(eval LDFLAGS := $(shell OTTERIO_RELEASE="RELEASE" OTTERIO_HOTFIX="hotfix.$(shell git rev-parse --short HEAD)" go run buildscripts/gen-ldflags.go $(shell git describe --tags --abbrev=0 | \
    sed 's#RELEASE\.\([0-9]\+\)-\([0-9]\+\)-\([0-9]\+\)T\([0-9]\+\)-\([0-9]\+\)-\([0-9]\+\)Z#\1-\2-\3T\4:\5:\6Z#')))
	$(eval TAG := "soulteary/otterio:$(shell git describe --tags --abbrev=0).hotfix.$(shell git rev-parse --short HEAD)")
hotfix: hotfix-vars install

docker-hotfix: hotfix checks
	@echo "Building otterio docker image '$(TAG)'"
	@docker build -t $(TAG) . -f Dockerfile.dev

docker: build checks
	@echo "Building otterio docker image '$(TAG)'"
	@docker build -t $(TAG) . -f Dockerfile.dev

# Builds otterio and installs it to $GOPATH/bin.
install: build
	@echo "Installing otterio binary to '$(GOPATH)/bin/otterio'"
	@mkdir -p $(GOPATH)/bin && cp -f $(PWD)/otterio $(GOPATH)/bin/otterio
	@echo "Installation successful. To learn more, try \"otterio --help\"."

clean:
	@echo "Cleaning up all the generated files"
	@find . -name '*.test' | xargs rm -fv
	@find . -name '*~' | xargs rm -fv
	@rm -rvf otterio
	@rm -rvf build
	@rm -rvf release
	@rm -rvf .verify*
