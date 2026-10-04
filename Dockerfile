# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26-alpine
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS builder

# Compile the supplied build context, including local edits, never remote main.
WORKDIR /src
ENV CGO_ENABLED=0 GO111MODULE=on
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
ARG VCS_REF=unknown
ARG VERSION
RUN set -eu; \
    if [ -n "${VERSION:-}" ]; then \
      LDFLAGS="$(OTTERIO_BUILD_COMMIT="$VCS_REF" go run buildscripts/gen-ldflags.go "$VERSION")"; \
    else \
      LDFLAGS="$(OTTERIO_BUILD_COMMIT="$VCS_REF" go run buildscripts/gen-ldflags.go)"; \
    fi; \
    GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build \
      -tags kqueue -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o /out/otterio .

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.3
ARG VCS_REF=unknown
LABEL maintainer="soulteary (community fork of Apache-licensed MinIO codebase, https://github.com/soulteary/otterio)" \
      org.opencontainers.image.source="https://github.com/soulteary/otterio" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.licenses="Apache-2.0"

ENV OTTERIO_ACCESS_KEY_FILE=access_key \
    OTTERIO_SECRET_KEY_FILE=secret_key \
    OTTERIO_ROOT_USER_FILE=access_key \
    OTTERIO_ROOT_PASSWORD_FILE=secret_key \
    OTTERIO_KMS_MASTER_KEY_FILE=kms_master_key \
    OTTERIO_SSE_MASTER_KEY_FILE=sse_master_key

EXPOSE 9000
COPY --from=builder /out/otterio /usr/bin/otterio
COPY CREDITS /licenses/CREDITS
COPY LICENSE /licenses/LICENSE
COPY NOTICE /licenses/NOTICE
COPY dockerscripts/docker-entrypoint.sh /usr/bin/docker-entrypoint.sh
RUN microdnf update --nodocs && \
    microdnf install curl ca-certificates shadow-utils util-linux --nodocs && \
    microdnf clean all && \
    chmod +x /usr/bin/otterio /usr/bin/docker-entrypoint.sh && \
    echo 'hosts: files mdns4_minimal [NOTFOUND=return] dns mdns4' >> /etc/nsswitch.conf

ENTRYPOINT ["/usr/bin/docker-entrypoint.sh"]
VOLUME ["/data"]
CMD ["otterio"]
