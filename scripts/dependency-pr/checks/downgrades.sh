#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"

downs=$(jq -r '[.updates[] | select(.semver == "downgrade") | "\(.name) \(.from) → \(.to)"] | join(", ")' "$CLASSIFICATION")
if [ -n "$downs" ]; then
  detail "- Downgrades usually mean the branch is stale and the base branch already moved past these versions."
  FIX_KIND=comment fix_recipe dependabot-rebase "@dependabot rebase"
  recommend "Rebase the PR (comment \`@dependabot rebase\`) or drop the stale pins that would now be downgrades."
  fail "Downgrades: $downs"
fi
pass "No downgrades"
