#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || incomplete "npm ci failed, so prettier did not run"
cd "$WT/$UI_DIR_REL" || exit 1

run npx --no-install prettier . --check 2>&1 | tee "$ARTIFACTS/prettier.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  grep -E '^\[warn\] ' "$ARTIFACTS/prettier.out" | grep -v 'Code style issues' | sort -u >"$ARTIFACTS/prettier.files"
  findings_file "$ARTIFACTS/prettier.files"
  detail_block "$ARTIFACTS/prettier.files" 20
  fix_recipe ui-prettier "cd internal/dev_server/ui && npm ci && npm run prettier:write" internal/dev_server/ui/
  recommend "Run \`npm run prettier:write\` in internal/dev_server/ui and commit (a prettier bump can reformat files)."
  fail "prettier would reformat $(wc -l <"$ARTIFACTS/prettier.files") file(s)"
fi
pass "prettier --check clean ($(npx --no-install prettier --version))"
