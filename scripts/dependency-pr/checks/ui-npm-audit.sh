#!/usr/bin/env bash
# Compares npm audit for runtime dependencies between base and PR. Needs the registry.
#
# The comparison uses advisory IDs. npm audit also flags each package that
# depends on a vulnerable package (@launchpad-ui/core "via"
# @launchpad-ui/navigation "via" react-router in #723). Such a package has no
# advisory of its own, and a new path to an old advisory is not a new advisory.
source "$VERIFY_ROOT/lib/check.sh"

audit() {
  (cd "$1/$UI_DIR_REL" && npm audit --omit=dev --json 2>/dev/null) |
    jq '{error: (.error.summary // null),
         advisories: ([(.vulnerabilities // {})[] | .via[] | objects
                       | {id: ((.url // "" | capture("(?<g>GHSA-[0-9a-z]{4}-[0-9a-z]{4}-[0-9a-z]{4})").g) // "npm-\(.source)"),
                          package: .name, severity, title}]
                      | group_by(.id) | map(.[0] + {packages: (map(.package) | unique)}))}'
}
audit "$BASE_WT" >"$ARTIFACTS/base.json" || incomplete "npm audit did not run on base"
audit "$PR_WT" >"$ARTIFACTS/pr.json" || incomplete "npm audit did not run on the PR"
cat "$ARTIFACTS/pr.json"
for s in base pr; do
  err=$(jq -r '.error // empty' "$ARTIFACTS/$s.json")
  [ -n "$err" ] && incomplete "npm audit failed on $s: $err"
done

jq -n --slurpfile b "$ARTIFACTS/base.json" --slurpfile p "$ARTIFACTS/pr.json" '
  def rank: {"info":0,"low":1,"moderate":2,"high":3,"critical":4}[.] // 0;
  def show: "\(.id) (\(.packages | join(", ")), \(.severity))";
  ($b[0].advisories | map(.id)) as $bids | ($p[0].advisories | map(.id)) as $pids
  | ($p[0].advisories | map(select(.id as $i | $bids | index($i) | not))) as $new
  | {
      new: [$new[] | show],
      new_serious: [$new[] | select((.severity | rank) >= 3) | show],
      fixed: [$b[0].advisories[] | select(.id as $i | $pids | index($i) | not) | show],
      total_pr: ($pids | length), total_base: ($bids | length)
    }' >"$ARTIFACTS/delta.json"

fixed=$(jq -r '.fixed | join(", ")' "$ARTIFACTS/delta.json")
[ -n "$fixed" ] && detail "- Advisories resolved by this PR: $fixed"
serious=$(jq -r '.new_serious | join(", ")' "$ARTIFACTS/delta.json")
new=$(jq -r '.new | join(", ")' "$ARTIFACTS/delta.json")
read -r tb tp < <(jq -r '"\(.total_base) \(.total_pr)"' "$ARTIFACTS/delta.json")
[ -n "$new" ] && detail "- New advisories on the PR: $new"
[ -n "$serious" ] && fail "New high/critical advisories: $serious"
[ -n "$new" ] && decide "Accept these new advisories in runtime UI dependencies: $new?" "npm audit (runtime dependencies only) reports advisories on the PR that base does not have: $new"
pass "No new advisories (base $tb, PR $tp advisories)"
