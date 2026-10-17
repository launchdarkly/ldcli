#!/usr/bin/env bash
# Shows what changes beyond the direct update: modules compiled into ldcli
# (Go) or lockfile entries (npm). New npm install scripts need a decision,
# because npm runs them on every install.
source "$VERIFY_ROOT/lib/check.sh"

summaries=()
scripts=()
for eco in gomod npm-ui npm-wrapper; do
  has_ecosystem "$eco" || continue
  if ! python3 "$VERIFY_ROOT/lib/deps.py" transitive "$BASE_WT" "$PR_WT" "$eco" >"$ARTIFACTS/$eco.json" 2>"$ARTIFACTS/$eco.err"; then
    cat "$ARTIFACTS/$eco.err"
    incomplete "could not compare the $eco dependency graph: $(tail -n1 "$ARTIFACTS/$eco.err" | cut -c1-160)"
  fi
  cat "$ARTIFACTS/$eco.json"
  read -r n_add n_rm n_chg n_major < <(jq -r '"\(.added | length) \(.removed | length) \(.changed | length) \([.changed[] | select(.semver == "major")] | length)"' "$ARTIFACTS/$eco.json")
  summaries+=("$eco: $n_chg changed ($n_major major), $n_add added, $n_rm removed")
  {
    printf -- '- %s (%s): %s changed, %s added, %s removed\n' "$eco" "$(jq -r .scope "$ARTIFACTS/$eco.json")" "$n_chg" "$n_add" "$n_rm"
    jq -r '.changed[] | "  - `\(.name)` \(.from) → \(.to) (\(.semver))"' "$ARTIFACTS/$eco.json" | head -n 30
    jq -r '.added[] | "  - added `\(.)`"' "$ARTIFACTS/$eco.json" | head -n 20
    jq -r '.removed[] | "  - removed `\(.)`"' "$ARTIFACTS/$eco.json" | head -n 20
  } >>"$ARTIFACTS/details.md"
  while IFS= read -r s; do [ -n "$s" ] && scripts+=("$s"); done < <(jq -r '.install_scripts[]' "$ARTIFACTS/$eco.json")
done

if [ ${#scripts[@]} -gt 0 ]; then
  fingerprint "${scripts[*]}"
  decide "Allow the new npm install scripts in ${scripts[*]}?" "npm runs these scripts on every install. They are new in this PR: ${scripts[*]}."
fi
if [ ${#summaries[@]} -eq 0 ]; then
  skip "no Go or npm ecosystem in this PR"
fi
info "$(join_by '; ' "${summaries[@]}")"
