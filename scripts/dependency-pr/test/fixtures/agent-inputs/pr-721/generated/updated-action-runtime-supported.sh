#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"

bin=$(go_tool github.com/rhysd/actionlint/cmd/actionlint v1.7.7) || incomplete "could not install actionlint"
mapfile -t names < <(updates_for github-actions | jq -r '.name' | sort -u)
[ ${#names[@]} -gt 0 ] || skip "no GitHub Actions updates"
(cd "$WT" && "$bin" -oneline -no-color) >"$ARTIFACTS/actionlint.out" 2>&1
rc=$?
cat "$ARTIFACTS/actionlint.out"
[ "$rc" -le 1 ] || incomplete "actionlint could not run (exit $rc)"
: >"$ARTIFACTS/findings"
for n in "${names[@]}"; do
  grep -F "the runner of \"$n@" "$ARTIFACTS/actionlint.out" | grep -F 'too old to run' >>"$ARTIFACTS/findings" || true
done
uses=$(cd "$WT" && grep -rhoE "uses: *($(IFS='|'; echo "${names[*]}" | sed 's/[.]/\\./g'))@[^ #]+" .github | sed 's/uses: *//' | sort | uniq -c | sed 's/^ *//' | paste -sd, -)
detail "- Steps that use the updated action: ${uses:-none}"
if [ -s "$ARTIFACTS/findings" ]; then
  detail_block "$ARTIFACTS/findings" 10
  fail "actionlint: $(wc -l <"$ARTIFACTS/findings") step(s) use a version of ${names[*]} whose runtime GitHub Actions no longer supports"
fi
pass "every step that uses ${names[*]} declares a runtime that GitHub Actions supports (actionlint [action] rule)"
