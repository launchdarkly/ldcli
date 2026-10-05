#!/usr/bin/env bash
# CLI-library bumps (cobra, pflag, viper, glamour) can change flags, defaults,
# or help rendering without failing any test. Diff the full help tree.
source "$VERIFY_ROOT/lib/check.sh"

for side in base pr; do
  wt="$BASE_WT"
  [ "$side" = pr ] && wt="$PR_WT"
  if ! (cd "$wt" && run go build -o "$ARTIFACTS/ldcli-$side" .); then
    skip "could not build the $side binary"
  fi
  help_dump "$ARTIFACTS/ldcli-$side" "$ARTIFACTS/help-$side"
done

if diff -u "$ARTIFACTS/help-base/all.txt" "$ARTIFACTS/help-pr/all.txt" >"$ARTIFACTS/help.diff"; then
  pass "Help output identical for $(wc -l <"$ARTIFACTS/help-pr/.commands") commands"
fi
added=$(comm -13 "$ARTIFACTS/help-base/.commands" "$ARTIFACTS/help-pr/.commands" | paste -sd, -)
removed=$(comm -23 "$ARTIFACTS/help-base/.commands" "$ARTIFACTS/help-pr/.commands" | paste -sd, -)
sections=$(awk '/^[ +-]?### /{s=substr($0, 2)} /^[+-][^+-]/{print s}' "$ARTIFACTS/help.diff" | sed -E 's/^#* ?//' | sort -u)
n_sections=$(printf '%s\n' "$sections" | grep -c .)
[ -n "$added" ] && detail "- Commands added: $added"
[ -n "$removed" ] && detail "- Commands removed: $removed"
detail "- Help text changed for: $(printf '%s\n' "$sections" | head -n 15 | paste -sd, -)"
detail_block "$ARTIFACTS/help.diff" 60
fingerprint_file "$ARTIFACTS/help.diff"
warn "Help output differs for $n_sections command(s)${removed:+; removed: $removed}${added:+; added: $added}"
