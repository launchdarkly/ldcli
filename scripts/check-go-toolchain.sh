#!/usr/bin/env bash
# Fails the build if a binary was not compiled with the toolchain pinned in
# go.mod. Run by goreleaser as a post-build hook, so a release built with an
# older Go (e.g. one missing crypto/x509 security fixes) never gets published.
set -euo pipefail

binary="$1"
want="$(awk '$1 == "toolchain" { print $2 }' go.mod)"
got="$(go version "$binary" | awk '{ print $NF }')"

if [ -z "$want" ]; then
  echo "check-go-toolchain: no toolchain directive in go.mod" >&2
  exit 1
fi

if [ "$got" != "$want" ]; then
  echo "check-go-toolchain: $binary was built with $got, go.mod pins $want" >&2
  exit 1
fi

echo "check-go-toolchain: $binary built with $got"
