#!/usr/bin/env bash
# Verifies a release candidate (the release-please PR) against the last
# release, without side effects on GitHub.
#
# Usage:
#   scripts/release/verify.sh [--pr N | --head REF] [options]
#
# With no --pr or --head, the script finds the open release-please PR.
#
# Options:
#   --repo OWNER/NAME   GitHub repository (default: launchdarkly/ldcli)
#   --remote NAME       git remote to fetch from (default: origin)
#   --since TAG         the last release (default: the newest v* tag before the head)
#   --out-dir DIR       output directory (default: .verify-out/release-pr-N or release-<head>)
#   --profile P         full (default: adds the goreleaser snapshot, Docker image,
#                       golangci-lint) | fast
#   --phase P           all (default) | prepare | baseline | units | render
#                         prepare:  worktrees, metadata, and the change inventory
#                         baseline: the baseline checks (runs prepare first if needed)
#                         units:    the agent checks in generated/checks.json
#                         render:   the verdict, result.json, and report.md
#                       "all" runs prepare, baseline, units (if a plan exists), render.
#   --only IDS          comma-separated baseline check ids (debugging)
#   --only-unit IDS     comma-separated unit ids for --phase units
#   --no-pr-meta        do not read PR metadata or PR text from GitHub
#   -h, --help
#
# Outputs (in the out dir): inventory.json, inventory.md, prs/<N>.md (untrusted
# PR text), result.json, report.md, logs/, work/{base,pr} (git worktrees),
# work/revert-* (candidate minus some commits), state/.
# The agent writes agent/plan.json, generated/checks.json, generated/*.sh, and
# a separate judge agent writes agent/judgement.json.
#
# Worktree "base" is the last release tag. Worktree "pr" is the release
# candidate (the head of the release PR). SIDE=pr in a check means the candidate.
#
# Verdicts: safe-to-release, needs-human, block, incomplete.
# Exit: 0 safe to release, 1 needs-human or block, 2 incomplete or verifier error.
# This script does not push, comment, approve, merge, or tag.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="$(dirname "$SCRIPT_DIR")"
source "$SCRIPTS_DIR/dependency-pr/lib/common.sh"
source "$SCRIPTS_DIR/dependency-pr/lib/runner.sh"
RELEASE_ROOT="$SCRIPT_DIR"
export RELEASE_ROOT

PR="" HEAD_REF="" REPO="launchdarkly/ldcli" REMOTE="origin" SINCE="" OUT=""
PROFILE="full" PHASE="all" ONLY="" ONLY_UNIT="" NO_PR_META=false
RELEASE_BRANCH="release-please--branches--main"

usage() { sed -n '2,/^set -uo/p' "$0" | sed '$d; s/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --pr) PR="${2:?}"; shift 2 ;;
    --head) HEAD_REF="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --remote) REMOTE="${2:?}"; shift 2 ;;
    --since) SINCE="${2:?}"; shift 2 ;;
    --out-dir) OUT="${2:?}"; shift 2 ;;
    --profile) PROFILE="${2:?}"; shift 2 ;;
    --phase) PHASE="${2:?}"; shift 2 ;;
    --only) ONLY="${2:?}"; shift 2 ;;
    --only-unit) ONLY_UNIT="${2:?}"; shift 2 ;;
    --no-pr-meta) NO_PR_META=true; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; die_infra "unknown argument: $1" ;;
  esac
done

[ -z "$PR" ] || [ -z "$HEAD_REF" ] || die_infra "pass only one of --pr and --head"
[ -z "$PR" ] || [[ "$PR" =~ ^[0-9]+$ ]] || die_infra "--pr must be a number"
case "$PROFILE" in fast | full) ;; *) die_infra "--profile must be fast or full" ;; esac
case "$PHASE" in all | prepare | baseline | units | render) ;; *) die_infra "--phase must be all, prepare, baseline, units, or render" ;; esac

require_tools git jq python3
REPO_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)" || die_infra "not inside a git repository"

if [ -z "$PR" ] && [ -z "$HEAD_REF" ]; then
  [ "$NO_PR_META" != true ] || die_infra "pass --pr or --head with --no-pr-meta"
  require_tools gh
  PR=$(gh pr list --repo "$REPO" --head "$RELEASE_BRANCH" --state open --json number --jq '.[0].number // empty') ||
    die_infra "could not list PRs in $REPO (is gh authenticated?)"
  [ -n "$PR" ] || die_infra "no open release-please PR (head $RELEASE_BRANCH) in $REPO"
  log "release PR: #$PR"
