#!/usr/bin/env bash
# Usage: render-comment.sh <result.json>  — prints the PR comment as Markdown.
set -euo pipefail

RESULT="${1:?usage: render-comment.sh <result.json>}"
MAX_CHARS=60000 # GitHub caps comments at 65536 characters.

render() {
  local with_details="$1"
  jq -r --argjson with_details "$with_details" '
    def esc: tostring | gsub("\\|"; "\\|") | gsub("\n"; " ");
    def rlabel:
      {"pass": "pass", "fail": "**FAIL**", "warn": "warn", "skip": "skipped", "error": "**ERROR**",
       "regression": "**FAIL** (passes on base)", "changed": "**FAIL** (differs from base)",
       "pre-existing": "pre-existing on base", "base-inconclusive": "**FAIL** (base inconclusive)"}[.] // .;
    def glabel:
      {"proven": "proven (fails on base, passes on PR)", "not-discriminating": "passes on both: does not count",
       "regression": "**REGRESSION** (passes on base, fails on PR)", "fails-both": "**fails on both**",
       "holds": "holds on both", "invalid": "invalid (fails on base): discarded",
       "error": "**ERROR**", "skip": "skipped"}[.] // .;
    def sha7: if . == null then "?" else .[0:7] end;

    . as $r
    | $r.pr as $pr
    | $r.classification as $c
    | ($c.updates | map(select(.direct))) as $direct
    | ($c.updates | map(select(.direct | not))) as $transitive
    | [
        "<!-- ldcli-dependency-verify -->",
        "## Dependency verification: \({"safe-to-merge": "SAFE TO MERGE", "needs-human": "NEEDS HUMAN REVIEW", "block": "BLOCK"}[$r.verdict])",
        "",
        "Risk tier **\($r.tier)**\(if ($c.tags | length) > 0 then " (" + ($c.tags | join(", ")) + ")" else "" end) · ecosystems: \($c.ecosystems | join(", ")) · head `\($pr.head_sha | sha7)` "
          + (if $pr.merge.status == "merged"
             then "tested as merged into `\($pr.base_ref)` @ `\($pr.base_sha | sha7)`"
             else "conflicts with `\($pr.base_ref)`; tested as-is against merge base `\($pr.merge_base | sha7)`" end),
        "",
        (if ($r.reasons | length) > 0 then
          "**Why**", "", ($r.reasons[] | "- \(if .level == "block" then "**block**: " else "" end)\(.text)"), ""
         else "All checks passed and the risk tier allows merging on these results.", "" end),
        (if ($r.recommendations | length) > 0 then
          "**Suggested actions**", "", ($r.recommendations[] | "- \(.)"), ""
         else empty end),

        "### Updates",
        "",
        (if ($direct | length) == 0 and ($transitive | length) == 0 then "No dependency version change detected.", ""
         else
          "| Package | From | To | Change | Ecosystem | Tier |",
          "|---|---|---|---|---|---|",
          ($direct[] | "| `\(.name)` | \(.from // "∅") | \(.to // "∅") | \(.semver)\(if .breaking and .semver != "major" then " (0.x: breaking allowed)" else "" end) | \(.ecosystem)\(if .dev then ", dev" else "" end) | \(.tier) |"),
          (if ($transitive | length) > 0 then
            "",
            "<details><summary>\($transitive | length) transitive update(s)</summary>",
            "",
            ($transitive[0:60][] | "- `\(.name)` \(.from // "∅") → \(.to // "∅") (\(.semver))"),
            (if ($transitive | length) > 60 then "- …" else empty end),
            "</details>"
           else empty end),
          ""
         end),
        (if ($c.go_directive.from != $c.go_directive.to) then "Go directive: `\($c.go_directive.from)` → `\($c.go_directive.to)`", "" else empty end),

        "### Baseline checks",
        "",
        "| Check | Result | Summary |",
        "|---|---|---|",
        ($r.checks[] | select((.pr.profile_skipped // false) | not)
          | "| \(.title | esc) | \(.outcome | rlabel) | \(.pr.summary | esc) |"),
        (if any($r.checks[]; .pr.profile_skipped // false) then
          "", "Not run in the fast profile: \([$r.checks[] | select(.pr.profile_skipped // false) | .id] | join(", "))."
         else empty end),
        "",

        (if ($r.pre_existing | length) > 0 then
          "### Already failing on `\($pr.base_ref)` (not caused by this PR)",
          "",
          ($r.pre_existing[] | "- **\(.title)**: \(.summary)"),
          ""
         else empty end),

        "### Generated checks",
        "",
        (if ($r.generated_checks | length) == 0 then
          "None recorded for this PR.", ""
         else
          "A generated check counts only when it fails on the old version and passes on the new one. \($r.generated_proven) of \($r.generated_checks | length) proven.",
          "",
          "| Check | Kind | Base | PR | Outcome |",
          "|---|---|---|---|---|",
          ($r.generated_checks[] | "| \(.title | esc) | \(.kind) | \(.base.status) | \(.pr.status) | \(.outcome | glabel) |"),
          ""
         end),

        (if $r.impact != null then
          "### Impact review",
          "",
          ($r.impact.summary // empty),
          "",
          (($r.impact.changelog // [])[] | "- **\(.package)** \(.range // ""): \(.notes)\(if .breaking then " **(breaking)**" else "" end)\(if .url then " ([source](\(.url)))" else "" end)"),
          (if (($r.impact.usage // []) | length) > 0 then "- Used in: " + ($r.impact.usage | map("`\(.)`") | join(", ")) else empty end),
          (($r.impact.findings // [])[] | "- \(.severity): \(.text)\(if ((.evidence // []) | length) > 0 then " (" + (.evidence | join(", ")) + ")" else "" end)"),
          ""
         else empty end),

        (if $with_details then
          ([$r.checks[], ($r.generated_checks[] | . + {id: ("generated: " + .id)})]
           | map(select(.pr.details != null or (.base.details // null) != null))) as $d
          | if ($d | length) > 0 then
              "<details><summary>Check details</summary>",
              "",
              ($d[] | "#### \(.id)", "", (.pr.details // .base.details), ""),
              "</details>",
              ""
            else empty end
         else
          "_Check details omitted to fit GitHub'\''s comment size limit; see the run logs._", ""
         end),

        "<sub>Generated by `scripts/dependency-pr/verify.sh` (profile \($r.profile)) at \($r.generated_at). This comment is informational: the verifier never approves or merges.</sub>"
      ]
    | .[]' "$RESULT"
}

out=$(render true)
if [ "${#out}" -gt "$MAX_CHARS" ]; then
  out=$(render false)
fi
printf '%s\n' "$out"
