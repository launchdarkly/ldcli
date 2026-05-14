---
phase: 02-start-a-rollout
created: 2026-05-13
---

# Phase 2 — Follow-ups (deferred, not blocking)

## Variation resolution ergonomics (Option A — hybrid)

`--target-variation` / `--original-variation` currently accept the variation **UUID** only.
Eventually we want to accept variation **values** (e.g. `true` / `false` for booleans, or
variation **name/key** for multivariate flags) and resolve to UUID via a flag GET when the
input is not UUID-shaped. UUID-shaped inputs pass through unchanged so scripted/agent use
pays zero pre-fetch cost.

**Why deferred:** Phase 2 ships works-but-clunky. Confirmed acceptable for now (2026-05-13).

**Sketch:**
- In `cmd/flags/rollouts/start.go` (or a helper), if `value` matches `^[0-9a-f-]{36}$` → use as-is.
- Otherwise: GET the flag, build `name|key|stringified-value → _id` map, look up, fail with
  `usage error` listing available variations if not found.
- Add a single round-trip for the human path; agent/CI path unaffected.

**Connects to:**
- CONTEXT.md "Claude's Discretion" item (variation keys vs IDs).
- D-12 discouraged pre-fetch *for error detection* — this is input resolution, a different concern.
- 02-REVIEW.md does not flag this; surfaced via human review on 2026-05-13.
