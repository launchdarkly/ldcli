# Helpers sourced by every check script (baseline and generated).
#
# Environment provided by the runner:
#   WT              worktree under test (the check's working directory)
#   SIDE            "pr" or "base"
#   BASE_WT PR_WT   both worktrees, for checks that compare sides
#   ARTIFACTS       per-check, per-side directory for status and evidence
#   CLASSIFICATION  path to classification.json
#   PR_META         path to pr.json (PR metadata, CI rollup, merge state)
#   PROFILE         "fast" or "full"
#   VERIFY_ROOT     scripts/dependency-pr
#
# Protocol: finish with exactly one of these. A script that exits without one
# (crash, timeout, `set -e` abort) is recorded as "error".
#   pass "summary"                the check found no problem
#   info "summary"                no problem, but the summary is worth showing
#   fail "summary"                the PR must not merge as it is (add `recommend` for the fix)
#   decide "question" "evidence"  only a person can make this choice; ask one exact question
#   incomplete "reason"           the check could not run here (missing tool, no network)
#   skip "reason"                 the check does not apply to this PR
# Everything printed to stdout/stderr goes to the check's log.

: "${ARTIFACTS:?ARTIFACTS must be set by the runner}"
: "${WT:?WT must be set by the runner}"
mkdir -p "$ARTIFACTS"

_finish() {
  printf '%s\n' "$1" >"$ARTIFACTS/status"
  shift
  printf '%s\n' "$*" >"$ARTIFACTS/summary"
  exit 0
}
pass() { _finish pass "$@"; }
info() { _finish info "$@"; }
fail() { _finish fail "$@"; }
skip() { _finish skip "$@"; }
incomplete() { _finish incomplete "$@"; }
decide() {
  printf '%s\n' "$1" >"$ARTIFACTS/question"
  shift
  _finish decide "$@"
}

# The fix for a failure, shown in the comment.
recommend() { printf '%s\n' "$*" >>"$ARTIFACTS/recommendations"; }

# A machine-applicable fix for a failure, copied into result.json so that a
# later step (apply-fixes.sh) can run it, commit the expected paths, and
# re-run verify.sh. Only record deterministic, mechanical commands.
#   fix_recipe <id> <command run from the repo root> <expected path>...
# Set FIX_NEEDS_DECISION=1 when a person must approve the fix first.
# Set FIX_KIND=comment when the fix is a PR comment (e.g. "@dependabot rebase").
fix_recipe() {
  local id="$1" cmd="$2"
  shift 2
  jq -n --arg id "$id" --arg cmd "$cmd" --arg decision "${FIX_NEEDS_DECISION:-0}" --arg kind "${FIX_KIND:-commit}" \
    '{id: $id, kind: $kind, command: $cmd, paths: $ARGS.positional, needs_decision: ($decision == "1")}' \
    --args "$@" >"$ARTIFACTS/fix.json"
}

# Markdown lines shown under the check in the comment's details section.
detail() { printf '%s\n' "$*" >>"$ARTIFACTS/details.md"; }
detail_block() {
  # detail_block <file> [max_lines]
  local f="$1" max="${2:-40}"
  {
    printf '```\n'
    head -n "$max" "$f"
    local n
    n=$(wc -l <"$f")
    [ "$n" -gt "$max" ] && printf '… (%s more lines, see log)\n' "$((n - max))"
    printf '```\n'
  } >>"$ARTIFACTS/details.md"
}

# Fingerprint of a failure. When a check fails on the PR it is re-run on base;
# an identical fingerprint there means the failure is pre-existing on main.
# Keep it free of absolute paths, timings, and line numbers that can shift.
fingerprint() { printf '%s' "$*" | sha256sum | cut -c1-16 >"$ARTIFACTS/fingerprint"; }
fingerprint_file() { sha256sum <"$1" | cut -c1-16 >"$ARTIFACTS/fingerprint"; }

join_by() {
  local sep="$1" out="" item
  shift
  for item in "$@"; do out="${out:+$out$sep}$item"; done
  printf '%s' "$out"
}

run() {
  printf '+ %s\n' "$*" >&2
  "$@"
}