fi

RUN_KEY="release-pr-$PR"
[ -n "$HEAD_REF" ] && RUN_KEY="release-$(sanitize "$HEAD_REF")"
OUT="${OUT:-$REPO_ROOT/.verify-out/$RUN_KEY}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
WORK="$OUT/work" STATE="$OUT/state" LOGS="$OUT/logs"
BASE_WT="$WORK/base" PR_WT="$WORK/pr"
REF_NS="refs/verify/$RUN_KEY"
mkdir -p "$STATE" "$LOGS"
RELEASE_INVENTORY="$OUT/inventory.json"
RELEASE_OUT="$OUT"
export RELEASE_INVENTORY RELEASE_OUT

init_go_cache_env
VERIFY_SECRETS="$(collect_secrets)"
export -n VERIFY_SECRETS

fetch_with_retry() {
  local i
  for i in 1 2 3 4; do
    git -C "$REPO_ROOT" fetch -q --no-tags "$REMOTE" "$@" && return 0
    log "fetch failed (attempt $i); retrying"
    sleep $((2 ** (i + 1)))
  done
  return 1
}

remove_worktree() {
  if [ -d "$1" ]; then
    git -C "$REPO_ROOT" worktree remove --force "$1" 2>/dev/null || rm -rf "$1"
  fi
  git -C "$REPO_ROOT" worktree prune
}

# ---------------------------------------------------------------- prepare

