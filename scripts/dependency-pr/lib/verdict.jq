# jq module: turns check records, classification, and agent notes into a verdict.
# Use with: jq -L "$VERIFY_ROOT/lib" 'include "verdict"; ...'
#
# Verdicts, strongest first:
#   block          the PR must not merge as it is (breakage, or a fix is required)
#   needs-human    verification is complete, but a person must answer a specific question
#   incomplete     a required check, gate, or review did not run or proved nothing; a rerun or the agent closes it, not a person
#
# A gate (registry "gate": true, or a matched "required_for_tags") must pass.
# "pre-existing" or "incomplete" never satisfies a gate.
#   safe-to-merge  every required check ran and passed
include "semver";

def nonpass: . == "fail" or . == "decide";

# A failure that also happens on base with the same fingerprint is
# pre-existing and does not count against the PR.
def baseline_outcome:
  .pr.status as $p | (.base.status // null) as $b
  | if ($p | IN("pass", "info", "skip", "incomplete", "error")) then $p
    elif $b == null then $p
    elif ($b | IN("pass", "info")) then "regression"
    elif ($b | nonpass) then
      (if (.pr.fingerprint // .pr.summary) == (.base.fingerprint // .base.summary)
       then "pre-existing" else "changed" end)
    else "base-inconclusive"
    end;

# A discriminating check counts ("proven") only when it fails on the old
# version and passes on the new one.
def generated_outcome:
  .pr.status as $p | .base.status as $b
  | def passed: IN("pass", "info");
    if $p == "error" or $b == "error" then "error"
    elif ($p | IN("skip", "incomplete")) or ($b | IN("skip", "incomplete")) then "incomplete"
    elif .kind == "discriminating" then
      (if ($b | passed | not) and ($p | passed) then "proven"
       elif ($b | passed) and ($p | passed) then "not-discriminating"
       elif ($b | passed) then "regression"
       else "fails-both" end)
    else
      (if ($b | passed) and ($p | passed) then "holds"
       elif ($b | passed) then "regression"
       else "invalid" end)
    end;

def tag_required($cls): [ (.required_for_tags // [])[] | select(. as $t | $cls.tags | index($t) != null) ];

def build_result($meta; $cls; $checks; $gen; $impact; $profile; $generated_at):
  ($checks | map(. + {outcome: baseline_outcome})) as $checks
  | ($gen | map(. + {outcome: generated_outcome} | . + {counted: (.outcome == "proven")})) as $gen
  | ($gen | map(select(.counted)) | length) as $proven
  | (if $impact != null and ($impact.tier // null) != null
     then max_tier($cls.tier; $impact.tier) else $cls.tier end) as $tier
  | ($cls.updates | map(select(.direct)) | map(.name)) as $direct

  | [ ( $checks[]
        | select(.pr.status == "fail" and (.outcome | IN("pre-existing") | not))
        | {source: .id, title,
           problem: (.pr.summary
                     + (if .outcome == "changed" then " (base fails too, but this PR changes the result)"
                        elif .outcome == "base-inconclusive" then " (could not compare with base)"
                        else "" end)),
           fix: (.pr.recommendations // []), recipe: .pr.fix} ),
      ( $gen[] | select(.outcome == "regression")
        | {source: "generated:\(.id)", title, problem: .pr.summary,
           fix: (.pr.recommendations // []), recipe: null} ),
      ( ($impact.findings // [])[] | select(.severity == "block")
        | {source: "impact", title: "Impact review", problem: .text, fix: [], recipe: null} )
    ] as $blocks

  | [ ( $checks[]
        | select(.pr.status == "decide" and .outcome != "pre-existing")
        | {source: .id, question: (.pr.question // .title), evidence: ([.pr.summary] + (.pr.recommendations // [])),
           recipe: .pr.fix} ),
      ( if $tier != "low" and ($impact.behavior_changes_reachable // false) and $proven == 0 and (($impact.no_local_proof // "") != "") then
          {source: "impact", question: "Accept the upstream behavior changes that reach ldcli, which no local check can prove?",
           evidence: ([$impact.no_local_proof] + [($impact.changelog // [])[] | "\(.package) \(.range // ""): \(.notes)"]), recipe: null}
        else empty end ),
      ( ($impact.findings // [])[] | select(.severity == "decide" or .severity == "warn")
        | {source: "impact", question: (.question // .text), evidence: ([.text] + (.evidence // []) | unique),
           recipe: null} )
    ] as $decisions

  # A reachable change that no local check can prove is a decision. If a check
  # already asks a question, add the reason to its evidence instead of asking twice.
  | ($decisions | map(select(.source == "impact" and (.question | startswith("Accept the upstream behavior changes"))))) as $nlp
  | ($decisions | map(select((.source == "impact" and (.question | startswith("Accept the upstream behavior changes"))) | not))) as $others
  | (if ($nlp | length) > 0 and ($others | length) > 0
     then ($others | .[0].evidence += $nlp[0].evidence)
     else $decisions end) as $decisions

  | [ ( $checks[] | select(.outcome == "error")
        | {source: .id, reason: "\(.title): the check crashed (\(.pr.summary))"} ),
      ( $checks[] | select(.outcome == "incomplete" and .required)
        | {source: .id, reason: "\(.title): \(.pr.summary)"} ),
      ( $checks[] | select((.outcome == "incomplete" or (.pr.profile_skipped // false)) and (.required | not))
        | tag_required($cls) as $hit | select(($hit | length) > 0)
        | {source: .id,
           reason: "\(.title): \(.pr.summary). This check is required for \($hit | join(", ")) updates."} ),
      ( $checks[] | select(.outcome == "pre-existing" and ((.gate // false) or ((tag_required($cls) | length) > 0)))
        | {source: .id,
           reason: "\(.title) failed the same way on base and on the PR, so this gate does not show that the update works (\(.pr.summary))"} ),
      ( $gen[] | select(.outcome | IN("error", "incomplete", "fails-both", "invalid"))
        | {source: "generated:\(.id)",
           reason: (({"error": "the generated check crashed",
                     "incomplete": "the generated check could not run",
                     "fails-both": "the discriminating check fails on both versions; fix the check or the analysis",
                     "invalid": "the guard fails on base; fix the check"}[.outcome]) + " (\(.title))")} ),
      ( if $tier != "low" then
          ( if $impact == null then
              {source: "impact", reason: "The \($tier)-risk update needs an impact review (agent/impact.json): upstream notes, reach into ldcli, and breaking changes"}
            else
              ( [ $direct[] | select(. as $n | ($impact.changelog // []) | map(.package) | index($n) | not) ] as $missing
                | if ($missing | length) > 0 then
                    {source: "impact", reason: "The impact review does not cover these direct updates: \($missing | join(", "))"}
                  else empty end ),
              ( if ($impact | has("behavior_changes_reachable") | not) then
                  {source: "impact", reason: "The impact review must state behavior_changes_reachable (true or false)"}
                elif $impact.behavior_changes_reachable == true and $proven == 0 and (($impact.no_local_proof // "") == "") then
                  {source: "impact", reason: "The impact review found upstream behavior changes that reach ldcli, but no generated check proved one (it must fail on the old version and pass on the new one)"}
                else empty end )
            end )
        else empty end )
    ] as $incomplete

  | [ $checks[] | select((.outcome == "incomplete" or (.pr.profile_skipped // false)) and (.required | not) and ((tag_required($cls) | length) == 0))
      | {source: .id, reason: "\(.title): \(.pr.summary)"} ] as $not_run

  | (if ($blocks | length) > 0 then "block"
     elif ($decisions | length) > 0 then "needs-human"
     elif ($incomplete | length) > 0 then "incomplete"
     else "safe-to-merge" end) as $verdict
  | {
      schema: 2,
      generated_at: $generated_at,
      profile: $profile,
      pr: $meta,
      classification: $cls,
      tier: $tier,
      verdict: $verdict,
      exit_code: ({"safe-to-merge": 0, "needs-human": 1, "block": 1, "incomplete": 2}[$verdict]),
      blocks: $blocks,
      decisions: $decisions,
      incomplete: $incomplete,
      not_run: $not_run,
      fixes: [ $blocks[], $decisions[] | .recipe | select(. != null) ] | unique_by(.id),
      pre_existing: [ $checks[] | select(.outcome == "pre-existing") | {id, title, summary: .pr.summary} ],
      checks: $checks,
      generated_checks: $gen,
      generated_proven: $proven,
      impact: $impact
    };
