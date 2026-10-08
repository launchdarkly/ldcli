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

out=$(verdict "[$(chk ci decide)]" '[]' high "$(jq '. + {no_local_proof: "runs only in release workflows"}' <<<"$REACHABLE")" '[]' "$SQL")
check "no-local-proof reason joins an existing question instead of a second one" "1 true" "$(v '"\(.decisions | length) \(.decisions[0].evidence | index("runs only in release workflows") != null)"')"

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

# ---- findings subsets

fnd() {
  # fnd <pr-findings-json> <base-findings-json>: an actionlint-like check that fails on both sides
  jq -n --argjson p "$1" --argjson b "$2" \
    '{id: "actionlint", title: "actionlint", required: false,
      pr: {status: "fail", summary: "p", fingerprint: "fp-p", findings: $p, recommendations: []},
      base: {status: "fail", summary: "b", fingerprint: "fp-b", findings: $b}}'
}
out=$(verdict "[$(fnd '["a", "c"]' '["a", "b", "c"]')]" '[]' low)
check "PR findings are a subset of base → pre-existing, safe" "pre-existing safe-to-merge" "$(v '"\(.checks[0].outcome) \(.verdict)"')"
out=$(verdict "[$(fnd '["a", "d"]' '["a", "b", "c"]')]" '[]' low)
check "PR adds a finding → changed, block" "changed block" "$(v '"\(.checks[0].outcome) \(.verdict)"')"

# ---- check scripts, with stub tools and no network

# run_check <script> <worktree> [env...]: runs one check, prints "status|summary|question"
run_check() {
  local script="$1" wt="$2" art
  shift 2
  art=$(mktemp -d "$tmp/art.XXXX")
  (cd "$wt" && env WT="$wt" SIDE=pr BASE_WT="${BASE_WT:-$wt}" PR_WT="$wt" ARTIFACTS="$art" VERIFY_ROOT="$ROOT" \
    PR_META="${PR_META:-/dev/null}" CLASSIFICATION="${CLASSIFICATION:-/dev/null}" PROFILE=full "$@" \
    bash "$ROOT/checks/$script.sh") >"$art/log" 2>&1
  printf '%s|%s|%s' "$(cat "$art/status" 2>/dev/null)" "$(cat "$art/summary" 2>/dev/null)" "$(cat "$art/question" 2>/dev/null)"
}
stub() {
  # stub <dir> <name> <script body>
  mkdir -p "$1"
  printf '#!/usr/bin/env bash\n%s\n' "$3" >"$1/$2"
  chmod +x "$1/$2"
}

wt="$tmp/wt" && mkdir -p "$wt"
stub "$tmp/bin-lock" golangci-lint 'echo "level=error msg=\"Running error: context loading failed\""; echo "Error: parallel golangci-lint is running"; exit 3'
out=$(run_check golangci-lint "$wt" PATH="$tmp/bin-lock:$PATH")
check "golangci-lint lock contention → incomplete, not a failing block" "incomplete" "${out%%|*}"
stub "$tmp/bin-lint" golangci-lint 'echo "cmd/a.go:3:1: unused variable (unused)"; exit 1'
out=$(run_check golangci-lint "$wt" PATH="$tmp/bin-lint:$PATH")
check "golangci-lint finding → fail" "fail|golangci-lint reports 1 finding(s)" "${out%|*}"

mkdir -p "$wt/internal/dev_server/ui"
stub "$tmp/bin-npm" npm 'cat <<"LOG"
npm warn ERESOLVE overriding peer dependency
npm warn Could not resolve dependency:
npm warn peer react@"18.2.0" from @launchpad-ui/overlay@0.3.30
npm error code ERESOLVE
npm error ERESOLVE could not resolve
npm error Could not resolve dependency:
npm error peer react@">=19.2.7" from react-router@8.0.1
LOG
[ "$1" = ci ] && exit 1; echo 10.9.0'
out=$(run_check ui-npm-ci "$wt" PATH="$tmp/bin-npm:$PATH")
check "ui-npm-ci names the conflict from npm error lines, not npm warn lines" \
  'fail|npm ci fails with ERESOLVE peer conflict: peer react@">=19.2.7" from react-router@8.0.1' "${out%|*}"

