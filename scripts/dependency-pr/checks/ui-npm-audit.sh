#!/usr/bin/env bash
# Compares npm audit for runtime dependencies between base and PR. Needs the registry.
source "$VERIFY_ROOT/lib/check.sh"

audit() {
  (cd "$1/$UI_DIR_REL" && npm audit --omit=dev --json 2>/dev/null) |
    jq '{error: (.error.summary // null),
         vulns: ((.vulnerabilities // {}) | to_entries
                 | map({key: .key, value: .value.severity}) | from_entries)}'
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
  $b[0].vulns as $b | $p[0].vulns as $p
  | {
      new: [$p | to_entries[] | select(($b[.key] // null) == null or (.value | rank) > ($b[.key] | rank)) | "\(.key) (\(.value))"],
      new_serious: [$p | to_entries[] | select((.value | rank) >= 3 and (($b[.key] // null) == null or (.value | rank) > ($b[.key] | rank))) | "\(.key) (\(.value))"],
      fixed: [$b | to_entries[] | select(($p[.key] // null) == null) | "\(.key) (\(.value))"],
      total_pr: ($p | length), total_base: ($b | length)
    }' >"$ARTIFACTS/delta.json"

fixed=$(jq -r '.fixed | join(", ")' "$ARTIFACTS/delta.json")
[ -n "$fixed" ] && detail "- Advisories resolved by this PR: $fixed"
serious=$(jq -r '.new_serious | join(", ")' "$ARTIFACTS/delta.json")
new=$(jq -r '.new | join(", ")' "$ARTIFACTS/delta.json")
read -r tb tp < <(jq -r '"\(.total_base) \(.total_pr)"' "$ARTIFACTS/delta.json")
[ -n "$serious" ] && fail "New high/critical advisories: $serious"
[ -n "$new" ] && decide "Accept these new advisories in runtime UI dependencies: $new?" "npm audit (runtime dependencies only) reports advisories on the PR that base does not have: $new"
pass "No new advisories (base $tb, PR $tp affected packages)"
