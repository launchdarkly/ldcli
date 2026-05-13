---
phase: 02-start-a-rollout
plan: "01"
subsystem: rollouts
tags: [rollouts, cobra, semantic-patch, prerequisites]
dependency_graph:
  requires: []
  provides:
    - internal/rollouts/instructions.go:SemanticPatch.EnvironmentKey
    - internal/rollouts/instructions.go:StartInstruction (full Phase 2 shape)
    - internal/rollouts/instructions.go:StageInput
    - internal/rollouts/instructions.go:MetricSource
    - internal/rollouts/instructions.go:MetricMonitoringPref
    - cmd/cliflags/flags.go:StagesFlag
    - cmd/cliflags/flags.go:TargetVariationFlag
    - cmd/cliflags/flags.go:OriginalVariationFlag
    - cmd/cliflags/flags.go:RandomizationUnitFlag
    - cmd/cliflags/flags.go:PauseOnRegressionFlag
    - cmd/cliflags/flags.go:RevertOnRegressionFlag
    - cmd/cliflags/flags.go:RuleIDFlag
  affects:
    - Plan 02-02 (consumes all of the above; can now compile without touching these files)
tech_stack:
  added: []
  patterns:
    - PAPERCUT comments (PC-010, PC-012, PC-013, PC-014) co-located with struct fields
key_files:
  created: []
  modified:
    - internal/rollouts/instructions.go
    - cmd/cliflags/flags.go
    - .planning/STATE.md
  deleted:
    - internal/rollouts/idempotency.go
decisions:
  - SemanticPatch.EnvironmentKey added without omitempty (server requires it for routing)
  - IsGroup kept out of MetricSource entirely (no comment reference either) per D-06
  - ExtensionDurationMillis kept out of StartInstruction per Q5 recommendation
  - Flag constants inserted alphabetically within existing const block
metrics:
  duration: "~8 minutes"
  completed_date: "2026-05-13"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 3
  files_deleted: 1
---

# Phase 02 Plan 01: Prerequisites for Start-a-Rollout Summary

Three small prerequisite changes that Plan 02-02 (the vertical Start slice) depends on: SemanticPatch.EnvironmentKey added and StartInstruction fleshed out in `instructions.go`; idempotency dead code deleted; seven CLI flag constants registered in `cliflags/flags.go`.

## What Was Built

### Task 1: Fix SemanticPatch + flesh out StartInstruction (commit 2ed0d55)

`internal/rollouts/instructions.go` — replaced the Phase 1 stubs with production-ready types:

**SemanticPatch** — added `EnvironmentKey string \`json:"environmentKey"\`` as the first field (before `Comment`). The server resolves environment routing from `request.Body.EnvironmentKey` per gonfalon `instruction_start_automated_release.go`. This was Pitfall 2 in the research — omitting it would have caused silent wrong-env or 400 responses.

**StartInstruction** — replaced the single-field stub with the full Phase 2 shape:
- `kind` (always "startAutomatedRelease")
- `releaseKind` (PAPERCUT PC-012: in request vs `kind` in response)
- `originalVariationId` (PAPERCUT PC-013: unified name; UUID _id only — NOT variation key per Q1)
- `targetVariationId` (UUID _id only)
- `randomizationUnit`
- `stages` ([]StageInput)
- `metrics,omitempty` + `metricMonitoringPreferences,omitempty` (PAPERCUT PC-010: parallel collections)
- `ruleId,omitempty` (D-07: empty = fallthrough rule)

**Supporting types added:**
- `StageInput{Allocation int, DurationMillis int64}` — basis-points and millis (PAPERCUT PC-014: CLI converts from percent + Go duration string per D-02/D-03)
- `MetricSource{Key string}` — metric groups (`isGroup`) deferred to v1.1 per D-06
- `MetricMonitoringPref{AutoRollback bool}` — false = pause, true = revert per D-04

`StopInstruction` and `DismissRegressionInstruction` stubs left unchanged for Phase 4.

### Task 2: Delete idempotency.go + update STATE.md (commit 8ffd3ca)

