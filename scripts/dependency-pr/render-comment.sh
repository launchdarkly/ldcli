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
      {"pass": "pass", "info": "pass (note)", "fail": "**FAIL**", "decide": "decision", "skip": "does not apply",
       "incomplete": "**not run**", "error": "**ERROR**",
       "regression": "**FAIL** (passes on base)", "changed": "**FAIL** (differs from base)",
       "pre-existing": "fails on base too", "base-inconclusive": "**FAIL** (base not compared)"}[.] // .;
    def glabel:
      {"proven": "proven (fails on base, passes on PR)", "not-discriminating": "passes on both: does not count",
       "regression": "**REGRESSION** (passes on base, fails on PR)", "fails-both": "**fails on both**",
       "holds": "holds on both", "invalid": "**invalid** (fails on base)",
       "error": "**ERROR**", "incomplete": "**not run**"}[.] // .;
    def sha7: if . == null then "?" else .[0:7] end;

    . as $r
    | $r.pr as $pr
    | $r.classification as $c
    | ($c.updates | map(select(.direct))) as $direct
    | ($c.updates | map(select(.direct | not))) as $transitive
    | [
        "<!-- ldcli-dependency-verify -->",
        "## Dependency verification: \({"safe-to-merge": "SAFE TO MERGE", "needs-human": "NEEDS A HUMAN DECISION", "block": "BLOCK", "incomplete": "INCOMPLETE"}[$r.verdict])",
        "",
        "Risk tier **\($r.tier)**\(if ($c.tags | length) > 0 then " (" + ($c.tags | join(", ")) + ")" else "" end) · ecosystems: \($c.ecosystems | join(", ")) · head `\($pr.head_sha | sha7)` "
          + (if $pr.merge.status == "merged"
             then "tested as merged into `\($pr.base_ref)` @ `\($pr.base_sha | sha7)`"
             else "conflicts with `\($pr.base_ref)`, so it was tested as it is against merge base `\($pr.merge_base | sha7)`" end),
        "",
        (if $r.verdict == "safe-to-merge" then
          "Every required check ran and passed. No decision is necessary.", ""
         else empty end),

        (if ($r.blocks | length) > 0 then
          "### Must fix before merge", "",
          ($r.blocks[]
           | "- **\(.title)**: \(.problem)",
             (.fix[] | "  - Fix: \(.)"),
             (if .recipe != null and .recipe.kind == "commit" then
                "  - Mechanical fix: `\(.recipe.command)` (changes \(.recipe.paths | map("`\(.)`") | join(", ")))"
              else empty end)),
          ""
         else empty end),

        (if ($r.decisions | length) > 0 then
          "### Decisions for a human", "",
          "Verification is complete for these items. Each one needs a choice, not more checks.", "",
          ($r.decisions | to_entries[]
           | "\(.key + 1). **\(.value.question)**",
             (.value.evidence[] | "   - \(.)")),
          ""
         else empty end),

        (if ($r.incomplete | length) > 0 then
          "### Incomplete verification", "",
          "These items did not run. Run `verify.sh` again where the missing tool is available, or finish the agent review. A person does not have to do these checks.", "",
          ($r.incomplete[] | "- \(.reason)"),
          ""
         else empty end),

        "### Updates",
        "",
        (if ($direct | length) == 0 and ($transitive | length) == 0 then "No dependency version change was found.", ""
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

        "### What the verifier checked",
        "",
        "| Check | Result | Summary |",
        "|---|---|---|",
        ($r.checks[] | select((.pr.profile_skipped // false) | not) | select(.outcome != "skip")
          | "| \(.title | esc) | \(.outcome | rlabel) | \(.pr.summary | esc) |"),
        (if ($r.not_run | length) > 0 then
          "", "Optional checks that did not run: \($r.not_run | map(.source) | join(", "))."
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
          "None for this PR.", ""
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
          (if ($r.impact | has("behavior_changes_reachable")) then
             "- Upstream behavior changes that reach ldcli: \(if $r.impact.behavior_changes_reachable then "yes" else "no" end)"
           else empty end),
          (($r.impact.findings // [])[] | select(.severity == "info") | "- Note: \(.text)\(if ((.evidence // []) | length) > 0 then " (" + (.evidence | join(", ")) + ")" else "" end)"),
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
          "_The check details are not shown because of the GitHub comment size limit. See the run logs._", ""
         end),

        "<sub>Generated by `scripts/dependency-pr/verify.sh` (profile \($r.profile)) at \($r.generated_at). The verifier does not approve or merge.</sub>"
      ]
    | .[]' "$RESULT"
}

out=$(render true)
if [ "${#out}" -gt "$MAX_CHARS" ]; then
  out=$(render false)
fi
printf '%s\n' "$out"
