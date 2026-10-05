#!/usr/bin/env bash
# Compares reachable vulnerabilities between base and PR. Needs network for the vuln DB.
source "$VERIFY_ROOT/lib/check.sh"

bin=$(go_tool golang.org/x/vuln/cmd/govulncheck v1.1.4) || skip "could not install govulncheck"
vulns() { (cd "$1" && "$bin" ./...); }

vulns "$BASE_WT" >"$ARTIFACTS/base.out" 2>&1
rc_base=$?
vulns "$PR_WT" >"$ARTIFACTS/pr.out" 2>&1
rc_pr=$?
cat "$ARTIFACTS/pr.out"
# govulncheck exits 3 when vulnerabilities are found; anything else non-zero is a tool failure.
for rc in $rc_base $rc_pr; do
  if [ "$rc" -ne 0 ] && [ "$rc" -ne 3 ]; then
    skip "govulncheck could not run (exit $rc); see log"
  fi
done

ids() { grep -oE 'GO-[0-9]{4}-[0-9]+' "$1" | sort -u; }
new=$(comm -13 <(ids "$ARTIFACTS/base.out") <(ids "$ARTIFACTS/pr.out") | paste -sd, -)
fixed=$(comm -23 <(ids "$ARTIFACTS/base.out") <(ids "$ARTIFACTS/pr.out") | paste -sd, -)
[ -n "$fixed" ] && detail "- Fixed by this PR: $fixed"
if [ -n "$new" ]; then
  fail "New reachable vulnerabilities: $new"
fi
pass "No new reachable vulnerabilities${fixed:+ (fixes $fixed)}"