lock() {
  # lock <dir> <esbuild-version> <esbuild-script> <extra-package-json-or-empty>
  mkdir -p "$1/internal/dev_server/ui"
  jq -n --arg v "$2" --argjson s "$3" --argjson extra "${4:-{\}}" \
    '{packages: ({"": {}, "node_modules/esbuild": {version: $v, hasInstallScript: $s}} + $extra)}' \
    >"$1/internal/dev_server/ui/package-lock.json"
}
lock "$tmp/lb" 0.25.5 true
lock "$tmp/lp" 0.28.1 true '{"node_modules/newpkg": {"version": "1.0.0", "hasInstallScript": true}}'
check "install scripts: a package that already had one on base is not new" '["newpkg@1.0.0"]' \
  "$(python3 "$LIB/deps.py" transitive "$tmp/lb" "$tmp/lp" npm-ui | jq -c '.install_scripts')"

jq -n '{title: "chore(deps): bump the npm_and_yarn group across 1 directory with 2 updates",
        body: "Bumps the npm_and_yarn group with 2 updates: [dompurify](https://github.com/cure53/DOMPurify) and [uuid](https://github.com/uuidjs/uuid).\nUpdates `dompurify` from 3.2.4 to 3.4.13"}' >"$tmp/meta.json"
jq -n '{group: true, security: false, direct_update_count: 3, updates: [
          {name: "dompurify", direct: true, from: "3.2.4", to: "3.4.13", semver: "minor", ecosystem: "npm-ui"},
          {name: "uuid", direct: true, from: "9.0.1", to: null, semver: "removed", ecosystem: "npm-ui"},
          {name: "react-router", direct: true, from: "7.18.2", to: "8.3.0", semver: "major", ecosystem: "npm-ui"},
          {name: "react-router-dom", direct: false, from: "7.18.2", to: "8.3.0", semver: "major", ecosystem: "npm-ui"}]}' >"$tmp/cls.json"
out=$(run_check pr-disclosure "$wt" PR_META="$tmp/meta.json" CLASSIFICATION="$tmp/cls.json")
check "pr-disclosure asks about the direct update that the description does not name" \
  "decide|Accept the 1 direct update(s) that the PR description does not name: react-router 7.18.2 → 8.3.0 (major)?" \
  "$(cut -d'|' -f1,3 <<<"$out")"
jq '.updates |= map(select(.name != "react-router")) | .direct_update_count = 2' "$tmp/cls.json" >"$tmp/cls2.json"
out=$(run_check pr-disclosure "$wt" PR_META="$tmp/meta.json" CLASSIFICATION="$tmp/cls2.json")
check "pr-disclosure passes when the description names every direct update" "pass" "${out%%|*}"

jq -n '{merge: {status: "conflict", conflicts: ["go.mod"]}, behind_by: 5, author: "app/dependabot", base_ref: "main"}' >"$tmp/pr-conflict.json"
run_check pr-state "$wt" PR_META="$tmp/pr-conflict.json" CLASSIFICATION="$tmp/cls.json" >/dev/null
details=$(cat "$(ls -dt "$tmp"/art.* | head -n1)/details.md")
check "pr-state does not say a conflicting PR was merged into main" "0" "$(grep -c 'merged into' <<<"$details")"

# Workflows pin setup-go at v4 and v5. The PR moves both to v6, so the old version is v4.
for side in b p; do mkdir -p "$tmp/gh-$side/.github/workflows"; done
printf 'on: pull_request\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-go@v4\n' >"$tmp/gh-b/.github/workflows/a.yml"
printf 'on: pull_request\njobs:\n  b:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-go@v5\n' >"$tmp/gh-b/.github/workflows/b.yml"
printf 'on: pull_request\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-go@v6\n' >"$tmp/gh-p/.github/workflows/a.yml"
cp "$tmp/gh-p/.github/workflows/a.yml" "$tmp/gh-p/.github/workflows/b.yml"
sed -i 's/  a:/  b:/' "$tmp/gh-p/.github/workflows/b.yml"
printf '.github/workflows/a.yml\n.github/workflows/b.yml\n' >"$tmp/changed.txt"
echo '{}' >"$tmp/pr0.json"
"$LIB/classify.sh" "$tmp/gh-b" "$tmp/gh-p" "$tmp/changed.txt" "$tmp/pr0.json" "$tmp/cls-gh.json" 2>"$tmp/classify.err"
check "classify: mixed old refs use the lowest replaced ref" "v4 v6 v4,v5" \
  "$(jq -r '.updates[0] | "\(.from) \(.to) \([.replaced[].ref] | join(","))"' "$tmp/cls-gh.json" 2>/dev/null)"

