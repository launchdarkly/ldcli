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
check "block beats incomplete beats needs-human" "block 1 1 1" "$(v '"\(.verdict) \(.blocks | length) \(.decisions | length) \(.incomplete | length)"')"

out=$(verdict "[$(chk h decide), $(chk d incomplete)]" '[]' low)
check "incomplete outranks needs-human: a question waits until verification is complete (#719)" "incomplete 2 1" "$(v '"\(.verdict) \(.exit_code) \(.decisions | length)"')"

# ---- rendering

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
render() {
  jq '.pr += {base_ref: "main", base_sha: "def", merge: {status: "merged"}} | .classification += {go_directive: {}}' >"$tmp/result.json"
  "$ROOT/render-comment.sh" "$tmp/result.json"
}
verdict "[$(chk drift fail fail fp1 fp1), $(chk help decide), $(chk docker incomplete)]" "[$(gen g1 discriminating fail pass)]" medium "$REVIEWED" '[]' "$SQL" | render >"$tmp/c1.md"
check "comment has marker" 1 "$(grep -c '^<!-- ldcli-dependency-verify -->$' "$tmp/c1.md")"
check "comment headline: incomplete when a decision and an incomplete item are both open" 1 "$(grep -c '^## Dependency verification: INCOMPLETE$' "$tmp/c1.md")"
check "comment says that the questions wait for the incomplete items" 1 "$(grep -c 'The verdict stays incomplete until' "$tmp/c1.md")"
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

# ---- 2026-10-13 recheck

# Bug 1: ci-status passed #717 from the rollup alone (Bugbot, Semgrep, bot
# oversight). Go and dev-server UI ended failure with 0 jobs, and Dependency
# Scan stayed queued. The expected workflows are compared with the runs.
ci="$tmp/ci" && mkdir -p "$ci/.github/workflows"
printf 'name: Go\non:\n  push:\n    branches: [main]\n  pull_request:\n    branches: [main]\njobs: {}\n' >"$ci/.github/workflows/go.yml"
printf 'name: dev-server UI\non:\n  pull_request:\n    branches: [main]\njobs: {}\n' >"$ci/.github/workflows/dev-server-ui.yml"
printf 'name: Dependency Scan\non:\n  pull_request:\njobs: {}\n' >"$ci/.github/workflows/dependency-scan.yml"
printf 'name: Lint PR title\non:\n  pull_request_target:\n    types: [opened, edited, synchronize]\njobs: {}\n' >"$ci/.github/workflows/lint-pr-title.yml"
printf 'name: Docs\non:\n  pull_request:\n    paths: ["docs/**", "!docs/internal/**"]\njobs: {}\n' >"$ci/.github/workflows/docs.yml"
printf 'name: Labels\non:\n  pull_request:\n    types: [labeled]\njobs: {}\n' >"$ci/.github/workflows/labels.yml"
printf 'name: Release\non:\n  push:\n    branches: [main]\njobs: {}\n' >"$ci/.github/workflows/release.yml"
jq -n '{changed_files: [".github/workflows/go.yml", ".github/workflows/dependency-scan.yml"]}' >"$tmp/cls-ci.json"
H=348e5d8bd25abfeefa62f61b5195746f6dd64e70
run_json() {
  # run_json <name> <file> <event> <status> <conclusion|null> <jobs> [head]
  jq -nc --arg n "$1" --arg f "$2" --arg e "$3" --arg s "$4" --argjson c "$5" --argjson j "$6" --arg h "${7:-$H}" \
    '{id: ($f | length), name: $n, path: ".github/workflows/\($f)", event: $e, status: $s, conclusion: $c,
      jobs_count: $j, head_sha: $h, created_at: (env.CREATED // "2026-10-07T16:52:53Z"), run_attempt: 1}'
}
rollup='[{"name": "Cursor Bugbot", "status": "COMPLETED", "conclusion": "SUCCESS", "workflow": ""},
         {"name": "semgrep-cloud-platform/scan", "status": "COMPLETED", "conclusion": "SUCCESS", "workflow": ""},
         {"name": "Require additional human oversight on bot PRs", "status": "COMPLETED", "conclusion": "SUCCESS", "workflow": ""}]'
