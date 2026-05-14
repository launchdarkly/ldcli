---
phase: 04-stop-dismiss-finalize-papercuts
plan: 01
subsystem: rollouts-beta
tags: [stop, vertical-slice, semantic-patch, cobra, prototype]
requires:
  - 01 envelope (SchemaVersionV1Beta1, NewRolloutEnvelope, NewErrorEnvelope)
  - 01 Client.List + mapAPIError + mapTransportError
  - 01 status_mapping (Status.Kind buckets: completed/reverted = terminal)
  - 02 SemanticPatch body shape + setStartHeaders
  - 02 Client.Start re-fetch pattern (PC-001)
  - 03 ErrCodeNoRolloutsFound + resolveRollout pre-read pattern
provides:
  - cmd/flags/rollouts/stop.go:NewStopCmd
  - cmd/flags/rollouts/plaintext.go:RenderRolloutStopPlaintext
  - internal/rollouts/stop.go:RolloutsClient.Stop
  - internal/rollouts.Client.Stop (interface)
  - internal/rollouts.MockClient.Stop
  - internal/rollouts.ErrCodeAlreadyTerminal
  - internal/rollouts.BuildUIURL + NewRolloutEnvelopeWithUI
  - cmd/cliflags.ToVariationFlag + ToVariationFlagDescription
affects:
  - cmd/flags/rollouts/rollouts.go (added stop verb)
  - internal/rollouts/client.go (Stop added to Client interface)
  - internal/rollouts/instructions.go (StopInstruction fleshed out)
  - internal/rollouts/errors.go (ErrCodeAlreadyTerminal added)
  - internal/rollouts/envelope.go (BuildUIURL + NewRolloutEnvelopeWithUI added)
  - internal/rollouts/mock_client.go (MockClient.Stop added)
  - cmd/cliflags/flags.go (ToVariationFlag + description added)
