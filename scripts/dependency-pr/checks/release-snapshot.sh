#!/usr/bin/env bash
# Cross-compiles every release target the way releases do: goreleaser inside
# goreleaser-cross (CGO for SQLite with musl static, mingw, osxcross). PR CI
# only builds linux/amd64 with the host gcc. A CGO-off cross build is not a
# substitute: internal/dev_server/db/backup needs CGO to compile at all.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  incomplete "Docker is not available, so the goreleaser-cross build did not run"
fi
image="${VERIFY_GORELEASER_IMAGE:-$(rg -o --no-filename 'ghcr\.io/launchdarkly/goreleaser-cross@sha256:[0-9a-f]{64}' .github/actions/publish/action.yml | head -n1)}"
[ -n "$image" ] || incomplete "could not find the goreleaser-cross image in .github/actions/publish/action.yml"
detail "- Image: \`${image:0:60}…\`"

# The image is a private org package. A failed pull is a gap in this
# environment, not a property of the PR, so it must never count as a failure
# (which base would repeat and the verdict would file as pre-existing).
if ! docker image inspect "$image" >/dev/null 2>&1 && ! run docker pull -q "$image" >"$ARTIFACTS/pull.out" 2>&1; then
  cat "$ARTIFACTS/pull.out"
  incomplete "could not pull the goreleaser-cross image ($(tail -n1 "$ARTIFACTS/pull.out" | cut -c1-120)). Log in to ghcr.io with a token that can read launchdarkly packages."
fi

# A worktree's .git file points into the main repository's git dir, so both are
# mounted at their real paths (the publish action mounts "$PWD:$PWD" likewise).
common=$(cd "$WT" && cd "$(git rev-parse --git-common-dir)" && pwd)
run docker run --rm -v "$WT:$WT" -v "$common:$common" -w "$WT" \
  --entrypoint bash "$image" -c \
  "git config --global --add safe.directory '*' && goreleaser build --snapshot --clean --config .goreleaser.yaml" \
  2>&1 | tee "$ARTIFACTS/goreleaser.out"
rc=${PIPESTATUS[0]}
# The container runs as root; remove its dist/ before restoring the tree.
docker run --rm -v "$WT:$WT" --entrypoint rm "$image" -rf "$WT/dist" >/dev/null 2>&1
restore_tree

if [ "$rc" -ne 0 ]; then
  grep -E 'error|failed|\.go:[0-9]+' "$ARTIFACTS/goreleaser.out" | sed -E 's/:[0-9]+:[0-9]+:/:/; s/[0-9.]+m?s\b//g' | sort -u >"$ARTIFACTS/errs"
  fingerprint_file "$ARTIFACTS/errs"
  detail_block "$ARTIFACTS/errs" 30
  fail "goreleaser snapshot build fails"
fi
pass "All release targets build in goreleaser-cross"