# A stub gh serves action.yml for each ref. v6 drops the python-version default
# and, like release-please-action, sets an output that no version declares.
mkdir -p "$tmp/yml"
printf 'inputs:\n  go-version:\n    default: "1.x"\n  token:\n    default: ${{ github.token }}\n  cache:\n    default: true\nruns:\n  using: node16\n' >"$tmp/yml/v4"
printf 'inputs:\n  go-version:\n    default: "1.x"\n  cache:\n    default: true\nruns:\n  using: node20\n' >"$tmp/yml/v5"
# v6 writes the token default with a GitHub Enterprise Server fallback. On
# github.com it gives the same value, so it is not a changed default.
cat >"$tmp/yml/v6" <<'YML'
inputs:
  go-version:
    description: x
  token:
    default: ${{ github.server_url == 'https://github.com' && github.token || '' }}
  cache:
    default: true
runs:
  using: node24
YML
stub "$tmp/bin-gh" gh "ref=\"\${2##*ref=}\"; f=\"$tmp/yml/\$ref\"; [ -f \"\$f\" ] || exit 1; base64 -w0 \"\$f\""
out=$(BASE_WT="$tmp/gh-b" run_check actions-coverage "$tmp/gh-p" PATH="$tmp/bin-gh:$PATH" CLASSIFICATION="$tmp/cls-gh.json")
check "actions-coverage: an empty field does not shift the runtime, and every old ref is compared" \
  "decide|Accept the changed input defaults of actions/setup-go in a.yml, b.yml (go-version: 1.x → none)?" "$(cut -d'|' -f1,3 <<<"$out")"
check "actions-coverage evidence names the runtimes of both old refs" 1 \
  "$(grep -c 'v4, v5 → v6: inputs and outputs that the repo uses are unchanged; runtime node16, node20 → node24' <<<"$out")"

printf 'on: push\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-go@v6\n        id: rp\n      - run: echo ${{ steps.rp.outputs.release_created }}\n' >"$tmp/gh-p/.github/workflows/b.yml"
out=$(BASE_WT="$tmp/gh-b" run_check actions-coverage "$tmp/gh-p" PATH="$tmp/bin-gh:$PATH" CLASSIFICATION="$tmp/cls-gh.json")
check "actions-coverage: an output that no version declares is not reported as removed" "decide" "${out%%|*}"

# ---- second 2026-10-06 run

# Bug 1: names in bot-generated blocks (Cursor Bugbot summary) or in quoted
# upstream notes (<details>) do not disclose an update.
jq -n '{title: "chore(deps): bump the npm_and_yarn group across 1 directory with 2 updates",
        body: ("Bumps the npm_and_yarn group with 2 updates: [dompurify](https://github.com/cure53/DOMPurify) and [uuid](https://github.com/uuidjs/uuid).\n"
               + "<details>\n<summary>Commits</summary>\nAlso see react-router.\n</details>\n"
               + "<!-- CURSOR_SUMMARY -->\n> It also moves **`react-router`** to 8.3.0.\n<!-- /CURSOR_SUMMARY -->\n")}' >"$tmp/meta-bot.json"
out=$(run_check pr-disclosure "$wt" PR_META="$tmp/meta-bot.json" CLASSIFICATION="$tmp/cls.json")
check "pr-disclosure ignores names in the Bugbot summary and in quoted notes" \
  "decide|Accept the 1 direct update(s) that the PR description does not name: react-router 7.18.2 → 8.3.0 (major)?" \
  "$(cut -d'|' -f1,3 <<<"$out")"

# Bug 2: a direct update that a named update forces is not a decision, and a
# single update is not "grouped".
jq -n '{title: "chore(deps): bump go.uber.org/mock from 0.5.2 to 0.6.0", body: "Bumps [go.uber.org/mock](https://github.com/uber/mock) from 0.5.2 to 0.6.0."}' >"$tmp/meta-mock.json"
jq -n '{group: true, security: false, direct_update_count: 2, updates: [
          {name: "go.uber.org/mock", direct: true, from: "v0.5.2", to: "v0.6.0", semver: "minor", ecosystem: "gomod"},
          {name: "golang.org/x/term", direct: true, from: "v0.33.0", to: "v0.34.0", semver: "minor", ecosystem: "gomod"}]}' >"$tmp/cls-mock.json"
