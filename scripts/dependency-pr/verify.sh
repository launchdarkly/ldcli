#!/usr/bin/env bash
# Verifies a dependency-update PR (Dependabot) without side effects on GitHub.
#
# Usage:
#   scripts/dependency-pr/verify.sh (--pr N | --branch NAME) [options]
#
# Options:
#   --repo OWNER/NAME   GitHub repo for PR metadata (default: launchdarkly/ldcli)
#   --remote NAME       git remote to fetch from (default: origin)
#   --base BRANCH       base branch for --branch mode (default: main; --pr uses the PR's base)
#   --out-dir DIR       output directory (default: .verify-out/pr-N or .verify-out/branch-NAME)
#   --profile P         fast (default) | full (adds govulncheck, golangci-lint)
#   --phase P           all (default) | generated (re-run generated checks only, reusing
#                       the worktrees and baseline results) | render (recompute verdict and comment)
#   --only IDS          comma-separated baseline check ids to run (debugging)
#   --head-sha SHA      verify this commit as the PR head (replay of a past state)
#   --base-sha SHA      use this commit as the base instead of the base branch tip
#   --no-pr-meta        do not read PR metadata (title, CI results) from GitHub
#   -h, --help
#
# Outputs (in the out dir): result.json, comment.md, logs/<side>/<check>.log,
# work/{base,pr} (git worktrees), state/ (raw check records).
# Generated per-PR checks are read from <out-dir>/generated/checks.json and the
# agent's impact review from <out-dir>/agent/impact.json, when present.
#
# Verdicts: safe-to-merge, needs-human (a specific decision), block (must fix),
# incomplete (a required check or review did not run; rerun or finish the review).
# Exit: 0 safe to merge, 1 needs-human or block, 2 incomplete or verifier error.
# This script does not push, comment, approve, or merge. The caller runs
# post-comment.sh to post the comment.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/common.sh"

PR="" BRANCH="" REPO="launchdarkly/ldcli" REMOTE="origin" BASE_BRANCH="main"
OUT="" PROFILE="fast" PHASE="all" ONLY="" HEAD_PIN="" BASE_PIN="" NO_PR_META=false

usage() { sed -n '2,/^set -uo/p' "$0" | sed '$d; s/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --pr) PR="${2:?}"; shift 2 ;;
    --branch) BRANCH="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --remote) REMOTE="${2:?}"; shift 2 ;;
    --base) BASE_BRANCH="${2:?}"; shift 2 ;;
    --out-dir) OUT="${2:?}"; shift 2 ;;
    --profile) PROFILE="${2:?}"; shift 2 ;;
    --phase) PHASE="${2:?}"; shift 2 ;;
    --only) ONLY="${2:?}"; shift 2 ;;
    --head-sha) HEAD_PIN="${2:?}"; shift 2 ;;
    --base-sha) BASE_PIN="${2:?}"; shift 2 ;;
    --no-pr-meta) NO_PR_META=true; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; die_infra "unknown argument: $1" ;;
  esac
done

[ -n "$PR$BRANCH" ] || { usage >&2; die_infra "pass --pr N or --branch NAME"; }
[ -z "$PR" ] || [ -z "$BRANCH" ] || die_infra "pass only one of --pr and --branch"
[ -z "$PR" ] || [[ "$PR" =~ ^[0-9]+$ ]] || die_infra "--pr must be a number"
case "$PROFILE" in fast | full) ;; *) die_infra "--profile must be fast or full" ;; esac
case "$PHASE" in all | generated | render) ;; *) die_infra "--phase must be all, generated, or render" ;; esac

REPO_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)" || die_infra "not inside a git repository"
RUN_KEY="pr-$PR"
[ -n "$BRANCH" ] && RUN_KEY="branch-$(sanitize "$BRANCH")"
OUT="${OUT:-$REPO_ROOT/.verify-out/$RUN_KEY}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
WORK="$OUT/work" STATE="$OUT/state" LOGS="$OUT/logs"
BASE_WT="$WORK/base" PR_WT="$WORK/pr"
REF_NS="refs/verify/$RUN_KEY"
mkdir -p "$STATE" "$LOGS"

require_tools git jq
init_go_cache_env

# ---------------------------------------------------------------- setup

