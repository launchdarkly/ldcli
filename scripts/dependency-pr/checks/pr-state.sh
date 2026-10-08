#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"

merge_status=$(jq -r '.merge.status' "$PR_META")
behind=$(jq -r '.behind_by // 0' "$PR_META")
author=$(jq -r '.author // ""' "$PR_META")
base_ref=$(jq -r '.base_ref' "$PR_META")

if [ "$merge_status" = "conflict" ]; then
  files=$(jq -r '.merge.conflicts | join(", ")' "$PR_META")
  [ "$behind" -gt 0 ] && detail "- Branch is $behind commit(s) behind \`$base_ref\`."
  detail "- Conflicting files: $files. The PR does not merge into \`$base_ref\`, so the checks ran on the PR head as it is, compared with its merge base."
  recommend "Rebase the PR (comment \`@dependabot rebase\`) to resolve conflicts with \`$base_ref\`."
  FIX_KIND=comment fix_recipe dependabot-rebase "@dependabot rebase"
  fail "Conflicts with $base_ref ($files)"
fi

if [ "$behind" -gt 0 ]; then
  detail "- Branch is $behind commit(s) behind \`$base_ref\`. The checks ran on the PR merged into the current \`$base_ref\`."
fi

# Generated outputs are compared with a fresh build by the drift checks, so
# they need no human review here.
generated_re='^(internal/dev_server/ui/dist/|internal/dev_server/api/server\.gen\.go$|cmd/resources/resource_cmds\.go$|.*/mocks?/|.*mocks?\.go$)'
other=$(jq -r '.other_files[]' "$CLASSIFICATION" | grep -vE "$generated_re" | paste -sd, - | sed 's/,/, /g')
generated=$(jq -r '.other_files[]' "$CLASSIFICATION" | grep -E "$generated_re" | paste -sd, - | sed 's/,/, /g')
[ -n "$generated" ] && detail "- Generated files in the PR (compared with a fresh build by the drift checks): $generated"
case "$author" in
  app/dependabot | dependabot\[bot\] | dependabot | "") ;;
  *) detail "- The author is $author, not Dependabot." ;;
esac

if [ -n "$other" ]; then
  detail "- Files outside dependency manifests: $other"
  decide "Accept the changes to files outside the dependency manifests ($other)?" "The PR changes $other in addition to the dependency update."
fi
if [ -n "$generated" ]; then
  info "Merges cleanly into $base_ref; changes manifests and generated files only"
fi
pass "Merges cleanly into $base_ref; only dependency manifests changed"
