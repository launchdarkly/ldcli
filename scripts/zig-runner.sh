#!/usr/bin/env bash

set -eu

# based on https://github.com/goreleaser/goreleaser-example-zig-cgo/tree/master
docker image inspect zig-xbuilder &>/dev/null || docker build -t zig-xbuilder -f - . <<EOF
FROM ghcr.io/goreleaser/goreleaser-cross:latest AS gr_cross

FROM golang:1.22-bullseye

ENV PATH="$PATH:/usr/local/share/zig-linux-x86_64-0.14.0-dev.1021+fc2924080:/go/bin:/usr/local/go/bin"

COPY --from=gr_cross /usr/local/osxcross/SDK/MacOSX12.0.sdk/System/Library/Frameworks /host/Frameworks

RUN set -eux; \
    apt update; \
    apt install xz-utils bash -y; \
    wget -q -O- https://ziglang.org/builds/zig-linux-x86_64-0.14.0-dev.1021+fc2924080.tar.xz | tar -Jxf - -C /usr/local/share/; \
    zig version; \
    curl -sfL -o- https://github.com/goreleaser/goreleaser/releases/download/v2.1.0/goreleaser_Linux_x86_64.tar.gz  | tar -zxf - -C /usr/local/bin/; \
    go version; \
    goreleaser --version
EOF

exec docker run -v "$PWD:$PWD:z" -w "$PWD" -it --rm --entrypoint /usr/local/bin/goreleaser zig-xbuilder build --snapshot --clean --config .goreleaser.zig.yaml
