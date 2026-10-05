#!/usr/bin/env bash
# A dependency that is never imported (react-window in #831) passes CI no matter
# how breaking the bump is. Flag it so the reviewer can remove it instead.
source "$VERIFY_ROOT/lib/check.sh"
ui="$WT/$UI_DIR_REL"
ensure_ui_deps >/dev/null 2>&1 || true

re_escape() { printf '%s' "$1" | sed -E 's/[][\.*^$+?(){}|/]/\\&/g'; }

imported() {
  local pat
  pat=$(re_escape "$1")
  rg -q -e "(from|import|require\()\s*['\"]${pat}(/[^'\"]*)?['\"]" \
    "$ui/src" "$ui"/*.config.* "$ui"/eslint.config.js "$ui/index.html" 2>/dev/null
}

mentioned_in_tooling() {
  local pat
  pat=$(re_escape "$1")
  rg -q -e "$pat" "$ui"/*.config.* "$ui"/tsconfig*.json "$ui"/eslint.config.js 2>/dev/null && return 0
  jq -e --arg n "$1" '.scripts // {} | to_entries | any(.value | contains($n))' "$ui/package.json" >/dev/null && return 0
  # Tools invoked through package.json scripts by their bin name.
  local bins
  bins=$(jq -r '.bin // {} | if type == "string" then empty else keys[] end' "$ui/node_modules/$1/package.json" 2>/dev/null)
  for b in $bins; do
    jq -e --arg b "$b" '.scripts // {} | to_entries | any(.value | test("(^|[ &|;])" + $b + "( |$)"))' "$ui/package.json" >/dev/null && return 0
  done
  return 1
}

unused=()
while IFS= read -r u; do
  [ -n "$u" ] || continue
  name=$(jq -r '.name' <<<"$u")
  dev=$(jq -r '.dev' <<<"$u")
  target="$name"
  if [[ "$name" == @types/* ]]; then
    target="${name#@types/}"
    [[ "$target" == *__* ]] && target="@${target/__//}"
  fi
  meta="$ui/node_modules/$name/package.json"
  if [ -f "$meta" ]; then
    engines=$(jq -r '.engines.node // empty' "$meta")
    peers=$(jq -r '.peerDependencies // {} | to_entries | map("\(.key)@\(.value)") | join(", ")' "$meta")
    deprecated=$(jq -r '.deprecated // empty' "$meta")
    [ -n "$engines" ] && detail "- \`$name\` requires node \`$engines\`"
    [ -n "$peers" ] && detail "- \`$name\` peers: $peers"
    [ -n "$deprecated" ] && detail "- \`$name\` is deprecated: $deprecated"
  fi
  if imported "$target"; then
    detail "- \`$name\` is imported by the UI"
  elif [ "$dev" = "true" ] && mentioned_in_tooling "$target"; then
    detail "- \`$name\` is used by tooling/config"
  else
    unused+=("$name")
    detail "- \`$name\` is not imported in src/ or referenced by tooling"
  fi
done < <(updates_for npm-ui | jq -c 'select(.direct)')

if [ ${#unused[@]} -gt 0 ]; then
  recommend "Remove unused dependencies instead of bumping them: ${unused[*]}"
  warn "Not used anywhere: ${unused[*]} (the bump has no runtime effect; consider removing)"
fi
pass "All updated direct dependencies are used"
