#!/usr/bin/env bash
# Compares vulnerabilities between base and PR. Needs network for the vuln DB.
#
# govulncheck reports each advisory at its most precise level: a function that
# ldcli calls ("reachable"), a package that ldcli imports, or a module in the
# build. Only a new reachable advisory fails. The details list the advisories
# that the PR fixes at each level, and the reachable ones that stay.
source "$VERIFY_ROOT/lib/check.sh"

bin=$(go_tool golang.org/x/vuln/cmd/govulncheck v1.1.4) || incomplete "could not install govulncheck"
scan() {
  (cd "$1" && "$bin" -format json ./...) >"$ARTIFACTS/$2.raw.json" 2>"$ARTIFACTS/$2.err" || return 1
  jq -s '[.[] | select(.finding) | .finding
          | {id: .osv, module: .trace[0].module, fixed: .fixed_version,
             level: (if .trace[0].function then 3 elif .trace[0].package then 2 else 1 end)}]
         | group_by(.id) | map(max_by(.level))' "$ARTIFACTS/$2.raw.json" >"$ARTIFACTS/$2.json"
}
scan "$BASE_WT" base || { cat "$ARTIFACTS/base.err"; incomplete "govulncheck could not run on base; see log"; }
scan "$PR_WT" pr || { cat "$ARTIFACTS/pr.err"; incomplete "govulncheck could not run on the PR; see log"; }
cat "$ARTIFACTS/pr.json"

jq -n --slurpfile b "$ARTIFACTS/base.json" --slurpfile p "$ARTIFACTS/pr.json" '
  def lvl: {"3": "reachable", "2": "imported package", "1": "required module"}[tostring];
  def ids: map(.id);
  ($b[0] | map({(.id): .}) | add // {}) as $bm | ($p[0] | map({(.id): .}) | add // {}) as $pm
  | {
      new_reachable: [$p[0][] | select(.level == 3 and (($bm[.id].level // 0) < 3)) | "\(.id) (\(.module))"],
      new_other: [$p[0][] | select(.level < 3 and ($bm[.id] == null)) | "\(.id) (\(.module), \(.level | lvl))"],
      fixed: [$b[0][] | select($pm[.id] == null) | {id, module, level}],
      still_reachable: [$p[0][] | select(.level == 3) | "\(.id) (\(.module)\(if .fixed then ", fixed in \(.fixed)" else "" end))"]
    }
  | . + {fixed_by_level: (.fixed | group_by(.level) | map({level: (.[0].level | lvl), ids: map(.id)}) | reverse)}' >"$ARTIFACTS/delta.json"

jq -r '.fixed_by_level[] | "- Fixed by this PR (\(.level)): \(.ids | join(", "))"' "$ARTIFACTS/delta.json" >>"$ARTIFACTS/details.md"
jq -r 'if (.still_reachable | length) > 0 then "- Still reachable on the PR: \(.still_reachable | join(", "))" else empty end' "$ARTIFACTS/delta.json" >>"$ARTIFACTS/details.md"
jq -r 'if (.new_other | length) > 0 then "- New, but not reachable: \(.new_other | join(", "))" else empty end' "$ARTIFACTS/delta.json" >>"$ARTIFACTS/details.md"

new=$(jq -r '.new_reachable | join(", ")' "$ARTIFACTS/delta.json")
[ -n "$new" ] && fail "New reachable vulnerabilities: $new"
summary=$(jq -r '
  (.fixed | length) as $n
  | [ "No new reachable vulnerabilities",
      (if $n > 0 then "fixes \($n) advisor\(if $n == 1 then "y" else "ies" end) (" + (.fixed_by_level | map("\(.ids | length) \(.level)") | join(", ")) + ")" else empty end),
      (if (.still_reachable | length) > 0 then "still reachable: " + (.still_reachable | map(split(" ")[0]) | join(", ")) else empty end) ]
  | join("; ")' "$ARTIFACTS/delta.json")
pass "$summary"
