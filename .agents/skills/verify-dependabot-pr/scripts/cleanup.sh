#!/usr/bin/env bash
# Removes everything a verification run created: the temporary worktree, the
# refs prepare-tree.sh fetched, any dev-server still running on SMOKE_PORT, and
# SMOKE_DIR itself.
#
#   scripts/cleanup.sh
#
# Run it from the ldcli checkout or the temporary worktree, with isolate.sh's
# variables still set.

set -euo pipefail

if [ -z "${SMOKE_DIR:-}" ]; then
  echo "SMOKE_DIR isn't set, so there's nothing to clean up." >&2
  exit 1
fi
case "$SMOKE_DIR" in
  */ldcli-verify.*) ;;
  *)
    echo "Refusing to delete $SMOKE_DIR: it wasn't created by isolate.sh." >&2
    exit 1
    ;;
esac

# The shared git directory, so this also works when run from inside the
# temporary worktree.
repo="$(git rev-parse --path-format=absolute --git-common-dir)"

if [ -n "${SMOKE_PORT:-}" ] && command -v pkill >/dev/null 2>&1; then
  pkill -f -- "dev-server start --port $SMOKE_PORT" 2>/dev/null || true
fi

if [ -d "$SMOKE_DIR/tree" ]; then
  git -C "$repo" worktree remove --force "$SMOKE_DIR/tree"
fi
git -C "$repo" worktree prune

git -C "$repo" for-each-ref --format='%(refname)' refs/verify/ | while read -r ref; do
  git -C "$repo" update-ref -d "$ref"
done

# The Go module cache and npm leave read-only files behind.
chmod -R u+w "$SMOKE_DIR" 2>/dev/null || true
rm -rf "$SMOKE_DIR"

echo "Removed $SMOKE_DIR, the verification worktree, and refs/verify/*."
