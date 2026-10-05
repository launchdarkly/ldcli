#!/usr/bin/env bash
# Installs the root npm package the way users do: the go-npm postinstall
# downloads the GitHub release binary for the version in package.json.
source "$VERIFY_ROOT/lib/check.sh"

version=$(jq -r '.version' "$WT/package.json")
tmp="$ARTIFACTS/install"
rm -rf "$tmp" && mkdir -p "$tmp"
cp "$WT/package.json" "$WT/package-lock.json" "$tmp/"

(cd "$tmp" && run npm ci --no-audit --no-fund) >"$ARTIFACTS/npm-ci.out" 2>&1
rc=$?
cat "$ARTIFACTS/npm-ci.out"
if [ $rc -ne 0 ]; then
  grep -E 'npm error|Error' "$ARTIFACTS/npm-ci.out" | grep -v 'A complete log' | sed -E "s#$tmp/?##g" | sort -u >"$ARTIFACTS/errs"
  fingerprint_file "$ARTIFACTS/errs"
  detail_block "$ARTIFACTS/errs" 20
  fail "npm ci of the wrapper package fails (postinstall downloads v$version)"
fi

bin="$tmp/bin/ldcli"
if [ ! -x "$bin" ]; then
  fingerprint "no-binary"
  fail "postinstall finished but bin/ldcli is missing"
fi
out=$("$bin" --version 2>&1)
detail "- \`bin/ldcli --version\`: $out"
if ! grep -q "$version" <<<"$out"; then
  fingerprint "version-mismatch"
  fail "Installed binary reports '$out', expected $version"
fi

(cd "$WT" && run npm pack --dry-run --json 2>/dev/null) >"$ARTIFACTS/pack.json"
files=$(jq -r '.[0].files | map(.path) | join(", ")' "$ARTIFACTS/pack.json" 2>/dev/null)
detail "- \`npm pack\` contents: ${files:-unavailable}"
pass "npm install fetches a working ldcli $version"
