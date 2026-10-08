#!/usr/bin/env bash
# PR CI runs only the workflows that trigger on pull_request. Release
# workflows (release-please, manual-publish, the publish composite) never run
# on a PR. This check verifies what it can from the action metadata, and asks
# a person only to accept what no check can run.
source "$VERIFY_ROOT/lib/check.sh"

python3 -c 'import yaml' 2>/dev/null || incomplete "python3 with PyYAML is required for workflow analysis"
updates_for github-actions | jq -s '.' >"$ARTIFACTS/updates.json"
if ! run python3 "$VERIFY_ROOT/lib/actions_analysis.py" "$WT" "$ARTIFACTS/updates.json" "$BASE_WT" >"$ARTIFACTS/report.json"; then
  incomplete "workflow analysis failed; see log"
fi
cat "$ARTIFACTS/report.json"
R="$ARTIFACTS/report.json"

detail "| Action | Change | Used in | Runs on PR CI? | Upstream action.yml |"
detail "|---|---|---|---|---|"
jq -r '.actions[] |
  "| `\(.name)` | \(if (.replaced | length) > 1 then (.replaced | join(", ")) else (.from // "∅") end) → \(.to // "∅") | \([.usages[].file] | unique | join(", ")) | \(
    if (.usages | length) == 0 then "n/a"
    elif all(.usages[]; .runs_on_pr) then "yes"
    elif any(.usages[]; .runs_on_pr) then "partly, not " + ([.usages[] | select(.runs_on_pr | not) | .workflows[]] | unique | join(", "))
    else "**no**" end) | \(
    if .upstream == null then "not compared"
    elif .upstream.status != "ok" then "could not fetch"
    else ("inputs used: " + (if (.upstream.inputs_used | length) > 0 then (.upstream.inputs_used | join(", ")) else "none" end)
          + "; outputs read: " + (if (.upstream.outputs_read | length) > 0 then (.upstream.outputs_read | join(", ")) else "none" end)
          + "; runtime " + (.upstream.runs_using | join(" → "))
          + (if (.upstream.defaults_changed | length) > 0 then "; changed defaults: " + ([.upstream.defaults_changed[] | "\(.input) \(.from // "none") → \(.to // "none")"] | join(", ")) else "" end)) end) |"' "$R" >>"$ARTIFACTS/details.md"
jq -r '.actions[] | select(.upstream.status == "ok" and ((.upstream.outputs_undeclared // []) | length) > 0)
  | "- `\(.name)` does not declare the outputs \(.upstream.outputs_undeclared | join(", ")) in either version. The action sets them at run time, so this check cannot compare them."' "$R" >>"$ARTIFACTS/details.md"
jq -r '.actions[] | select(.upstream.status == "ok" and ((.upstream.defaults_same_on_github_com // []) | length) > 0)
  | "- `\(.name)`: the default of \([.upstream.defaults_same_on_github_com[].input] | join(", ")) changed only for GitHub Enterprise Server. It gives the same value on github.com."' "$R" >>"$ARTIFACTS/details.md"
jq -r '.permissions_changes[] | "- permissions changed in \(.where): `\(.from)` → `\(.to)`"' "$R" >>"$ARTIFACTS/details.md"

broken=$(jq -r '[.actions[] | select(.upstream.status == "ok") | .name as $n | .upstream
  | ((.removed_but_used | map("\($n) no longer has input \(.)")) + (.new_required | map("\($n) requires new input \(.)")) + (.outputs_missing | map("\($n) no longer has output \(.)")))[]] | join("; ")' "$R")
if [ -n "$broken" ]; then
  fingerprint "$broken"
  recommend "Update the workflow steps for the new action interface: $broken"
  fail "Workflow steps do not match the new action interface: $broken"
fi

unfetched=$(jq -r '[.actions[] | select(.breaking and .upstream.status != "ok") | .name] | join(", ")' "$R")
[ -n "$unfetched" ] && incomplete "could not fetch the upstream action.yml to compare inputs and outputs for: $unfetched"

questions=()
evidence=()
perm=$(jq -r '[.permissions_changes[] | "\(.where): \(.from) → \(.to)"] | join("; ")' "$R")
[ -n "$perm" ] && questions+=("accept the workflow permission changes ($perm)")
# Fields are joined with the unit separator, not a tab. Tab is IFS whitespace,
# so read would merge empty fields and shift the later fields left.
while IFS=$'\x1f' read -r name from to untested runtime defaults selfhosted used; do
  [ -n "$name" ] || continue
  ev="$name $from → $to: inputs and outputs that the repo uses are unchanged; runtime $runtime${defaults:+; changed defaults: $defaults}${selfhosted:+; non-standard runner labels: $selfhosted}"
  evidence+=("$ev")
  if [ -n "$untested" ]; then
    questions+=("accept $name $to in workflows that PR CI does not run ($untested)")
  elif [ -n "$defaults" ]; then
    questions+=("accept the changed input defaults of $name in $used ($defaults)")
  fi
done < <(jq -r '.actions[] | select(.upstream.status == "ok") | select(.breaking or (.upstream.defaults_changed | length) > 0) |
  [.name, (if (.replaced | length) > 1 then (.replaced | join(", ")) else .from end), .to,
   ([.usages[] | select(.runs_on_pr | not) | .workflows[]] | unique | map(sub("^\\.github/workflows/"; "")) | join(", ")),
   (.upstream.runs_using | join(" → ")),
   ([.upstream.defaults_changed[] | "\(.input): \(.from // "none") → \(.to // "none")"] | join(", ")),
   (.self_hosted_runners | join(", ")),
   ([.usages[].workflows[]] | unique | map(sub("^\\.github/workflows/"; "")) | join(", "))] | map(. // "" | tostring) | join("\u001f")' "$R")

if [ ${#questions[@]} -gt 0 ]; then
  fingerprint "${questions[*]}"
  q="$(join_by '; and ' "${questions[@]}")"
  decide "${q^}?" "$(join_by ' | ' "${evidence[@]}")"
fi

untested_minor=$(jq -r '[.actions[] | select(.breaking | not) | .usages[] | select(.runs_on_pr | not) | .workflows[]] | unique | join(", ")' "$R")
if [ -n "$untested_minor" ]; then
  info "Interfaces match. Non-breaking update also used in workflows that PR CI does not run: $untested_minor"
fi
pass "Every usage runs on PR CI, and the inputs, outputs, and runtime match"
