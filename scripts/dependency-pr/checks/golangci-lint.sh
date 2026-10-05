#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

if ! command -v golangci-lint >/dev/null 2>&1; then
  skip "golangci-lint not installed (CI runs v1.63.4 through pre-commit)"
fi
run golangci-lint run ./... 2>&1 | tee "$ARTIFACTS/lint.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  grep -E '\.go:[0-9]+' "$ARTIFACTS/lint.out" | sed -E 's/:[0-9]+:[0-9]+:/:/' | sort -u >"$ARTIFACTS/lint.errs"
  fingerprint_file "$ARTIFACTS/lint.errs"
  detail_block "$ARTIFACTS/lint.out" 30
  fail "golangci-lint reports $(wc -l <"$ARTIFACTS/lint.errs") finding(s)"
fi
pass "golangci-lint clean ($(golangci-lint --version | awk '{print $4}'))"
