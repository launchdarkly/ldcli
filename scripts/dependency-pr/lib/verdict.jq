# jq module: turns check records, classification, and agent notes into a verdict.
# Use with: jq -L "$VERIFY_ROOT/lib" 'include "verdict"; ...'
include "semver";

def nonpass: . == "fail" or . == "warn";

# Baseline check outcome. A failure that also fails on base with the same
# fingerprint is pre-existing and does not count against the PR.
def baseline_outcome:
  .pr.status as $p | (.base.status // null) as $b
  | if $p == "pass" then "pass"
    elif $p == "skip" then "skip"
    elif $p == "error" then "error"
    elif $b == null then $p
    elif $b == "pass" then "regression"
    elif ($b | nonpass) then
      (if (.pr.fingerprint // .pr.summary) == (.base.fingerprint // .base.summary)
       then "pre-existing" else "changed" end)
    else "base-inconclusive"
    end;

# Generated check outcome. A discriminating check counts ("proven") only when
# it fails on the old version and passes on the new one.
def generated_outcome:
  .pr.status as $p | .base.status as $b
  | if $p == "error" or $b == "error" then "error"
    elif $p == "skip" or $b == "skip" then "skip"
    elif .kind == "discriminating" then
      (if ($b | nonpass) and $p == "pass" then "proven"
       elif $b == "pass" and $p == "pass" then "not-discriminating"
       elif $b == "pass" then "regression"
       else "fails-both" end)
    else
      (if $b == "pass" and $p == "pass" then "holds"
       elif $b == "pass" then "regression"
       else "invalid" end)
    end;

def level_for($sev): if $sev == "block" then "block" else "needs-human" end;

def build_result($meta; $cls; $checks; $gen; $impact; $profile; $generated_at):
  ($checks | map(. + {outcome: baseline_outcome})) as $checks
  | ($gen | map(. + {outcome: generated_outcome} | . + {counted: (.outcome == "proven")})) as $gen
  | ($gen | map(select(.counted)) | length) as $proven
  | (if $impact != null and ($impact.tier // null) != null
     then max_tier($cls.tier; $impact.tier) else $cls.tier end) as $tier
  | [
      ( $checks[]
        | select(.outcome | IN("fail", "warn", "regression", "changed", "base-inconclusive"))
        | {level: (if .pr.status == "fail" then level_for(.pr.severity // .on_fail) else "needs-human" end),
           source: .id,
           text: ("\(.title): \(.pr.summary)"
                  + (if .outcome == "changed" then " (also fails on base, but this PR changes the result)"
                     elif .outcome == "base-inconclusive" then " (could not compare with base)"
                     else "" end))} ),
      ( $checks[] | select(.outcome == "error")
        | {level: "needs-human", source: .id, text: "Verifier error in \(.id): \(.pr.summary)"} ),
      ( $checks[] | select(.outcome == "skip" and .required and ((.pr.profile_skipped // false) | not))
        | {level: "needs-human", source: .id, text: "Required check not run (\(.id)): \(.pr.summary)"} ),
      ( $checks[] | select(.outcome == "skip" and (.required | not))
        | [ (.required_for_tags // [])[] | select(. as $t | $cls.tags | index($t) != null) ] as $hit
        | select(($hit | length) > 0)
        | {level: "needs-human", source: .id,
           text: "\(.title) not run (\(.pr.summary)); it is required for \($hit | join(", ")) updates"} ),
      ( $gen[] | select(.outcome == "regression")
        | {level: level_for(.severity // "attention"), source: "generated:\(.id)",
           text: "Generated check regressed: \(.title): \(.pr.summary)"} ),
      ( $gen[] | select(.outcome == "fails-both")
        | {level: "needs-human", source: "generated:\(.id)",
           text: "Generated check fails on both versions: \(.title): \(.pr.summary)"} ),
      ( $gen[] | select(.outcome == "error")
        | {level: "needs-human", source: "generated:\(.id)", text: "Generated check errored: \(.id)"} ),
      ( if $tier == "high" then
          {level: "needs-human", source: "risk",
           text: "High risk tier: \(($cls.tier_reasons + (($impact.tier_reasons) // [])) | join("; "))"}
        else empty end ),
      ( if $tier == "medium" and $impact == null then
          {level: "needs-human", source: "risk", text: "Medium risk tier and no agent impact review recorded (agent/impact.json)"}
        else empty end ),
      ( if $tier == "medium" and $proven == 0 then
          {level: "needs-human", source: "risk",
           text: "Medium risk tier and no generated check has proven itself (it must fail on the old version and pass on the new one)"}
        else empty end ),
      ( ($impact.findings // [])[] | select(.severity == "block" or .severity == "warn")
        | {level: (if .severity == "block" then "block" else "needs-human" end), source: "impact", text: .text} )
    ] as $reasons
  | (if any($reasons[]; .level == "block") then "block"
     elif ($reasons | length) > 0 then "needs-human"
     else "safe-to-merge" end) as $verdict
  | ([$checks[], $gen[]] | any(.outcome == "error")) as $infra
  | {
      schema: 1,
      generated_at: $generated_at,
      profile: $profile,
      pr: $meta,
      classification: $cls,
      tier: $tier,
      verdict: $verdict,
      exit_code: (if $infra then 2 elif $verdict == "safe-to-merge" then 0 else 1 end),
      reasons: $reasons,
      recommendations: (
        [ ($checks[] | select(.outcome | IN("pass", "pre-existing", "skip") | not) | (.pr.recommendations // [])[]),
          ($gen[] | select(.outcome == "regression") | (.pr.recommendations // [])[]),
          (($impact.recommendations // [])[]) ] | unique),
      pre_existing: [ $checks[] | select(.outcome == "pre-existing") | {id, title, summary: .pr.summary} ],
      checks: $checks,
      generated_checks: $gen,
      generated_proven: $proven,
      impact: $impact
    };
