#!/usr/bin/env bash

set -eu

# based on https://github.com/goreleaser/goreleaser-cross
docker image inspect grcross-xbuilder &>/dev/null || docker build -t grcross-xbuilder -f - . <<EOF
FROM ghcr.io/goreleaser/goreleaser-cross:latest

RUN set -eux; \
    apt update; \
    apt-get install libc6-dev libc6-dev-i386 musl-tools musl-dev -y
EOF

exec docker run -it --rm -v "$PWD:$PWD" -w "$PWD" grcross-xbuilder build --snapshot --clean --config .goreleaser.grcross.yaml
