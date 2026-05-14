---
phase: 04-stop-dismiss-finalize-papercuts
plan: 02
subsystem: rollouts-beta
tags: [dismiss-regression, vertical-slice, semantic-patch, cobra, eventual-consistency, pc-007]
requires:
  - 04-01 envelope (BuildUIURL, NewRolloutEnvelopeWithUI, NewErrorEnvelope)
  - 04-01 Client.Stop (pattern template for DismissRegression)
  - 04-01 RolloutsClient concrete shape (stop.go split-file precedent)
  - 01 Client.List + Client.Get + mapAPIError + mapTransportError
  - 01 ErrCodeNoRolloutsFound (reused — no new constant)
  - 02 SemanticPatch body shape + setStartHeaders
provides:
  - cmd/flags/rollouts/dismiss.go:NewDismissCmd
  - cmd/flags/rollouts/plaintext.go:RenderRolloutDismissPlaintext
  - internal/rollouts/dismiss.go:RolloutsClient.DismissRegression (bounded-backoff polling loop)
  - internal/rollouts.Client.DismissRegression (interface, 3-value return)
  - internal/rollouts.MockClient.DismissRegression
  - internal/rollouts.ErrCodeNoActiveRegression
  - internal/rollouts.DismissRegressionInstruction (fleshed out from stub)
affects:
  - cmd/flags/rollouts/rollouts.go (added dismiss-regression verb)
  - internal/rollouts/client.go (DismissRegression added to Client interface)
  - internal/rollouts/instructions.go (DismissRegressionInstruction doc comment + PC-007 ref)
  - internal/rollouts/errors.go (ErrCodeNoActiveRegression added)
  - internal/rollouts/mock_client.go (MockClient.DismissRegression added)
