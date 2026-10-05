#!/usr/bin/env bash
# PR CI only exercises workflows that trigger on pull_request. Release
# workflows (release-please, manual-publish, the publish composite) never run
# on a PR, so a green PR says nothing about them.
source "$VERIFY_ROOT/lib/check.sh"

if ! python3 -c 'import yaml' 2>/dev/null; then
  skip "python3 with PyYAML is required for workflow analysis"
fi
updates_for github-actions | jq -s '.' >"$ARTIFACTS/updates.json"
if ! run python3 "$VERIFY_ROOT/lib/actions_analysis.py" "$WT" "$ARTIFACTS/updates.json" >"$ARTIFACTS/report.json"; then
  skip "workflow analysis failed; see log"
fi
cat "$ARTIFACTS/report.json"

detail "| Action | Change | Used in | Runs on PR CI? | Inputs |"
detail "|---|---|---|---|---|"
jq -r '.actions[] |
  "| `\(.name)` | \(.from // "∅") → \(.to // "∅") | \([.usages[].file] | unique | join(", ")) | \(
    if (.usages | length) == 0 then "n/a"
    elif all(.usages[]; .runs_on_pr) then "yes"
    elif any(.usages[]; .runs_on_pr) then "partly: not " + ([.usages[] | select(.runs_on_pr | not) | .workflows[]] | unique | join(", "))
    else "**no**" end) | \(
    if .inputs == null then "not a major bump"
    elif .inputs.status != "ok" then "could not fetch action.yml"
    else ((if (.inputs.removed_but_used | length) > 0 then "removed: " + (.inputs.removed_but_used | join(", ")) + "; " else "" end)
          + (if (.inputs.new_required | length) > 0 then "new required: " + (.inputs.new_required | join(", ")) + "; " else "" end)
          + "runtime " + (.inputs.runs_using | join(" → "))) end) |"' "$ARTIFACTS/report.json" >>"$ARTIFACTS/details.md"

broken=$(jq -r '[.actions[] | select(.inputs.status == "ok") | select((.inputs.removed_but_used + .inputs.new_required) | length > 0) | .name] | join(", ")' "$ARTIFACTS/report.json")
untested=$(jq -r '[.actions[].usages[] | select(.runs_on_pr | not) | (if (.workflows | length) > 0 then .workflows[] else .file end)] | unique | join(", ")' "$ARTIFACTS/report.json")
unfetched=$(jq -r '[.actions[] | select(.inputs.status == "unavailable") | .name] | join(", ")' "$ARTIFACTS/report.json")

if [ -n "$broken" ]; then
  fingerprint "$broken"
  fail "Input incompatibility for: $broken"
fi
problems=()
[ -n "$untested" ] && problems+=("not exercised by PR CI: $untested")
[ -n "$unfetched" ] && problems+=("could not compare inputs for: $unfetched")
if [ ${#problems[@]} -gt 0 ]; then
  fingerprint "$untested|$unfetched"
  [ -n "$untested" ] && recommend "Dry-run the release path the bumped action affects (e.g. manual-publish with dry-run) or review the action's changelog for those workflows."
  warn "$(IFS='; '; echo "${problems[*]}")"
fi
pass "Every usage runs on pull_request CI; inputs compatible"