stub "$tmp/bin-go" go 'cat <<"G"
example.com/ldcli go.uber.org/mock@v0.6.0
example.com/ldcli golang.org/x/term@v0.34.0
go.uber.org/mock@v0.6.0 golang.org/x/tools@v0.36.0
golang.org/x/tools@v0.36.0 golang.org/x/net@v0.43.0
golang.org/x/net@v0.43.0 golang.org/x/term@v0.34.0
G'
out=$(run_check pr-disclosure "$wt" PR_META="$tmp/meta-mock.json" CLASSIFICATION="$tmp/cls-mock.json" PATH="$tmp/bin-go:$PATH")
check "pr-disclosure: an update that a named update forces is a note, not a decision" "info" "${out%%|*}"
check "pr-disclosure names the module path that forces the update" 1 \
  "$(grep -c 'golang.org/x/term v0.33.0 → v0.34.0, required by go.uber.org/mock v0.6.0 through golang.org/x/tools v0.36.0, golang.org/x/net v0.43.0' <<<"$out")"
check "pr-disclosure does not call a single update grouped" 0 "$(grep -c 'grouped' <<<"$out")"

# Bug 3: npm audit compares advisory IDs. A package that is flagged only
# "via" another package (@launchpad-ui/core in #723) has no new advisory.
for side in auditbase auditpr; do mkdir -p "$tmp/$side/internal/dev_server/ui"; done
adv() { jq -n --arg id "$1" '{name: "react-router", url: "https://github.com/advisories/\($id)", severity: "high", title: "t", source: 1}'; }
jq -n --argjson a "$(adv GHSA-aaaa-bbbb-cccc)" '{vulnerabilities: {"react-router": {severity: "high", via: [$a]},
  "@launchpad-ui/navigation": {severity: "moderate", via: ["react-router"]}}}' >"$tmp/audit-base.json"
jq --argjson a "$(adv GHSA-aaaa-bbbb-cccc)" '.vulnerabilities["@launchpad-ui/core"] = {severity: "moderate", via: ["@launchpad-ui/navigation"]}' \
  "$tmp/audit-base.json" >"$tmp/audit-pr.json"
jq --argjson a "$(adv GHSA-dddd-eeee-ffff)" '.vulnerabilities["react-router"].via += [$a]' "$tmp/audit-pr.json" >"$tmp/audit-pr-new.json"
stub "$tmp/bin-audit" npm "case \"\$PWD\" in *auditbase/*) cat '$tmp/audit-base.json' ;; *) cat \"\${AUDIT_PR:-$tmp/audit-pr.json}\" ;; esac"
out=$(BASE_WT="$tmp/auditbase" run_check ui-npm-audit "$tmp/auditpr" PATH="$tmp/bin-audit:$PATH")
check "ui-npm-audit: a new path to an old advisory is not a new advisory" "pass" "${out%%|*}"
out=$(BASE_WT="$tmp/auditbase" run_check ui-npm-audit "$tmp/auditpr" PATH="$tmp/bin-audit:$PATH" AUDIT_PR="$tmp/audit-pr-new.json")
check "ui-npm-audit: a new advisory ID fails" "fail|New high/critical advisories: GHSA-dddd-eeee-ffff (react-router, high)" "${out%|*}"