fetch_with_retry() {
  local i
  for i in 1 2 3 4; do
    git -C "$REPO_ROOT" fetch -q --no-tags "$REMOTE" "$@" && return 0
    log "fetch failed (attempt $i); retrying"
    sleep $((2 ** (i + 1)))
  done
  return 1
}

ensure_commit() {
  # ensure_commit <sha> <ref>: makes the commit available locally and points <ref> at it.
  git -C "$REPO_ROOT" cat-file -e "$1^{commit}" 2>/dev/null || fetch_with_retry "$1" ||
    die_infra "could not fetch commit $1 from $REMOTE"
  git -C "$REPO_ROOT" update-ref "$2" "$1"
}

setup_refs() {
  local gh_json="$STATE/gh-pr.json"
  echo 'null' >"$gh_json"
  if [ -z "$PR" ] && [ "$NO_PR_META" != true ] && command -v gh >/dev/null 2>&1; then
    PR=$(gh pr list --repo "$REPO" --head "$BRANCH" --state open --json number --jq '.[0].number // empty' 2>/dev/null || true)
    [ -n "$PR" ] && log "branch $BRANCH has open PR #$PR"
  fi
  if [ -n "$PR" ] && [ "$NO_PR_META" != true ]; then
    require_tools gh
    gh pr view "$PR" --repo "$REPO" \
      --json number,url,title,body,author,headRefName,headRefOid,baseRefName,labels,statusCheckRollup,state,isDraft \
      >"$gh_json" || die_infra "could not read PR #$PR from $REPO (is gh authenticated?)"
    [ -n "$BRANCH" ] || BASE_BRANCH=$(jq -r '.baseRefName' "$gh_json")
  fi

  if [ -n "$HEAD_PIN" ]; then
    ensure_commit "$HEAD_PIN" "$REF_NS/head"
  else
    local head_src="refs/pull/$PR/head"
    [ -n "$BRANCH" ] && head_src="refs/heads/$BRANCH"
    log "fetching $head_src from $REMOTE"
    if ! fetch_with_retry "+$head_src:$REF_NS/head"; then
      if [ -n "$BRANCH" ] && git -C "$REPO_ROOT" rev-parse -q --verify "$BRANCH^{commit}" >/dev/null; then
        log "using the local branch $BRANCH"
        git -C "$REPO_ROOT" update-ref "$REF_NS/head" "$BRANCH"
      else
        die_infra "could not fetch $head_src from $REMOTE"
      fi
    fi
  fi
  if [ -n "$BASE_PIN" ]; then
    ensure_commit "$BASE_PIN" "$REF_NS/base"
  elif ! fetch_with_retry "+refs/heads/$BASE_BRANCH:$REF_NS/base"; then
    git -C "$REPO_ROOT" rev-parse -q --verify "$BASE_BRANCH^{commit}" >/dev/null || die_infra "could not fetch $BASE_BRANCH from $REMOTE"
    log "using the local branch $BASE_BRANCH"
    git -C "$REPO_ROOT" update-ref "$REF_NS/base" "$BASE_BRANCH"
  fi
  HEAD_SHA=$(git -C "$REPO_ROOT" rev-parse "$REF_NS/head")
  BASE_SHA=$(git -C "$REPO_ROOT" rev-parse "$REF_NS/base")
  local expected
  expected=$(jq -r '.headRefOid // empty' "$gh_json")
  if [ -z "$HEAD_PIN" ] && [ -n "$expected" ] && [ "$expected" != "$HEAD_SHA" ]; then
    log "warning: PR head moved during setup ($expected → $HEAD_SHA); verifying $HEAD_SHA"
  fi
}

remove_worktree() {
  if [ -d "$1" ]; then
    git -C "$REPO_ROOT" worktree remove --force "$1" 2>/dev/null || rm -rf "$1"
  fi
  git -C "$REPO_ROOT" worktree prune
}

