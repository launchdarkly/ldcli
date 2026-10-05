#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

bin=$(go_tool github.com/rhysd/actionlint/cmd/actionlint v1.7.7) || skip "could not install actionlint"
run "$bin" -oneline -no-color | tee "$ARTIFACTS/actionlint.out"
rc=${PIPESTATUS[0]}
if [ "$rc" -eq 1 ]; then
  grep -E '^\.github/' "$ARTIFACTS/actionlint.out" | sed -E 's/:[0-9]+:[0-9]+:/:/' | sort -u >"$ARTIFACTS/findings"
  fingerprint_file "$ARTIFACTS/findings"
  detail_block "$ARTIFACTS/actionlint.out" 25
  fail "actionlint reports $(wc -l <"$ARTIFACTS/findings") finding(s)"
elif [ "$rc" -ne 0 ]; then
  skip "actionlint could not run (exit $rc); see log"
fi
pass "actionlint clean"
