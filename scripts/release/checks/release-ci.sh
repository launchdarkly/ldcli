#!/usr/bin/env bash
# A PR that GITHUB_TOKEN opens (the release-please PR) starts no pull_request
# workflows, or they wait for approval. Then the only CI signal is the push
# run on the main commit below the release commit. That signal is enough only
# if the release commit changes nothing but version files.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1
: "${PR_META:?}"

[ "$(jq -r '.ci_runs == null and .ci_runs_parent == null' "$PR_META")" = true ] &&
  incomplete "No CI data (run without --no-pr-meta, with gh authenticated)"

# The workflows that check pull requests: a pull_request trigger, not pull_request_target.
mapfile -t expected < <(grep -lE '^[[:space:]]+pull_request:' .github/workflows/*.yml 2>/dev/null |
  xargs -r -n1 sed -n 's/^name:[[:space:]]*//p' | tr -d "\"'" | sort -u)
[ ${#expected[@]} -gt 0 ] || incomplete "Found no pull_request workflows in .github/workflows"
detail "- Workflows that check pull requests: $(join_by ', ' "${expected[@]}")"

# summarize <runs-field> <workflow>: the newest run of the workflow, as "status/conclusion url".
summarize() {
  jq -r --arg f "$1" --arg w "$2" '
    [.[$f] // [] | .[] | select(.name == $w) | select(.event == "push" or .event == "pull_request")]
    | sort_by(.id) | last // empty | "\(.status)/\(.conclusion // "none") \(.html_url)"' "$PR_META"
}

head_ok=true parent_ok=true problems=()
for w in "${expected[@]}"; do
  h=$(summarize ci_runs "$w")
  p=$(summarize ci_runs_parent "$w")
  detail "- \`$w\`: release head: ${h:-no run}; main commit below: ${p:-no run}"
  case "$h" in completed/success*) ;; *) head_ok=false ;; esac
  case "$p" in
    completed/success*) ;;
    completed/failure* | completed/timed_out* | completed/cancelled*) parent_ok=false; problems+=("$w ${p%% *} on the main commit below the release") ;;
    *) parent_ok=false ;;
  esac
done

[ "$head_ok" = true ] && pass "CI passed on the release head for $(join_by ', ' "${expected[@]}")"
[ ${#problems[@]} -gt 0 ] && { fingerprint "$(printf '%s\n' "${problems[@]}")"; fail "$(join_by '; ' "${problems[@]}")"; }

head=$(jq -r '.head_sha' "$PR_META")
parent=$(jq -r '.parent_sha // empty' "$PR_META")
[ -n "$parent" ] || incomplete "CI did not pass on the release head, and the head has no parent to compare"
others=$(git diff --name-only "$parent" "$head" | grep -vxE 'CHANGELOG\.md|package\.json|PROVENANCE\.md|\.release-please-manifest\.json' | paste -sd, -)
if [ "$parent_ok" = true ] && [ -z "$others" ]; then
  info "CI did not pass on the release head (no run, or waiting for approval). It passed on the main commit ${parent:0:7}, and the release commit changes only version files"
fi
[ -n "$others" ] && detail "- The release commit also changes: $others"
incomplete "CI did not pass on the release head, and the main commit below it is not a full substitute"
