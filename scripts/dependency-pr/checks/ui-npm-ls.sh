#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
ensure_ui_deps || skip "npm ci failed"
cd "$WT/$UI_DIR_REL" || exit 1

run npm ls --all >/dev/null 2>"$ARTIFACTS/ls.err"
if [ $? -ne 0 ]; then
  cat "$ARTIFACTS/ls.err"
  grep -E 'npm error (invalid|missing|extraneous|peer)' "$ARTIFACTS/ls.err" | sed -E "s#$WT/##g" | sort -u >"$ARTIFACTS/problems"
  fingerprint_file "$ARTIFACTS/problems"
  detail_block "$ARTIFACTS/problems" 20
  warn "npm ls reports $(wc -l <"$ARTIFACTS/problems") problem(s): $(head -n2 "$ARTIFACTS/problems" | sed -E 's/^npm error //; s# /?internal/dev_server/ui/node_modules/[^ ]*##' | paste -sd';' -)"
fi
pass "npm ls --all reports a valid tree"
