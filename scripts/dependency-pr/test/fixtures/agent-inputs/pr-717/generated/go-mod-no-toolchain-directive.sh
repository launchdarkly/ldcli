#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"

mapfile -t files < <(cd "$WT" && grep -rhoE 'go-version-file: *[^ #]+' .github | sed 's/go-version-file: *//' | tr -d "\"'" | sort -u)
[ ${#files[@]} -gt 0 ] || skip "no workflow passes go-version-file to actions/setup-go"
for f in "${files[@]}"; do
  [ -f "$WT/$f" ] || fail "go-version-file $f does not exist"
  godir=$(grep -m1 -E '^go [0-9]' "$WT/$f" | awk '{print $2}')
  tc=$(grep -m1 -E '^toolchain ' "$WT/$f" | awk '{print $2}')
  detail "- \`$f\`: go directive \`${godir:-none}\`, toolchain directive \`${tc:-none}\`"
  [ -n "$godir" ] || fail "$f has no go directive, so setup-go cannot select a version"
  [ -z "$tc" ] || fail "$f has 'toolchain $tc'; setup-go v6 installs that instead of go $godir"
done
pass "go-version-file (${files[*]}) has a go directive and no toolchain directive, so setup-go v4, v5, and v6 select the same Go version"
