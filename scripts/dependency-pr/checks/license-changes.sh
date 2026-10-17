#!/usr/bin/env bash
# Licenses of the dependencies that this PR adds or changes. A license change,
# or a license outside license-policy.json, needs a decision from a person.
source "$VERIFY_ROOT/lib/check.sh"

checked=0
: >"$ARTIFACTS/problems.jsonl"
for eco in gomod npm-ui npm-wrapper; do
  has_ecosystem "$eco" || continue
  if ! python3 "$VERIFY_ROOT/lib/deps.py" licenses "$BASE_WT" "$PR_WT" "$eco" "$VERIFY_ROOT/license-policy.json" >"$ARTIFACTS/$eco.json" 2>"$ARTIFACTS/$eco.err"; then
    cat "$ARTIFACTS/$eco.err"
    incomplete "could not read the $eco licenses: $(tail -n1 "$ARTIFACTS/$eco.err" | cut -c1-160)"
  fi
  cat "$ARTIFACTS/$eco.json"
  checked=$((checked + $(jq -r .checked "$ARTIFACTS/$eco.json")))
  jq -r '.all[] | "- `\(.package)`: \(if .changed then "\(.from) → " else "" end)\(.to)\(if .allowed then "" else " (not in the license policy)" end)\(if .dev then " (dev only)" else "" end)"' "$ARTIFACTS/$eco.json" | head -n 40 >>"$ARTIFACTS/details.md"
  jq -c '.problems[]' "$ARTIFACTS/$eco.json" >>"$ARTIFACTS/problems.jsonl"
done

if [ -s "$ARTIFACTS/problems.jsonl" ]; then
  # One item per package family: lightningcss and its 11 lightningcss-<platform>
  # binaries in #779 are one decision, not 12.
  jq -s -r '
    def lic_label: if .changed then "the license change from \(.from) to \(.to)" else .to end;
    sort_by(.name | length)
    | reduce .[] as $p ([];
        ([to_entries[] | .value.root.name as $rn
          | select(.value.root.version == $p.version and (.value.root | lic_label) == ($p | lic_label)
                   and ($p.name | startswith($rn + "-"))) | .key] | first) as $i
        | if $i == null then . + [{root: $p, members: [$p]}] else .[$i].members += [$p] end)
    | map((.members | length) as $n | (.members | all(.dev)) as $dev
          | "\(.root | lic_label) for \(.root.name) \(.root.version)"
            + (if $n > 1 then " and \($n - 1) \(.root.name)-* package(s)" else "" end)
            + (if $dev then " (dev-only build tools)" elif $n == 1 and (.root.file // null) then " (\(.root.file))" else "" end))
    | join("; ")' "$ARTIFACTS/problems.jsonl" >"$ARTIFACTS/question.txt"
  jq -s -r '.[] | .package' "$ARTIFACTS/problems.jsonl" | sort -u >"$ARTIFACTS/problem-packages"
  findings_file "$ARTIFACTS/problem-packages"
  dev_note=""
  if jq -s -e 'all(.[]; .dev)' "$ARTIFACTS/problems.jsonl" >/dev/null; then
    dev_note=" Only devDependencies use these packages, so they do not ship in the UI bundle."
  fi
  decide "Accept $(cat "$ARTIFACTS/question.txt")?" \
    "Packages: $(paste -sd' ' "$ARTIFACTS/problem-packages").$dev_note The license policy (scripts/dependency-pr/license-policy.json) allows $(jq -r '.allowed | join(", ")' "$VERIFY_ROOT/license-policy.json")."
fi
[ "$checked" -eq 0 ] && pass "No added or changed dependency to check"
pass "All $checked added or changed dependencies use a license in the policy, with no license change"
