#!/usr/bin/env bash

set -eu
exec docker run -it --rm -v "$PWD:$PWD" -w "$PWD" ghcr.io/goreleaser/goreleaser-cross:latest "$@"
