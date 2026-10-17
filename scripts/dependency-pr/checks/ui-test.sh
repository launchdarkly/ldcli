#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || incomplete "npm ci failed, so the tests did not run"
cd "$WT/$UI_DIR_REL" || exit 1

run npm test 2>&1 | tee "$ARTIFACTS/test.out"
rc=${PIPESTATUS[0]}
counts=$(grep -E '^\s*Tests\s' "$ARTIFACTS/test.out" | tail -n1 | sed -E 's/\s+\(.*//; s/^\s*Tests\s+//')
if [ "$rc" -ne 0 ]; then
  grep -E '(FAIL|×|✗)\s' "$ARTIFACTS/test.out" | sed -E "s#$WT/##g; s/ [0-9]+ms$//" | sort -u >"$ARTIFACTS/failures"
  fingerprint_file "$ARTIFACTS/failures"
  detail_block "$ARTIFACTS/failures" 30
  fail "vitest fails${counts:+ ($counts)}"
fi
pass "vitest passes${counts:+ ($counts)}"
