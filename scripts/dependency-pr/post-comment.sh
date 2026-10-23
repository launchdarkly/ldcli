#!/usr/bin/env bash
# Posts (or updates) the single verifier comment on a PR. This is the only
# script in scripts/dependency-pr that writes to GitHub, and it only comments:
# it never approves, requests changes, or merges. verify.sh never calls it.
#
# Usage: post-comment.sh --out-dir DIR [--repo OWNER/NAME] [--dry-run] [--force]
#   --out-dir DIR  a verify.sh output directory (needs result.json and comment.md)
#   --dry-run      print what would be posted and where, without writing
#   --force        post even if the PR head moved since verification
#
# Exit: 0 posted (or dry run), 1 refused (stale result, not a PR run), 2 error.
set -euo pipefail

OUT="" REPO="" DRY_RUN=false FORCE=false
MARKER="<!-- ldcli-dependency-verify -->"

while [ $# -gt 0 ]; do
  case "$1" in
    --out-dir) OUT="${2:?}"; shift 2 ;;
    --repo) REPO="${2:?}"; shift 2 ;;
    --dry-run) DRY_RUN=true; shift ;;
    --force) FORCE=true; shift ;;
    -h | --help) sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

[ -n "$OUT" ] || { echo "--out-dir is required" >&2; exit 2; }
result="$OUT/result.json" comment="$OUT/comment.md"
[ -f "$result" ] && [ -f "$comment" ] || { echo "missing $result or $comment" >&2; exit 2; }
grep -qF "$MARKER" "$comment" || { echo "$comment lacks the verifier marker" >&2; exit 2; }

pr=$(jq -r '.pr.number // empty' "$result")
[ -n "$pr" ] || { echo "result.json has no PR number (branch run without a PR); nothing to post" >&2; exit 1; }
REPO="${REPO:-$(jq -r '.pr.repo' "$result")}"
verified=$(jq -r '.pr.head_sha' "$result")

current=$(gh pr view "$pr" --repo "$REPO" --json headRefOid --jq .headRefOid) || { echo "cannot read PR #$pr" >&2; exit 2; }
if [ "$current" != "$verified" ] && [ "$FORCE" != true ]; then
  echo "refusing to post: PR #$pr head is now ${current:0:7}, but the result is for ${verified:0:7}. Re-run verify.sh." >&2
  exit 1
fi

existing=$(gh api "repos/$REPO/issues/$pr/comments" --paginate \
  --jq ".[] | select(.body | contains(\"$MARKER\")) | .id" | tail -n1)

if [ "$DRY_RUN" = true ]; then
  if [ -n "$existing" ]; then
    echo "[dry-run] would update comment $existing on $REPO#$pr ($(wc -c <"$comment") bytes)"
  else
    echo "[dry-run] would create a comment on $REPO#$pr ($(wc -c <"$comment") bytes)"
  fi
  exit 0
fi

body=$(jq -Rs '{body: .}' "$comment")
if [ -n "$existing" ]; then
  gh api -X PATCH "repos/$REPO/issues/comments/$existing" --input - <<<"$body" --jq .html_url
else
  gh api -X POST "repos/$REPO/issues/$pr/comments" --input - <<<"$body" --jq .html_url
fi