ci_meta() {
  # ci_meta <runs-json> [rollup-head]
  jq -n --arg h "$H" --arg rh "${2:-$H}" --argjson ci "$rollup" --argjson runs "$1" \
    '{head_sha: $h, base_ref: "main", ci: $ci, ci_head: $rh, ci_runs: $runs}' >"$tmp/ci-meta.json"
}
ci_check() { BASE_WT="$ci" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci.json"; }
# Green runs from today, so that the age of the runs does not change the summary.
export CREATED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
green="[$(run_json Go go.yml pull_request completed '"success"' 1), $(run_json 'dev-server UI' dev-server-ui.yml pull_request completed '"success"' 1),
        $(run_json 'Dependency Scan' dependency-scan.yml pull_request completed '"success"' 2), $(run_json 'Lint PR title' lint-pr-title.yml pull_request_target completed '"success"' 1)]"
unset CREATED

ci_meta "[$(run_json Go go.yml pull_request completed '"failure"' 0), $(run_json 'dev-server UI' dev-server-ui.yml pull_request completed '"failure"' 0),
          $(run_json 'Dependency Scan' dependency-scan.yml pull_request queued null 0), $(run_json 'Lint PR title' lint-pr-title.yml pull_request_target queued null 0)]"
out=$(ci_check)
check "ci-status: 0-job failures and queued runs are incomplete, not green (#717)" "incomplete" "${out%%|*}"
check "ci-status names each workflow that did not run" 1 \
  "$(grep -c 'Go (ended `failure` with 0 jobs, so no job ran), Lint PR title (queued since 2026-10-07), dev-server UI (ended `failure` with 0 jobs, so no job ran)' <<<"$out")"
check "ci-status names the queued Dependency Scan run" 1 "$(grep -c 'Dependency Scan (queued since 2026-10-07)' <<<"$out")"

ci_meta "$green"
out=$(ci_check)
check "ci-status: every expected workflow green → pass" "pass|All 4 expected workflow run(s) and 3 other check(s) green on PR head 348e5d8" "${out%|*}"
check "ci-status: a paths filter, a negated path, and activity types exclude workflows" "docs.yml labels.yml" \
  "$(jq -r '[.not_expected[].path | sub(".github/workflows/"; "")] | join(" ")' "$(ls -dt "$tmp"/art.* | head -n1)/report.json")"

check "ci-status path filters: ** spans directories, * does not, and a later ! pattern excludes" "True False True False" \
  "$(python3 -B -c 'import sys; sys.path.insert(0, sys.argv[1]); from ci_runs import matches as m
p = ["docs/**", "!docs/internal/**"]
print(m(p, "docs/a/b.md"), m(p, "docs/internal/x.md"), m(["**/*.md"], "README.md"), m(["*.md"], "docs/a.md"))' "$LIB")"

ci_meta "$(jq -c 'map(select(.name != "Go"))' <<<"$green")"
out=$(ci_check)
check "ci-status: an expected workflow with no run is incomplete" "incomplete" "${out%%|*}"
check "ci-status says the missing workflow has no run" 1 "$(grep -c 'Go (no run on this head)' <<<"$out")"

ci_meta "$(jq -c 'map(if .name == "Go" then .conclusion = "startup_failure" | .jobs_count = 0 else . end)' <<<"$green")"
check "ci-status: a startup failure is incomplete" "incomplete" "$(ci_check | cut -d'|' -f1)"

ci_meta "$(jq -c 'map(if .name == "Go" then .conclusion = "failure" | .jobs_count = 2 else . end)' <<<"$green")"
check "ci-status: a failure in a job that ran → fail" "fail|Failing on PR head 348e5d8: Go (\`failure\` in 2 job(s))" "$(ci_check | cut -d'|' -f1,2)"

