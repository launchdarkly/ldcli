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

verdict() {
  # verdict <checks-json> <generated-json> <tier> [impact-json] [tags-json] [direct-updates-json]
  jq -n -L "$LIB" --argjson checks "$1" --argjson gen "$2" --arg tier "$3" --argjson impact "${4:-null}" \
    --argjson tags "${5:-[]}" --argjson updates "${6:-[]}" '
    include "verdict";
    build_result({head_sha: "abc"}; {tier: $tier, tier_reasons: ["test"], updates: $updates, ecosystems: ["gomod"], tags: $tags};
                 $checks; $gen; $impact; "fast"; "now")'
}
chk() {
  # chk <id> <pr-status> [base-status] [pr-fp] [base-fp] [extra-json]
  jq -n --arg id "$1" --arg p "$2" --arg b "${3:-}" --arg pf "${4:-}" --arg bf "${5:-}" --argjson extra "${6:-{\}}" '
    {id: $id, title: $id, required: true,
     pr: {status: $p, summary: "s", question: (if $p == "decide" then "Accept \($id)?" else null end),
          fingerprint: (if $pf == "" then null else $pf end), recommendations: ["fix \($id)"],
          fix: (if $p == "fail" then {id: "fix-\($id)", kind: "commit", command: "make \($id)", paths: ["x"], needs_decision: false} else null end)},
     base: (if $b == "" then null else {status: $b, summary: "s", fingerprint: (if $bf == "" then null else $bf end)} end)} + $extra'
}
gen() {
  # gen <id> <kind> <base-status> <pr-status>
  jq -n --arg id "$1" --arg k "$2" --arg b "$3" --arg p "$4" \
    '{id: $id, title: $id, kind: $k, base: {status: $b, summary: "s"}, pr: {status: $p, summary: "s"}}'
}
v() { jq -r "$1" <<<"$out"; }
SQL='[{"name": "github.com/mattn/go-sqlite3", "direct": true}]'
REVIEWED='{"summary": "r", "changelog": [{"package": "github.com/mattn/go-sqlite3", "notes": "n"}], "behavior_changes_reachable": false, "findings": []}'
REACHABLE='{"summary": "r", "changelog": [{"package": "github.com/mattn/go-sqlite3", "notes": "n"}], "behavior_changes_reachable": true, "findings": []}'

out=$(verdict "[$(chk a pass)]" '[]' low)
check "all pass, low tier → safe" "safe-to-merge 0" "$(v '"\(.verdict) \(.exit_code)"')"

out=$(verdict "[$(chk drift fail fail fp1 fp1)]" '[]' low)
check "same failure on base → pre-existing, safe" "pre-existing safe-to-merge" "$(v '"\(.checks[0].outcome) \(.verdict)"')"

out=$(verdict "[$(chk build fail fail fp1 fp1 '{"gate": true}')]" '[]' low)
check "gate failing the same way on base → incomplete, never safe" "incomplete 2" "$(v '"\(.verdict) \(.exit_code)"')"

out=$(verdict "[$(chk release-snapshot fail fail fp1 fp1 '{"required": false, "required_for_tags": ["cgo"]}')]" '[]' low null '["cgo"]')
check "false-safe case: tag gate fails on both (e.g. image pull) → incomplete" "incomplete" "$(v .verdict)"

out=$(verdict "[$(chk release-snapshot incomplete "" "" "" '{"required": false, "required_for_tags": ["cgo"]}')]" '[]' low null '["cgo"]')
check "tag gate could not run → incomplete" "incomplete" "$(v .verdict)"

out=$(verdict "[$(chk release-snapshot skip "" "" "" '{"required": false, "required_for_tags": ["cgo"]}' | jq '.pr.profile_skipped = true')]" '[]' low null '["tui"]')
check "tag gate not needed for other tags → safe" "safe-to-merge" "$(v .verdict)"

out=$(verdict "[$(chk drift fail fail fp1 fp2)]" '[]' low)
check "different failure on base → changed, block" "changed block" "$(v '"\(.checks[0].outcome) \(.verdict)"')"

out=$(verdict "[$(chk tidy fail pass fp1)]" '[]' low)
check "regression → block with fix recipe" "block 1 fix-tidy" "$(v '"\(.verdict) \(.exit_code) \(.fixes[0].id)"')"

out=$(verdict "[$(chk help decide)]" '[]' low)
check "decide → needs-human with the exact question" "needs-human Accept help?" "$(v '"\(.verdict) \(.decisions[0].question)"')"

out=$(verdict "[$(chk smoke error)]" '[]' low)
check "check crash → incomplete, exit 2" "incomplete 2" "$(v '"\(.verdict) \(.exit_code)"')"

out=$(verdict "[$(chk docker incomplete)]" '[]' low)
check "required check did not run → incomplete" "incomplete" "$(v .verdict)"

out=$(verdict "[$(chk audit incomplete "" "" "" '{"required": false}')]" '[]' low)
check "optional check did not run → safe, listed as not run" "safe-to-merge 1" "$(v '"\(.verdict) \(.not_run | length)"')"