# Bug 4: note coverage. A CHANGELOG that stops before the old version is
# partial. A version without a tag reads the CHANGELOG on the default branch.
# The release scan goes past 3 pages when it has not reached the old version.
out=$(python3 -B - "$LIB" <<'PY'
import base64, sys
sys.path.insert(0, sys.argv[1])
import upstream
vite = "## 8.2.1\n- fix\n## 8.1.0\n- feat\n## 8.0.0\n- breaking\n### 7.3.x (2025)\nsee 7.3 changelog\n"
core = "## 0.59.18\n- a\n## 0.59.17\n- b\n## 0.52.0\n- Remove pagination package\n## 0.49.22\n- c\n"
def content(t):
    return {"content": base64.b64encode(t.encode()).decode()}
def releases(page):
    if page > 5:
        return []
    if page < 5:
        return [{"tag_name": f"x@9.{page}.{i}", "body": ""} for i in range(100)]
    return [{"tag_name": "v1.3.0", "body": "three"}, {"tag_name": "v1.2.0", "body": "two"}, {"tag_name": "v1.0.0", "body": "old"}]
def fake(path):
    if path.startswith("repos/v/v/contents/packages/vite/CHANGELOG.md"):
        return content(vite)
    if path == "repos/l/l/contents/packages/core/CHANGELOG.md":
        return content(core)
    if path.startswith("repos/r/r/releases"):
        return releases(int(path.split("&page=")[1]))
    return None
upstream.gh_json = fake
_, cov = upstream.changelog_section("v/v", "packages/vite/CHANGELOG.md", "6.4.3", "8.2.1", "v8.2.1")
print(cov["status"], cov["lowest"])
text, cov = upstream.changelog_section("l/l", "packages/core/CHANGELOG.md", "0.49.22", "0.59.17", "@launchpad-ui/core@0.59.17")
print(cov["status"], "0.52.0" in text, "0.49.22" in text)
src, text, cov = upstream.release_notes("r/r", "1.0.0", "1.3.0", "v1.3.0")
print(src, cov["status"], cov["lowest"])
PY
)
check "upstream: a CHANGELOG that stops before the old version is partial" "partial 8.0.0" "$(sed -n 1p <<<"$out")"
check "upstream: a version without a tag reads the CHANGELOG on the default branch" "full True False" "$(sed -n 2p <<<"$out")"
check "upstream: the release scan goes past 3 pages" "release notes full 1.2.0" "$(sed -n 3p <<<"$out")"

# Bug 6: one license question per package family, which says dev-only.
lic() {
  # lic <dir> <package>...: a lockfile with MPL-2.0 dev packages
  local d="$1" p
  shift
  mkdir -p "$d/internal/dev_server/ui"
  for p in "$@"; do printf '%s\n' "$p"; done | jq -R -s 'split("\n") | map(select(length > 0))
    | {packages: ({"": {}} + (map({key: "node_modules/\(.)", value: {version: "1.33.0", license: "MPL-2.0", dev: true}}) | from_entries))}' \
    >"$d/internal/dev_server/ui/package-lock.json"
}
lic "$tmp/lb2"
lic "$tmp/lp2" lightningcss lightningcss-darwin-arm64 lightningcss-linux-x64-gnu
jq -n '{ecosystems: ["npm-ui"]}' >"$tmp/cls-npm.json"
out=$(BASE_WT="$tmp/lb2" run_check license-changes "$tmp/lp2" CLASSIFICATION="$tmp/cls-npm.json")
check "license-changes asks one short question per package family" \
  "decide|Accept MPL-2.0 for lightningcss 1.33.0 and 2 lightningcss-* package(s) (dev-only build tools)?" "$(cut -d'|' -f1,3 <<<"$out")"

# Bug 7: a guard regression already shows that the change reaches ldcli.
out=$(verdict "[$(chk a pass)]" "[$(gen g1 guard pass fail)]" medium "$REACHABLE" '[]' "$SQL")
check "a guard regression replaces the missing discriminating proof" "block 0" "$(v '"\(.verdict) \(.incomplete | length)"')"

# Bug 9: govulncheck lists fixed advisories at each level and the reachable ones that stay.
gv() { jq -nc --arg id "$1" --arg f "$2" '{finding: {osv: $id, trace: [({module: "golang.org/x/net"} + (if $f == "fn" then {package: "p", function: "F"} elif $f == "pkg" then {package: "p"} else {} end))]}}'; }
{ gv GO-1 fn; gv GO-2 pkg; gv GO-3 mod; } >"$tmp/gv-base.json"
gv GO-1 fn >"$tmp/gv-pr.json"
stub "$tmp/bin-gv" govulncheck "case \"\$PWD\" in *gvbase*) cat '$tmp/gv-base.json' ;; *) cat '$tmp/gv-pr.json' ;; esac"
mkdir -p "$tmp/gvbase" "$tmp/gvpr"
out=$(BASE_WT="$tmp/gvbase" run_check govulncheck "$tmp/gvpr" PATH="$tmp/bin-gv:$PATH")
check "govulncheck reports fixed advisories by level and the reachable ones that stay" \
  "pass|No new reachable vulnerabilities; fixes 2 advisories (1 imported package, 1 required module); still reachable: GO-1" "${out%|*}"

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures test(s) failed"
  exit 1
fi
echo "all tests passed"