old=c3c375a8b88c8affe8970c096ff1c70b4309c36d
ci_meta "$(jq -c --arg o "$old" 'map(.head_sha = $o)' <<<"$green")" "$old"
out=$(ci_check)
check "ci-status: green results for an older head count as not run" "incomplete" "${out%%|*}"
check "ci-status says the rollup is for the older head" 1 "$(grep -c 'Cursor Bugbot (result is for c3c375a, not this head)' <<<"$out")"

jq -n --arg h "$H" --argjson ci "$rollup" '{head_sha: $h, base_ref: "main", ci: $ci, ci_head: $h, ci_runs: null}' >"$tmp/ci-meta.json"
check "ci-status: unknown workflow runs are incomplete" "incomplete" "$(ci_check | cut -d'|' -f1)"
check "ci-status is a required check, so incomplete CI makes the verdict incomplete" "true" \
  "$(jq -r '.checks[] | select(.id == "ci-status") | .required' "$ROOT/checks/registry.json")"

# Bug 4: actions-coverage showed "v4 → v6.0.3" for #719. The floating @v4 is
# v4.4.0 today, and @v6 is v6.1.0, so the exact pin v6.0.3 misses a release.
for side in b p; do mkdir -p "$tmp/co-$side/.github/workflows"; done
printf 'on: push\njobs:\n  r:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n' >"$tmp/co-b/.github/workflows/r.yml"
sed 's/@v4$/@v6.0.3/' "$tmp/co-b/.github/workflows/r.yml" >"$tmp/co-p/.github/workflows/r.yml"
printf '.github/workflows/r.yml\n' >"$tmp/co-changed.txt"
"$LIB/classify.sh" "$tmp/co-b" "$tmp/co-p" "$tmp/co-changed.txt" "$tmp/pr0.json" "$tmp/cls-co.json" 2>>"$tmp/classify.err"
mkdir -p "$tmp/co-api"
printf 'inputs:\n  fetch-depth:\n    default: 1\nruns:\n  using: node20\n' | base64 -w0 >"$tmp/co-api/v4"
printf 'inputs:\n  fetch-depth:\n    default: 1\nruns:\n  using: node24\n' | base64 -w0 >"$tmp/co-api/v6.0.3"
cp "$tmp/co-api/v6.0.3" "$tmp/co-api/v4.3.0"
# v4 and v4.4.0 (an annotated tag) are on commit a44; v6 and v6.1.0 on c61.
cat >"$tmp/co-api/routes" <<'R'
repos/actions/checkout/commits/v4	{"sha": "a44"}
repos/actions/checkout/commits/v6	{"sha": "c61"}
repos/actions/checkout/git/matching-refs/tags/v4.	[{"ref": "refs/tags/v4.3.0", "object": {"sha": "b43", "type": "commit"}}, {"ref": "refs/tags/v4.4.0", "object": {"sha": "t44", "type": "tag"}}]
repos/actions/checkout/git/matching-refs/tags/v6.	[{"ref": "refs/tags/v6.0.3", "object": {"sha": "d603", "type": "commit"}}, {"ref": "refs/tags/v6.1.0", "object": {"sha": "c61", "type": "commit"}}]
repos/actions/checkout/git/tags/t44	{"object": {"sha": "a44", "type": "commit"}}
R
stub "$tmp/bin-co" gh "case \"\$2\" in
  *contents/action.yml\\?ref=*) f=\"$tmp/co-api/\${2##*ref=}\"; [ -f \"\$f\" ] && cat \"\$f\" || exit 1 ;;
  *) awk -F'\t' -v p=\"\$2\" '\$1 == p {print \$2; found=1} END {exit !found}' \"$tmp/co-api/routes\" ;;
