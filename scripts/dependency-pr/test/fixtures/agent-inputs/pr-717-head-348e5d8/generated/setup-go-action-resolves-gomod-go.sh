#!/usr/bin/env bash
# Runs the real setup-go action code (dist/setup/index.js) at every ref that the
# workflows pin, with the inputs that the workflows pass, against go.mod in $WT.
# Uses the network: codeload.github.com for the action and the Go download manifest.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || incomplete "no worktree"
command -v node >/dev/null || incomplete "node is not installed"
command -v curl >/dev/null || incomplete "curl is not installed"

refs=$(grep -hoE 'actions/setup-go@[^[:space:]#]+' .github/workflows/*.yml | sed 's/.*@//' | sort -u)
[ -n "$refs" ] || fail "no setup-go step found in .github/workflows"
gomod_go=$(awk '$1=="go"{print $2; exit}' go.mod)

declare -A resolved
for ref in $refs; do
  dir="$ARTIFACTS/setup-go-$ref"
  mkdir -p "$dir/src" "$dir/toolcache" "$dir/tmp"
  if ! curl -fsSL "https://codeload.github.com/actions/setup-go/tar.gz/refs/tags/$ref" | tar -xz -C "$dir/src" --strip-components=1; then
    incomplete "could not download actions/setup-go@$ref"
  fi
  : >"$dir/output" && : >"$dir/path" && : >"$dir/env"
  echo "+ node actions/setup-go@$ref/dist/setup/index.js (go-version-file: go.mod)"
  env -i PATH="/usr/bin:/bin" HOME="$ARTIFACTS" \
    "INPUT_GO-VERSION-FILE=go.mod" "INPUT_CHECK-LATEST=false" INPUT_CACHE=false INPUT_TOKEN= \
    RUNNER_TOOL_CACHE="$dir/toolcache" RUNNER_TEMP="$dir/tmp" RUNNER_OS=Linux RUNNER_ARCH=X64 \
    GITHUB_WORKSPACE="$WT" GITHUB_OUTPUT="$dir/output" GITHUB_PATH="$dir/path" GITHUB_ENV="$dir/env" \
    GITHUB_SERVER_URL=https://github.com GITHUB_ACTIONS=true \
    "$(command -v node)" "$dir/src/dist/setup/index.js" >"$dir/run.log" 2>&1
  rc=$?
  cat "$dir/run.log"
  grep -q '^::error::' "$dir/run.log" && rc=1
  v=$(sed -n 's/^go-version<<.*//; s/^go-version=//p' "$dir/output" | head -n1)
  [ -n "$v" ] || v=$(awk '/^go-version<</{getline; print; exit}' "$dir/output")
  gobin=$(head -n1 "$dir/path")
  [ $rc -eq 0 ] && [ -n "$gobin" ] && [ -x "$gobin/go" ] || {
    grep -qiE 'ENOTFOUND|ECONNRESET|ETIMEDOUT|EAI_AGAIN|getaddrinfo' "$dir/run.log" && incomplete "network error while setup-go@$ref ran"
    fail "actions/setup-go@$ref did not set up Go from go.mod (exit $rc)"
  }
  real=$("$gobin/go" version)
  echo "setup-go@$ref: go-version output '$v', binary: $real"
  resolved[$ref]="$real"
  detail "- \`actions/setup-go@$ref\` with \`go-version-file: go.mod\` (go $gomod_go): installed \`$real\`."
done

for ref in "${!resolved[@]}"; do
  [[ "${resolved[$ref]}" == "go version go${gomod_go}"* ]] ||
    fail "setup-go@$ref installed ${resolved[$ref]}, not a Go $gomod_go release"
done
pass "every pinned setup-go ref installs a Go $gomod_go release from go.mod"
