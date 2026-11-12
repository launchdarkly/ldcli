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
jq -r '.workflows[] | "| `\(.path)` | \(.event) | \(if .run then "[\(.run.id)](\(.run.html_url // "")) \(.run.created_at // "" | .[0:10])" else "none" end) | \({"ok": "pass", "fail": "**FAIL**", "rerun": "**rerun needed**"}[.state] // "**not run**"): \(.reason) |"' "$R" >>"$ARTIFACTS/details.md"
jq -r '.not_expected[] | "- `\(.path)` (\(.event)) does not run for this PR: \(.reason)."' "$R" >>"$ARTIFACTS/details.md"
jq -r '.rollup[] | select(.workflow == "") | "- Check `\(.name)`: \(.reason)"' "$R" >>"$ARTIFACTS/details.md"
jq -r 'if (.other_heads | length) > 0 then "- Runs for other commits (\(.other_heads | join(", "))) do not count." else empty end' "$R" >>"$ARTIFACTS/details.md"
jq -r 'if (.age_days // 0) > 0 then "- The oldest run is from \(.oldest_run[0:10]), \(.age_days) day(s) ago." else empty end' "$R" >>"$ARTIFACTS/details.md"
jq -r '(.workflow_changes_since // [])[] | "- `\(.)` changed a workflow on the base branch after that run."' "$R" >>"$ARTIFACTS/details.md"

names() { jq -r --arg s "$1" '[(.workflows[] | select(.state == $s) | "\(.name) (\(.reason))"), (.rollup[] | select(.state == $s) | "\(.name) (\(.reason))")] | unique | join(", ")' "$R"; }
failing=$(names fail)
not_run=$(names incomplete)
rerun=$(names rerun)
rerun_jobs=$(jq -r '[.workflows[] | select(.state == "rerun") | "\(.name): \(.rerun_jobs | join(", "))"] | join("; ")' "$R")
changes=$(jq -r '(.workflow_changes_since // []) | map(split(" ")[0]) | join(", ")' "$R")
age=$(jq -r '.age_days // 0' "$R")
n_wf=$(jq '.workflows | length' "$R")
n_other=$(jq '[.rollup[] | select(.workflow == "")] | length' "$R")

if [ -n "$failing" ]; then
  fail "Failing on PR head $head: $failing"
fi
reasons=()
if [ -n "$not_run" ]; then
  reasons+=("CI did not finish on PR head $head: $not_run")
  recommend "Re-run the workflows on GitHub. A person with write access can do this, or comment \`@dependabot rebase\` if the PR is behind its base branch."
fi
if [ -n "$rerun" ]; then
  reasons+=("CI failed on PR head $head only in code that this PR does not change, which is often a flaky test: $rerun")
  recommend "Re-run the failed job ($rerun_jobs) on GitHub. If it fails again, look at the test on the base branch, not at this PR."
fi
if [ -n "$changes" ]; then
  reasons+=("CI on PR head $head ran before the base branch changed its workflows ($changes), so it did not test the workflows that a merge uses")
  recommend "Run CI again on the current base branch: comment \`@dependabot rebase\`, or merge the base branch into the PR."
fi
if [ ${#reasons[@]} -gt 0 ]; then
  incomplete "$(join_by '. ' "${reasons[@]}"). A rerun on GitHub closes this item"
fi
if [ "$((n_wf + n_other))" -eq 0 ]; then
  incomplete "No CI results on PR head $head"
fi
stale=""
[ "$age" -gt 14 ] && stale=" (the oldest run is $age days old)"
pass "All $n_wf expected workflow run(s) and $n_other other check(s) green on PR head $head$stale"