esac"
out=$(BASE_WT="$tmp/co-b" run_check actions-coverage "$tmp/co-p" PATH="$tmp/bin-co:$PATH" CLASSIFICATION="$tmp/cls-co.json")
check "actions-coverage resolves the floating old ref to the exact release" 1 \
  "$(grep -c '| `actions/checkout` | v4 (v4.4.0) → v6.0.3 |' "$(ls -dt "$tmp"/art.* | head -n1)/details.md")"
check "actions-coverage flags an exact pin that is older than its floating major" \
  "decide|Accept actions/checkout v6.0.3 in workflows that PR CI does not run (r.yml); and accept the exact pin actions/checkout@v6.0.3, which is older than v6.1.0 that @v6 resolves to today?" \
  "${out%%|*}|${out##*|}"
sed -i 's/@v6.0.3$/@v4.3.0/' "$tmp/co-p/.github/workflows/r.yml"
"$LIB/classify.sh" "$tmp/co-b" "$tmp/co-p" "$tmp/co-changed.txt" "$tmp/pr0.json" "$tmp/cls-co.json" 2>>"$tmp/classify.err"
out=$(BASE_WT="$tmp/co-b" run_check actions-coverage "$tmp/co-p" PATH="$tmp/bin-co:$PATH" CLASSIFICATION="$tmp/cls-co.json")
check "actions-coverage: an exact pin below what the replaced floating ref gives is a step back" \
  "decide|Accept the exact pin actions/checkout@v4.3.0, which is older than v4.4.0 that @v4 (replaced by the PR) resolves to today?" \
  "${out%%|*}|${out##*|}"

# Bug 5: the #721 question named the input default (python-version: 3.x →
# none). It must name the effect on the step that uses the Python.
for side in b p; do mkdir -p "$tmp/py-$side/.github/workflows"; done
printf 'on: pull_request\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-python@v3\n      - uses: pre-commit/action@2c7b3805fd2a0fd8c1884dcaebf91fc102a13ecd # v3.0.1\n' >"$tmp/py-b/.github/workflows/go.yml"
sed 's/setup-python@v3/setup-python@v6/' "$tmp/py-b/.github/workflows/go.yml" >"$tmp/py-p/.github/workflows/go.yml"
printf '.github/workflows/go.yml\n' >"$tmp/py-changed.txt"
"$LIB/classify.sh" "$tmp/py-b" "$tmp/py-p" "$tmp/py-changed.txt" "$tmp/pr0.json" "$tmp/cls-py.json" 2>>"$tmp/classify.err"
printf 'inputs:\n  python-version:\n    default: "3.x"\nruns:\n  using: node16\n' | base64 -w0 >"$tmp/co-api/v3"
printf 'inputs:\n  python-version:\n    description: x\nruns:\n  using: node24\n' | base64 -w0 >"$tmp/co-api/v6"
out=$(BASE_WT="$tmp/py-b" run_check actions-coverage "$tmp/py-p" PATH="$tmp/bin-co:$PATH" CLASSIFICATION="$tmp/cls-py.json")
check "actions-coverage asks about the effect of a dropped python-version default, not the default" \
  "decide|Accept that pre-commit in go.yml will use the runner's Python 3.12 instead of the newest 3.x in the tool cache?" \
  "${out%%|*}|${out##*|}"
echo 3.11 >"$tmp/py-p/.python-version"
out=$(BASE_WT="$tmp/py-b" run_check actions-coverage "$tmp/py-p" PATH="$tmp/bin-co:$PATH" CLASSIFICATION="$tmp/cls-py.json")
check "actions-coverage: with a .python-version file, the effect names that version" \
  "Accept that pre-commit in go.yml will use the Python in .python-version (3.11) instead of the newest 3.x in the tool cache?" "${out##*|}"

# ---- 2026-10-20 fixes