out=$(verdict "[$(chk a pass)]" '[]' medium null '[]' "$SQL")
check "medium tier without impact review → incomplete" "incomplete" "$(v .verdict)"

out=$(verdict "[$(chk a pass)]" '[]' medium '{"summary": "r", "changelog": [], "findings": []}' '[]' "$SQL")
check "impact review must cover each direct update and state reachability" "incomplete 2" "$(v '"\(.verdict) \(.incomplete | length)"')"

out=$(verdict "[$(chk a pass)]" '[]' medium "$REVIEWED" '[]' "$SQL")
check "medium, reviewed, changes not reachable → safe" "safe-to-merge" "$(v .verdict)"

out=$(verdict "[$(chk a pass)]" "[$(gen g1 discriminating fail pass)]" medium "$REACHABLE" '[]' "$SQL")
check "reachable change + proven check → safe" "proven true safe-to-merge" "$(v '"\(.generated_checks[0].outcome) \(.generated_checks[0].counted) \(.verdict)"')"

out=$(verdict "[$(chk a pass)]" "[$(gen g1 discriminating pass pass)]" medium "$REACHABLE" '[]' "$SQL")
check "passes on old version → does not count → incomplete" "not-discriminating false incomplete" "$(v '"\(.generated_checks[0].outcome) \(.generated_checks[0].counted) \(.verdict)"')"

out=$(verdict "[$(chk a pass)]" '[]' high "$(jq '. + {no_local_proof: "runs only in release workflows"}' <<<"$REACHABLE")" '[]' "$SQL")
check "reachable change with no possible local proof → a decision, not incomplete" "needs-human 0" "$(v '"\(.verdict) \(.incomplete | length)"')"

out=$(verdict "[$(chk a pass)]" "[$(gen g1 guard pass fail)]" low)
check "guard regresses → block" "regression block" "$(v '"\(.generated_checks[0].outcome) \(.verdict)"')"

out=$(verdict "[$(chk a pass)]" "[$(gen g1 guard fail fail)]" low)
check "guard failing on base → invalid → incomplete" "invalid incomplete" "$(v '"\(.generated_checks[0].outcome) \(.verdict)"')"

out=$(verdict "[$(chk a pass)]" '[]' high "$REVIEWED" '[]' "$SQL")
check "high tier alone does not ask a person" "safe-to-merge" "$(v .verdict)"

out=$(verdict "[$(chk a pass)]" '[]' low '{"findings": [{"severity": "block", "text": "breaking API used"}]}')
check "agent block finding → block" "block" "$(v .verdict)"

out=$(verdict "[$(chk a pass)]" '[]' low '{"findings": [{"severity": "decide", "text": "t", "question": "Accept X?"}]}')
check "agent decision → needs-human with its question" "needs-human Accept X?" "$(v '"\(.verdict) \(.decisions[0].question)"')"

out=$(verdict "[$(chk a pass)]" '[]' low '{"tier": "high", "findings": []}')
check "agent can raise the tier" "high incomplete" "$(v '"\(.tier) \(.verdict)"')"

out=$(verdict "[$(chk b fail pass), $(chk h decide), $(chk d incomplete)]" '[]' low)
check "block beats needs-human beats incomplete" "block 1 1 1" "$(v '"\(.verdict) \(.blocks | length) \(.decisions | length) \(.incomplete | length)"')"

# ---- rendering

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
render() {
  jq '.pr += {base_ref: "main", base_sha: "def", merge: {status: "merged"}} | .classification += {go_directive: {}}' >"$tmp/result.json"
  "$ROOT/render-comment.sh" "$tmp/result.json"
}
verdict "[$(chk drift fail fail fp1 fp1), $(chk help decide), $(chk docker incomplete)]" "[$(gen g1 discriminating fail pass)]" medium "$REVIEWED" '[]' "$SQL" | render >"$tmp/c1.md"
check "comment has marker" 1 "$(grep -c '^<!-- ldcli-dependency-verify -->$' "$tmp/c1.md")"
check "comment headline" 1 "$(grep -c '^## Dependency verification: NEEDS A HUMAN DECISION$' "$tmp/c1.md")"
check "comment asks the exact question" 1 "$(grep -c '^1\. \*\*Accept help?\*\*$' "$tmp/c1.md")"
check "comment lists incomplete items separately" 1 "$(grep -c '^### Incomplete verification$' "$tmp/c1.md")"
check "comment lists pre-existing issue" 1 "$(grep -c 'Already failing on `main`' "$tmp/c1.md")"
check "comment shows proven generated check" 1 "$(grep -c 'proven (fails on base, passes on PR)' "$tmp/c1.md")"
verdict "[$(chk tidy fail pass fp1)]" '[]' low | render >"$tmp/c2.md"
check "block comment shows the mechanical fix" 1 "$(grep -c 'Mechanical fix: `make tidy`' "$tmp/c2.md")"
verdict "[$(chk a pass)]" '[]' low | render >"$tmp/c3.md"
check "safe comment says no decision is necessary" 1 "$(grep -c 'No decision is necessary' "$tmp/c3.md")"

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures test(s) failed"
  exit 1
fi
echo "all tests passed"
