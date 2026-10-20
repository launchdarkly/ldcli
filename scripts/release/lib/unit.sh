# shellcheck shell=bash disable=SC2034  # The variables are for the check that sources this file.
# Helpers for agent-written release checks. A check starts with:
#   source "$VERIFY_ROOT/lib/check.sh"
#   source "$RELEASE_ROOT/lib/unit.sh"
#
# SIDE is "base" (the last release), "pr" (the candidate), or "revert" (the
# candidate with some commits reverted). WT is the worktree for that side.

# ldcli_bin: prints the path of an ldcli binary built from $WT. Each commit
# builds once per run, so many checks can share the build.
ldcli_bin() {
  local sha dir
  sha=$(git -C "$WT" rev-parse HEAD) || return 1
  if [ -n "$(git -C "$WT" status --porcelain --untracked-files=no)" ]; then
    sha="$sha-dirty-$$"
  fi
  dir="$RELEASE_OUT/state/bin/$sha"
  if [ ! -x "$dir/ldcli" ]; then
    mkdir -p "$dir"
    (cd "$WT" && go build -o "$dir/ldcli.tmp.$$" . >&2) || return 1
    mv "$dir/ldcli.tmp.$$" "$dir/ldcli"
  fi
  printf '%s\n' "$dir/ldcli"
}

# fake_api_start <routes.json>: starts a fake LaunchDarkly API (lib/fake_api.py)
# and sets FAKE_API_URI and FAKE_API_RECORD (one JSON line for each request).
fake_api_start() {
  local routes="$1" pf="$ARTIFACTS/fake-api.port"
  FAKE_API_RECORD="$ARTIFACTS/fake-api.requests.jsonl"
  : >"$FAKE_API_RECORD"
  rm -f "$pf"
  python3 "$RELEASE_ROOT/lib/fake_api.py" --routes "$routes" --record "$FAKE_API_RECORD" --port-file "$pf" \
    >"$ARTIFACTS/fake-api.log" 2>&1 &
  FAKE_API_PID=$!
  for _ in $(seq 1 50); do
    [ -s "$pf" ] && break
    sleep 0.1
  done
  [ -s "$pf" ] || return 1
  FAKE_API_URI="http://127.0.0.1:$(cat "$pf")"
  trap 'kill $FAKE_API_PID 2>/dev/null' EXIT
}

# live_api: succeeds if a read-only proxy to a live LaunchDarkly account is
# available, and sets LIVE_BASE_URI and LIVE_PROJECT. The proxy refuses writes
# and adds the token, so pass any placeholder as --access-token.
live_api() {
  [ -n "${RELEASE_LIVE_BASE_URI:-}" ] || return 1
  LIVE_BASE_URI="$RELEASE_LIVE_BASE_URI"
  LIVE_PROJECT="${RELEASE_LIVE_PROJECT:-}"
}

# wait_http <url> [tries]: waits until the URL answers (any HTTP status).
wait_http() {
  for _ in $(seq 1 "${2:-60}"); do
    curl -s -o /dev/null "$1" && return 0
    sleep 0.5
  done
  return 1
}