# Lenient bug (#863): release-secrets v1.2.0 → v1.2.1 is a patch, but its
# composite steps move configure-aws-credentials from v4 to v6 and replace the
# SSM loader action with new code. The release-only workflow does not run on
# PR CI, so a person must accept it. The old check said "Interfaces match".
for side in b p; do mkdir -p "$tmp/rs-$side/.github/workflows"; done
printf 'on: push\njobs:\n  release:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: launchdarkly/gh-actions/actions/release-secrets@release-secrets-v1.2.0\n        with:\n          aws_assume_role: x\n' >"$tmp/rs-b/.github/workflows/release.yml"
sed 's/v1.2.0$/v1.2.1/' "$tmp/rs-b/.github/workflows/release.yml" >"$tmp/rs-p/.github/workflows/release.yml"
printf '.github/workflows/release.yml\n' >"$tmp/rs-changed.txt"
"$LIB/classify.sh" "$tmp/rs-b" "$tmp/rs-p" "$tmp/rs-changed.txt" "$tmp/pr0.json" "$tmp/cls-rs.json" 2>>"$tmp/classify.err"
mkdir -p "$tmp/rs-api"
cat >"$tmp/rs-api/release-secrets-v1.2.0" <<'YML'
inputs:
  aws_assume_role:
    required: true
runs:
  using: composite
  steps:
    - name: Configure AWS Credentials
      uses: aws-actions/configure-aws-credentials@v4
    - name: Load environment variables
      uses: dkershner6/aws-ssm-getparameters-action@4fcb4872421f387a6c43058473acc1b22443fe13
YML
cat >"$tmp/rs-api/release-secrets-v1.2.1" <<'YML'
inputs:
  aws_assume_role:
    required: true
runs:
  using: composite
  steps:
    - name: Configure AWS Credentials
      uses: aws-actions/configure-aws-credentials@e7f100cf4c008499ea8adda475de1042d6975c7b # v6
    - name: Load environment variables
      shell: bash
      run: node "$GITHUB_ACTION_PATH/ssm/dist/index.js"