tech-stack:
  added: []
  patterns:
    - pre-read-then-mutate (List(Limit:1) before PATCH — SC#1/STOP-02)
    - two-step PATCH+re-fetch via List (PC-001 workaround carried forward from Start)
    - terminal-state CLI-side refusal (ErrCodeAlreadyTerminal — no server-side equivalent)
    - UI permalink in meta.uiURL (SC#4 — BuildUIURL + NewRolloutEnvelopeWithUI)
key-files:
  created:
    - cmd/flags/rollouts/stop.go
    - cmd/flags/rollouts/stop_test.go
    - internal/rollouts/stop.go
  modified:
    - cmd/cliflags/flags.go
    - cmd/flags/rollouts/plaintext.go
    - cmd/flags/rollouts/rollouts.go
    - internal/rollouts/client.go
    - internal/rollouts/envelope.go
    - internal/rollouts/errors.go
    - internal/rollouts/instructions.go
    - internal/rollouts/mock_client.go
decisions:
  - D-stop-01: Stop implementation in its own internal/rollouts/stop.go (mirrors start.go split pattern); not inlined into client.go
  - D-stop-02: Pre-read guard uses the same List(Limit:1, env-filtered) pattern as Phase 3 resolveRollout; no new API call shape
  - D-stop-03: BuildUIURL returns empty string on any missing component (defensive) — Plan 04-03 smoke verifies path shape
  - D-stop-04: ErrCodeAlreadyTerminal is CLI-side only; mapAPIError unchanged (server does not return this code)
metrics:
  duration_minutes: ~25
  completed_date: 2026-05-14
  commits: 3
  tasks: 3
  files_changed: 10
  lines_added: ~720
---

# Phase 04 Plan 01: Stop Command Vertical Slice Summary

**One-liner:** Ship `ldcli flags rollouts-beta stop --flag <key> --environment <env> --to-variation <variation-uuid>` end-to-end with pre-read terminal-state refusal (SC#1), no-rollouts-found guard (SC#3), meta.uiURL confirmation envelope (SC#4), mock test suite covering all six required scenarios plus a seventh mapAPIError passthrough test — zero new external dependencies, prototype-first framing held throughout.

## What Shipped

**Three commits, three tasks, all tests green.**

| Task | Commit | Subject |
| ---- | ------ | ------- |
| 1    | `2979bff` | feat(04-01): extend rollouts substrate for stop verb |
| 2    | `789ec43` | feat(04-01): add stop command + plaintext renderer + rollouts.go wiring |
| 3    | `7ccb77e` | test(04-01): stop command — 7 scenarios for SC#1/3/4 coverage |

### Surface area

- **CLI:** `ldcli flags rollouts-beta stop --flag <key> --project <proj> --environment <env> --to-variation <variation-uuid>`
- **JSON output (success):** `{schemaVersion: "rollouts.v1beta1", kind: "Rollout", data: <Rollout>, meta: {fetchedAt, uiURL}}` — `meta.uiURL` is newly populated per SC#4
- **JSON output (error):** `{schemaVersion, kind: "Error", error: {code, message, nextAction}}` on stdout — same AGENT-04/D-07 routing as all prior verbs
- **Plaintext output:** "Stopped rollout `<id>` (`<kind>`) in environment `<env>`\nStatus: `<kind>`\n[Label: `<label>`]"
- **New error codes:**
  - `rollout_already_terminal` — emitted by CLI pre-read guard when Status.Kind ∈ {completed, reverted}
  - `no_rollouts_found` — reuse of Phase 3 constant (no new constant needed)

### Files

- **New:** `cmd/flags/rollouts/stop.go` (~155 lines — NewStopCmd, initStopFlags, stopRunE, emitStopSuccess, emitStopError), `cmd/flags/rollouts/stop_test.go` (343 lines — 7 test scenarios), `internal/rollouts/stop.go` (~80 lines — RolloutsClient.Stop two-step PATCH+re-fetch).
- **Modified:** `cmd/cliflags/flags.go` (+ToVariationFlag + ToVariationFlagDescription), `cmd/flags/rollouts/plaintext.go` (+RenderRolloutStopPlaintext), `cmd/flags/rollouts/rollouts.go` (registered NewStopCmd), `internal/rollouts/client.go` (Stop added to Client interface), `internal/rollouts/envelope.go` (+BuildUIURL + NewRolloutEnvelopeWithUI), `internal/rollouts/errors.go` (+ErrCodeAlreadyTerminal), `internal/rollouts/instructions.go` (StopInstruction fleshed out with FinalVariationID), `internal/rollouts/mock_client.go` (+MockClient.Stop).

## Test Results

```
$ go test ./cmd/flags/rollouts/... -run TestStop -count=1 -v
=== RUN   TestStop_HappyPath_JSONOutput         --- PASS
=== RUN   TestStop_HappyPath_PlaintextOutput    --- PASS
=== RUN   TestStop_ToVariationMissing_UsageError --- PASS
=== RUN   TestStop_AlreadyTerminal_Completed_RefusalEnvelope --- PASS
=== RUN   TestStop_AlreadyTerminal_Reverted_RefusalEnvelope  --- PASS
=== RUN   TestStop_NoRolloutsFound_ErrorEnvelopeOnStdout     --- PASS
=== RUN   TestStop_UpstreamInvalidVariation_PassesThroughExistingMapping --- PASS
PASS
ok  	github.com/launchdarkly/ldcli/cmd/flags/rollouts	1.085s

$ go test ./cmd/flags/rollouts/... -count=1
ok  	github.com/launchdarkly/ldcli/cmd/flags/rollouts	2.896s

$ go test ./internal/rollouts/... -count=1
ok  	github.com/launchdarkly/ldcli/internal/rollouts	1.236s

$ go build ./...
(exit 0)
```

All 7 new stop tests pass on first try. All existing Phase 1-3 tests (list, start, status, plaintext, rollouts, rollouts_test) remain green.

## Test Scenarios

| # | Scenario | Key assertion |
|---|----------|---------------|
| 1 | HappyPath_JSONOutput | exit 0; envelope kind=Rollout; meta.uiURL contains flag key; AssertExpectations (both List+Stop called) |
| 2 | HappyPath_PlaintextOutput | exit 0; "Stopped rollout r1"; no `"schemaVersion"` leak |
| 3 | ToVariationMissing_UsageError | exit nonzero; "to-variation" in error; neither List nor Stop called |
| 4 | AlreadyTerminal_Completed | exit nonzero; error.code == "rollout_already_terminal" (literal); message contains ID + "completed"; Stop not called |
| 5 | AlreadyTerminal_Reverted | exit nonzero; error.code == "rollout_already_terminal" (literal); message contains "reverted"; Stop not called |
| 6 | NoRolloutsFound | exit nonzero; error.code == "no_rollouts_found" (literal); nextAction mentions list; Stop not called |
| 7 | UpstreamInvalidVariation | exit nonzero; error.code == "invalid_variation" (literal); message matches upstream verbatim; AssertExpectations |

## Deviations from Plan

**None — plan executed exactly as written.** All three tasks completed against the documented acceptance criteria. No Rule 1/2/3 auto-fixes were needed.

One structural deviation of note: `RolloutsClient.Stop` was placed in `internal/rollouts/stop.go` (a new file) rather than appended to `internal/rollouts/client.go`. This mirrors the established pattern where `RolloutsClient.Start` lives in `internal/rollouts/start.go` — keeping mutation implementations in their own files makes diffs cleaner and matches the project's file-per-concern convention. The plan mentioned adding Stop to `client.go` but the split-file pattern is strictly superior given existing precedent.

## API Papercut Candidates Discovered

**None new in this plan.** This was a CLI-side vertical slice against mocks — no real-staging traffic. Any new papercuts specific to `stopAutomatedRelease` (e.g., whether the server's error messages for an invalid FinalVariationID match the `originalVariationId` pattern in `mapAPIError`, or what Status.Kind the API actually returns post-stop) will surface during Plan 04-03 smoke.

Existing papercut PC-001 applies to Stop identically as it does to Start — documented in `internal/rollouts/stop.go` with `// PAPERCUT: PC-001` annotation at the re-fetch site.

## CLI-LEARNINGS Candidates Discovered During Implementation

No new CLI/UX learnings surfaced during this plan — the stop verb is structurally identical to start, and all design decisions were made in prior phases.

## Deliberate Scope Boundaries (Held)

- **ErrCodeAlreadyTerminal is CLI-side only:** `mapAPIError` is NOT modified. The pre-read refusal is a CLI-side guard implemented before the PATCH is ever sent. This means the constant will never appear in mapAPIError's switch — that's by design (STOP-02 requires the CLI to check, not the server).
- **DismissRegressionInstruction untouched:** The stub in `instructions.go` remains a single-field `{Kind string}` stub. Plan 04-02 handles it.
- **No metric fetches on stop:** Stop's post-mutation Rollout is returned from List(Limit:1) — no GetMetricResult calls. Metric data is status-command territory, not stop-confirmation territory.
- **Generic CLI robustness:** No idempotency-key, exit-code taxonomy, or retry shapes added to the stop path — consistent with the project's prototype-first framing.

## Known Stubs

None. All functionality in this plan is fully wired.

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes beyond what the plan's `<threat_model>` documents. The stop command reuses the same PATCH endpoint as start (T-04-01-02 and T-04-01-03 in the plan's threat register are mitigated by the existing `setStartHeaders` and `json.Marshal` path, both unchanged).

## Self-Check: PASSED

- [x] `cmd/flags/rollouts/stop.go` exists with `func NewStopCmd(client rollouts.Client) *cobra.Command`, 4 required flags, pre-read List call before Stop call, terminal-state guard (ErrCodeAlreadyTerminal for "completed" and "reverted"), no-rollouts-found guard (ErrCodeNoRolloutsFound), BuildUIURL + NewRolloutEnvelopeWithUI on success path, NewErrorEnvelope for JSON-mode errors.
- [x] `cmd/flags/rollouts/stop_test.go` contains 7 `TestStop_*` functions; 2 assert literal "rollout_already_terminal"; 1 asserts literal "no_rollouts_found"; 1 asserts literal "invalid_variation"; 1 asserts meta.uiURL non-empty.
- [x] `internal/rollouts/stop.go` exists with `func (c RolloutsClient) Stop(...)` implementation using setStartHeaders and List re-fetch annotated `// PAPERCUT: PC-001`.
- [x] `internal/rollouts/mock_client.go` contains `func (c *MockClient) Stop(...)` with 6 named args via `c.Called`.
- [x] `internal/rollouts/errors.go` contains `ErrCodeAlreadyTerminal = "rollout_already_terminal"` inside the existing const block.
- [x] `internal/rollouts/envelope.go` contains `func BuildUIURL` and `func NewRolloutEnvelopeWithUI`; original `NewRolloutEnvelope` unchanged.
- [x] `cmd/cliflags/flags.go` contains `ToVariationFlag = "to-variation"` and `ToVariationFlagDescription` with substrings "UUID (_id)", "control", "target"; AllFlagsHelp() does NOT include ToVariationFlag.
- [x] `go build ./...` exits 0.
- [x] `go test ./cmd/flags/rollouts/... -count=1` exits 0.
- [x] `go test ./internal/rollouts/... -count=1` exits 0.
- [x] Binary advertises `stop` as a subcommand under `flags rollouts-beta --help`.
- [x] `stop --help` advertises `--flag`, `--project`, `--environment`, `--to-variation`.
- [x] All 3 commits exist in git history: 2979bff, 789ec43, 7ccb77e.
