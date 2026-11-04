#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || incomplete "npm ci failed, so eslint did not run"
cd "$WT/$UI_DIR_REL" || exit 1

run npm run lint 2>&1 | tee "$ARTIFACTS/lint.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  # eslint ends a report of findings with "✖ N problems". Without it, eslint
  # or npm crashed, and a crash says nothing about the code.
  if ! grep -qE '✖ [0-9]+ problems?' "$ARTIFACTS/lint.out"; then
    detail_block "$ARTIFACTS/lint.out" 30
    incomplete "eslint did not finish (no problem report): $(grep -m1 -iE 'error|oops|cannot' "$ARTIFACTS/lint.out" | cut -c1-160)"
  fi
  sed -E "s#$WT/##g" "$ARTIFACTS/lint.out" | grep -E 'error|warning' | sed -E 's/^\s*[0-9]+:[0-9]+\s+//' | sort -u >"$ARTIFACTS/lint.errs"
  fingerprint_file "$ARTIFACTS/lint.errs"
  detail_block "$ARTIFACTS/lint.out" 30
  fail "eslint reports problems ($(grep -oE '[0-9]+ problems?' "$ARTIFACTS/lint.out" | tail -n1))"
fi
pass "eslint clean"
