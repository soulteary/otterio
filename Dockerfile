FROM golang:1.26-alpine as builder

# community fork of Apache-licensed MinIO codebase. Not affiliated with, endorsed by, or sponsored by MinIO, Inc.
LABEL maintainer="soulteary (community fork of Apache-licensed MinIO codebase, https://github.com/soulteary/otterio)"

ENV GOPATH /go
ENV CGO_ENABLED 0
ENV GO111MODULE on

RUN  \
     apk add --no-cache git && \
     git clone https://github.com/soulteary/otterio && cd otterio && \
     git checkout main && go install -v -ldflags "$(go run buildscripts/gen-ldflags.go)"

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.3

ENV OTTERIO_ACCESS_KEY_FILE=access_key \
    OTTERIO_SECRET_KEY_FILE=secret_key \
    OTTERIO_ROOT_USER_FILE=access_key \
    OTTERIO_ROOT_PASSWORD_FILE=secret_key \
    OTTERIO_KMS_MASTER_KEY_FILE=kms_master_key \
    OTTERIO_SSE_MASTER_KEY_FILE=sse_master_key

EXPOSE 9000

COPY --from=builder /go/bin/otterio /usr/bin/otterio
COPY --from=builder /go/otterio/CREDITS /licenses/CREDITS
COPY --from=builder /go/otterio/LICENSE /licenses/LICENSE
COPY --from=builder /go/otterio/NOTICE /licenses/NOTICE
COPY --from=builder /go/otterio/dockerscripts/docker-entrypoint.sh /usr/bin/

RUN  \
     microdnf update --nodocs && \
     microdnf install curl ca-certificates shadow-utils util-linux --nodocs && \
     microdnf clean all && \
     echo 'hosts: files mdns4_minimal [NOTFOUND=return] dns mdns4' >> /etc/nsswitch.conf

ENTRYPOINT ["/usr/bin/docker-entrypoint.sh"]

VOLUME ["/data"]

CMD ["otterio"]
