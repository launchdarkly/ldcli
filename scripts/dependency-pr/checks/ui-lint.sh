#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || skip "npm ci failed; cannot lint"
cd "$WT/$UI_DIR_REL" || exit 1

run npm run lint 2>&1 | tee "$ARTIFACTS/lint.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  sed -E "s#$WT/##g" "$ARTIFACTS/lint.out" | grep -E 'error|warning' | sed -E 's/^\s*[0-9]+:[0-9]+\s+//' | sort -u >"$ARTIFACTS/lint.errs"
  fingerprint_file "$ARTIFACTS/lint.errs"
  detail_block "$ARTIFACTS/lint.out" 30
  fail "eslint reports problems ($(grep -oE '[0-9]+ problems?' "$ARTIFACTS/lint.out" | tail -n1))"
fi
pass "eslint clean"
