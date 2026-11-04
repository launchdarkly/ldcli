#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
trap restore_tree EXIT
ensure_ui_deps || incomplete "npm ci failed, so the routing test did not run"
dir="$WT/$UI_DIR_REL"
mkdir -p "$dir/src/__verify__"
cp "$(dirname "$0")/app_routing_test.tsx.txt" "$dir/src/__verify__/zz_routing.test.tsx"
cd "$dir" || incomplete "UI directory missing"
run npx --no-install vitest run src/__verify__ >"$ARTIFACTS/vitest.out" 2>&1
rc=$?
cat "$ARTIFACTS/vitest.out"
summary=$(grep -m1 -E '^ +Tests ' "$ARTIFACTS/vitest.out" | sed -E 's/ +/ /g; s/^ //')
[ -n "$summary" ] || incomplete "vitest did not report results: $(tail -n1 "$ARTIFACTS/vitest.out")"
detail "react-router $(jq -r .version node_modules/react-router/package.json), react $(jq -r .version node_modules/react/package.json): $summary"
[ $rc -eq 0 ] && pass "App.tsx routes, redirects, and params work ($summary)"
detail_block "$ARTIFACTS/vitest.out" 40
fail "App.tsx routing changed ($summary)"
