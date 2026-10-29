#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || incomplete "npm ci failed, so the tests did not run"
cd "$WT/$UI_DIR_REL" || exit 1

run npm test 2>&1 | tee "$ARTIFACTS/test.out"
rc=${PIPESTATUS[0]}
counts=$(grep -E '^\s*Tests\s' "$ARTIFACTS/test.out" | tail -n1 | sed -E 's/\s+\(.*//; s/^\s*Tests\s+//')
if [ "$rc" -ne 0 ]; then
  grep -E '(FAIL|×|✗)\s' "$ARTIFACTS/test.out" | sed -E "s#$WT/##g; s/ [0-9]+ms$//" | sort -u >"$ARTIFACTS/failures"
  # No test summary and no failing test: vitest or npm crashed before the tests ran.
  if [ -z "$counts" ] && [ ! -s "$ARTIFACTS/failures" ] && ! grep -qE '^\s*Test Files\s' "$ARTIFACTS/test.out"; then
    detail_block "$ARTIFACTS/test.out" 30
    incomplete "vitest did not finish (exit $rc, no test summary): $(grep -m1 -iE 'error|cannot' "$ARTIFACTS/test.out" | cut -c1-160)"
  fi
  fingerprint_file "$ARTIFACTS/failures"
  detail_block "$ARTIFACTS/failures" 30
  fail "vitest fails${counts:+ ($counts)}"
fi
pass "vitest passes${counts:+ ($counts)}"
