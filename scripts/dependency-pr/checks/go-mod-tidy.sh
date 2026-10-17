#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

if ! run go mod tidy; then
  restore_tree
  fail "go mod tidy failed"
fi
if ! git diff --quiet -- go.mod go.sum; then
  git diff -- go.mod go.sum >"$ARTIFACTS/tidy.patch"
  git diff --stat -- go.mod go.sum >"$ARTIFACTS/tidy.stat"
  detail "go mod tidy would change:"
  detail_block "$ARTIFACTS/tidy.patch" 30
  fingerprint_file "$ARTIFACTS/tidy.patch"
  n=$(grep -c '^[+-][^+-]' "$ARTIFACTS/tidy.patch")
  restore_tree
  recommend "Run \`go mod tidy\` and commit go.mod/go.sum."
  fix_recipe go-mod-tidy "go mod tidy" go.mod go.sum
  fail "go mod tidy changes go.mod/go.sum ($n lines)"
fi
restore_tree
pass "go.mod/go.sum are tidy"
