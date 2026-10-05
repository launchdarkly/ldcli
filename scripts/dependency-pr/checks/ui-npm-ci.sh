#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT/$UI_DIR_REL" || exit 1

detail "- node $(node --version), npm $(npm --version). CI uses \`node-version: lts/*\`."
run npm ci --no-audit --no-fund 2>&1 | tee "$ARTIFACTS/npm-ci.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  # Drop log-file paths and timestamps so base and PR fingerprints compare.
  grep -E 'ERESOLVE|While resolving|Found:|Could not resolve|peer |Conflicting peer|code E' "$ARTIFACTS/npm-ci.out" |
    grep -v 'A complete log' | sed -E "s#$WT/##g" | sort -u >"$ARTIFACTS/npm-ci.errs"
  fingerprint_file "$ARTIFACTS/npm-ci.errs"
  detail_block "$ARTIFACTS/npm-ci.errs" 25
  if grep -q ERESOLVE "$ARTIFACTS/npm-ci.out"; then
    conflict=$(grep -m1 -E 'Could not resolve dependency|Conflicting peer dependency' -A2 "$ARTIFACTS/npm-ci.out" | grep -oE '(peer )?[@a-z0-9/._-]+@"?[^ "]+"?( from [@a-z0-9/._-]+@[^ ]+)?' | head -n2 | paste -sd' ' -)
    recommend "Resolve the peer-dependency conflict with a coordinated upgrade or a scoped \`overrides\` entry (see #777), or close in favor of a focused PR."
    fail "npm ci fails with ERESOLVE peer conflict${conflict:+: $conflict}"
  fi
  fail "npm ci fails: $(grep -m1 'npm error' "$ARTIFACTS/npm-ci.out" | sed 's/^npm error //')"
fi
sha256sum <package-lock.json | cut -c1-16 >node_modules/.verify-lock-hash
pass "npm ci succeeds"
