#!/usr/bin/env bash
# Unit tests for the deterministic logic (semver classification, verdict rules,
# comment rendering). No network, git, or Go needed. Run: scripts/dependency-pr/test/run.sh
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/lib"
failures=0

check() {
  # check <description> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf 'ok   %s\n' "$1"
  else
    printf 'FAIL %s\n     expected: %s\n     actual:   %s\n' "$1" "$2" "$3"
    failures=$((failures + 1))
  fi
}

semver() { jq -nr -L "$LIB" --arg f "$1" --arg t "$2" 'include "semver"; semver_class($f; $t)'; }
breaking() { jq -nr -L "$LIB" --arg f "$1" --arg t "$2" 'include "semver"; is_breaking($f; $t)'; }

check "patch" patch "$(semver v1.14.28 v1.14.52)"
check "minor" minor "$(semver 7.12.0 7.18.2)"
check "major" major "$(semver 1.8.10 2.3.2)"
check "action major, short ref" major "$(semver v4 v6.0.3)"
check "prefixed tag" minor "$(semver release-secrets-v1.0.1 release-secrets-v1.2.0)"
check "downgrade" downgrade "$(semver v0.50.0 v0.46.0)"
check "pseudo-version" pseudo "$(semver v0.21.1-0.20250623103423-23b8fd6302d7 v0.21.1-0.20250801000000-abcdefabcdef)"
check "prerelease" prerelease "$(semver 1.0.0-rc.1 1.0.0)"
check "docker tag" minor "$(semver 3.19.1 3.24.2)"
check "0.x minor is breaking" true "$(breaking v0.4.8 v0.7.4)"
check "1.x minor is not breaking" false "$(breaking v1.9.1 v1.10.2)"
check "major is breaking" true "$(breaking 1.8.10 2.3.2)"

# ---- verdict rules

res() { jq -c -n "$1"; }
verdict() {
  # verdict <checks-json> <generated-json> <tier> [impact-json]
  jq -n -L "$LIB" --argjson checks "$1" --argjson gen "$2" --arg tier "$3" --argjson impact "${4:-null}" '
    include "verdict";
    build_result({head_sha: "abc"}; {tier: $tier, tier_reasons: ["test"], updates: [], ecosystems: ["gomod"], tags: []};
                 $checks; $gen; $impact; "fast"; "now")'
}
chk() {
  # chk <id> <on_fail> <pr-status> [base-status] [pr-fp] [base-fp]
  jq -n --arg id "$1" --arg sev "$2" --arg p "$3" --arg b "${4:-}" --arg pf "${5:-}" --arg bf "${6:-}" '
    {id: $id, title: $id, on_fail: $sev, required: true,
     pr: {status: $p, summary: "s", fingerprint: (if $pf == "" then null else $pf end)},
     base: (if $b == "" then null else {status: $b, summary: "s", fingerprint: (if $bf == "" then null else $bf end)} end)}'
}
gen() {
  # gen <id> <kind> <base-status> <pr-status>
  jq -n --arg id "$1" --arg k "$2" --arg b "$3" --arg p "$4" \
    '{id: $id, title: $id, kind: $k, severity: "attention", base: {status: $b, summary: "s"}, pr: {status: $p, summary: "s"}}'
}
IMPACT='{"summary": "reviewed", "findings": []}'

out=$(verdict "[$(chk a block pass)]" '[]' low)
check "all pass, low tier → safe" "safe-to-merge 0" "$(jq -r '"\(.verdict) \(.exit_code)"' <<<"$out")"

out=$(verdict "[$(chk drift attention fail fail fp1 fp1)]" '[]' low)
check "same failure on base → pre-existing" "pre-existing safe-to-merge" "$(jq -r '"\(.checks[0].outcome) \(.verdict)"' <<<"$out")"

out=$(verdict "[$(chk drift attention fail fail fp1 fp2)]" '[]' low)
check "different failure on base → changed" "changed needs-human" "$(jq -r '"\(.checks[0].outcome) \(.verdict)"' <<<"$out")"

out=$(verdict "[$(chk build block fail pass fp1)]" '[]' low)
check "block check regresses → block" "regression block 1" "$(jq -r '"\(.checks[0].outcome) \(.verdict) \(.exit_code)"' <<<"$out")"

out=$(verdict "[$(chk tidy attention fail pass fp1)]" '[]' low)
check "attention check regresses → needs-human" "needs-human" "$(jq -r '.verdict' <<<"$out")"

out=$(verdict "[$(chk smoke block error)]" '[]' low)
check "check error → exit 2" "needs-human 2" "$(jq -r '"\(.verdict) \(.exit_code)"' <<<"$out")"

out=$(verdict "[$(chk docker block skip)]" '[]' low)
check "required check skipped → needs-human" "needs-human" "$(jq -r '.verdict' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" "[$(gen g1 discriminating fail pass)]" medium "$IMPACT")
check "medium + impact + proven check → safe" "proven true safe-to-merge" "$(jq -r '"\(.generated_checks[0].outcome) \(.generated_checks[0].counted) \(.verdict)"' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" "[$(gen g1 discriminating pass pass)]" medium "$IMPACT")
check "passes on old version → does not count" "not-discriminating false needs-human" "$(jq -r '"\(.generated_checks[0].outcome) \(.generated_checks[0].counted) \(.verdict)"' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" "[$(gen g1 discriminating fail pass)]" medium)
check "medium without impact review → needs-human" "needs-human" "$(jq -r '.verdict' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" "[$(gen g1 guard pass fail)]" low)
check "guard regresses → finding" "regression needs-human" "$(jq -r '"\(.generated_checks[0].outcome) \(.verdict)"' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" "[$(gen g1 guard fail fail)]" low)
check "guard failing on base → invalid, ignored" "invalid safe-to-merge" "$(jq -r '"\(.generated_checks[0].outcome) \(.verdict)"' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" '[]' high "$IMPACT")
check "high tier → needs-human" "needs-human" "$(jq -r '.verdict' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" '[]' low '{"findings": [{"severity": "block", "text": "breaking API used"}]}')
check "agent block finding → block" "block" "$(jq -r '.verdict' <<<"$out")"

out=$(verdict "[$(chk a block pass)]" '[]' low '{"tier": "high", "findings": []}')
check "agent can raise the tier" "high needs-human" "$(jq -r '"\(.tier) \(.verdict)"' <<<"$out")"

# ---- rendering

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
verdict "[$(chk drift attention fail fail fp1 fp1)]" "[$(gen g1 discriminating fail pass)]" medium "$IMPACT" |
  jq '.pr += {base_ref: "main", base_sha: "def", merge: {status: "merged"}} | .classification += {go_directive: {}}' >"$tmp/result.json"
"$ROOT/render-comment.sh" "$tmp/result.json" >"$tmp/comment.md"
check "comment has marker" 1 "$(grep -c '^<!-- ldcli-dependency-verify -->$' "$tmp/comment.md")"
check "comment lists pre-existing issue" 1 "$(grep -c 'Already failing on `main`' "$tmp/comment.md")"
check "comment shows proven generated check" 1 "$(grep -c 'proven (fails on base, passes on PR)' "$tmp/comment.md")"

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures test(s) failed"
  exit 1
fi
echo "all tests passed"
