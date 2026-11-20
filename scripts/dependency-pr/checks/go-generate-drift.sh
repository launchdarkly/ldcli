#!/usr/bin/env bash
# Catches the #720 failure mode: a generator bump merges green, but the
# committed output was never regenerated and regenerating breaks the build.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

restore_tree
n_gen=$(rg -l '^//go:generate' --glob '*.go' . | wc -l)

run go generate ./... 2>&1 | tee "$ARTIFACTS/generate.out"
if [ "${PIPESTATUS[0]}" -ne 0 ]; then
  grep -vE '^[0-9]{4}/[0-9]{2}/[0-9]{2} ' "$ARTIFACTS/generate.out" | sort -u >"$ARTIFACTS/generate.errs"
  fingerprint_file "$ARTIFACTS/generate.errs"
  detail_block "$ARTIFACTS/generate.out" 30
  restore_tree
  fail "go generate fails"
fi

if [ -z "$(git status --porcelain)" ]; then
  pass "No drift across $n_gen go:generate directive(s)"
fi

git diff >"$ARTIFACTS/drift.patch"
git status --porcelain >"$ARTIFACTS/drift.files"
git diff --stat=100 --stat-graph-width=20 >"$ARTIFACTS/drift.stat"
n_files=$(wc -l <"$ARTIFACTS/drift.files")
detail "Files rewritten by \`go generate ./...\` (patch saved as drift.patch next to this check's status):"
detail_block "$ARTIFACTS/drift.stat" 20

build_ok=true
if ! go build ./... >"$ARTIFACTS/rebuild.out" 2>&1; then
  build_ok=false
  sed -E 's/:[0-9]+:[0-9]+:/:/' "$ARTIFACTS/rebuild.out" | sort -u >"$ARTIFACTS/rebuild.errs"
  detail "The regenerated code does not compile:"
  detail_block "$ARTIFACTS/rebuild.out" 15
fi
fingerprint "$(cat "$ARTIFACTS/drift.stat" "$ARTIFACTS/rebuild.errs" 2>/dev/null)"
restore_tree

if [ "$build_ok" = false ]; then
  recommend "Regenerate (\`make generate\`) in this PR and bump the generator's runtime library alongside it (e.g. oapi-codegen with oapi-codegen/runtime) so the regenerated code compiles."
  fail "go generate rewrites $n_files file(s) and the regenerated code does not compile"
fi
recommend "Run \`make generate\` and commit the regenerated files."
# shellcheck disable=SC2046
fix_recipe go-generate "go generate ./..." $(awk '{print $2}' "$ARTIFACTS/drift.files")
fail "go generate rewrites $n_files file(s); regenerated code compiles"