- Deleted `internal/rollouts/idempotency.go` via `git rm`. The sole export `SetIdempotencyKey` had zero callers anywhere in the codebase (confirmed by grep before deletion). D-10 (Idempotency-Key out of scope for entire project) is now honored in code.
- Updated `.planning/STATE.md` stack-research decision row: removed the `google/uuid (already vendored) for Idempotency-Key` clause. `go-retryablehttp@v0.7.7` and `golang.org/x/term` entries preserved.

### Task 3: Phase 2 flag constants in cmd/cliflags/flags.go (commit 3b48e7b)

Added 14 new constants (7 flag names + 7 paired descriptions) in alphabetical order within the existing `const` block:

| Constant | Value |
|----------|-------|
| `OriginalVariationFlag` | `"original-variation"` |
| `PauseOnRegressionFlag` | `"pause-on-regression"` |
| `RandomizationUnitFlag` | `"randomization-unit"` |
| `RevertOnRegressionFlag` | `"revert-on-regression"` |
| `RuleIDFlag` | `"rule-id"` |
| `StagesFlag` | `"stages"` |
| `TargetVariationFlag` | `"target-variation"` |

Description strings encode load-bearing substrings: "UUID (_id)" for variation flags (Q1 UUID-only requirement), "Comma-separated list of stages" with basis-point/millis translation note, "Existing rule UUID" for RuleIDFlag, and "A metric cannot appear in both flags" for both regression flags (D-04 mutual-exclusivity).

`AllFlagsHelp()` is unchanged — the new constants are per-command flags not suitable for config persistence.

## Evidence of Non-Breaking Changes

```
go build ./...              → OK (3 commits)
go test ./internal/rollouts/ → ok (0.661s)
go test ./cmd/flags/rollouts/ → ok (1.402s)
gofmt -l instructions.go flags.go → (no output — clean)
```

## Pointers for Plan 02-02

Plan 02-02 (`internal/rollouts/start.go` + `cmd/flags/rollouts/start.go`) can now:

1. Build `SemanticPatch{EnvironmentKey: envKey, Instructions: []interface{}{instr}}` — `EnvironmentKey` is now at the top of the struct with JSON tag `"environmentKey"`.

2. Populate `StartInstruction` with all fields — exact JSON tags:
   `"kind"`, `"releaseKind"`, `"originalVariationId"`, `"targetVariationId"`, `"randomizationUnit"`, `"stages"`, `"metrics,omitempty"`, `"metricMonitoringPreferences,omitempty"`, `"ruleId,omitempty"`

3. Call `cmd.Flags().String(cliflags.StagesFlag, ...)`, `cmd.Flags().StringArray(cliflags.PauseOnRegressionFlag, ...)`, etc. — all 7 flag constants are available.

4. Reference `cliflags.StagesFlagDescription`, `cliflags.OriginalVariationFlagDescription`, etc. in `initStartFlags`.

## Deviations from Plan

None — plan executed exactly as written.

One minor note: the plan's `<verify>` command for Task 1 checked `grep -c 'IsGroup' ... | grep -q '^0$'` (no IsGroup anywhere in the file). The initial write included "IsGroup is intentionally omitted" in the MetricSource doc comment, which triggered that check. Fixed by rewriting the comment to omit the word "IsGroup" entirely — result: `MetricSource` is clean per D-06 with no reference to the deferred field.

## Known Stubs

- `StopInstruction{Kind string}` — single-field stub, Phase 4 fleshes out
- `DismissRegressionInstruction{Kind string}` — single-field stub, Phase 4 fleshes out

These are intentional per-plan stubs, not blocking the Phase 2 goal.

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries introduced in this plan. All changes are struct field additions, constant declarations, and a file deletion.

## Self-Check: PASSED

Files created/modified:
- `internal/rollouts/instructions.go` — FOUND
- `cmd/cliflags/flags.go` — FOUND
- `.planning/STATE.md` — FOUND

Files deleted:
- `internal/rollouts/idempotency.go` — confirmed absent (`! test -e` passes)

Commits:
- `2ed0d55` — FOUND (feat(02-01): fix SemanticPatch.EnvironmentKey + flesh out StartInstruction)
- `8ffd3ca` — FOUND (chore(02-01): delete idempotency.go and strike google/uuid from STATE.md)
- `3b48e7b` — FOUND (feat(02-01): add Phase 2 flag constants to cmd/cliflags/flags.go)
