#!/usr/bin/env bash
# The changelog is what users read. It must not list a change that the release
# does not ship (a commit and its revert), and a person must accept each shipped
# change that it leaves out (release-please hides chore commits, including
# dependency updates that change code).
source "$VERIFY_ROOT/lib/check.sh"
: "${RELEASE_INVENTORY:?}"

if [ "$(jq -r '.changelog == null' "$RELEASE_INVENTORY")" = true ]; then
  fail "CHANGELOG.md has no section for the release version"
fi

jq -r '.flags[] | select(.kind == "changelog-lists-reverted-commit" or .kind == "revert-listed-as-change") | .text' \
  "$RELEASE_INVENTORY" >"$ARTIFACTS/wrong.txt"
jq -r '.flags[] | select(.kind == "shipped-change-not-in-changelog") | .text' "$RELEASE_INVENTORY" >"$ARTIFACTS/missing.txt"
jq -r '.flags[] | select(.kind == "not-conventional") | .text' "$RELEASE_INVENTORY" >"$ARTIFACTS/ignored.txt"

n_entries=$(jq '[.changelog[] | length] | add // 0' "$RELEASE_INVENTORY")
detail "- The changelog has $n_entries entries in $(jq -r '.changelog | keys | join(", ")' "$RELEASE_INVENTORY")."
if [ -s "$ARTIFACTS/ignored.txt" ]; then
  detail "- release-please ignores these commits, because their titles are not conventional commits:"
  sed 's/^/  - /' "$ARTIFACTS/ignored.txt" >>"$ARTIFACTS/details.md"
fi

if [ -s "$ARTIFACTS/wrong.txt" ]; then
  detail "- The changelog lists changes that this release does not ship:"
  sed 's/^/  - /' "$ARTIFACTS/wrong.txt" >>"$ARTIFACTS/details.md"
  if [ -s "$ARTIFACTS/missing.txt" ]; then
    detail "- Shipped changes that the changelog does not list:"
    sed 's/^/  - /' "$ARTIFACTS/missing.txt" >>"$ARTIFACTS/details.md"
  fi
  recommend "Correct the release notes with a release-please commit override. In the description of each merged PR, add a block from \`BEGIN_COMMIT_OVERRIDE\` to \`END_COMMIT_OVERRIDE\` that holds the correct commit message. For a commit and its revert, give both a type that the changelog hides, for example \`chore:\`. Then run release-please again, for example with a push to main."
  findings_file "$ARTIFACTS/wrong.txt"
  fail "The changelog lists $(wc -l <"$ARTIFACTS/wrong.txt") change(s) that the release does not ship"
fi

if [ -s "$ARTIFACTS/missing.txt" ]; then
  n=$(wc -l <"$ARTIFACTS/missing.txt")
  detail "- Shipped changes that the changelog does not list:"
  sed 's/^/  - /' "$ARTIFACTS/missing.txt" >>"$ARTIFACTS/details.md"
  findings_file "$ARTIFACTS/missing.txt"
  decide "Release without changelog entries for these $n shipped change(s)?" "$n commit(s) change shipped files, but their titles start with chore, so the changelog hides them"
fi
pass "Every changelog entry ships, and every shipped change has an entry"
