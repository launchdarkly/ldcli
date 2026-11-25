#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || incomplete "no worktree"
grep -q '^toolchain ' go.mod && fail "go.mod has a toolchain directive, which setup-go v6 now prefers over the go directive"
gover=$(awk '$1=="go"{print $2; exit}' go.mod)
bad=""
for wf in $(grep -l 'actions/setup-go@' .github/workflows/*.yml); do
  block=$(awk '/uses: actions\/setup-go@/{f=1;next} f&&/^ *- /{exit} f' "$wf")
  grep -q 'go-version-file: go.mod' <<<"$block" || bad="$bad $wf"
  grep -qE '^\s*(go-version|check-latest|cache-dependency-path|architecture):' <<<"$block" && bad="$bad $wf(extra-inputs)"
done
[ -z "$bad" ] || fail "setup-go steps use other inputs:$bad"
detail "go.mod: go $gover, no toolchain directive. Every setup-go step passes only go-version-file: go.mod."
pass "setup-go resolves Go from 'go $gover' with no toolchain directive in every workflow"
