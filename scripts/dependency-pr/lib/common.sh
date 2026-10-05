# Shared helpers for scripts/dependency-pr. Source, do not execute.

VERIFY_ROOT="${VERIFY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
export VERIFY_ROOT

log() { printf '[verify] %s\n' "$*" >&2; }

die_infra() {
  printf '[verify] ERROR: %s\n' "$*" >&2
  exit 2
}

require_tools() {
  local missing=()
  for t in "$@"; do
    command -v "$t" >/dev/null 2>&1 || missing+=("$t")
  done
  [ ${#missing[@]} -eq 0 ] || die_infra "missing required tools: ${missing[*]}"
}

# Local credentials and CLI config leak into `go test` and the binary smoke
# test (several cmd/ tests fail when LD_ACCESS_TOKEN or ~/.config/ldcli exist),
# so every check runs with LD_* unset and private XDG config/state/data dirs.
# Build and module caches are kept so runs stay fast.
hermetic_env_args() {
  local sandbox="$1"
  mkdir -p "$sandbox/config" "$sandbox/state" "$sandbox/data"
  local args=()
  local v
  while IFS= read -r v; do
    args+=("-u" "$v")
  done < <(env | sed -n 's/^\(LD_[A-Za-z0-9_]*\)=.*/\1/p')
  args+=(
    "XDG_CONFIG_HOME=$sandbox/config"
    "XDG_STATE_HOME=$sandbox/state"
    "XDG_DATA_HOME=$sandbox/data"
    "GOCACHE=${GOCACHE_REAL}"
    "GOMODCACHE=${GOMODCACHE_REAL}"
    "LD_ANALYTICS_OPT_OUT=true"
    "CI=true"
    "NO_COLOR=1"
    "npm_config_fund=false"
    "npm_config_update_notifier=false"
  )
  printf '%s\n' "${args[@]}"
}

init_go_cache_env() {
  if command -v go >/dev/null 2>&1; then
    GOCACHE_REAL="$(go env GOCACHE)"
    GOMODCACHE_REAL="$(go env GOMODCACHE)"
  else
    GOCACHE_REAL="${HOME}/.cache/go-build"
    GOMODCACHE_REAL="${HOME}/go/pkg/mod"
  fi
  export GOCACHE_REAL GOMODCACHE_REAL
}

now_s() { date +%s; }

sanitize() { printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '-'; }
