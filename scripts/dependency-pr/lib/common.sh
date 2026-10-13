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
    "GH_CONFIG_DIR=${GH_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/gh}"
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

# Generated checks are written for one PR and are not reviewed like the
# baseline, so they also get a private HOME and no global or system git
# configuration. A global `insteadOf` rule can put an access token into a
# remote URL, and a check that prints the URL writes the token to its log.
# Tool caches stay shared so the checks stay fast.
generated_env_args() {
  local sandbox="$1"
  mkdir -p "$sandbox/home"
  printf '%s\n' \
    "HOME=$sandbox/home" \
    "GIT_CONFIG_GLOBAL=/dev/null" \
    "GIT_CONFIG_NOSYSTEM=1" \
    "GIT_AUTHOR_NAME=dependency-pr-verify" "GIT_AUTHOR_EMAIL=verify@localhost" \
    "GIT_COMMITTER_NAME=dependency-pr-verify" "GIT_COMMITTER_EMAIL=verify@localhost" \
    "GIT_TERMINAL_PROMPT=0" \
    "GOPATH=${GOPATH_REAL}" \
    "XDG_CACHE_HOME=${XDG_CACHE_HOME:-$HOME/.cache}" \
    "npm_config_cache=${npm_config_cache:-$HOME/.npm}" \
    "DOCKER_CONFIG=${DOCKER_CONFIG:-$HOME/.docker}"
}

# Prints the secret values that scrub.py removes from logs, one per line:
# token-like environment variables, the gh token, and credentials in the
# global git configuration. Keep the output in an unexported variable.
collect_secrets() {
  local name
  while IFS= read -r name; do
    printf '%s\n' "${!name}"
  done < <(env | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' | grep -iE 'TOKEN|SECRET|PASSWORD|PASSWD|API_KEY|ACCESS_KEY|PRIVATE_KEY|CREDENTIAL' || true)
  command -v gh >/dev/null 2>&1 && gh auth token 2>/dev/null
  git config --global --get-regexp '^(url\..*|http\..*extraheader|credential\..*)' 2>/dev/null |
    grep -oE '://[^/[:space:]@]+@|[[:alnum:]+/=_-]{16,}$' | sed -E 's#^://##; s#@$##; s#^[^:]*:##'
}

# scrub_files <file>...: removes the secrets from the files, in place.
scrub_files() {
  [ $# -gt 0 ] || return 0
  VERIFY_SECRETS="${VERIFY_SECRETS:-}" python3 "$VERIFY_ROOT/lib/scrub.py" "$@"
}

init_go_cache_env() {
  if command -v go >/dev/null 2>&1; then
    GOCACHE_REAL="$(go env GOCACHE)"
    GOMODCACHE_REAL="$(go env GOMODCACHE)"
    GOPATH_REAL="$(go env GOPATH)"
  else
    GOCACHE_REAL="${HOME}/.cache/go-build"
    GOMODCACHE_REAL="${HOME}/go/pkg/mod"
    GOPATH_REAL="${HOME}/go"
  fi
  export GOCACHE_REAL GOMODCACHE_REAL GOPATH_REAL
}

now_s() { date +%s; }

sanitize() { printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '-'; }