setup_worktrees() {
  remove_worktree "$BASE_WT"
  remove_worktree "$PR_WT"
  mkdir -p "$WORK"
  MERGE_BASE=$(git -C "$REPO_ROOT" merge-base "$BASE_SHA" "$HEAD_SHA") || die_infra "no merge base between PR and $BASE_BRANCH"
  BEHIND=$(git -C "$REPO_ROOT" rev-list --count "$MERGE_BASE..$BASE_SHA")
  git -C "$REPO_ROOT" diff --name-only "$MERGE_BASE" "$HEAD_SHA" >"$STATE/changed-files.txt"

  git -C "$REPO_ROOT" worktree add -q --detach "$PR_WT" "$BASE_SHA" || die_infra "worktree add failed"
  MERGE_STATUS="merged" CONFLICTS="[]" BASE_TESTED="$BASE_SHA"
  if ! git -C "$PR_WT" -c user.name="dependency-pr-verify" -c user.email="verify@localhost" \
    merge -q --no-ff --no-edit -m "verify: merge $RUN_KEY into $BASE_BRANCH" "$HEAD_SHA" >"$LOGS/merge.log" 2>&1; then
    CONFLICTS=$(git -C "$PR_WT" diff --name-only --diff-filter=U | jq -R -s 'split("\n") | map(select(length > 0))')
    git -C "$PR_WT" merge --abort 2>/dev/null
    git -C "$PR_WT" checkout -q --detach "$HEAD_SHA" || die_infra "checkout of PR head failed"
    MERGE_STATUS="conflict"
    # Compare against the commit the PR branched from, so main's newer changes
    # don't show up as downgrades.
    BASE_TESTED="$MERGE_BASE"
    log "PR conflicts with $BASE_BRANCH; testing the PR head against its merge base"
  fi
  git -C "$REPO_ROOT" worktree add -q --detach "$BASE_WT" "$BASE_TESTED" || die_infra "worktree add failed"
  PR_TESTED=$(git -C "$PR_WT" rev-parse HEAD)
}

