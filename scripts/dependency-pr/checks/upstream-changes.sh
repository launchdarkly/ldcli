#!/usr/bin/env bash
# Evidence for the impact review: for each direct update, the upstream
# repository, the compare link, and the release notes in the version range.
# The agent reads the notes (state/checks/upstream-changes/pr/notes/) and
# records its conclusions in agent/impact.json. This check never decides.
#
# The summary gives the real note coverage. Notes that stop before the old
# version are "partial", so the reviewer reads the compare diff for the rest.
source "$VERIFY_ROOT/lib/check.sh"

command -v gh >/dev/null 2>&1 || incomplete "gh is not installed, so upstream notes were not collected"
if ! python3 "$VERIFY_ROOT/lib/upstream.py" "$CLASSIFICATION" "$WT" "$ARTIFACTS/notes" >"$ARTIFACTS/report.json" 2>"$ARTIFACTS/err"; then
  cat "$ARTIFACTS/err"
  incomplete "could not collect upstream notes: $(tail -n1 "$ARTIFACTS/err" | cut -c1-160)"
fi
cat "$ARTIFACTS/report.json"

n=$(jq length "$ARTIFACTS/report.json")
[ "$n" -eq 0 ] && skip "no direct update with an upstream source"

jq -r '.[] | "- `\(.name)` \(.from) → \(.to): \(if .repo then "[\(.repo)](https://github.com/\(.repo))" else "source not found" end)\(if .compare_url then ", [\(.commits) commits, \(.files_changed) files](\(.compare_url))" else "" end)\(if .tags_missing then ", no upstream tag for " + (.tags_missing | join(" and ")) + ", so no compare link" else "" end), notes: \(
    if .notes_coverage == "full" then "\(.notes_source), full range"
    elif .notes_coverage == "partial" then "\(.notes_source), **partial** (back to \(.notes_lowest // "?") only, not \(.from))"
    else "**none found**" end)\(if (.keywords // {}) != {} then ", mentions: " + ([.keywords | to_entries[] | "\(.key) (\(.value))"] | join(", ")) else "" end)"' \
  "$ARTIFACTS/report.json" >>"$ARTIFACTS/details.md"

coverage=$(jq -r '
  (map(select(.notes_coverage == "full")) | length) as $full
  | [ "Upstream notes cover the full range for \($full) of \(length) direct update(s)",
      (map(select(.notes_coverage == "partial")) | if length > 0 then "partial for " + (map("\(.name) (back to \(.notes_lowest // "?"), not \(.from))") | join(", ")) else empty end),
      (map(select(.notes_coverage == "none")) | if length > 0 then "none for " + (map(.name) | join(", ")) else empty end) ]
  | join("; ")' "$ARTIFACTS/report.json")
flags=$(jq -r '[.[] | (.keywords // {}) | keys[]] | group_by(.) | map("\(.[0]) (\(length))") | join(", ")' "$ARTIFACTS/report.json")
gaps=$(jq '[.[] | select(.notes_coverage != "full")] | length' "$ARTIFACTS/report.json")
tail=""
if [ "$(jq -r '.tier' "$CLASSIFICATION")" != low ]; then
  tail=". The impact review must cover them"
  [ "$gaps" -gt 0 ] && tail="$tail, and read the compare diff where the notes are partial or missing"
fi
info "$coverage${flags:+; notes mention: $flags}$tail"
