#!/usr/bin/env bash
# Scripts and CI jobs call ldcli with fixed commands and flags. A removed
# command or flag, or a changed default, can break them without a failing test.
# Added commands and flags are listed for the reader.
source "$VERIFY_ROOT/lib/check.sh"

# help_dump covers two levels of commands. New command groups (for example
# "sync prompts <command>") go deeper, so add levels 3 and 4.
_deeper_of() {
  local bin="$1" prefix="$2" words cand
  read -ra words <<<"$prefix"
  "$bin" __complete "${words[@]}" "" 2>/dev/null |
    awk -F'\t' '!/^:/ && $1 != "" && $1 !~ /^-/ && $1 != "help" {print $1}' |
    while IFS= read -r cand; do
      # A leaf command can complete argument values. Keep only real subcommands.
      "$bin" "${words[@]}" "$cand" --help 2>/dev/null | grep -q "ldcli $prefix $cand" && printf '%s %s\n' "$prefix" "$cand"
    done
}
export -f _deeper_of
deep_help() {
  local bin="$1" dir="$2" level c
  for level in 3 4; do
    awk -v n=$((level - 1)) 'NF == n' "$dir/.commands" |
      xargs -P "$JOBS" -I{} bash -c '_deeper_of "$0" "$1"' "$bin" {} | sort -u >"$dir/.level$level"
    [ -s "$dir/.level$level" ] || break
    while IFS= read -r c; do
      { printf '### ldcli %s\n' "$c"; read -ra w <<<"$c"; "$bin" "${w[@]}" --help 2>&1; } >>"$dir/all.txt"
    done <"$dir/.level$level"
    sort -u -o "$dir/.commands" "$dir/.commands" "$dir/.level$level"
  done
}

for side in base pr; do
  wt="$BASE_WT"
  [ "$side" = pr ] && wt="$PR_WT"
  if ! (cd "$wt" && run go build -o "$ARTIFACTS/ldcli-$side" .); then
    incomplete "could not build the $side binary"
  fi
  help_dump "$ARTIFACTS/ldcli-$side" "$ARTIFACTS/help-$side"
  deep_help "$ARTIFACTS/ldcli-$side" "$ARTIFACTS/help-$side"
done
diff -u "$ARTIFACTS/help-base/all.txt" "$ARTIFACTS/help-pr/all.txt" >"$ARTIFACTS/help.diff"
python3 "$RELEASE_ROOT/lib/cli_surface.py" "$ARTIFACTS/help-base/all.txt" "$ARTIFACTS/help-pr/all.txt" >"$ARTIFACTS/surface.json" ||
  incomplete "could not compare the help output"

list() { jq -r --arg k "$1" '.[$k][]' "$ARTIFACTS/surface.json"; }
section() {
  local title="$1" key="$2" n
  n=$(jq --arg k "$key" '.[$k] | length' "$ARTIFACTS/surface.json")
  [ "$n" -gt 0 ] || return 0
  detail "- $title ($n):"
  list "$key" | head -n 40 | sed 's/^/  - `/; s/$/`/' >>"$ARTIFACTS/details.md"
  [ "$n" -gt 40 ] && detail "  - … $((n - 40)) more (see surface.json)"
}
section "Commands added" commands_added
section "Flags added" flags_added
section "Commands removed" commands_removed
section "Flags removed" flags_removed
section "Defaults changed" defaults_changed
section "Flag types changed" types_changed
detail "- Commands: $(jq -r '"\(.commands.base) in the last release, \(.commands.candidate) in the candidate"' "$ARTIFACTS/surface.json")"

{ list commands_removed; list flags_removed; list defaults_changed; list types_changed; } >"$ARTIFACTS/breaking.txt"
n_break=$(grep -c . "$ARTIFACTS/breaking.txt")
n_add=$(jq '(.commands_added | length) + (.flags_added | length)' "$ARTIFACTS/surface.json")
if [ "$n_break" -gt 0 ]; then
  version=$(jq -r '."." // empty' "$PR_WT/.release-please-manifest.json")
  base_version=$(jq -r '."." // empty' "$BASE_WT/.release-please-manifest.json")
  findings_file "$ARTIFACTS/breaking.txt"
  if [ "${version%%.*}" != "${base_version%%.*}" ]; then
    info "$n_break removed or changed item(s) in a major release; $n_add added"
  fi
  decide "Release $version (not a major version) with these $n_break removed or changed command(s), flag(s), or default(s)?" \
    "Scripts that use them can break; $n_add command(s) and flag(s) added"
fi
[ "$n_add" -gt 0 ] && info "No command, flag, or default removed or changed; $n_add added"
pass "The CLI surface is the same as in the last release"
