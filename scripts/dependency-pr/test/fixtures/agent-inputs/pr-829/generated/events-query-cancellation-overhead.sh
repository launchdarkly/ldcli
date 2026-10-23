#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
dst="$WT/internal/dev_server/events_db/zz_verify_alloc_test.go"
cp "$(dirname "$0")/events_query_alloc_test.go.txt" "$dst"
trap 'rm -f "$dst"' EXIT

out=$(cd "$WT" && go test -count=1 -run '^TestVerifyQueryEventsCancellationOverhead$' -v ./internal/dev_server/events_db/ 2>&1)
rc=$?
echo "$out"
measured=$(grep -m1 '^allocs:' <<<"$out")
detail "- $SIDE: ${measured:-no measurement}"
[ -n "$measured" ] || fail "test did not run: $(tail -n1 <<<"$out")"
[ $rc -eq 0 ] && pass "QueryEvents adds no per-row allocations under a cancellable context ($measured)"
fail "QueryEvents allocates per row under a cancellable context ($measured)"
