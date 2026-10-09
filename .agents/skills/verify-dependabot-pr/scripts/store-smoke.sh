#!/usr/bin/env bash
# Builds ldcli, starts the dev-server with a dummy token, and checks that it
# serves the UI and the API, creates its databases, and still works after a
# restart against the same data.
#
#   scripts/store-smoke.sh
#
# Run it from the root of the tree under test, after sourcing isolate.sh.
#
# Exit codes:
#   0  every check passed
#   1  a check failed; the dev-server log is printed
#   3  CGO or a C compiler is missing, so the SQLite driver can't be built

set -euo pipefail

for var in SMOKE_DIR SMOKE_PORT XDG_STATE_HOME LD_ANALYTICS_OPT_OUT; do
  if [ -z "${!var:-}" ]; then
    echo "$var isn't set. Run: source scripts/isolate.sh" >&2
    exit 1
  fi
done

if [ "$(go env CGO_ENABLED)" != "1" ] || ! command -v "$(go env CC)" >/dev/null 2>&1; then
  echo "CGO is off or there's no C compiler, so the SQLite driver can't be built." >&2
  echo "Run go test ./internal/dev_server/... instead and say so in the report." >&2
  exit 3
fi

base="http://127.0.0.1:$SMOKE_PORT"
log="$SMOKE_DIR/dev-server.log"
db_dir="$XDG_STATE_HOME/ldcli"
pid=""

stop_server() {
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    wait "$pid" 2>/dev/null || true
  fi
  pid=""
}
trap stop_server EXIT

fail() {
  echo "FAIL: $1" >&2
  echo "--- dev-server log ($log) ---" >&2
  tail -n 40 "$log" >&2 || true
  exit 1
}

start_server() {
  ./ldcli dev-server start --port "$SMOKE_PORT" --access-token dummy-for-local-smoke >>"$log" 2>&1 &
  pid=$!
  for _ in $(seq 1 60); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' "$base/ui/")" = "200" ]; then
      return 0
    fi
    kill -0 "$pid" 2>/dev/null || fail "dev-server exited during startup"
    sleep 0.5
  done
  fail "/ui/ didn't return 200 within 30 seconds"
}

check_status() {
  local path="$1" code
  code="$(curl -s -o /dev/null -w '%{http_code}' "$base$path")"
  [ "$code" = "200" ] || fail "GET $path returned $code"
  echo "PASS: GET $path returned 200"
}

echo "Building ldcli..."
make build >/dev/null

start_server
check_status /ui/
check_status /dev/projects
for db in dev_server.db dev_server_events.db; do
  [ -f "$db_dir/$db" ] || fail "$db_dir/$db wasn't created"
  echo "PASS: $db created under the temporary state directory"
done

stop_server
echo "Restarting against the same state directory..."
start_server
check_status /ui/
check_status /dev/projects
stop_server

echo "All store checks passed."
