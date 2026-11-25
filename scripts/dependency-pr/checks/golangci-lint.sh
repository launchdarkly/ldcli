#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

if ! command -v golangci-lint >/dev/null 2>&1; then
  incomplete "golangci-lint is not installed (CI runs v1.63.4 through pre-commit)"
fi
# Verifier runs can overlap. Without --allow-parallel-runners, the second
# golangci-lint stops with "parallel golangci-lint is running".
run golangci-lint run --allow-parallel-runners ./... 2>&1 | tee "$ARTIFACTS/lint.out"
rc=${PIPESTATUS[0]}
if [ "$rc" -ne 0 ]; then
  grep -E '\.go:[0-9]+' "$ARTIFACTS/lint.out" | sed -E 's/:[0-9]+:[0-9]+:/:/' | sort -u >"$ARTIFACTS/lint.errs"
  # A non-zero exit with no finding is a tool problem (a lock, a config or
  # load error), not a property of the PR.
  if [ ! -s "$ARTIFACTS/lint.errs" ]; then
    incomplete "golangci-lint exited $rc without a finding: $(grep -m1 -iE 'error|level=' "$ARTIFACTS/lint.out" | cut -c1-160)"
  fi
  findings_file "$ARTIFACTS/lint.errs"
  detail_block "$ARTIFACTS/lint.out" 30
  fail "golangci-lint reports $(wc -l <"$ARTIFACTS/lint.errs") finding(s)"
fi
pass "golangci-lint clean ($(golangci-lint --version | awk '{print $4}'))"
