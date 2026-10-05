#!/usr/bin/env bash
# The published image (Dockerfile.goreleaser) is only built at release time.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

dockerfile="Dockerfile.goreleaser"
while IFS= read -r u; do
  [ -n "$u" ] || continue
  name=$(jq -r '.name' <<<"$u")
  tag=$(jq -r '.to' <<<"$u")
  case "$name" in
    */*.*/* | *.*/*) detail "- \`$name:$tag\`: not on Docker Hub; tag not resolved" ;;
    *)
      repo="$name"
      [[ "$repo" == */* ]] || repo="library/$repo"
      if info=$(curl -fsS "https://hub.docker.com/v2/repositories/$repo/tags/$tag"); then
        detail "- \`$name:$tag\` resolves (digest \`$(jq -r '.digest // "?"' <<<"$info" | cut -c1-19)\`, pushed $(jq -r '.tag_last_pushed // .last_updated // "?"' <<<"$info"))"
      else
        fingerprint "missing-tag:$name:$tag"
        fail "Base image tag $name:$tag not found on Docker Hub"
      fi
      ;;
  esac
done < <(updates_for docker)

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  skip "Docker unavailable: base tag resolves, but the release image was not built or run"
fi

ctx="$ARTIFACTS/ctx"
mkdir -p "$ctx"
# Static, CGO-free binary so it runs on musl; the release binary is built with
# CGO in goreleaser-cross, which this does not reproduce.
run env CGO_ENABLED=0 GOOS=linux go build -o "$ctx/ldcli" . || fail "static go build failed"
cp "$dockerfile" "$ctx/Dockerfile"
img="ldcli-verify:$SIDE-$$"
if ! run docker build -q -t "$img" "$ctx" >"$ARTIFACTS/build.out" 2>&1; then
  cat "$ARTIFACTS/build.out"
  fingerprint_file "$ARTIFACTS/build.out"
  detail_block "$ARTIFACTS/build.out" 20
  fail "docker build of $dockerfile fails"
fi
out=$(docker run --rm "$img" --version 2>&1)
rc=$?
docker run --rm --entrypoint cat "$img" /etc/os-release 2>/dev/null | grep -E '^PRETTY_NAME' | sed 's/^/- image: /' >>"$ARTIFACTS/details.md"
docker rmi -f "$img" >/dev/null 2>&1
if [ $rc -ne 0 ]; then
  fingerprint "run-failed"
  fail "Image builds but \`ldcli --version\` fails inside it: $out"
fi
pass "Release image builds and runs (\`$out\`)"
