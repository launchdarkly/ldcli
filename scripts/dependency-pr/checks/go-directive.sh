#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"

read -r gofrom goto < <(jq -r '"\(.go_directive.from // "none") \(.go_directive.to // "none")"' "$CLASSIFICATION")
read -r tfrom tto < <(jq -r '"\(.toolchain.from // "none") \(.toolchain.to // "none")"' "$CLASSIFICATION")
detail "- Local toolchain: $(go version | awk '{print $3}')"

changes=()
[ "$gofrom" != "$goto" ] && changes+=("go directive $gofrom → $goto")
[ "$tfrom" != "$tto" ] && changes+=("toolchain $tfrom → $tto")

if [ ${#changes[@]} -gt 0 ]; then
  detail "- CI picks its Go version from go.mod (\`go-version-file\`), so this changes the Go used by every workflow."
  detail "- golangci-lint v1.63.4 (pre-commit) must be able to read the new Go's export data, and the release image (goreleaser-cross, pinned by digest) must ship a new enough Go."
  recommend "Confirm golangci-lint and the goreleaser-cross image support the new Go version before merging."
  fail "$(IFS='; '; echo "${changes[*]}")"
fi
pass "go $gofrom (unchanged)"
