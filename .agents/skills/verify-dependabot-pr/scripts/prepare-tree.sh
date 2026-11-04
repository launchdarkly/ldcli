#!/usr/bin/env bash
# Merges a pull request onto the latest main in a temporary worktree, so checks
# run against what main would look like after merging. Never pushes.
#
#   scripts/prepare-tree.sh <pr-number>
#
# Run it from inside the ldcli checkout, after sourcing isolate.sh.
# Prints the worktree path on the last line of output.
#
# Exit codes:
#   0  the worktree is ready
#   1  usage or setup error
#   2  the PR doesn't merge cleanly onto main

set -euo pipefail

pr="${1:-}"
if ! [[ "$pr" =~ ^[0-9]+$ ]]; then
  echo "usage: $0 <pr-number>" >&2
  exit 1
fi
if [ -z "${SMOKE_DIR:-}" ] || [ ! -d "$SMOKE_DIR" ]; then
  echo "SMOKE_DIR isn't set. Run: source scripts/isolate.sh" >&2
  exit 1
fi

remote="${VERIFY_REMOTE:-origin}"
repo="$(git rev-parse --show-toplevel)"
tree="$SMOKE_DIR/tree"
main_ref="refs/verify/main"
pr_ref="refs/verify/pr-$pr"

if [ -e "$tree" ]; then
  echo "$tree already exists. Run scripts/cleanup.sh first." >&2
  exit 1
fi

# Named refs rather than FETCH_HEAD: FETCH_HEAD only records the first ref of a
# multi-ref fetch, and each worktree has its own.
git -C "$repo" fetch --quiet "$remote" "+main:$main_ref" "+pull/$pr/head:$pr_ref"

read -r behind ahead < <(git -C "$repo" rev-list --left-right --count "$main_ref...$pr_ref")
main_sha="$(git -C "$repo" rev-parse --short "$main_ref")"
pr_sha="$(git -C "$repo" rev-parse --short "$pr_ref")"
echo "PR #$pr ($pr_sha) is $behind commit(s) behind main ($main_sha) and $ahead ahead."

git -C "$repo" worktree add --quiet --detach "$tree" "$main_ref"

# The merge commit stays local, so a placeholder identity is fine.
if ! git -C "$tree" -c user.name="dependabot verification" -c user.email="verify@localhost" \
  merge --quiet --no-ff --no-edit "$pr_ref" >/dev/null 2>&1; then
  echo "PR #$pr doesn't merge cleanly onto main. Conflicting files:" >&2
  git -C "$tree" diff --name-only --diff-filter=U | sed 's/^/  /' >&2
  git -C "$tree" merge --abort
  exit 2
fi

echo "Tested tree: PR #$pr merged onto main at $main_sha"
echo "$tree"
