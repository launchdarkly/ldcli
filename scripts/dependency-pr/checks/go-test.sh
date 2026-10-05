#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

run go test ./... 2>&1 | tee "$ARTIFACTS/test.out"
rc=${PIPESTATUS[0]}
pkgs=$(grep -cE '^(ok|FAIL)\s' "$ARTIFACTS/test.out")

if [ "$rc" -ne 0 ]; then
  grep -E '^(--- FAIL|FAIL\s|panic:)' "$ARTIFACTS/test.out" |
    sed -E 's/ \([0-9.]+s\)//; s/\s+[0-9.]+s$//; s/\s+\[[^]]*\]$//' | sort -u >"$ARTIFACTS/failures"
  fingerprint_file "$ARTIFACTS/failures"
  detail_block "$ARTIFACTS/failures" 30
  fail "$(grep -c '^FAIL\s' "$ARTIFACTS/failures") package(s) failing: $(grep '^FAIL\s' "$ARTIFACTS/failures" | awk '{print $2}' | sed 's#github.com/launchdarkly/ldcli/##' | paste -sd, -)"
fi
pass "$pkgs packages pass"
