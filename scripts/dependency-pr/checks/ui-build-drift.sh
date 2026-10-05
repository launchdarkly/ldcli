#!/usr/bin/env bash
# The rebuilt dist/ is left in place on purpose: binary-smoke runs later and
# embeds it, which tests what main would serve once dist is rebuilt.
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || skip "npm ci failed; cannot build"
cd "$WT/$UI_DIR_REL" || exit 1

run npm run build 2>&1 | tee "$ARTIFACTS/build.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  grep -E 'error|Error' "$ARTIFACTS/build.out" | sed -E "s#$WT/##g; s/\([0-9]+,[0-9]+\)//" | sort -u >"$ARTIFACTS/build.errs"
  fingerprint_file "$ARTIFACTS/build.errs"
  detail_block "$ARTIFACTS/build.out" 30
  severity block
  fail "npm run build fails"
fi

changed=$(git -C "$WT" status --porcelain -- "$UI_DIR_REL")
if [ -n "$changed" ]; then
  git -C "$WT" diff --stat -- "$UI_DIR_REL" >"$ARTIFACTS/drift.stat"
  printf '%s\n' "$changed" >>"$ARTIFACTS/drift.stat"
  fingerprint "$(git -C "$WT" diff -- "$UI_DIR_REL")"
  detail "Build output differs from the committed files:"
  detail_block "$ARTIFACTS/drift.stat" 15
  recommend "Rebuild the UI and commit dist: \`cd internal/dev_server/ui && npm ci && npm run build\` (the dev-server UI CI job fails until then)."
  fail "Committed dist/ is stale: the build rewrites $(printf '%s\n' "$changed" | wc -l) file(s)"
fi
pass "Build succeeds; committed dist/ matches"
