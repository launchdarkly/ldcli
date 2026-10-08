#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"

if [ "$(jq -r '.ci == null' "$PR_META")" = "true" ]; then
  skip "No PR metadata (branch mode without an open PR)"
fi

summary=$(jq -r '
  [.ci[] | {name, state: ((.conclusion // .state // .status // "") | ascii_upcase)}] as $c
  | {
      failing: [$c[] | select(.state | IN("FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR")) | .name] | unique,
      pending: [$c[] | select(.state | IN("QUEUED", "IN_PROGRESS", "PENDING", "WAITING", "REQUESTED", "EXPECTED", "")) | .name] | unique,
      total: ($c | length)
    }' "$PR_META")

failing=$(jq -r '.failing | join(", ")' <<<"$summary")
pending=$(jq -r '.pending | join(", ")' <<<"$summary")
total=$(jq -r '.total' <<<"$summary")
head=$(jq -r '.head_sha[0:7]' "$PR_META")

detail "- CI results are for the PR head \`$head\` as pushed, which may be behind the base branch."
if [ -n "$failing" ]; then
  fail "Failing on PR head $head: $failing"
fi
if [ -n "$pending" ]; then
  incomplete "CI is still running on PR head $head: $pending"
fi
if [ "$total" -eq 0 ]; then
  incomplete "No CI results on PR head $head"
fi
pass "All $total CI checks green on PR head $head"
