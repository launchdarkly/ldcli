#!/usr/bin/env bash
# Evidence for the impact review: for each direct update, the upstream
# repository, the compare link, and the release notes in the version range.
# The agent reads the notes (state/checks/upstream-changes/pr/notes/) and
# records its conclusions in agent/impact.json. This check never decides.
source "$VERIFY_ROOT/lib/check.sh"

command -v gh >/dev/null 2>&1 || incomplete "gh is not installed, so upstream notes were not collected"
if ! python3 "$VERIFY_ROOT/lib/upstream.py" "$CLASSIFICATION" "$WT" "$ARTIFACTS/notes" >"$ARTIFACTS/report.json" 2>"$ARTIFACTS/err"; then
  cat "$ARTIFACTS/err"
  incomplete "could not collect upstream notes: $(tail -n1 "$ARTIFACTS/err" | cut -c1-160)"
fi
cat "$ARTIFACTS/report.json"

n=$(jq length "$ARTIFACTS/report.json")
[ "$n" -eq 0 ] && skip "no direct update with an upstream source"

jq -r '.[] | "- `\(.name)` \(.from) → \(.to): \(if .repo then "[\(.repo)](https://github.com/\(.repo))" else "source not found" end)\(if .compare_url then ", [\(.commits) commits, \(.files_changed) files](\(.compare_url))" else "" end), notes: \(.notes_source // "none found")\(if (.keywords // {}) != {} then ", mentions: " + ([.keywords | to_entries[] | "\(.key) (\(.value))"] | join(", ")) else "" end)"' \
  "$ARTIFACTS/report.json" >>"$ARTIFACTS/details.md"

with_notes=$(jq '[.[] | select(.notes_file)] | length' "$ARTIFACTS/report.json")
flags=$(jq -r '[.[] | (.keywords // {}) | keys[]] | group_by(.) | map("\(.[0]) (\(length))") | join(", ")' "$ARTIFACTS/report.json")
info "Upstream notes for $with_notes of $n direct update(s)${flags:+; notes mention: $flags}. The impact review must cover them"