tech-stack:
  added: []
  patterns:
    - pre-read-then-mutate (List(Limit:1) before PATCH — SC#3/STOP-04)
    - bounded-backoff-polling (PC-007 workaround: 1s/3s/5s Get loop after 204 PATCH)
    - context-cancellation-aware-polling (select+time.After, NOT time.Sleep)
    - meta.warnings for non-fatal eventual-consistency notice (3-value return threads through)
    - UI permalink in meta.uiURL (SC#4 — reuses Plan 04-01 BuildUIURL helper)
key-files:
  created:
    - cmd/flags/rollouts/dismiss.go
    - cmd/flags/rollouts/dismiss_test.go
    - internal/rollouts/dismiss.go
  modified:
    - cmd/flags/rollouts/plaintext.go
    - cmd/flags/rollouts/rollouts.go
    - internal/rollouts/client.go
    - internal/rollouts/errors.go
    - internal/rollouts/instructions.go
    - internal/rollouts/mock_client.go
decisions:
  - D-dismiss-01: DismissRegression implementation in internal/rollouts/dismiss.go (new file) rather than appended to client.go — mirrors start.go/stop.go split-file pattern
  - D-dismiss-02: Bounded-backoff schedule is 1s + 3s + 5s (~9s total); derived from RESEARCH.md architecture Anti-Pattern 3 guidance; Plan 04-03 smoke measures empirically
  - D-dismiss-03: 3-value return signature (*Rollout, []string, error) threads warnings from the polling loop all the way to meta.warnings without requiring a wrapper struct
  - D-dismiss-04: No --rollout-id flag (prototype scope); dismissal always targets the most-recent rollout per pre-read List(Limit:1)
metrics:
  duration_minutes: ~20
  completed_date: 2026-05-14
  commits: 3
  tasks: 3
  files_changed: 9
  lines_added: ~800
---

# Phase 04 Plan 02: Dismiss-Regression Command Vertical Slice Summary

**One-liner:** Ship `ldcli flags rollouts-beta dismiss-regression --flag <key> --environment <env>` end-to-end with pre-read no-active-regression refusal (SC#3), no-rollouts-found guard (SC#3), bounded-backoff PC-007 polling loop (SC#2), meta.warnings on timeout (SC#2), meta.uiURL confirmation envelope (SC#4), and 7-scenario test suite — zero new external dependencies, prototype-first framing held throughout.

## What Shipped

**Three commits, three tasks, all tests green.**

| Task | Commit | Subject |
| ---- | ------ | ------- |
| 1    | `1897497` | feat(04-02): extend rollouts substrate for dismiss-regression verb |
| 2    | `dfae8e2` | feat(04-02): add dismiss-regression command + plaintext renderer + rollouts.go wiring |
| 3    | `8a9d017` | test(04-02): dismiss-regression — 7 scenarios for SC#2/3/4 coverage |

### Surface area

- **CLI:** `ldcli flags rollouts-beta dismiss-regression --flag <key> --project <proj> --environment <env>`
- **JSON output (success, dismissal landed):** `{schemaVersion: "rollouts.v1beta1", kind: "Rollout", data: <Rollout>, meta: {fetchedAt, uiURL}}` — `meta.uiURL` populated per SC#4; `meta.warnings` absent
- **JSON output (success, timeout):** Same envelope shape + `meta.warnings: ["Dismissal patch succeeded but...PC-007..."]` — exit 0 (PATCH succeeded; the eventual-consistency window is upstream's behavior)
- **JSON output (error):** `{schemaVersion, kind: "Error", error: {code, message, nextAction}}` on stdout — AGENT-04/D-07 routing same as all prior verbs
- **Plaintext output:** "Dismissed regression on rollout `<id>` (`<kind>`) in environment `<env>`\nStatus: `<kind>`\n[Label: `<label>`]"; timeout warnings routed to stderr
- **New error codes:**
  - `no_active_regression` — emitted by CLI pre-read guard when Status.Kind is NOT "regressed"
  - `no_rollouts_found` — reuse of Phase 3 constant (no new constant needed)

### Files

- **New:** `cmd/flags/rollouts/dismiss.go` (~160 lines — NewDismissCmd, initDismissFlags, dismissRunE, emitDismissSuccess, emitDismissError), `cmd/flags/rollouts/dismiss_test.go` (~270 lines — 7 test scenarios), `internal/rollouts/dismiss.go` (~125 lines — RolloutsClient.DismissRegression with bounded-backoff loop).
- **Modified:** `cmd/flags/rollouts/plaintext.go` (+RenderRolloutDismissPlaintext), `cmd/flags/rollouts/rollouts.go` (registered NewDismissCmd after NewStopCmd), `internal/rollouts/client.go` (DismissRegression added to Client interface with 3-value return), `internal/rollouts/errors.go` (+ErrCodeNoActiveRegression), `internal/rollouts/instructions.go` (DismissRegressionInstruction doc comment + PC-007 reference), `internal/rollouts/mock_client.go` (+MockClient.DismissRegression with nil-safe *Rollout and []string extraction).

### Bounded-backoff polling loop (PC-007 workaround)

The upstream `dismissRegression` PATCH returns 204 No Content with no body (PAPERCUT PC-007). The CLI works around this with:

1. PATCH → 204 (discard body)
2. `List(Limit:1, env-filtered)` → initial re-fetch to get current state
3. If `Status.Kind != "regressed"` → return immediately (no backoff needed)
4. If still regressed → loop: 1s wait → `Get(rolloutID)` → check; 3s wait → `Get` → check; 5s wait → `Get` → check
5. If still regressed after 5s poll → return stale rollout + `[]string{"...PC-007..."}` warnings (success exit)

The loop is context-cancellation aware (`select { case <-ctx.Done(): ... case <-time.After(wait): ... }`). The budget was NOT made configurable (prototype scope per `project_prototype_first_framing.md` memory).

## Test Results

```
$ go test ./cmd/flags/rollouts/... -run TestDismiss -count=1 -v
=== RUN   TestDismiss_HappyPath_JSONOutput           --- PASS
=== RUN   TestDismiss_HappyPath_PlaintextOutput      --- PASS
=== RUN   TestDismiss_NoActiveRegression_RefusalEnvelope         --- PASS
=== RUN   TestDismiss_NoActiveRegression_PausedState_RefusalEnvelope --- PASS
=== RUN   TestDismiss_NoRolloutsFound_ErrorEnvelopeOnStdout      --- PASS
=== RUN   TestDismiss_EventualConsistencyTimeout_WarningInEnvelope --- PASS
=== RUN   TestDismiss_UpstreamForbidden_PassesThroughExistingMapping --- PASS
PASS
ok  	github.com/launchdarkly/ldcli/cmd/flags/rollouts	1.075s

$ go test ./cmd/flags/rollouts/... -count=1
ok  	github.com/launchdarkly/ldcli/cmd/flags/rollouts	3.553s

$ go test ./internal/rollouts/... -count=1
ok  	github.com/launchdarkly/ldcli/internal/rollouts	1.328s

$ go build ./...
(exit 0)
```

All 7 new dismiss tests pass. All existing Phase 1-3 + Plan 04-01 tests remain green.

## Test Scenarios

| # | Scenario | Key assertion |
|---|----------|---------------|
| 1 | HappyPath_JSONOutput | exit 0; kind=Rollout; data.status.kind=active; meta.uiURL non-empty; meta.warnings empty; AssertExpectations (List+DismissRegression) |
| 2 | HappyPath_PlaintextOutput | exit 0; "Dismissed regression on rollout r1"; no schemaVersion leak |
| 3 | NoActiveRegression_Active | exit 1; error.code="no_active_regression" (literal); message has ID+"active"; nextAction mentions status; DismissRegression not called |
| 4 | NoActiveRegression_Paused | exit 1; error.code="no_active_regression" (literal); message mentions "paused"; proves ANY non-regressed state triggers refusal |
| 5 | NoRolloutsFound | exit 1; error.code="no_rollouts_found" (literal); nextAction mentions list; DismissRegression not called |
| 6 | EventualConsistencyTimeout | exit 0; kind=Rollout (not Error); data.status.kind=regressed (stale verbatim); meta.warnings[0] contains "PC-007"; meta.uiURL non-empty |
| 7 | UpstreamForbidden | exit 1; error.code="forbidden" (literal); proves Phase 1 mapAPIError reused verbatim |

## Deviations from Plan

**None — plan executed exactly as written.**

One structural note: `RolloutsClient.DismissRegression` was placed in `internal/rollouts/dismiss.go` (a new file) rather than appended to `internal/rollouts/client.go`. This mirrors the established split-file pattern (start.go, stop.go) and was the correct approach per the plan's guidance on using the stop.go structural template.

## PAPERCUT Annotation Sites

- `internal/rollouts/dismiss.go` line 85: `// PAPERCUT: PC-007 — upstream returns 204 No Content with no state; the CLI does an explicit re-fetch loop to surface the post-dismiss state.`
- `internal/rollouts/dismiss.go` line 100 (polling loop comment block): `// PC-007 polling loop: backoff 1s, 3s, 5s (cumulative ~9s, capped at 10s) then give up.`
- `internal/rollouts/instructions.go` DismissRegressionInstruction doc comment: references PC-007.

## Open Questions for Plan 04-03 Smoke

1. **Is the polling budget right?** The 1s/3s/5s (~9s) schedule was derived from RESEARCH.md architecture guidance; real-staging smoke will measure whether the dismissal typically propagates in <1s, 1-4s, or 4-10s.
2. **Does the upstream instruction body really have no fields besides Kind?** Architecture research suggested "empty-besides-kind" but this was NOT verified against real staging. If Plan 04-03 smoke shows a `metricKey` or `rolloutId` body field is required, add to DismissRegressionInstruction and log as new papercut.
3. **What is the actual post-dismiss `Status.Kind`?** The tests use "active" as the expected post-dismiss state, but the real API behavior is unverified. Plan 04-03 will confirm.
4. **Does the BuildUIURL path shape match the real LD UI?** The URL shape was not verified against real staging; Plan 04-03 smoke will test the `meta.uiURL` value.

## Known Stubs

None. All functionality is fully wired — the bounded-backoff loop, warnings passthrough, and error refusals are all covered by tests.

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes beyond what the plan's `<threat_model>` documents.

## Self-Check: PASSED

- [x] `cmd/flags/rollouts/dismiss.go` exists with `func NewDismissCmd(client rollouts.Client) *cobra.Command`, Use:"dismiss-regression", 3 required flags (--flag/--project/--environment, NO --to-variation), pre-read List call before DismissRegression, no-active-regression guard (ErrCodeNoActiveRegression when Status.Kind != "regressed"), no-rollouts-found guard (ErrCodeNoRolloutsFound), BuildUIURL + NewRolloutEnvelopeWithUI + meta.warnings wiring on success path.
- [x] `cmd/flags/rollouts/dismiss_test.go` contains 7 `TestDismiss_*` functions; 2 assert literal "no_active_regression"; 1 asserts literal "no_rollouts_found"; 1 asserts "PC-007" in meta.warnings; 1 asserts meta.uiURL non-empty; 3 assert DismissRegression not called (pre-read guard proofs).
- [x] `internal/rollouts/dismiss.go` exists with `func (c RolloutsClient) DismissRegression(...)` implementation using setStartHeaders and bounded-backoff loop with `time.After` + `ctx.Done` annotated `// PAPERCUT: PC-007`.
- [x] `internal/rollouts/mock_client.go` contains `func (c *MockClient) DismissRegression(...)` with 6 named args via `c.Called`, nil-safe extraction for *Rollout and []string, 3-value return.
- [x] `internal/rollouts/errors.go` contains `ErrCodeNoActiveRegression = "no_active_regression"` inside the existing const block.
- [x] `internal/rollouts/client.go` Client interface includes `DismissRegression` with 3-value return signature.
- [x] `internal/rollouts/instructions.go` DismissRegressionInstruction has doc comment referencing PC-007.
- [x] `cmd/flags/rollouts/plaintext.go` contains `func RenderRolloutDismissPlaintext` with "Dismissed regression on rollout" header.
- [x] `cmd/flags/rollouts/rollouts.go` contains `cmd.AddCommand(NewDismissCmd(client))` after NewStopCmd.
- [x] `go build ./...` exits 0.
- [x] `go test ./cmd/flags/rollouts/... -count=1` exits 0.
- [x] `go test ./internal/rollouts/... -count=1` exits 0.
- [x] Binary advertises `dismiss-regression` under `flags rollouts-beta --help`.
- [x] `dismiss-regression --help` advertises `--flag`, `--project`, `--environment`; NO `--to-variation`.
- [x] All 3 commits exist in git history: 1897497, dfae8e2, 8a9d017.
