#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || incomplete "npm ci failed, so prettier did not run"
cd "$WT/$UI_DIR_REL" || exit 1

# The installed binary, not npx: an npx earlier on PATH can crash before
# prettier starts (/exec-daemon/npx on the cloud VM).
prettier="./node_modules/.bin/prettier"
[ -x "$prettier" ] || incomplete "prettier is not installed in node_modules, so it did not run"
run "$prettier" . --check 2>&1 | tee "$ARTIFACTS/prettier.out"
rc=${PIPESTATUS[0]}
if [ "$rc" -ne 0 ]; then
  grep -E '^\[(warn|error)\] ' "$ARTIFACTS/prettier.out" | grep -vE 'Code style issues|^\[error\] (No files|No parser)' | sort -u >"$ARTIFACTS/prettier.files"
  # A non-zero exit with no file in the output is a crash, not a format problem.
  if [ ! -s "$ARTIFACTS/prettier.files" ]; then
    incomplete "prettier did not finish (exit $rc, no file reported): $(grep -m1 -iE 'error|cannot|not found' "$ARTIFACTS/prettier.out" | cut -c1-160)"
  fi
  findings_file "$ARTIFACTS/prettier.files"
  detail_block "$ARTIFACTS/prettier.files" 20
  fix_recipe ui-prettier "cd internal/dev_server/ui && npm ci && npm run prettier:write" internal/dev_server/ui/
  recommend "Run \`npm run prettier:write\` in internal/dev_server/ui and commit (a prettier bump can reformat files)."
  fail "prettier would reformat $(wc -l <"$ARTIFACTS/prettier.files") file(s)"
fi
version=$("$prettier" --version 2>/dev/null) || incomplete "prettier --version failed after the check"
pass "prettier --check clean ($version)"
