FROM ubuntu:24.04

ENV DEBIAN_FRONTEND noninteractive
ENV LANG C.UTF-8
ENV GOROOT /usr/local/go
ENV GOPATH /usr/local/gopath
ENV PATH /opt/mint-venv/bin:$GOPATH/bin:$GOROOT/bin:$PATH
ENV MINT_ROOT_DIR /mint
COPY mint /mint

RUN apt-get --yes update && apt-get --yes upgrade && \
    apt-get --yes --quiet install wget jq curl git dnsmasq && \
    cd /mint && /mint/release.sh

WORKDIR /mint

ENTRYPOINT ["/mint/entrypoint.sh"]
