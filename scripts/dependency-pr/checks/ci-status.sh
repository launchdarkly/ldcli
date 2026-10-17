#!/usr/bin/env bash
# CI on the PR head. The status-check rollup does not show a workflow run that
# failed before any job started, or a run that waits in the queue with no jobs.
# So this check also compares the workflows that must run for this PR (their
# triggers match the changed paths) with the workflow runs on the head commit.
# A run that did not finish, did not start a job, or is missing is incomplete.
# A result for another commit counts as not run.
source "$VERIFY_ROOT/lib/check.sh"

if [ "$(jq -r '.ci == null' "$PR_META")" = "true" ]; then
  skip "No PR metadata (branch mode without an open PR)"
fi
python3 -c 'import yaml' 2>/dev/null || incomplete "python3 with PyYAML is required to read the workflow triggers"
if ! run python3 "$VERIFY_ROOT/lib/ci_runs.py" "$WT" "$BASE_WT" "$PR_META" "$CLASSIFICATION" >"$ARTIFACTS/report.json"; then
  incomplete "could not compare the expected workflows with the workflow runs; see log"
fi
cat "$ARTIFACTS/report.json"
R="$ARTIFACTS/report.json"
head=$(jq -r '.head[0:7]' "$R")

detail "Workflows that PR CI must run on \`$head\`, and their runs on that commit:"
detail ""
detail "| Workflow | Event | Run | Result |"
detail "|---|---|---|---|"
jq -r '.workflows[] | "| `\(.path)` | \(.event) | \(if .run then "[\(.run.id)](\(.run.html_url // "")) \(.run.created_at // "" | .[0:10])" else "none" end) | \(if .state == "ok" then "pass" elif .state == "fail" then "**FAIL**" else "**not run**" end): \(.reason) |"' "$R" >>"$ARTIFACTS/details.md"
jq -r '.not_expected[] | "- `\(.path)` (\(.event)) does not run for this PR: \(.reason)."' "$R" >>"$ARTIFACTS/details.md"
jq -r '.rollup[] | select(.workflow == "") | "- Check `\(.name)`: \(.reason)"' "$R" >>"$ARTIFACTS/details.md"
jq -r 'if (.other_heads | length) > 0 then "- Runs for other commits (\(.other_heads | join(", "))) do not count." else empty end' "$R" >>"$ARTIFACTS/details.md"

names() { jq -r --arg s "$1" '[(.workflows[] | select(.state == $s) | "\(.name) (\(.reason))"), (.rollup[] | select(.state == $s) | "\(.name) (\(.reason))")] | unique | join(", ")' "$R"; }
failing=$(names fail)
not_run=$(names incomplete)
n_wf=$(jq '.workflows | length' "$R")
n_other=$(jq '[.rollup[] | select(.workflow == "")] | length' "$R")

if [ -n "$failing" ]; then
  fail "Failing on PR head $head: $failing"
fi
if [ -n "$not_run" ]; then
  incomplete "CI did not finish on PR head $head: $not_run. Re-run the workflows on the PR"
fi
if [ "$((n_wf + n_other))" -eq 0 ]; then
  incomplete "No CI results on PR head $head"
fi
pass "All $n_wf expected workflow run(s) and $n_other other check(s) green on PR head $head"