# go_tool <module/cmd/path> <version>: prints the path of a pinned Go tool,
# installing it once into a shared cache so its download output stays out of
# check results.
go_tool() {
  local pkg="$1" version="$2" name dir
  name="$(basename "$pkg")"
  command -v "$name" >/dev/null 2>&1 && { command -v "$name"; return 0; }
  dir="${XDG_CACHE_HOME:-$HOME/.cache}/ldcli-verify/bin/$name-$version"
  if [ ! -x "$dir/$name" ]; then
    mkdir -p "$dir"
    GOBIN="$dir" go install "$pkg@$version" >&2 || return 1
  fi
  printf '%s\n' "$dir/$name"
}

# Updates of one ecosystem from classification.json as compact JSON lines.
updates_for() {
  jq -c --arg e "$1" '.updates[] | select(.ecosystem == $e)' "$CLASSIFICATION"
}

has_ecosystem() {
  jq -e --arg e "$1" '.ecosystems | index($e) != null' "$CLASSIFICATION" >/dev/null
}

UI_DIR_REL="internal/dev_server/ui"

# Installs UI dependencies once per lockfile content.
ensure_ui_deps() {
  local dir="$WT/$UI_DIR_REL" stamp hash
  stamp="$dir/node_modules/.verify-lock-hash"
  hash=$(sha256sum <"$dir/package-lock.json" | cut -c1-16)
  if [ -f "$stamp" ] && [ "$(cat "$stamp")" = "$hash" ]; then
    return 0
  fi
  (cd "$dir" && run npm ci --no-audit --no-fund) || return 1
  printf '%s\n' "$hash" >"$stamp"
}

build_ldcli() {
  # build_ldcli <output-path>
  (cd "$WT" && run go build -o "$1" .)
}

free_port() {
  local p
  for _ in $(seq 1 50); do
    p=$((20000 + RANDOM % 20000))
    if ! (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null; then
      printf '%s\n' "$p"
      return 0
    fi
  done
  return 1
}

JOBS="$(nproc 2>/dev/null || echo 4)"

_subcommands_of() {
  "$1" __complete "$2" "" 2>/dev/null |
    awk -F'\t' -v p="$2" '!/^:/ && $1 != "" && $1 !~ /^-/ && $1 != "help" {print p " " $1}'
}
_help_one() {
  # _help_one <bin> <dir> <command words>; unquoted $3 splits "flags list" into args.
  local f
  f="$2/$(printf '%s' "$3" | tr ' ' '_').txt"
  # shellcheck disable=SC2086
  { printf '### ldcli %s\n' "$3"; "$1" $3 --help 2>&1; } >"$f" || printf '%s\n' "$3" >>"$2/.failures"
}
export -f _subcommands_of _help_one

# Top-level commands and their direct subcommands, via cobra's hidden
# completion command, one per line ("flags", "flags list", ...).
list_commands() {
  local bin="$1" top
  top=$("$bin" __complete "" 2>/dev/null | awk -F'\t' '!/^:/ && $1 != "" && $1 != "help" {print $1}')
  {
    printf '%s\n' "$top"
    printf '%s\n' "$top" | xargs -P "$JOBS" -I{} bash -c '_subcommands_of "$0" "$1"' "$bin" {}
  } | sort -u
}

# help_dump <bin> <dir>: writes <dir>/all.txt (help for every command, sorted),
# <dir>/.commands and <dir>/.failures (commands whose --help exits non-zero).
help_dump() {
  local bin="$1" dir="$2" c
  mkdir -p "$dir/cmd"
  : >"$dir/.failures"
  list_commands "$bin" >"$dir/.commands"
  xargs -P "$JOBS" -I{} bash -c '_help_one "$0" "$1" "$2"' "$bin" "$dir/cmd" {} <"$dir/.commands"
  mv "$dir/cmd/.failures" "$dir/.failures" 2>/dev/null || true
  {
    printf '### ldcli\n'
    "$bin" --help 2>&1
    while IFS= read -r c; do cat "$dir/cmd/$(printf '%s' "$c" | tr ' ' '_').txt"; done <"$dir/.commands"
  } >"$dir/all.txt"
}

restore_tree() {
  git -C "$WT" checkout -q -- . && git -C "$WT" clean -fdq
}
