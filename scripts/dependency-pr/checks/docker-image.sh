#!/usr/bin/env bash
# The published image (Dockerfile.goreleaser) is only built at release time.
# This builds it from the PR with a binary made the way the release linux
# target makes it (CGO for SQLite, musl, static), then runs it.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

dockerfile="Dockerfile.goreleaser"
while IFS= read -r u; do
  [ -n "$u" ] || continue
  name=$(jq -r '.name' <<<"$u")
  tag=$(jq -r '.to' <<<"$u")
  case "$name" in
    */*.*/* | *.*/*) detail "- \`$name:$tag\`: not on Docker Hub, so the tag was not looked up" ;;
    *)
      repo="$name"
      [[ "$repo" == */* ]] || repo="library/$repo"
      code=$(curl -sS -o "$ARTIFACTS/tag.json" -w '%{http_code}' "https://hub.docker.com/v2/repositories/$repo/tags/$tag") || code=000
      case "$code" in
        200) detail "- \`$name:$tag\` exists (digest \`$(jq -r '.digest // "?"' "$ARTIFACTS/tag.json" | cut -c1-19)\`, pushed $(jq -r '.tag_last_pushed // .last_updated // "?"' "$ARTIFACTS/tag.json"))" ;;
        404) fingerprint "missing-tag:$name:$tag"; fail "Base image tag $name:$tag does not exist on Docker Hub" ;;
        *) incomplete "Docker Hub lookup for $name:$tag failed (HTTP $code)" ;;
      esac
      ;;
  esac
done < <(updates_for docker)

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  incomplete "Docker is not available. The base tag exists, but the release image was not built or run"
fi
command -v musl-gcc >/dev/null 2>&1 || incomplete "musl-gcc is not installed (package musl-tools), so the static release binary was not built"

ctx="$ARTIFACTS/ctx"
mkdir -p "$ctx"
if ! run env CGO_ENABLED=1 CC=musl-gcc GOOS=linux go build -ldflags '-s -w -extldflags "-static"' -o "$ctx/ldcli" . >"$ARTIFACTS/go-build.out" 2>&1; then
  cat "$ARTIFACTS/go-build.out"
  fingerprint "$(sed -E 's/:[0-9]+:[0-9]+:/:/' "$ARTIFACTS/go-build.out")"
  detail_block "$ARTIFACTS/go-build.out" 20
  fail "The static musl build of ldcli fails"
fi
cp "$dockerfile" "$ctx/Dockerfile"
base_image=$(sed -n 's/^FROM[[:space:]]\{1,\}\([^[:space:]]*\).*/\1/p' "$dockerfile" | head -n1)
if ! run docker pull -q "$base_image" >"$ARTIFACTS/pull.out" 2>&1; then
  cat "$ARTIFACTS/pull.out"
  incomplete "could not pull $base_image ($(tail -n1 "$ARTIFACTS/pull.out" | cut -c1-120))"
fi
img="ldcli-verify:$SIDE-$$"
if ! run docker build -q -t "$img" "$ctx" >"$ARTIFACTS/build.out" 2>&1; then
  cat "$ARTIFACTS/build.out"
  fingerprint_file "$ARTIFACTS/build.out"
  detail_block "$ARTIFACTS/build.out" 20
  fail "docker build of $dockerfile fails"
fi
cleanup() {
  [ -n "${cid:-}" ] && docker rm -f "$cid" >/dev/null 2>&1
  docker rmi -f "$img" >/dev/null 2>&1
}
trap cleanup EXIT

problems=()
version=$(docker run --rm "$img" --version 2>&1) || problems+=("ldcli --version fails in the image: $version")
detail "- \`ldcli --version\` in the image: $version"
docker run --rm --entrypoint cat "$img" /etc/os-release 2>/dev/null | sed -n 's/^PRETTY_NAME=/- image OS: /p' | tr -d '"' >>"$ARTIFACTS/details.md"

# HTTPS to LaunchDarkly must work with the CA bundle of the image.
if docker run --rm --entrypoint wget "$img" -q -O /dev/null https://app.launchdarkly.com >"$ARTIFACTS/tls.out" 2>&1; then
  detail "- HTTPS to app.launchdarkly.com works with the CA bundle of the image"
elif grep -qiE 'certificate|ssl|tls' "$ARTIFACTS/tls.out"; then
  problems+=("HTTPS fails in the image: $(head -n1 "$ARTIFACTS/tls.out")")
else
  detail "- HTTPS from the image was not tested (no network from the container: $(head -n1 "$ARTIFACTS/tls.out"))"
fi

# The dev server uses SQLite through CGO; start it inside the image.
port=$(free_port) || incomplete "no free port for the dev server"
cid=$(docker run -d -p "127.0.0.1:$port:8765" "$img" dev-server start --port 8765 --analytics-opt-out --access-token verify-smoke-placeholder 2>"$ARTIFACTS/run.err")
up=false
for _ in $(seq 1 60); do
  if out=$(curl -fsS "http://127.0.0.1:$port/dev/projects" 2>/dev/null); then up=true; break; fi
  sleep 0.5
done
if [ "$up" = true ] && jq -e 'type == "array"' <<<"$out" >/dev/null 2>&1; then
  detail "- dev-server in the image served /dev/projects (\`${out:0:40}\`)"
else
  docker logs "$cid" >"$ARTIFACTS/dev-server.log" 2>&1
  detail_block "$ARTIFACTS/dev-server.log" 15
  problems+=("dev-server does not start in the image")
fi

if [ ${#problems[@]} -gt 0 ]; then
  fingerprint "$(printf '%s\n' "${problems[@]}")"
  fail "$(join_by '; ' "${problems[@]}")"
fi
pass "The release image builds; ldcli, HTTPS, and the dev server (SQLite) work in it ($version)"
