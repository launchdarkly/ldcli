#!/usr/bin/env bash
# Cross-compiles every release target the way releases do: goreleaser inside
# goreleaser-cross (CGO for SQLite with musl static, mingw, osxcross). PR CI
# only builds linux/amd64 with the host gcc. A CGO-off cross build is not a
# substitute: internal/dev_server/db/backup needs CGO to compile at all.
#
# The release image (ghcr.io/launchdarkly/goreleaser-cross) is a private org
# package. If it cannot be pulled (no ghcr.io credentials), the check uses the
# public upstream image that it is based on, plus the musl.cc toolchains that it
# adds under /musl. The publish action downloaded the same tarballs before #543.
# Both are pinned by digest. Fidelity gaps of the fallback: the public image and
# musl.cc replace the LaunchDarkly image, which can have extras. With either
# image, the binaries are not executed, and goreleaser build skips the dockers
# and brews steps.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

PUBLIC_IMAGE="${VERIFY_GORELEASER_PUBLIC_IMAGE:-goreleaser/goreleaser-cross@sha256:c4bfde12925cd9e23faae3e355ec7656119348ace0b9075ba7ba94e51229ab8f}" # v1.24.2
MUSL_TOOLCHAINS=(
  "x86_64 c5d410d9f82a4f24c549fe5d24f988f85b2679b452413a9f7e5f7b956f2fe7ea"
  "aarch64 c909817856d6ceda86aa510894fa3527eac7989f0ef6e87b5721c58737a06c38"
  "i686 93bd5504d5d0349258c43d7a668b506acbd627d31c0843a96e4b4c47b27a0180"
)

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  incomplete "Docker is not available, so the goreleaser-cross build did not run"
fi
# Trees from before #629 (a conflicting PR is tested at its old head) use a tag
# such as :v1.24.2 instead of a digest.
release_image="${VERIFY_GORELEASER_IMAGE:-$(rg -o --no-filename 'ghcr\.io/launchdarkly/goreleaser-cross(@sha256:[0-9a-f]{64}|:[A-Za-z0-9._-]+)' .github/actions/publish/action.yml | head -n1)}"
[ -n "$release_image" ] || incomplete "could not find the goreleaser-cross image in .github/actions/publish/action.yml"

# musl_root: prints a directory that holds the merged musl.cc cross toolchains.
musl_root() {
  local cache="${XDG_CACHE_HOME:-$HOME/.cache}/ldcli-verify/musl-cross" entry arch sum tgz
  if [ ! -f "$cache/root/.complete" ]; then
    rm -rf "$cache/root" && mkdir -p "$cache/dl" "$cache/root" || return 1
    for entry in "${MUSL_TOOLCHAINS[@]}"; do
      read -r arch sum <<<"$entry"
      tgz="$cache/dl/$arch-linux-musl-cross.tgz"
      if ! echo "$sum  $tgz" | sha256sum -c --status 2>/dev/null; then
        curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 "https://musl.cc/$arch-linux-musl-cross.tgz" -o "$tgz" >&2 || return 1
        echo "$sum  $tgz" | sha256sum -c --status || { echo "checksum mismatch for $tgz" >&2; return 1; }
      fi
      tar -xzf "$tgz" -C "$cache/root" --strip-components=1 || return 1
    done
    touch "$cache/root/.complete"
  fi
  printf '%s\n' "$cache/root"
}

# A failed pull is a gap in this environment, not a property of the PR. It must
# never count as a failure, because base repeats it and the verdict then files
# it as pre-existing.
extra=()
if docker image inspect "$release_image" >/dev/null 2>&1 || run docker pull -q "$release_image" >"$ARTIFACTS/pull.out" 2>&1; then
  image="$release_image"
  image_label="the release image"
  detail "- Image: release image \`${image:0:60}…\`"
else
  cat "$ARTIFACTS/pull.out"
  image="$PUBLIC_IMAGE"
  image_label="the public image with musl.cc toolchains, not the release image"
  detail "- Release image not pullable ($(tail -n1 "$ARTIFACTS/pull.out" | cut -c1-120)). Fallback: public \`goreleaser/goreleaser-cross:v1.24.2\` (\`${image:0:60}…\`) with sha256-pinned musl.cc toolchains at \`/musl\`."
  if ! docker image inspect "$image" >/dev/null 2>&1 && ! run docker pull -q "$image" >"$ARTIFACTS/pull-public.out" 2>&1; then
    cat "$ARTIFACTS/pull-public.out"
    incomplete "could not pull the goreleaser-cross release image or the public fallback image ($(tail -n1 "$ARTIFACTS/pull-public.out" | cut -c1-120))"
  fi
  musl=$(musl_root) || incomplete "could not fetch the musl.cc cross toolchains for the public goreleaser-cross image"
  extra+=(-v "$musl:/musl:ro")
fi

# A worktree's .git file points into the main repository's git dir, so both are
# mounted at their real paths (the publish action mounts "$PWD:$PWD" likewise).
common=$(cd "$WT" && cd "$(git rev-parse --git-common-dir)" && pwd)
run docker run --rm -v "$WT:$WT" -v "$common:$common" "${extra[@]}" \
  -v ldcli-verify-gomod:/root/go/pkg/mod -v ldcli-verify-gocache:/root/.cache/go-build \
  -w "$WT" --entrypoint bash "$image" -c \
  "git config --global --add safe.directory '*' && goreleaser build --snapshot --clean --config .goreleaser.yaml && for f in dist/*/ldcli*; do printf '%s: ' \"\$f\"; file -b \"\$f\" | cut -d, -f1-2; done" \
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
targets=$(grep -cE '^dist/' "$ARTIFACTS/goreleaser.out")
[ "$targets" -gt 0 ] || incomplete "goreleaser exited 0, but no binary was found under dist/"
grep -E '^dist/' "$ARTIFACTS/goreleaser.out" | sed 's/^/- /' >>"$ARTIFACTS/details.md"
if [ "$image" != "$release_image" ]; then
  detail "- Fidelity gap: the public image plus musl.cc replace the LaunchDarkly image, which can have extras that this build does not have."
fi
detail "- Fidelity gap: the binaries are built, but not executed."
detail "- Fidelity gap: \`goreleaser build\` does not run the \`dockers\`, \`docker_manifests\`, or \`brews\` steps."
pass "All $targets release targets build in goreleaser-cross ($image_label)"
