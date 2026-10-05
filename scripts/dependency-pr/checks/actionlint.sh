#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

ACTIONLINT_VERSION="v1.7.7"
if command -v actionlint >/dev/null 2>&1; then
  cmd=(actionlint)
else
  cmd=(go run "github.com/rhysd/actionlint/cmd/actionlint@$ACTIONLINT_VERSION")
fi

run "${cmd[@]}" -oneline -no-color 2>&1 | tee "$ARTIFACTS/actionlint.out"
rc=${PIPESTATUS[0]}
if [ "$rc" -eq 1 ]; then
  sed -E 's/:[0-9]+:[0-9]+:/:/' "$ARTIFACTS/actionlint.out" | sort -u >"$ARTIFACTS/findings"
  fingerprint_file "$ARTIFACTS/findings"
  detail_block "$ARTIFACTS/actionlint.out" 25
  fail "actionlint reports $(wc -l <"$ARTIFACTS/findings") finding(s)"
elif [ "$rc" -ne 0 ]; then
  skip "actionlint could not run (exit $rc); see log"
fi
pass "actionlint clean"
