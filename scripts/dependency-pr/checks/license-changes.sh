#!/usr/bin/env bash
# Licenses of the dependencies that this PR adds or changes. A license change,
# or a license outside license-policy.json, needs a decision from a person.
source "$VERIFY_ROOT/lib/check.sh"

problems=()
checked=0
for eco in gomod npm-ui npm-wrapper; do
  has_ecosystem "$eco" || continue
  if ! python3 "$VERIFY_ROOT/lib/deps.py" licenses "$BASE_WT" "$PR_WT" "$eco" "$VERIFY_ROOT/license-policy.json" >"$ARTIFACTS/$eco.json" 2>"$ARTIFACTS/$eco.err"; then
    cat "$ARTIFACTS/$eco.err"
    incomplete "could not read the $eco licenses: $(tail -n1 "$ARTIFACTS/$eco.err" | cut -c1-160)"
  fi
  cat "$ARTIFACTS/$eco.json"
  checked=$((checked + $(jq -r .checked "$ARTIFACTS/$eco.json")))
  jq -r '.all[] | "- `\(.package)`: \(if .changed then "\(.from) → " else "" end)\(.to)\(if .allowed then "" else " (not in the license policy)" end)"' "$ARTIFACTS/$eco.json" | head -n 40 >>"$ARTIFACTS/details.md"
  while IFS= read -r p; do [ -n "$p" ] && problems+=("$p"); done < <(jq -r '.problems[] | "\(.package): \(if .changed then "license changed from \(.from) to \(.to)" else "license \(.to) is not in the license policy" end)\(if .file then " (\(.file))" else "" end)"' "$ARTIFACTS/$eco.json")
done

if [ ${#problems[@]} -gt 0 ]; then
  fingerprint "${problems[*]}"
  decide "Accept these licenses: $(join_by '; ' "${problems[@]}")?" "The license policy (scripts/dependency-pr/license-policy.json) allows $(jq -r '.allowed | join(", ")' "$VERIFY_ROOT/license-policy.json")."
fi
[ "$checked" -eq 0 ] && pass "No added or changed dependency to check"
pass "All $checked added or changed dependencies use a license in the policy, with no license change"
