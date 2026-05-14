---
status: partial
phase: 02-start-a-rollout
source: [02-VERIFICATION.md]
started: 2026-05-14T00:32:00Z
updated: 2026-05-14T00:32:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Guarded rollout end-to-end
expected: Run on an account with guarded releases enabled — `data.kind = "guarded"`, `data.metricMonitoringPreferences` populated with metric key + `autoRollback: false` in returned rollout envelope; exit 0.
result: [pending]

### 2. ErrCodeInvalidVariation via originalVariationId server message
expected: Supply a non-existent UUID-shaped variation ID as `--original-variation` against a server that returns the literal message `"originalVariationId must be a valid variation id"` (not a 500) — `error.code = "invalid_variation"`, error envelope on stdout, exit 1.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
