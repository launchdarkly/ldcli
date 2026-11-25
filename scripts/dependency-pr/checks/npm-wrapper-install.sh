#!/usr/bin/env bash
# Exercises the root npm package's install path: go-npm's postinstall downloads
# the GitHub release binary for the version in package.json and copies it into
# $npm_config_prefix/bin (newer npm has no `npm bin`, so go-npm falls back to
# the prefix). Uses the PR's locked go-npm version and a scratch prefix.
source "$VERIFY_ROOT/lib/check.sh"

version=$(jq -r '.version' "$WT/package.json")
range=$(jq -r '.dependencies["@go-task/go-npm"] // "none"' "$WT/package.json")
tmp="$ARTIFACTS/install"
rm -rf "$tmp" && mkdir -p "$tmp/prefix/bin"
cp "$WT/package.json" "$WT/package-lock.json" "$tmp/"

errs() {
  grep -E 'npm error|Error' "$1" | grep -v 'A complete log' | sed -E "s#$tmp/?##g" | sort -u >"$ARTIFACTS/errs"
  fingerprint_file "$ARTIFACTS/errs"
  detail_block "$ARTIFACTS/errs" 20
}

if ! (cd "$tmp" && run npm ci --ignore-scripts --no-audit --no-fund) >"$ARTIFACTS/npm-ci.out" 2>&1; then
  cat "$ARTIFACTS/npm-ci.out"
  errs "$ARTIFACTS/npm-ci.out"
  fail "npm ci of the wrapper package fails"
fi
locked=$(jq -r '.version' "$tmp/node_modules/@go-task/go-npm/package.json")
detail "- go-npm: locked \`$locked\`; users installing from npm resolve \`$range\` (the lockfile is not published)"

if ! (cd "$tmp" && npm_config_prefix="$tmp/prefix" run npm run postinstall) >"$ARTIFACTS/postinstall.out" 2>&1; then
  cat "$ARTIFACTS/postinstall.out"
  errs "$ARTIFACTS/postinstall.out"
  fail "go-npm postinstall fails (downloads v$version)"
fi
cat "$ARTIFACTS/postinstall.out"

bin="$tmp/prefix/bin/ldcli"
[ -x "$bin" ] || { fingerprint "no-binary"; fail "postinstall finished but $bin is missing"; }
out=$("$bin" --version 2>&1)
detail "- installed \`ldcli --version\`: $out"
if ! grep -q "$version" <<<"$out"; then
  fingerprint "version-mismatch"
  fail "Installed binary reports '$out', expected $version"
fi

pack=$(cd "$WT" && npm pack --dry-run --json 2>/dev/null |
  jq -r '.[0] | "\(.entryCount) files, \(.unpackedSize / 1024 | floor) KB unpacked; top level: \([.files[].path | split("/")[0]] | unique | join(", "))"')
detail "- \`npm pack\`: ${pack:-unavailable}"
pass "go-npm $locked installs a working ldcli $version"
