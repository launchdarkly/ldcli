#!/usr/bin/env bash
# SEC-7924 (#668): third-party actions must be pinned to a full commit SHA with
# a version comment. GitHub-owned and LaunchDarkly-owned actions are exempt.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

exempt="${PIN_EXEMPT_OWNERS:-actions github launchdarkly}"
: >"$ARTIFACTS/violations"
while IFS=$'\t' read -r file ref comment; do
  case "$ref" in ./* | docker://*) continue ;; esac
  owner="${ref%%/*}"
  [[ " $exempt " == *" $owner "* ]] && continue
  sha="${ref##*@}"
  if ! [[ "$sha" =~ ^[0-9a-f]{40}$ ]]; then
    printf '%s: %s (not pinned to a commit SHA)\n' "$file" "$ref" >>"$ARTIFACTS/violations"
  elif [ -z "$comment" ]; then
    printf '%s: %s (missing "# vX" version comment)\n' "$file" "$ref" >>"$ARTIFACTS/violations"
  fi
done < <(rg --no-line-number --with-filename -o -g '*.yml' -g '*.yaml' \
  '^\s*-?\s*uses:\s*["'\'']?([^\s"'\''#]+)["'\'']?(?:\s*#\s*(\S+))?' -r '$1	$2' .github/workflows .github/actions 2>/dev/null |
  sed -E 's/:/\t/' | sort -u)

if [ -s "$ARTIFACTS/violations" ]; then
  sort -u -o "$ARTIFACTS/violations" "$ARTIFACTS/violations"
  findings_file "$ARTIFACTS/violations"
  detail_block "$ARTIFACTS/violations" 20
  recommend "Pin third-party actions to a full commit SHA with a \`# vX.Y.Z\` comment."
  fail "$(wc -l <"$ARTIFACTS/violations") unpinned third-party action reference(s)"
fi
pass "All third-party actions pinned to commit SHAs"