write_meta() {
  jq -n \
    --slurpfile gh "$STATE/gh-pr.json" \
    --arg mode "$(if [ -n "$HEAD_PIN" ]; then echo replay; elif [ -n "$BRANCH" ]; then echo branch; else echo pr; fi)" \
    --arg pr_ref "$PR" \
    --arg repo "$REPO" --arg branch "$BRANCH" --arg base_ref "$BASE_BRANCH" \
    --arg head "$HEAD_SHA" --arg base "$BASE_SHA" --arg mb "$MERGE_BASE" \
    --arg base_tested "$BASE_TESTED" --arg pr_tested "$PR_TESTED" \
    --argjson behind "$BEHIND" --arg merge "$MERGE_STATUS" --argjson conflicts "$CONFLICTS" '
    ($gh[0]) as $g
    | {
        mode: $mode, repo: $repo,
        number: ($g.number // null), url: ($g.url // null),
        pr_ref: (if $pr_ref == "" then null else ($pr_ref | tonumber) end),
        title: ($g.title // null), body: ($g.body // null),
        author: ($g.author.login // null), state: ($g.state // null),
        labels: (($g.labels // []) | map(.name)),
        head_ref: ($g.headRefName // $branch), head_sha: $head,
        base_ref: $base_ref, base_sha: $base, merge_base: $mb, behind_by: $behind,
        merge: {status: $merge, conflicts: $conflicts},
        tested: {base: $base_tested, pr: $pr_tested},
        ci: (if $g == null then null
             else ($g.statusCheckRollup // []) | map({name: (.name // .context), status, conclusion, state, workflow: .workflowName})
             end)
      }' >"$STATE/pr.json"
}

# ---------------------------------------------------------------- runner

# run_one <kind> <id> <script> <side> <timeout>; writes <state>/<kind>/<id>/<side>/result.json
run_one() {
  local kind="$1" id="$2" script="$3" side="$4" timeout="$5"
  local wt="$PR_WT"
  [ "$side" = base ] && wt="$BASE_WT"
  local art="$STATE/$kind/$id/$side" logf="$LOGS/$side/$id.log"
  [ "$kind" = generated ] && logf="$LOGS/$side/generated-$id.log"
  rm -rf "$art"
  mkdir -p "$art" "$(dirname "$logf")"
  local envargs=()
  mapfile -t envargs < <(hermetic_env_args "$art/sandbox")
  local start rc
  start=$(now_s)
  (
    cd "$wt" &&
      exec env "${envargs[@]}" \
        WT="$wt" SIDE="$side" BASE_WT="$BASE_WT" PR_WT="$PR_WT" ARTIFACTS="$art" \
        CLASSIFICATION="$STATE/classification.json" PR_META="$STATE/pr.json" \
        PROFILE="$PROFILE" VERIFY_ROOT="$VERIFY_ROOT" \
        timeout --kill-after=30 "$timeout" bash "$script"
  ) >"$logf" 2>&1 </dev/null
  rc=$?
  local status summary
  status=$(cat "$art/status" 2>/dev/null || echo error)
  summary=$(cat "$art/summary" 2>/dev/null || true)
  if [ "$status" = error ]; then
    if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
      summary="timed out after ${timeout}s"
    else
      summary="exited with code $rc without reporting a status (see log)"
    fi
  fi
  jq -n --arg status "$status" --arg summary "$summary" \
    --arg log "${logf#"$OUT"/}" --argjson duration "$(($(now_s) - start))" \
    --arg question "$(cat "$art/question" 2>/dev/null)" \
    --slurpfile fix <(cat "$art/fix.json" 2>/dev/null || echo null) \
    --arg fingerprint "$(cat "$art/fingerprint" 2>/dev/null)" \
    --rawfile details <(cat "$art/details.md" 2>/dev/null) \
    --rawfile recs <(cat "$art/recommendations" 2>/dev/null) '
    {status: $status, summary: $summary, log: $log, duration_s: $duration,
     question: (if $question == "" then null else $question end),
     fix: $fix[0],
     fingerprint: (if $fingerprint == "" then null else $fingerprint end),
     details: (if $details == "" then null else $details end),
     recommendations: ($recs | split("\n") | map(select(length > 0)))}' >"$art/result.json"
  log "  $id ($side): $status${summary:+ — $summary} [$(($(now_s) - start))s]"
}

applies() {
  # applies <check-json>: exit 0 if the check is relevant to this PR
  jq -e --slurpfile c "$STATE/classification.json" '
    . as $chk | $c[0] as $c
    | ((.when | index("any")) != null or any(.when[]; . as $w | $c.ecosystems | index($w) != null))
      and (if .packages then any($c.updates[]; .name | test($chk.packages)) else true end)' \
    <<<"$1" >/dev/null
}

run_baseline() {
  local registry="$VERIFY_ROOT/checks/registry.json"
  rm -rf "$STATE/checks"
  mkdir -p "$STATE/checks"
  : >"$STATE/check-order.txt"
  local entries=()
  mapfile -t entries < <(jq -c '.checks[]' "$registry")
  log "running baseline checks (profile $PROFILE)"
  local c id script prof timeout status
  for c in "${entries[@]}"; do
    id=$(jq -r '.id' <<<"$c")
    applies "$c" || continue
    if [ -n "$ONLY" ] && [[ ",$ONLY," != *",$id,"* ]]; then continue; fi
    script="$VERIFY_ROOT/checks/$(jq -r '.script' <<<"$c")"
    prof=$(jq -r '.profile // "fast"' <<<"$c")
    timeout=$(jq -r '.timeout // 600' <<<"$c")
    printf '%s\n' "$id" >>"$STATE/check-order.txt"
    mkdir -p "$STATE/checks/$id"
    printf '%s\n' "$c" >"$STATE/checks/$id/entry.json"
    if [ "$prof" = full ] && [ "$PROFILE" != full ]; then
      mkdir -p "$STATE/checks/$id/pr"
      jq -n '{status: "skip", summary: "runs with --profile full", profile_skipped: true}' >"$STATE/checks/$id/pr/result.json"
      continue
    fi
    run_one checks "$id" "$script" pr "$timeout"
    status=$(jq -r '.status' "$STATE/checks/$id/pr/result.json")
    if [ "$(jq -r '.compare_base // false' <<<"$c")" = true ] && { [ "$status" = fail ] || [ "$status" = decide ]; }; then
      run_one checks "$id" "$script" base "$timeout"
    fi
  done
}

run_generated() {
  local gdir="$OUT/generated" manifest="$OUT/generated/checks.json"
  rm -rf "$STATE/generated"
  mkdir -p "$STATE/generated"
  : >"$STATE/generated-order.txt"
  [ -f "$manifest" ] || return 0
  jq -e '.schema == 1 and (.checks | type == "array")' "$manifest" >/dev/null ||
    die_infra "$manifest: expected {\"schema\": 1, \"checks\": [...]}"
  local entries=() c id script kind timeout
  mapfile -t entries < <(jq -c '.checks[]' "$manifest")
  log "running ${#entries[@]} generated check(s) against base and PR"
  for c in "${entries[@]}"; do
    id=$(jq -r '.id // ""' <<<"$c")
    kind=$(jq -r '.kind // ""' <<<"$c")
    script="$gdir/$(jq -r '.script // ""' <<<"$c")"
    timeout=$(jq -r '.timeout // 600' <<<"$c")
    [[ "$id" =~ ^[a-z0-9][a-z0-9-]*$ ]] || die_infra "generated check id '$id' must be kebab-case"
    case "$kind" in discriminating | guard) ;; *) die_infra "generated check $id: kind must be discriminating or guard" ;; esac
    [ -f "$script" ] || die_infra "generated check $id: script $script not found"
    printf '%s\n' "$id" >>"$STATE/generated-order.txt"
    mkdir -p "$STATE/generated/$id"
    printf '%s\n' "$c" >"$STATE/generated/$id/entry.json"
    run_one generated "$id" "$script" base "$timeout"
    run_one generated "$id" "$script" pr "$timeout"
  done
}

# ---------------------------------------------------------------- finalize

collect() {
  # collect <kind> <order-file>: JSON array of {entry..., pr, base}
  local kind="$1" order="$2" id
  {
    while IFS= read -r id; do
      [ -n "$id" ] || continue
      local d="$STATE/$kind/$id"
      jq -n --slurpfile e "$d/entry.json" \
        --slurpfile pr <(cat "$d/pr/result.json" 2>/dev/null || echo null) \
        --slurpfile base <(cat "$d/base/result.json" 2>/dev/null || echo null) \
        '$e[0] + {pr: $pr[0], base: $base[0]}'
    done <"$order"
  } | jq -s '.'
}

finalize() {
  [ -f "$STATE/pr.json" ] && [ -f "$STATE/classification.json" ] || die_infra "no previous run in $OUT; run with --phase all first"
  local impact="$OUT/agent/impact.json"
  if [ -f "$impact" ]; then
    jq -e 'type == "object"' "$impact" >/dev/null 2>&1 || die_infra "$impact is not a JSON object"
  fi
  collect checks "$STATE/check-order.txt" >"$STATE/checks.json"
  collect generated "${STATE}/generated-order.txt" >"$STATE/generated.json" 2>/dev/null || echo '[]' >"$STATE/generated.json"
  jq -n -L "$VERIFY_ROOT/lib" \
    --slurpfile meta "$STATE/pr.json" --slurpfile cls "$STATE/classification.json" \
    --slurpfile checks "$STATE/checks.json" --slurpfile gen "$STATE/generated.json" \
    --slurpfile impact <(cat "$impact" 2>/dev/null || echo null) \
    --arg profile "$PROFILE" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
    include "verdict";
    build_result($meta[0]; $cls[0]; $checks[0]; $gen[0]; $impact[0]; $profile; $at)' >"$OUT/result.json" ||
    die_infra "could not assemble result.json"
  "$VERIFY_ROOT/render-comment.sh" "$OUT/result.json" >"$OUT/comment.md" || die_infra "could not render comment.md"
  local verdict code
  verdict=$(jq -r '.verdict' "$OUT/result.json")
  code=$(jq -r '.exit_code' "$OUT/result.json")
  log "verdict: $verdict (exit $code)"
  log "result:  $OUT/result.json"
  log "comment: $OUT/comment.md"
  exit "$code"
}

# ---------------------------------------------------------------- main

case "$PHASE" in
  all)
    require_tools go rg timeout
    setup_refs
    setup_worktrees
    write_meta
    log "classifying $(wc -l <"$STATE/changed-files.txt") changed file(s)"
    "$VERIFY_ROOT/lib/classify.sh" "$BASE_WT" "$PR_WT" "$STATE/changed-files.txt" "$STATE/pr.json" "$STATE/classification.json" ||
      die_infra "classification failed"
    log "ecosystems: $(jq -r '.ecosystems | join(", ")' "$STATE/classification.json"); tier: $(jq -r '.tier' "$STATE/classification.json")"
    run_baseline
    run_generated
    finalize
    ;;
  generated)
    [ -d "$PR_WT" ] && [ -d "$BASE_WT" ] || die_infra "worktrees missing in $WORK; run with --phase all first"
    expected=$(jq -r '.tested.pr' "$STATE/pr.json")
    [ "$(git -C "$PR_WT" rev-parse HEAD)" = "$expected" ] || die_infra "PR worktree HEAD changed since the baseline run"
    run_generated
    finalize
    ;;
  render)
    finalize
    ;;
esac
