#!/usr/bin/env bash
# Usage: classify.sh <base-wt> <pr-wt> <changed-files.txt> <pr.json> <out.json>
#
# Compares the base and PR worktrees (not the PR diff) so updates reflect what
# would actually change on the base branch after merging.
set -euo pipefail
source "$(dirname "$0")/common.sh"

BASE_WT="$1" PR_WT="$2" CHANGED="$3" PR_JSON="$4" OUT="$5"
UI_DIR="internal/dev_server/ui"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

go_mod_json() {
  if [ -f "$1/go.mod" ]; then (cd "$1" && go mod edit -json); else echo '{}'; fi
}

npm_json() {
  # npm_json <wt> <dir>: {deps, dev, lock} for one package directory
  local d="$1/$2" pkg lock
  pkg="$d/package.json"
  lock="$d/package-lock.json"
  [ -f "$pkg" ] || pkg=/dev/null
  [ -f "$lock" ] || lock=/dev/null
  jq -n --slurpfile p <(cat "$pkg" 2>/dev/null || echo '{}') --slurpfile l <(cat "$lock" 2>/dev/null || echo '{}') '
    ($p[0] // {}) as $p | ($l[0] // {}) as $l
    | {
        deps: ($p.dependencies // {}),
        dev: ($p.devDependencies // {}),
        lock: (($l.packages // {}) | to_entries
               | map(select(.key | test("^node_modules/(@[^/]+/)?[^/]+$")))
               | map({key: (.key | sub("^node_modules/"; "")), value: .value.version})
               | from_entries)
      }'
}

actions_uses() {
  # Prints "name<TAB>ref<TAB>comment-version" for each non-local `uses:`.
  local dirs=()
  [ -d "$1/.github/workflows" ] && dirs+=("$1/.github/workflows")
  [ -d "$1/.github/actions" ] && dirs+=("$1/.github/actions")
  [ ${#dirs[@]} -gt 0 ] || return 0
  rg --no-filename --no-line-number -o -g '*.yml' -g '*.yaml' \
    '^\s*-?\s*uses:\s*["'\'']?([^\s"'\''#]+)["'\'']?(?:\s*#\s*(\S+))?' -r '$1	$2' "${dirs[@]}" |
    awk -F'\t' '$1 !~ /^\.\// && $1 !~ /^docker:\/\// && index($1, "@") > 0 {
      at = index($1, "@"); printf "%s\t%s\t%s\n", substr($1, 1, at - 1), substr($1, at + 1), $2 }' |
    sort -u
}

actions_json() {
  actions_uses "$1" | jq -R -s '
    split("\n") | map(select(length > 0) | split("\t"))
    | map({name: .[0], ref: .[1],
           version: (if (.[1] | test("^[0-9a-f]{40}$")) and ((.[2] // "") != "") then .[2] else .[1] end)})
    | group_by(.name) | map({key: .[0].name, value: {refs: (map(.ref) | unique), versions: (map(.version) | unique),
                                                      pairs: (map({ref, version}) | unique)}})
    | from_entries'
}

docker_json() {
  local f out='{}'
  for f in "$1"/Dockerfile*; do
    [ -f "$f" ] || continue
    out=$(rg --no-line-number -o '^FROM\s+(\S+)' -r '$1' "$f" | jq -R -s --argjson acc "$out" '
      split("\n") | map(select(length > 0))
      | map(sub("@sha256:.*$"; "") | capture("^(?<name>[^:]+)(:(?<tag>.+))?$") | {key: .name, value: (.tag // "latest")})
      | from_entries | $acc + .')
  done
  printf '%s\n' "$out"
}

go_mod_json "$BASE_WT" >"$TMP/go.base.json"
go_mod_json "$PR_WT" >"$TMP/go.pr.json"
npm_json "$BASE_WT" "$UI_DIR" >"$TMP/ui.base.json"
npm_json "$PR_WT" "$UI_DIR" >"$TMP/ui.pr.json"
npm_json "$BASE_WT" "." >"$TMP/root.base.json"
npm_json "$PR_WT" "." >"$TMP/root.pr.json"
actions_json "$BASE_WT" >"$TMP/actions.base.json"
actions_json "$PR_WT" >"$TMP/actions.pr.json"
docker_json "$BASE_WT" >"$TMP/docker.base.json"
docker_json "$PR_WT" >"$TMP/docker.pr.json"

jq -n -L "$VERIFY_ROOT/lib" \
  --rawfile changed "$CHANGED" \
  --slurpfile pr "$PR_JSON" \
  --slurpfile risk "$VERIFY_ROOT/risk-map.json" \
  --slurpfile gob "$TMP/go.base.json" --slurpfile gop "$TMP/go.pr.json" \
  --slurpfile uib "$TMP/ui.base.json" --slurpfile uip "$TMP/ui.pr.json" \
  --slurpfile rootb "$TMP/root.base.json" --slurpfile rootp "$TMP/root.pr.json" \
  --slurpfile actb "$TMP/actions.base.json" --slurpfile actp "$TMP/actions.pr.json" \
  --slurpfile dockb "$TMP/docker.base.json" --slurpfile dockp "$TMP/docker.pr.json" '
include "semver";

def vsort: sort_by(vparse | if . == null then [-1] else .nums end);
def maxv: vsort | last;
def vsort_pairs: sort_by(.version | vparse | if . == null then [-1] else .nums end);

def go_updates:
  def reqmap: (.Require // []) | map({key: .Path, value: {v: .Version, indirect: (.Indirect // false)}}) | from_entries;
  ($gob[0] | reqmap) as $b | ($gop[0] | reqmap) as $p
  | [ (($b | keys) + ($p | keys) | unique)[] as $k
      | select($b[$k].v != $p[$k].v)
      | {ecosystem: "gomod", name: $k, from: $b[$k].v, to: $p[$k].v,
         direct: ((($p[$k] // $b[$k]).indirect) | not), dev: false} ];

def npm_updates($eco; $b; $p):
  [ (($b.lock | keys) + ($p.lock | keys) | unique)[] as $k
    | select($b.lock[$k] != $p.lock[$k])
    | ([$b.deps, $p.deps] | any(has($k))) as $rt
    | ([$b.dev, $p.dev] | any(has($k))) as $dv
    | {ecosystem: $eco, name: $k, from: $b.lock[$k], to: $p.lock[$k],
       direct: ($rt or $dv), dev: ($dv and ($rt | not))} ];

# A workflow can pin one action at several refs (v4 in one file, v5 in
# another). "replaced" holds every old ref that the PR removes, lowest version
# first, and "from" is the lowest of them, so the compared range covers all.
def action_updates:
  $actb[0] as $b | $actp[0] as $p
  | [ (($b | keys) + ($p | keys) | unique)[] as $k
      | select($b[$k].refs != $p[$k].refs)
      | ($b[$k].pairs // []) as $bp | ($p[$k].pairs // []) as $pp
      | ($pp | map(.ref)) as $prefs
      | ($bp | map(select(.ref as $r | $prefs | index($r) | not)) | vsort_pairs) as $replaced
      | (if ($replaced | length) > 0 then $replaced[0] else ($bp | vsort_pairs | last) end) as $old
      | ($pp | vsort_pairs | last) as $new
      | {ecosystem: "github-actions", name: $k,
         from: ($old.version // null), to: ($new.version // null),
         from_ref: ($old.ref // null), to_ref: ($new.ref // null),
         replaced: $replaced,
         from_all: ($b[$k].versions // []), to_all: ($p[$k].versions // []),
         from_refs: ($b[$k].refs // []), to_refs: ($p[$k].refs // []),
         direct: true, dev: false} ];

def docker_updates:
  $dockb[0] as $b | $dockp[0] as $p
  | [ (($b | keys) + ($p | keys) | unique)[] as $k
      | select($b[$k] != $p[$k])
      | {ecosystem: "docker", name: $k, from: $b[$k], to: $p[$k], direct: true, dev: false} ];

def ecosystems_from($files):
  [ $files[]
    | if test("^go\\.(mod|sum)$") then "gomod"
      elif test("^internal/dev_server/ui/package(-lock)?\\.json$") then "npm-ui"
      elif test("^package(-lock)?\\.json$") then "npm-wrapper"
      elif test("^\\.github/(workflows|actions)/") then "github-actions"
      elif test("(^|/)Dockerfile[^/]*$") then "docker"
      else empty end ] | unique;

def manifest_file: test("^go\\.(mod|sum)$|(^|/)package(-lock)?\\.json$|^\\.github/(workflows|actions)/|(^|/)Dockerfile[^/]*$");

($changed | split("\n") | map(select(length > 0))) as $files
| ($pr[0] // {}) as $pr
| (go_updates + npm_updates("npm-ui"; $uib[0]; $uip[0]) + npm_updates("npm-wrapper"; $rootb[0]; $rootp[0])
   + action_updates + docker_updates) as $raw
| $risk[0].rules as $rules
| [ $raw[]
    | semver_class(.from; .to) as $c
    | (if $c == "none" and .ecosystem == "github-actions" then "digest" else $c end) as $c
    | . + {semver: $c, breaking: is_breaking(.from; .to)}
    | . as $u
    | [ $rules[] | . as $rule | select($u.name | test($rule.match)) ] as $hits
    | ( if $c == "downgrade" then "medium"
        elif .breaking and .direct then "high"
        elif $c == "minor" and .direct and (.dev | not) then "medium"
        else "low" end ) as $base
    | ($hits | map(.tier) | reduce .[] as $t ("low"; max_tier(.; $t))) as $rt
    | (if .direct then $rt else max_tier("low"; (if $rt == "high" then "medium" else $rt end)) end) as $rt
    | . + {tier: max_tier($base; $rt),
           tags: ($hits | map(.tags[]) | unique),
           risk_notes: ($hits | map(.why) | unique)} ] as $updates
| ($updates | map(select(.direct)) | length) as $ndirect
| {from: ($gob[0].Go // null), to: ($gop[0].Go // null)} as $godir
| {from: ($gob[0].Toolchain.Name // null), to: ($gop[0].Toolchain.Name // null)} as $tool
| (ecosystems_from($files) + ($updates | map(.ecosystem)) | unique) as $ecos
| ( [ ($updates[] | select(.tier != "low") | "\(.name) \(.from // "∅") → \(.to // "∅"): \(.tier) (\(.semver)\(if .breaking then ", breaking range" else "" end)\(if (.tags | length) > 0 then "; " + (.tags | join(", ")) else "" end))"),
      (if $godir.from != $godir.to then "go directive \($godir.from) → \($godir.to): high" else empty end),
      (if $tool.from != $tool.to then "toolchain \($tool.from) → \($tool.to): high" else empty end),
      (if ($ecos | index("docker")) then "docker base image: at least medium" else empty end),
      (if $ndirect > 3 then "\($ndirect) direct updates in one PR: high" else empty end),
      (if ($updates | length) == 0 then "no dependency change detected between base and PR: medium" else empty end)
    ] ) as $reasons
| ( ($updates | map(.tier)) + [
      (if $godir.from != $godir.to or $tool.from != $tool.to then "high" else "low" end),
      (if ($ecos | index("docker")) then "medium" else "low" end),
      (if $ndirect > 3 then "high" else "low" end),
      (if ($updates | length) == 0 then "medium" else "low" end)
    ] | reduce .[] as $t ("low"; max_tier(.; $t)) ) as $tier
| {
    schema: 1,
    ecosystems: $ecos,
    changed_files: $files,
    other_files: ($files | map(select(manifest_file | not))),
    updates: $updates,
    direct_update_count: $ndirect,
    go_directive: $godir,
    toolchain: $tool,
    group: ($ndirect > 1 or (($pr.title // "") | test("group"; "i"))),
    security: ((($pr.title // "") + " " + ($pr.body // "")) | test("GHSA-|CVE-[0-9]{4}-|\\[security\\]"; "i")),
    tier: $tier,
    tags: (($updates | map(.tags[])) + (if $godir.from != $godir.to or $tool.from != $tool.to then ["go-directive"] else [] end) | unique),
    tier_reasons: $reasons
  }' >"$OUT"