prepare() {
  local gh_json="$STATE/gh-pr.json"
  echo 'null' >"$gh_json"
  if [ -n "$PR" ]; then
    if [ "$NO_PR_META" != true ]; then
      require_tools gh
      gh pr view "$PR" --repo "$REPO" \
        --json number,url,title,body,author,headRefName,headRefOid,baseRefName,state,isDraft,statusCheckRollup \
        >"$gh_json" || die_infra "could not read PR #$PR from $REPO"
    fi
    log "fetching refs/pull/$PR/head from $REMOTE"
    fetch_with_retry "+refs/pull/$PR/head:$REF_NS/head" || die_infra "could not fetch PR #$PR"
  else
    git -C "$REPO_ROOT" update-ref "$REF_NS/head" "$(git -C "$REPO_ROOT" rev-parse "$HEAD_REF^{commit}")" ||
      die_infra "unknown --head $HEAD_REF"
  fi
  HEAD_SHA=$(git -C "$REPO_ROOT" rev-parse "$REF_NS/head")

  git -C "$REPO_ROOT" fetch -q --tags "$REMOTE" 2>/dev/null || log "warning: could not fetch tags; using local tags"
  if [ -z "$SINCE" ]; then
    # The newest release tag in the history of the head, other than a tag on the head itself.
    SINCE=$(git -C "$REPO_ROOT" describe --tags --abbrev=0 --match 'v[0-9]*' "$HEAD_SHA^" 2>/dev/null) ||
      die_infra "no v* tag in the history of $HEAD_SHA; pass --since"
  fi
  BASE_SHA=$(git -C "$REPO_ROOT" rev-parse "$SINCE^{commit}") || die_infra "unknown --since $SINCE"
  git -C "$REPO_ROOT" merge-base --is-ancestor "$BASE_SHA" "$HEAD_SHA" ||
    die_infra "$SINCE is not an ancestor of the candidate $HEAD_SHA"
  log "candidate ${HEAD_SHA:0:7}, last release $SINCE (${BASE_SHA:0:7})"

  remove_worktree "$BASE_WT"
  remove_worktree "$PR_WT"
  local d
  for d in "$WORK"/revert-*; do [ -d "$d" ] && remove_worktree "$d"; done
  mkdir -p "$WORK"
  git -C "$REPO_ROOT" worktree add -q --detach "$BASE_WT" "$BASE_SHA" || die_infra "worktree add failed"
  git -C "$REPO_ROOT" worktree add -q --detach "$PR_WT" "$HEAD_SHA" || die_infra "worktree add failed"
  git -C "$REPO_ROOT" diff --name-only "$BASE_SHA" "$HEAD_SHA" >"$STATE/changed-files.txt"

  # CI on the release head, and on the main commit below it: a PR that
  # GITHUB_TOKEN opens starts no workflows, so main is often the only CI signal.
  local parent runs_head='null' runs_parent='null'
  parent=$(git -C "$REPO_ROOT" rev-parse "$HEAD_SHA^1" 2>/dev/null || true)
  if [ "$NO_PR_META" != true ] && command -v gh >/dev/null 2>&1; then
    runs_head=$(ci_runs "$HEAD_SHA") || runs_head='null'
    [ -n "$parent" ] && { runs_parent=$(ci_runs "$parent") || runs_parent='null'; }
  fi

  local version base_version
  version=$(git -C "$REPO_ROOT" show "$HEAD_SHA:.release-please-manifest.json" 2>/dev/null | jq -r '."." // empty')
  base_version=$(git -C "$REPO_ROOT" show "$BASE_SHA:.release-please-manifest.json" 2>/dev/null | jq -r '."." // empty')
  jq -n --slurpfile gh "$gh_json" --arg repo "$REPO" --arg head "$HEAD_SHA" --arg base "$BASE_SHA" \
    --arg since "$SINCE" --arg parent "$parent" --arg version "$version" --arg base_version "$base_version" \
    --arg pr_ref "$PR" --argjson runs_head "$runs_head" --argjson runs_parent "$runs_parent" '
    ($gh[0]) as $g
    | {
        mode: "release", repo: $repo,
        number: ($g.number // (if $pr_ref == "" then null else ($pr_ref | tonumber) end)),
        url: ($g.url // null), title: ($g.title // null), author: ($g.author.login // null),
        state: ($g.state // null), head_ref: ($g.headRefName // null), head_sha: $head,
        base_ref: $since, base_sha: $base, since: $since, parent_sha: $parent,
        version: $version, base_version: $base_version,
        tested: {base: $base, pr: $head},
        merge: {status: "not-merged", conflicts: []},
        ci_head: $head, ci_runs: $runs_head, ci_runs_parent: $runs_parent,
        ci: (if $g == null or ($g | has("statusCheckRollup") | not) then null
             else ($g.statusCheckRollup // []) | map({name: (.name // .context), status, conclusion, state, workflow: .workflowName})
             end)
      }' >"$STATE/pr.json" || die_infra "could not write pr.json"

  log "building the change inventory"
  local gh_flag=()
  [ "$NO_PR_META" = true ] && gh_flag=(--no-gh)
  python3 "$RELEASE_ROOT/lib/inventory.py" --repo-root "$REPO_ROOT" --base "$BASE_SHA" --base-label "$SINCE" \
    --head "$HEAD_SHA" --out-dir "$OUT" --gh-repo "$REPO" "${gh_flag[@]}" || die_infra "inventory failed"
  # The dependency-pr checks read the ecosystems from a classification file.
  jq '{schema: 1, tier: "high", tier_reasons: ["release"], tags: [], updates: [],
       ecosystems: (["gomod", "npm-ui", "npm-wrapper", "github-actions", "docker"])}' \
    "$RELEASE_INVENTORY" >"$STATE/classification.json"
  printf '%s\n' "$PROFILE" >"$STATE/profile"
  log "inventory: $OUT/inventory.md"
}

ci_runs() {
  gh api --paginate "repos/$REPO/actions/runs?head_sha=$1&per_page=100" \
    --jq '.workflow_runs[] | {id, name, path, event, status, conclusion, head_sha, html_url}' 2>/dev/null | jq -s '.'
}

ensure_prepared() {
  [ -f "$STATE/pr.json" ] && [ -f "$RELEASE_INVENTORY" ] && [ -d "$PR_WT" ] && [ -d "$BASE_WT" ] ||
    die_infra "no prepared run in $OUT; run with --phase prepare first"
  local saved
  saved=$(cat "$STATE/profile" 2>/dev/null || true)
  [ -n "$saved" ] && PROFILE="$saved"
}

# ---------------------------------------------------------------- baseline

run_baseline() {
  local registry="$RELEASE_ROOT/checks/registry.json"
  # With --only, keep the results of the other checks.
  [ -n "$ONLY" ] || rm -rf "$STATE/checks"
  mkdir -p "$STATE/checks"
  local entries=() c id script prof timeout status
  mapfile -t entries < <(jq -c '.checks[]' "$registry")
  log "running baseline checks (profile $PROFILE)"
  for c in "${entries[@]}"; do
    id=$(jq -r '.id' <<<"$c")
    if [ -n "$ONLY" ] && [[ ",$ONLY," != *",$id,"* ]]; then continue; fi
    script="$SCRIPTS_DIR/$(jq -r '.script' <<<"$c")"
    [ -f "$script" ] || die_infra "registry entry $id: $script not found"
    prof=$(jq -r '.profile // "fast"' <<<"$c")
    timeout=$(jq -r '.timeout // 600' <<<"$c")
    rm -rf "$STATE/checks/$id"
    mkdir -p "$STATE/checks/$id"
    printf '%s\n' "$c" >"$STATE/checks/$id/entry.json"
    if [ "$prof" = full ] && [ "$PROFILE" != full ]; then
      mkdir -p "$STATE/checks/$id/pr"
      jq -n '{status: "skip", summary: "runs with --profile full", profile_skipped: true}' >"$STATE/checks/$id/pr/result.json"
      continue
    fi
    # Agent checks can run at the same time and use the same worktrees.
    (flock 9 && run_one checks "$id" "$script" pr "$timeout") 9>"$STATE/.lock-pr"
    status=$(jq -r '.status' "$STATE/checks/$id/pr/result.json")
    if [ "$(jq -r '.compare_base // false' <<<"$c")" = true ] && { [ "$status" = fail ] || [ "$status" = decide ]; }; then
      (flock 9 && run_one checks "$id" "$script" base "$timeout") 9>"$STATE/.lock-base"
    fi
  done
  jq -r '.checks[].id' "$registry" | while IFS= read -r id; do
    [ -d "$STATE/checks/$id" ] && printf '%s\n' "$id"
  done >"$STATE/check-order.txt"
}

# ---------------------------------------------------------------- units

# revert_worktree <sha>...: prints the path of a worktree with the candidate
# minus the commits, or nothing if a revert conflicts.
revert_worktree() {
  local key dir sha
  key=$(printf '%s\n' "$@" | sort | sha256sum | cut -c1-10)
  dir="$WORK/revert-$key"
  if [ -f "$dir/.verify-revert-ok" ]; then
    printf '%s\n' "$dir"
    return 0
  fi
  [ -f "$dir/.verify-revert-failed" ] && return 1
  remove_worktree "$dir"
  git -C "$REPO_ROOT" worktree add -q --detach "$dir" "$(jq -r '.tested.pr' "$STATE/pr.json")" >&2 || return 1
  # Revert the newest commit first.
  for sha in $(git -C "$REPO_ROOT" rev-list --no-walk=sorted "$@"); do
    if ! git -C "$dir" -c user.name=release-verify -c user.email=verify@localhost revert --no-edit "$sha" >>"$LOGS/revert-$key.log" 2>&1; then
      git -C "$dir" revert --abort 2>/dev/null
      touch "$dir/.verify-revert-failed"
      log "  revert of $(printf '%s ' "$@")conflicts (see logs/revert-$key.log)"
      return 1
    fi
  done
  touch "$dir/.verify-revert-ok"
  printf '%s\n' "$dir"
}

# A read-only proxy to a live account, if the caller gives one. Checks get only
# the local proxy address. The token stays in this process and the proxy.
start_live_proxy() {
  unset RELEASE_LIVE_BASE_URI RELEASE_LIVE_PROJECT
  if [ -z "${RELEASE_VERIFY_LIVE_TOKEN:-}" ] || [ -z "${RELEASE_VERIFY_LIVE_BASE_URI:-}" ]; then
    log "no live LaunchDarkly account (RELEASE_VERIFY_LIVE_TOKEN, RELEASE_VERIFY_LIVE_BASE_URI); live checks report incomplete"
    return 0
  fi
  local pf="$STATE/live-proxy.$$.port" writes=()
  [ "${RELEASE_VERIFY_LIVE_WRITES:-}" = 1 ] && writes=(--allow-writes)
  rm -f "$pf"
  RV_TOKEN="$RELEASE_VERIFY_LIVE_TOKEN" python3 "$RELEASE_ROOT/lib/readonly_proxy.py" \
    --upstream "$RELEASE_VERIFY_LIVE_BASE_URI" --port-file "$pf" --log "$STATE/live-proxy.log" "${writes[@]}" \
    >"$LOGS/live-proxy.log" 2>&1 &
  LIVE_PROXY_PID=$!
  trap 'kill "$LIVE_PROXY_PID" 2>/dev/null' EXIT
  local i
  for i in $(seq 1 50); do [ -s "$pf" ] && break; sleep 0.1; done
  [ -s "$pf" ] || die_infra "the live proxy did not start (see $LOGS/live-proxy.log)"
  unset RELEASE_VERIFY_LIVE_TOKEN
  RELEASE_LIVE_BASE_URI="http://127.0.0.1:$(cat "$pf")"
  RELEASE_LIVE_PROJECT="${RELEASE_VERIFY_LIVE_PROJECT:-}"
  export RELEASE_LIVE_BASE_URI RELEASE_LIVE_PROJECT
  log "live account: read-only proxy at $RELEASE_LIVE_BASE_URI${writes:+ (writes allowed)}"
}

run_units() {
  local gdir="$OUT/generated" manifest="$STATE/checks-manifest.$$.json" validation="$STATE/plan-validation.$$.json"
  if ! python3 "$RELEASE_ROOT/lib/plan.py" validate --out-dir "$OUT" >"$validation"; then
    cat "$validation" >&2
    die_infra "the plan is not valid (see above)"
  fi
  jq -r '.problems[] | "  plan: \(.text)"' "$validation" >&2
  python3 "$RELEASE_ROOT/lib/plan.py" checks --out-dir "$OUT" >"$manifest" || die_infra "could not merge the checks"
  rm -f "$validation"
  mkdir -p "$STATE/generated"
  start_live_proxy
  local entries=() c id unit script timeout revert_shas rwt
  mapfile -t entries < <(jq -c '.checks[]' "$manifest")
  rm -f "$manifest"
  [ ${#entries[@]} -gt 0 ] || { log "the plan has no checks"; return 0; }
  log "running ${#entries[@]} unit check(s)"
  for c in "${entries[@]}"; do
    id=$(jq -r '.id' <<<"$c")
    unit=$(jq -r '.unit' <<<"$c")
    if [ -n "$ONLY_UNIT" ] && [[ ",$ONLY_UNIT," != *",$unit,"* ]]; then continue; fi
    script="$gdir/$(jq -r '.script' <<<"$c")"
    timeout=$(jq -r '.timeout // 600' <<<"$c")
    mkdir -p "$STATE/generated/$id"
    printf '%s\n' "$c" >"$STATE/generated/$id/entry.json"
    rm -rf "$STATE/generated/$id/revert"
    # Parallel runs (one subagent for each unit) share the worktrees, so checks
    # on the same worktree run one at a time.
    (flock 9 && run_one generated "$id" "$script" base "$timeout") 9>"$STATE/.lock-base"
    (flock 9 && run_one generated "$id" "$script" pr "$timeout") 9>"$STATE/.lock-pr"
    mapfile -t revert_shas < <(jq -r '.revert // [] | .[]' <<<"$c")
    if [ ${#revert_shas[@]} -gt 0 ]; then
      if rwt=$( (flock 9 && revert_worktree "${revert_shas[@]}") 9>"$STATE/.lock-revert"); then
        (flock 9 && PR_WT="$rwt" run_one generated "$id" "$script" revert "$timeout") 9>"$STATE/.lock-$(basename "$rwt")"
      else
        mkdir -p "$STATE/generated/$id/revert"
        jq -n '{status: "incomplete", summary: "the revert of these commits conflicts with later commits", revert_conflict: true}' \
          >"$STATE/generated/$id/revert/result.json"
      fi
    fi
  done
}

# ---------------------------------------------------------------- render

render() {
  python3 "$RELEASE_ROOT/lib/verdict.py" --out-dir "$OUT" >"$OUT/result.json" || die_infra "could not assemble result.json"
  python3 "$RELEASE_ROOT/lib/render.py" "$OUT/result.json" >"$OUT/report.md" || die_infra "could not render report.md"
  local verdict code
  verdict=$(jq -r '.verdict' "$OUT/result.json")
  code=$(jq -r '.exit_code' "$OUT/result.json")
  log "verdict: $verdict (exit $code)"
  log "result:  $OUT/result.json"
  log "report:  $OUT/report.md"
  exit "$code"
}

# ---------------------------------------------------------------- main

case "$PHASE" in
  prepare)
    prepare
    ;;
  baseline)
    [ -f "$STATE/pr.json" ] || prepare
    ensure_prepared
    require_tools go rg timeout
    run_baseline
    ;;
  units)
    ensure_prepared
    run_units
    ;;
  render)
    ensure_prepared
    render
    ;;
  all)
    require_tools go rg timeout
    prepare
    run_baseline
    [ -f "$OUT/agent/plan.json" ] && run_units
    render
    ;;
esac
