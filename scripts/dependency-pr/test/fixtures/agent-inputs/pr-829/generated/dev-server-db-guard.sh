#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
dst="$WT/internal/dev_server/db/zz_verify_reopen_test.go"
cp "$(dirname "$0")/db_reopen_test.go.txt" "$dst"
trap 'rm -f "$dst"' EXIT

cd "$WT" || exit 1
run go test -count=1 ./internal/dev_server/db/... ./internal/dev_server/events_db/... 2>&1 | tee "$ARTIFACTS/test.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  detail_block "$ARTIFACTS/test.out" 20
  fail "dev-server DB packages fail (reopen/migration, json_each pruning, backup round trip)"
fi
pass "Reopen + migrations, json_each pruning, and existing db/events_db/backup tests pass (uncached)"