YML
for f in "$tmp"/rs-api/release-secrets-v*; do base64 -w0 "$f" >"$f.b64"; done
echo '{"files": [{"filename": "actions/release-secrets/action.yml"}, {"filename": "actions/release-secrets/ssm/dist/index.js"}, {"filename": "actions/other/action.yml"}]}' >"$tmp/rs-api/compare"
stub "$tmp/bin-rs" gh "case \"\$2\" in
  *action.yml\\?ref=*) f=\"$tmp/rs-api/\${2##*ref=}.b64\"; [ -f \"\$f\" ] && cat \"\$f\" || exit 1 ;;
  *compare/*) cat \"$tmp/rs-api/compare\" ;;
  *) exit 1 ;;
esac"
out=$(BASE_WT="$tmp/rs-b" run_check actions-coverage "$tmp/rs-p" PATH="$tmp/bin-rs:$PATH" CLASSIFICATION="$tmp/cls-rs.json")
check "actions-coverage: a nested major update in a composite action asks about release-only workflows (#863)" \
  "decide|Accept launchdarkly/gh-actions/actions/release-secrets release-secrets-v1.2.1 in workflows that PR CI does not run (release.yml)?" \
  "${out%%|*}|${out##*|}"
check "actions-coverage names the nested changes of the composite action" 1 \
  "$(grep -c 'composite steps: aws-actions/configure-aws-credentials v4 → v6 (major); dkershner6/aws-ssm-getparameters-action removed (4fcb4872421f387a6c43058473acc1b22443fe13); run step "Load environment variables" added; 2 file(s) changed in actions/release-secrets' <<<"$out")"
rm "$tmp/rs-api/release-secrets-v1.2.1.b64"
out=$(BASE_WT="$tmp/rs-b" run_check actions-coverage "$tmp/rs-p" PATH="$tmp/bin-rs:$PATH" CLASSIFICATION="$tmp/cls-rs.json")
check "actions-coverage: an action.yml that cannot be fetched is incomplete, even for a patch" "incomplete" "${out%%|*}"

# Lenient bug (#870): npx crashed before prettier started. The check recorded
# "prettier would reformat 0 file(s)", base crashed the same way, and the
# failure counted as pre-existing, so the verdict was safe to merge.
ui="$tmp/uiwt/internal/dev_server/ui" && mkdir -p "$ui/node_modules/.bin"
echo '{"packages": {}}' >"$ui/package-lock.json"
sha256sum <"$ui/package-lock.json" | cut -c1-16 >"$ui/node_modules/.verify-lock-hash"
crash='echo "npm error Class extends value undefined is not a constructor or null" >&2; exit 1'
stub "$tmp/bin-crash" npx "$crash"
stub "$tmp/bin-crash" npm "$crash"
stub "$ui/node_modules/.bin" prettier "$crash"
out=$(run_check ui-prettier "$tmp/uiwt" PATH="$tmp/bin-crash:$PATH")
check "ui-prettier: a crash before prettier reports a file is incomplete, not a format failure" "incomplete" "${out%%|*}"
stub "$ui/node_modules/.bin" prettier '[ "$1" = --version ] && { echo 3.8.4; exit 0; }; echo "Checking formatting..."; echo "All matched files use Prettier code style!"'
out=$(run_check ui-prettier "$tmp/uiwt" PATH="$tmp/bin-crash:$PATH")
check "ui-prettier runs the installed prettier, so a broken npx on PATH does not matter" "pass|prettier --check clean (3.8.4)" "${out%|*}"
stub "$ui/node_modules/.bin" prettier 'echo "Checking formatting..."; echo "[warn] src/App.tsx"; echo "[warn] Code style issues found in the above file. Run Prettier with --write to fix."; exit 1'
out=$(run_check ui-prettier "$tmp/uiwt" PATH="$tmp/bin-crash:$PATH")
check "ui-prettier: a file to reformat is still a failure" "fail|prettier would reformat 1 file(s)" "${out%|*}"
check "ui-lint: an npm crash is incomplete" "incomplete" "$(run_check ui-lint "$tmp/uiwt" PATH="$tmp/bin-crash:$PATH" | cut -d'|' -f1)"
check "ui-test: an npm crash is incomplete" "incomplete" "$(run_check ui-test "$tmp/uiwt" PATH="$tmp/bin-crash:$PATH" | cut -d'|' -f1)"
check "ui-build-drift: an npm crash is incomplete" "incomplete" "$(run_check ui-build-drift "$tmp/uiwt" PATH="$tmp/bin-crash:$PATH" | cut -d'|' -f1)"
check "ui-npm-ls: an npm crash is incomplete" "incomplete" "$(run_check ui-npm-ls "$tmp/uiwt" PATH="$tmp/bin-crash:$PATH" | cut -d'|' -f1)"
stub "$tmp/bin-lintfail" npm 'printf "/w/src/App.tsx\n  3:1  error  x is unused  no-unused-vars\n\n✖ 1 problem (1 error, 0 warnings)\n"; exit 1'
check "ui-lint: an eslint finding is still a failure" "fail" "$(run_check ui-lint "$tmp/uiwt" PATH="$tmp/bin-lintfail:$PATH" | cut -d'|' -f1)"

# Posting log, 2026-10-17 (#869): the npm-only PR got "block" because the Go
# job failed in `go test` (the flaky TestStreamClientV2Handler on main). A
# failure in steps that read none of the changed files needs a rerun.
printf 'name: Go\non:\n  pull_request:\n    branches: [main]\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - name: build\n        run: go build .\n      - uses: pre-commit/action@2c7b3805fd2a0fd8c1884dcaebf91fc102a13ecd\n      - name: test\n        run: go test ./...\n' >"$ci/.github/workflows/go.yml"
jq -n '{ecosystems: ["npm-ui"], changed_files: ["internal/dev_server/ui/package.json", "internal/dev_server/ui/package-lock.json"], updates: []}' >"$tmp/cls-ci-npm.json"
flaky() { jq -c --argjson steps "$1" 'map(if .name == "Go" then .conclusion = "failure" | .failed_jobs = [{name: "build", conclusion: "failure", steps: $steps}] else . end)' <<<"$green"; }
rollup_go_fail='[{"name": "build", "status": "COMPLETED", "conclusion": "FAILURE", "workflow": "Go"}]'
ci_meta "$(flaky '["test"]')"
jq --argjson r "$rollup_go_fail" '.ci += $r' "$tmp/ci-meta.json" >"$tmp/ci-meta2.json" && mv "$tmp/ci-meta2.json" "$tmp/ci-meta.json"
out=$(BASE_WT="$ci" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci-npm.json")
check "ci-status: a go test failure on an npm-only PR is incomplete with a rerun, not a block (#869)" "incomplete" "${out%%|*}"
check "ci-status names the failed job and step" 1 "$(grep -c 'Go (`failure` in 1 job(s), only in job build, step test (`go test ./...`, gomod), which this PR does not change)' <<<"$out")"
check "ci-status recommends a rerun of the failed job" 1 \
  "$(grep -c 'Re-run the failed job (Go: build)' "$(ls -dt "$tmp"/art.* | head -n1)/recommendations")"
out=$(BASE_WT="$ci" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci.json")
check "ci-status: the same failure is a block when the PR changes the workflow" "fail" "${out%%|*}"
jq '.ecosystems = ["gomod"] | .changed_files = ["go.mod", "go.sum"]' "$tmp/cls-ci-npm.json" >"$tmp/cls-ci-go.json"
out=$(BASE_WT="$ci" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci-go.json")
check "ci-status: a go test failure on a Go update is a block" "fail" "${out%%|*}"
ci_meta "$(flaky '["Run pre-commit/action@2c7b3805fd2a0fd8c1884dcaebf91fc102a13ecd"]')"
out=$(BASE_WT="$ci" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci-npm.json")
check "ci-status: a failed action step (pre-commit runs the UI hooks) is a block" "fail" "${out%%|*}"
ci_meta "$(jq -c 'map(if .name == "Go" then .conclusion = "failure" else . end)' <<<"$green")"
out=$(BASE_WT="$ci" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci-npm.json")
check "ci-status: a failure without job details is a block" "fail" "${out%%|*}"

# PR #719: CI that ran before the base branch changed its workflows did not
# test the workflows that a merge uses.
cg="$tmp/ci-git" && cp -r "$ci" "$cg" && git -C "$cg" init -q && git -C "$cg" add -A &&
  GIT_COMMITTER_DATE="2026-08-01T00:00:00Z" git -C "$cg" -c user.name=t -c user.email=t@t commit -q --date=2026-08-01T00:00:00Z -m base
ci_meta "$(CREATED=2026-09-01T16:52:53Z && export CREATED && jq -c --arg c "$CREATED" 'map(.created_at = $c)' <<<"$green")"
out=$(BASE_WT="$cg" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci-npm.json")
check "ci-status: green CI with no later workflow change passes and gives the age" 1 "$(grep -c '^pass|.*(the oldest run is [0-9]* days old)' <<<"$out")"
echo '# changed' >>"$cg/.github/workflows/go.yml"
GIT_COMMITTER_DATE="2026-09-10T00:00:00Z" git -C "$cg" -c user.name=t -c user.email=t@t commit -qam "ci: bump setup-go"
out=$(BASE_WT="$cg" run_check ci-status "$ci" PR_META="$tmp/ci-meta.json" CLASSIFICATION="$tmp/cls-ci-npm.json")
check "ci-status: a workflow change on the base branch after the run is incomplete (#719)" "incomplete" "${out%%|*}"
check "ci-status names the commit that changed the workflow" 1 "$(grep -c 'ran before the base branch changed its workflows ([0-9a-f]\{7\})' <<<"$out")"

# Bug 3: a generated check inherited the global git configuration, and an
# `insteadOf` rule with an access token showed up in its log. The tokens are
# built at run time, so this file holds no token-shaped string.
fake_gh="gh""p_$(printf 'Z%.0s' {1..36})"
fake_url_secret="s3cr3t-$(printf 'q%.0s' {1..20})"
rh="$tmp/runner" && mkdir -p "$rh/home" "$rh/wt" "$rh/gen"
printf '[url "https://x-access-token:%s@github.com/"]\n\tinsteadOf = https://github.com/\n[user]\n\tname = Real Person\n' \
  "$fake_url_secret" >"$rh/home/.gitconfig"
cat >"$rh/gen/leaky.sh" <<'SH'
source "$VERIFY_ROOT/lib/check.sh"
echo "home=$HOME"
echo "global-user=$(git config --global user.name)"
echo "url=$(git ls-remote --get-url https://github.com/launchdarkly/ldcli)"
echo "token=$LEAK_TOKEN"
detail "- leaked $LEAK_TOKEN"
pass "printed $LEAK_TOKEN"
SH
out=$(
  export HOME="$rh/home" LEAK_TOKEN="$fake_gh"
  unset GIT_CONFIG_GLOBAL GIT_CONFIG_NOSYSTEM
  source "$LIB/common.sh"
  source "$LIB/runner.sh"
  init_go_cache_env
  VERIFY_SECRETS="$(collect_secrets)" && export -n VERIFY_SECRETS
  OUT="$rh/out" STATE="$rh/out/state" LOGS="$rh/out/logs" PR_WT="$rh/wt" BASE_WT="$rh/wt" PROFILE=fast
  log() { :; }
  run_one generated leaky "$rh/gen/leaky.sh" pr 30
  run_one checks leaky "$rh/gen/leaky.sh" pr 30
  cat "$LOGS/pr/generated-leaky.log"
  echo "---"
  cat "$LOGS/pr/leaky.log" "$STATE/generated/leaky/pr/result.json" "$STATE/checks/leaky/pr/result.json"
)
gen_log=$(sed '/^---$/q' <<<"$out")
check "generated check: HOME is private" 1 "$(grep -c "^home=$rh/out/state/generated/leaky/pr/sandbox/home$" <<<"$gen_log")"
check "generated check: the global git configuration does not apply" 1 "$(grep -c '^global-user=$' <<<"$gen_log")"
check "generated check: no insteadOf rewrite puts a credential into a URL" 1 \
  "$(grep -c '^url=https://github.com/launchdarkly/ldcli$' <<<"$gen_log")"
check "runner: both checks ran, and no token is in any log or result" "2 0" \
  "$(grep -c '^home=' <<<"$out") $(grep -cE "$fake_gh|$fake_url_secret" <<<"$out")"
check "runner: the tokens are replaced with [REDACTED]" 4 "$(grep -c 'printed \[REDACTED\]\|^token=\[REDACTED\]' <<<"$out")"
check "runner: a baseline check log with a credential URL is scrubbed" 1 \
  "$(sed -n '/^---$/,$p' <<<"$out" | grep -c '^url=https://\[REDACTED\]@github.com/launchdarkly/ldcli$')"
check "scrub: an Authorization header and an LD API token are removed" \
  "Authorization: Bearer [REDACTED] key=[REDACTED]" \
  "$(printf 'Authorization: Bearer abc.def key=%s\n' "api-$(printf '0%.0s' {1..8})-1111-2222-3333-444444444444" >"$tmp/scrub.txt" &&
     python3 "$LIB/scrub.py" "$tmp/scrub.txt" && cat "$tmp/scrub.txt")"

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures test(s) failed"
  exit 1
fi
echo "all tests passed"
