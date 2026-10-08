#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

run go build ./... 2>&1 | tee "$ARTIFACTS/build.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  grep -E '\.go:[0-9]+' "$ARTIFACTS/build.out" | sed -E 's/:[0-9]+:[0-9]+:/:/' | sort -u >"$ARTIFACTS/build.errs"
  fingerprint_file "$ARTIFACTS/build.errs"
  detail_block "$ARTIFACTS/build.out" 30
  fail "go build fails: $(head -n1 "$ARTIFACTS/build.errs")"
fi

run go vet ./... 2>&1 | tee "$ARTIFACTS/vet.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  grep -E '\.go:[0-9]+' "$ARTIFACTS/vet.out" | sed -E 's/:[0-9]+:[0-9]+:/:/' | sort -u >"$ARTIFACTS/vet.errs"
  fingerprint_file "$ARTIFACTS/vet.errs"
  detail_block "$ARTIFACTS/vet.out" 30
  fail "go vet reports $(wc -l <"$ARTIFACTS/vet.errs") finding(s)"
fi
pass "go build ./... and go vet ./... succeed"
