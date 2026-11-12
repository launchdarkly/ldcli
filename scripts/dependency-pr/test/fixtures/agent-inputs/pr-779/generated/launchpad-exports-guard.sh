#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
dir="$WT/$UI_DIR_REL"
packs="$ARTIFACTS/packs"
mkdir -p "$packs"
pkgs=$(grep -rhoE "from ['\"]@launchpad-ui/[a-z0-9-]+['\"]" "$dir/src" | grep -oE '@launchpad-ui/[a-z0-9-]+' | sort -u)
[ -n "$pkgs" ] || skip "src/ imports no @launchpad-ui package"
for p in $pkgs; do
  v=$(jq -r --arg k "node_modules/$p" '.packages[$k].version // empty' "$dir/package-lock.json")
  [ -n "$v" ] || incomplete "$p is not in package-lock.json"
  mkdir -p "$packs/$p"
  tgz=$(cd "$packs/$p" && npm pack "$p@$v" --silent 2>>"$ARTIFACTS/pack.err" | tail -n1)
  [ -n "$tgz" ] && [ -f "$packs/$p/$tgz" ] || incomplete "npm pack $p@$v failed (network?): $(tail -n1 "$ARTIFACTS/pack.err")"
  tar xzf "$packs/$p/$tgz" -C "$packs/$p" || incomplete "could not unpack $p@$v"
done
cp "$(dirname "$0")/lp_exports.mjs.txt" "$ARTIFACTS/lp_exports.mjs"
node "$ARTIFACTS/lp_exports.mjs" "$dir" "$packs" >"$ARTIFACTS/exports.out" 2>&1
rc=$?
cat "$ARTIFACTS/exports.out"
sum=$(grep -m1 '^SUMMARY' "$ARTIFACTS/exports.out")
[ -n "$sum" ] || incomplete "export scan did not finish: $(tail -n1 "$ARTIFACTS/exports.out")"
grep -v '^SUMMARY' "$ARTIFACTS/exports.out" >"$ARTIFACTS/exports.md"
detail_block "$ARTIFACTS/exports.md" 40
n=$(grep -c '^  MISSING' "$ARTIFACTS/exports.out")
[ $rc -eq 0 ] && pass "every @launchpad-ui name that src/ imports is exported at the locked versions ($sum)"
recommend "Migrate the listed imports to their replacements (for example @launchpad-ui/components) in the same PR, or keep the old version."
fail "$n imported @launchpad-ui name(s) are no longer exported at the locked versions"
