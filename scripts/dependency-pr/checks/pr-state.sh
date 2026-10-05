#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"

merge_status=$(jq -r '.merge.status' "$PR_META")
behind=$(jq -r '.behind_by // 0' "$PR_META")
author=$(jq -r '.author // ""' "$PR_META")
base_ref=$(jq -r '.base_ref' "$PR_META")

if [ "$behind" -gt 0 ]; then
  detail "- Branch is $behind commit(s) behind \`$base_ref\`; checks ran on the PR merged into the current \`$base_ref\`."
fi

if [ "$merge_status" = "conflict" ]; then
  files=$(jq -r '.merge.conflicts | join(", ")' "$PR_META")
  detail "- Conflicting files: $files. Checks ran on the PR head as-is, compared with its merge base."
  recommend "Rebase the PR (comment \`@dependabot rebase\`) to resolve conflicts with \`$base_ref\`."
  fail "Conflicts with $base_ref ($files)"
fi

problems=()
other=$(jq -r '.other_files | join(", ")' "$CLASSIFICATION")
if [ -n "$other" ]; then
  detail "- Files outside dependency manifests: $other"
  problems+=("touches non-manifest files: $other")
fi
case "$author" in
  app/dependabot | dependabot\[bot\] | dependabot | "") ;;
  *) problems+=("author is $author, not Dependabot") ;;
esac

if [ ${#problems[@]} -gt 0 ]; then
  warn "$(join_by '; ' "${problems[@]}")"
fi
pass "Merges cleanly into $base_ref; only dependency manifests changed"
