#!/usr/bin/env bash
# A change to the go or toolchain directive changes the Go that CI uses
# (go.yml reads go-version-file: go.mod). The "go-directive" risk tag makes
# golangci-lint and the goreleaser-cross snapshot required gates. This check
# covers the rest: the new Go is available, and no workflow pins an older Go.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1

read -r gofrom goto < <(jq -r '"\(.go_directive.from // "none") \(.go_directive.to // "none")"' "$CLASSIFICATION")
read -r tfrom tto < <(jq -r '"\(.toolchain.from // "none") \(.toolchain.to // "none")"' "$CLASSIFICATION")

if [ "$gofrom" = "$goto" ] && [ "$tfrom" = "$tto" ]; then
  pass "go $gofrom (unchanged)"
fi

detail "- go directive: \`$gofrom\` → \`$goto\`; toolchain: \`$tfrom\` → \`$tto\`"
# `go version` in the worktree selects (and if necessary downloads) the Go that go.mod asks for.
if ! used=$(go version 2>"$ARTIFACTS/go-version.err"); then
  incomplete "could not get the Go version that go.mod requires: $(head -n1 "$ARTIFACTS/go-version.err")"
fi
detail "- Go used for this PR: \`$(awk '{print $3}' <<<"$used")\`"

# Workflow steps that pin a literal Go version older than the new directive.
older() { [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$1" ] && [ "$1" != "$2" ]; }
stale=()
while IFS=: read -r file line value; do
  v=$(sed -E "s/[\"' ]//g" <<<"$value")
  [[ "$v" =~ ^[0-9]+\.[0-9]+ ]] || continue
  older "$v" "$goto" && stale+=("$file:$line pins Go $v")
done < <(rg -n --no-heading -o 'go-version:\s*\S+' .github 2>/dev/null | sed -E 's/go-version:\s*//')

if [ ${#stale[@]} -gt 0 ]; then
  fingerprint "$(printf '%s\n' "${stale[@]}" | sed -E 's/:[0-9]+ / /')"
  recommend "Raise the pinned Go version in these workflow steps to at least $goto: ${stale[*]}"
  fail "Workflows pin a Go version older than the new go directive $goto: $(join_by '; ' "${stale[@]}")"
fi
info "go directive $gofrom → $goto; the new Go is available and no workflow pins an older Go. golangci-lint and the release snapshot are required gates for this change"
