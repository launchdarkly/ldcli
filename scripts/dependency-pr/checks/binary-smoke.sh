#!/usr/bin/env bash
# Builds the CLI and exercises it offline: version, help for every command,
# and a dev-server start that serves the embedded UI and the /dev API.
source "$VERIFY_ROOT/lib/check.sh"

bin="$ARTIFACTS/ldcli"
if ! build_ldcli "$bin" >"$ARTIFACTS/build.out" 2>&1; then
  cat "$ARTIFACTS/build.out"
  fingerprint "$(sed -E 's/:[0-9]+:[0-9]+:/:/' "$ARTIFACTS/build.out")"
  fail "go build fails"
fi

problems=()
version=$("$bin" --version 2>&1) || problems+=("--version exits non-zero")
detail "- \`ldcli --version\`: $version"

help_dump "$bin" "$ARTIFACTS/help"
n_cmds=$(wc -l <"$ARTIFACTS/help/.commands")
if [ -s "$ARTIFACTS/help/.failures" ]; then
  sort -o "$ARTIFACTS/help/.failures" "$ARTIFACTS/help/.failures"
  problems+=("--help fails for: $(head -n5 "$ARTIFACTS/help/.failures" | paste -sd, -)")
fi
[ "$n_cmds" -lt 20 ] && problems+=("only $n_cmds commands discovered")

port=$(free_port) || fail "no free port for dev-server"
mkdir -p "$ARTIFACTS/xdg-state"
# The flag is required but unused when no project is configured, so the
# server starts without reaching LaunchDarkly.
XDG_STATE_HOME="$ARTIFACTS/xdg-state" "$bin" dev-server start --port "$port" --analytics-opt-out \
  --access-token verify-smoke-placeholder >"$ARTIFACTS/dev-server.log" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null; wait $pid 2>/dev/null' EXIT

up=false
for _ in $(seq 1 60); do
  if curl -fsS -o "$ARTIFACTS/ui.html" "http://127.0.0.1:$port/ui/" 2>/dev/null; then
    up=true
    break
  fi
  kill -0 $pid 2>/dev/null || break
  sleep 0.5
done
if [ "$up" = true ]; then
  grep -q '<div id="root">' "$ARTIFACTS/ui.html" || problems+=("/ui/ does not serve the app shell")
  ui_kb=$(($(wc -c <"$ARTIFACTS/ui.html") / 1024))
  projects=$(curl -fsS "http://127.0.0.1:$port/dev/projects" 2>/dev/null)
  jq -e 'type == "array"' <<<"$projects" >/dev/null 2>&1 || problems+=("/dev/projects did not return a JSON array: ${projects:0:80}")
  detail "- dev-server served /ui/ (${ui_kb} KB) and /dev/projects (\`${projects:0:40}\`)"
else
  tail -n 20 "$ARTIFACTS/dev-server.log"
  detail_block "$ARTIFACTS/dev-server.log" 15
  problems+=("dev-server did not serve /ui/ within 30s")
fi

if [ ${#problems[@]} -gt 0 ]; then
  fingerprint "$(printf '%s\n' "${problems[@]}" | sed -E 's/\([0-9]+ KB\)//')"
  fail "$(IFS='; '; echo "${problems[*]}")"
fi
pass "--version, --help for $n_cmds commands, dev-server UI and API all OK"
