#!/usr/bin/env bash
# Network: reads the action lockfile and the release-please schema through gh, and runs ajv-cli through npx.
source "$VERIFY_ROOT/lib/check.sh"
command -v gh >/dev/null 2>&1 || incomplete "gh is not installed"
command -v npx >/dev/null 2>&1 || incomplete "npx is not installed"

ref=$(grep -rhoE 'googleapis/release-please-action@[0-9a-f]{40}' "$WT/.github/workflows" | head -n1 | cut -d@ -f2)
[ -n "$ref" ] || skip "no workflow pins googleapis/release-please-action to a commit"
lib=$(gh api "repos/googleapis/release-please-action/contents/package-lock.json?ref=$ref" --jq .content 2>/dev/null | base64 -d |
  jq -r '.packages["node_modules/release-please"].version // empty')
[ -n "$lib" ] || incomplete "could not read the bundled release-please version at $ref"
gh api "repos/googleapis/release-please/contents/schemas/config.json?ref=v$lib" --jq .content 2>/dev/null | base64 -d >"$ARTIFACTS/schema.json"
jq -e . "$ARTIFACTS/schema.json" >/dev/null 2>&1 || incomplete "could not read schemas/config.json of release-please v$lib"
jq -e 'has(".")' "$WT/.release-please-manifest.json" >/dev/null || fail ".release-please-manifest.json has no root package"
cp "$WT/release-please-config.json" "$ARTIFACTS/config.json"
(cd "$ARTIFACTS" && run timeout 240 npx -y ajv-cli@5 validate --spec=draft7 --strict=false -s schema.json -d config.json) >"$ARTIFACTS/ajv.out" 2>&1
rc=$?
cat "$ARTIFACTS/ajv.out"
detail "- action \`${ref:0:12}\` bundles release-please v$lib"
if grep -q 'config.json valid' "$ARTIFACTS/ajv.out"; then
  pass "release-please-config.json is valid for release-please v$lib, which the pinned action bundles"
fi
grep -q 'config.json invalid' "$ARTIFACTS/ajv.out" || incomplete "ajv-cli did not run (exit $rc): $(tail -n1 "$ARTIFACTS/ajv.out")"
detail_block "$ARTIFACTS/ajv.out" 20
fail "release-please-config.json is not valid for release-please v$lib"
