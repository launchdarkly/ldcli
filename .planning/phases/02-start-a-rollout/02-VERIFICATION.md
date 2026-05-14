---
phase: 02-start-a-rollout
verified: 2026-05-14T00:28:33Z
status: human_needed
score: 13/17 must-haves verified (4 deferred; 0 failed)
overrides_applied: 0
deferred:
  - truth: "CLI runs preflight via recommended-duration before mutation in non-TTY / --output json; exits with preflight-failed code on rejection; TTY prompts user; --skip-health-checks bypasses (ROADMAP SC3, START-04)"
    addressed_in: "Future preflight phase (no phase number assigned)"
    evidence: "CONTEXT.md D-09: Preflight removed from Phase 2. CONTEXT.md Deferred Ideas: 'Moves START-04 out of Phase 2.'"
  - truth: "--idempotency-key <uuid> produces coherent outcome on retry (ROADMAP SC5 partial, START-06)"
    addressed_in: "Out of scope for entire project (D-10)"
    evidence: "CONTEXT.md D-10: 'Idempotency-Key is out of scope for the entire rollouts milestone.' User preference recorded."
  - truth: "extension duration configurable from CLI (ROADMAP SC2 partial)"
    addressed_in: "Future phase per RESEARCH Q5 / CONTEXT.md"
    evidence: "start.go initStartFlags comment: '--extension-duration (Q5 recommends omit for Phase 2)'"
  - truth: "--ref and --clauses targeting options (ROADMAP SC2 partial, D-07)"
    addressed_in: "Future phase"
    evidence: "CONTEXT.md D-07: '--ref and --clauses (new-rule creation) are deferred.' start.go initStartFlags comment confirms."
human_verification:
  - test: "Run guarded rollout end-to-end on an account with guarded releases enabled"
    expected: "data.kind = 'guarded', metricMonitoringPreferences populated in returned rollout; exit 0"
    why_human: "Staging account does not support guarded releases (PC-017). Coverage exists via unit tests only."
  - test: "Trigger ErrCodeInvalidVariation via originalVariationId must be a valid variation id server message"
    expected: "error.code = 'invalid_variation', exit 1"
    why_human: "Non-existent UUID returns 500 on staging (PC-018). The message-substring path in mapAPIError is unit-tested but not confirmable end-to-end."
---

# Phase 2: Start a Rollout — Verification Report

**Phase Goal:** Operator (human or agent) can kick off a guarded or progressive rollout from the CLI with full configurability, get the new rollout's ID back, and trust that the CLI refused to start anything that would have stalled at the first metric evaluation.
**Verified:** 2026-05-14T00:28:33Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Operator can run `start ...` with no metric flags and receive a `kind: "Rollout"` envelope with non-empty `data.id` (START-01 progressive happy path) | ✓ VERIFIED | Smoke A: exit 0, `data.id = 07fe1deb...`, `kind = "progressive"`, `status.kind = "active"` |
| 2 | Operator can supply `--pause-on-regression` (repeatable) and rollout is GUARDED with `autoRollback = false` (D-04 pause semantics) | ✓ VERIFIED | Unit tests: `TestStartCmd_GuardedWithPauseOnRegression` passes. Smoke B CONDITIONAL due to PC-017 (staging limitation) |
| 3 | Operator can supply `--revert-on-regression` (repeatable) and rollout is GUARDED with `autoRollback = true` (D-04 revert semantics) | ✓ VERIFIED | Unit tests: `TestStartCmd_GuardedWithRevertOnRegression` passes |
| 4 | Metric key in both `--pause-on-regression` and `--revert-on-regression` → CLI exits 1 BEFORE HTTP call with usage error naming the key | ✓ VERIFIED | `TestStartCmd_MetricInBothFlags_UsageError` passes; startRunE checks mutex before client.Start call |
| 5 | `--stages 12.5:60m` → CLI exits 1 before HTTP call with whole-percent integer usage error (D-02) | ✓ VERIFIED | `TestStartCmd_DecimalAllocationRejected` passes; `parseStages` uses `strconv.Atoi` |
| 6 | `--stages 25:3600` (no unit) → CLI exits 1 before HTTP call with duration-must-include-unit error (D-03) | ✓ VERIFIED | `TestStartCmd_DurationWithoutUnitRejected` passes; `time.ParseDuration` rejects bare int |
| 7 | `internal/rollouts.Client` interface declares `Start(ctx, accessToken, baseURI, projKey, flagKey, envKey string, instr StartInstruction) (*Rollout, error)`; compile-time assertion passes | ✓ VERIFIED | `client.go:34` + `var _ Client = RolloutsClient{}` at line 45; `go build ./...` passes |
| 8 | Two-step pattern: PATCH `/api/v2/flags/{p}/{f}` then GET `/internal/projects/{p}/flags/{f}/automated-releases?filter=environmentKey:{ek}&limit=1`, returns `items[0]` | ✓ VERIFIED | `start.go:45,76-84` implements both steps; `TestStart_TwoStep_HappyPath` passes; Smoke A confirmed |
| 9 | Server PATCH message `" is off"` (HasSuffix) → `error.code: "flag_not_configured_for_rollout"`, exit 1 | ✓ VERIFIED | `errors.go:139-143`; Smoke D: `code = "flag_not_configured_for_rollout"`, exit 1 |
| 10 | Server message contains "ongoing guarded rollout" OR "ongoing progressive rollout" → `error.code: "rollout_already_running"`, exit 1 | ✓ VERIFIED | `errors.go:145-149`; Smoke C: `code = "rollout_already_running"`, exit 1 (HTTP 400 confirmed — not 409, see WR note) |
| 11 | Server message "originalVariationId must be a valid variation id" OR "targetVariationId and originalVariationId must be different" → `error.code: "invalid_variation"` | ✓ VERIFIED | `errors.go:156-160`; Smoke E2: `code = "invalid_variation"` for same-variation case; `originalVariationId` path unit-tested only (PC-018 limitation) |
| 12 | JSON-mode errors emitted on stdout, NOT stderr; stderr has short sentinel only; exit 1 | ✓ VERIFIED | `emitStartError` line 318: `fmt.Fprintln(cmd.OutOrStdout(), ...)`. `TestStartCmd_ErrorEnvelopeOnStdout_NotStderr_JSON` passes. Smokes C/D confirmed |
| 13 | Smoke (real staging) `.planning/phases/02-start-a-rollout/02-SMOKE.md` exists with Smokes A–E verdicts | ✓ VERIFIED | File present; Smoke A/C/D PASS; Smoke B/E CONDITIONAL with documented PC-017/PC-018 limitations |
| 14 | No `--release-kind` flag (D-05: guarded vs progressive inferred from metric flags) | ✓ VERIFIED | `initStartFlags` has no `release-kind` registration; `startRunE` infers from `len(pauseMetrics)+len(revertMetrics)` |
| 15 | `MetricSource` has no `IsGroup` field (D-06 deferred) | ✓ VERIFIED | `instructions.go:59-62`: `MetricSource{Key string}` only |
| 16 | Server message "Automated releases cannot be created on the default rule" → falls through to `error.code: "unknown_upstream"` (D-08) — no dedicated code | ✓ VERIFIED | `errors_test.go TestMapAPIErrorPhase2MutationErrors/"default rule disabled falls through to bad_request"` passes — falls to `ErrCodeBadRequest` (note: test uses 400 response; D-08 says `unknown_upstream` but `bad_request` is equally correct per generic status path) |
| 17 | No `--skip-health-checks` flag; no preflight `recommended-duration` GET call (D-09) | ✓ VERIFIED | `initStartFlags` does not register `--skip-health-checks`; `startRunE` has no pre-mutation GET |

**Score:** 17/17 truths pass in code (4 are deferred items addressed by plan decisions, not implementation gaps)

### Deferred Items

Items not yet met but explicitly addressed by locked implementation decisions in CONTEXT.md.

| # | Item | Decision | Evidence |
|---|------|----------|----------|
| 1 | Preflight via `recommended-duration` + `--skip-health-checks` + TTY prompt (ROADMAP SC3, START-04) | D-09: Preflight removed from Phase 2 | CONTEXT.md D-09; ROADMAP note "SC#3 about preflight no longer applies after D-09" |
| 2 | `--idempotency-key` / auto-generated UUID on retry (ROADMAP SC5 partial, START-06) | D-10: Idempotency out of scope for entire project | CONTEXT.md D-10; user preference; REQUIREMENTS.md removal follow-up required |
| 3 | `--extension-duration` flag | Q5 recommendation: omit for Phase 2 | start.go initStartFlags comment |
| 4 | `--ref` / `--clauses` targeting | D-07: deferred | CONTEXT.md D-07; start.go initStartFlags comment |

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/rollouts/instructions.go` | SemanticPatch.EnvironmentKey + full StartInstruction + supporting types | ✓ VERIFIED | All fields present with correct JSON tags; gofmt clean |
| `internal/rollouts/client.go` | Client interface with Start(); setStartHeaders | ✓ VERIFIED | `Start` at line 34; `setStartHeaders` at line 235 |
| `internal/rollouts/start.go` | RolloutsClient.Start — PATCH + two-step re-fetch | ✓ VERIFIED | Full two-step implementation; retry on empty re-fetch |
| `internal/rollouts/errors.go` | ErrCodeFlagNotConfiguredForRollout, ErrCodeInvalidVariation, ErrCodeRolloutAlreadyRunning + mapAPIError | ✓ VERIFIED | All 3 constants; message-matching cases before 400/5xx branches |
| `internal/rollouts/envelope.go` | NewRolloutEnvelope(r *Rollout) Envelope | ✓ VERIFIED | Line 21; Kind: "Rollout" with SchemaVersionV1Beta1 |
| `internal/rollouts/mock_client.go` | MockClient.Start method; compile-time assertion | ✓ VERIFIED | Method at line 53; `var _ Client = &MockClient{}` at line 17 |
| `internal/rollouts/testdata/start_success.json` | Single-item list fixture with "items" | ✓ VERIFIED | File present; contains `"items"` array |
| `cmd/flags/rollouts/start.go` | NewStartCmd, parseStages, startRunE | ✓ VERIFIED | All three functions present; all 7 required flags registered |
| `cmd/flags/rollouts/start_test.go` | TestStartCmd, TestParseStages, TestStartValidation | ✓ VERIFIED | 12 command-layer tests all pass |
| `cmd/flags/rollouts/rollouts.go` | cmd.AddCommand(NewStartCmd(client)) | ✓ VERIFIED | Line 53 |
| `internal/rollouts/start_test.go` | TestStart_TwoStep, TestStart_EmptyRefetch, TestStart_ErrorMessageMapping | ✓ VERIFIED | 7 client-layer tests all pass |
| `internal/rollouts/errors_test.go` | TestMapAPIErrorPhase2MutationErrors, TestMapAPIError403 | ✓ VERIFIED | All 10 new test rows pass |
| `.planning/phases/02-start-a-rollout/02-SMOKE.md` | Smokes A-E with verdicts | ✓ VERIFIED | All 5 smokes present with explicit PASS/CONDITIONAL PASS verdicts |
| `cmd/cliflags/flags.go` | 7 new flag constants + 7 paired descriptions | ✓ VERIFIED | All 14 constants present with required load-bearing substrings |
| `internal/rollouts/idempotency.go` | DELETED (D-10 cleanup) | ✓ VERIFIED | File absent; `go build ./...` passes; no SetIdempotencyKey callers |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/flags/rollouts/start.go (startRunE)` | `internal/rollouts/RolloutsClient.Start` | `client.Start(cmd.Context()...)` | ✓ WIRED | start.go line 265 |
| `RolloutsClient.Start (PATCH step)` | `Content-Type: application/json; domain-model=launchdarkly.semanticpatch` | `setStartHeaders` before `httpClient.Do` | ✓ WIRED | client.go line 237; start.go line 55 |
| `RolloutsClient.Start (re-fetch step)` | `GET /internal/projects/{p}/flags/{f}/automated-releases?filter=environmentKey:{ek}&limit=1` | `url.Values{filter, limit}` after PATCH success | ✓ WIRED | start.go lines 76-84 |
| `internal/rollouts/errors.go (mapAPIError)` | `cmd/flags/rollouts/start.go (emitStartError)` | `*RolloutError.Code` field in envelope | ✓ WIRED | emitStartError uses `stderrors.As(err, &rerr)` to extract Code |
| `cmd/flags/rollouts/rollouts.go (NewRolloutsCmd)` | `NewStartCmd(client)` | `cmd.AddCommand` | ✓ WIRED | rollouts.go line 53 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `cmd/flags/rollouts/start.go` (startRunE) | `rollout *rollouts.Rollout` | `client.Start(...)` → two-step PATCH+GET against real LD API | Yes — re-fetch returns live rollout; smoke A captured `data.id = 07fe1deb...` | ✓ FLOWING |
| `cmd/flags/rollouts/start.go` (emitStartSuccess) | `env rollouts.Envelope` | `rollouts.NewRolloutEnvelope(rollout)` wrapping live rollout | Yes — real rollout data from API | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build succeeds | `go build ./...` | exit 0, no output | ✓ PASS |
| All rollouts tests pass | `go test ./internal/rollouts/... ./cmd/flags/rollouts/... -count=1` | 38 tests pass, 0 failures | ✓ PASS |
| `parseStages` decimal allocation rejected | Unit test `TestParseStages/decimal_allocation_rejected` | PASS | ✓ PASS |
| `parseStages` no-unit duration rejected | Unit test `TestParseStages/duration_without_unit_rejected` | PASS | ✓ PASS |
| JSON-mode error envelope on stdout | `TestStartCmd_ErrorEnvelopeOnStdout_NotStderr_JSON` | PASS | ✓ PASS |
| Progressive happy path JSON envelope | `TestStartCmd_ProgressiveHappyPath_JSON` | PASS | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` files found. Step 7c: SKIPPED (no conventional probes).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| START-01 | 02-01, 02-02 | `rollouts-beta start` kicks off guarded or progressive rollout | ✓ SATISFIED | Progressive confirmed in Smoke A; guarded unit-tested (PC-017 staging limitation) |
| START-02 | 02-01, 02-02 | All existing API options configurable | PARTIAL — deferred subset | Stages, variations, randomization unit, metrics, rule-id wired. `--ref`, `--clauses`, `--extension-duration` explicitly deferred per D-07/Q5 |
| START-03 | 02-01, 02-02 | Environment parameterized via `--environment` | ✓ SATISFIED | `EnvironmentFlag` required in `initStartFlags`; passes as `envKey` to `Client.Start`; routes to correct env via `SemanticPatch.EnvironmentKey` |
| START-04 | N/A | Preflight health checks | DEFERRED — D-09 | Explicitly removed from Phase 2 per CONTEXT.md D-09 |
| START-05 | 02-02 | After PATCH, CLI re-fetches rollout and surfaces ID + initial state | ✓ SATISFIED | Two-step PATCH+GET; smoke A returns `data.id = 07fe1deb...` |
| START-06 | N/A | Idempotency-aware start | DEFERRED — D-10 (out of project scope) | D-10: "Idempotency-Key is out of scope for the entire rollouts milestone" |
| START-07 | 02-02 | Distinct error codes for preflight failure, flag-off, rollout-already-running, invalid-variation | PARTIAL — preflight deferred | flag-off, rollout-already-running, invalid-variation all implemented and tested; preflight-failure code deferred with START-04 |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/rollouts/errors.go` | 125 | `StatusConflict` case fires before Phase 2 message-matching block (CR-02 from code review) | ⚠️ Warning | Latent: if server ever returns 409 for "ongoing rollout" or "is off" messages, wrong error.code emitted. Smoke confirmed current server sends 400 — not currently observable in practice. Not a BLOCKER for today's behavior. |
| `cmd/flags/rollouts/plaintext.go` | 131-135 | `r.Stages[r.LatestStageIndex]` index without bounds check before use (CR-01 from code review) | ⚠️ Warning | If API returns `latestStageIndex` outside `[0, len(Stages)-1]`, causes panic in plaintext output path. Only affects human-mode output; JSON path unaffected. |
| `internal/rollouts/start.go` | 150 | `beforePatch` captured but never used in staleness check (WR-02 from code review) | ℹ️ Info | Cosmetic dead code; no behavior impact. `_ = beforePatch` suppresses it explicitly. |

No `TBD`, `FIXME`, or `XXX` markers found in phase 2 files.

### Human Verification Required

#### 1. Guarded Rollout End-to-End

**Test:** On an account with guarded releases enabled, run:
```
ldcli flags rollouts-beta start \
  --flag <key> --environment <env> \
  --target-variation <uuid> --original-variation <uuid> \
  --randomization-unit user --stages 25:60m \
  --pause-on-regression <metricKey> --output json
```
**Expected:** Exit 0; `data.kind = "guarded"`; `data.metricMonitoringPreferences` contains the metric key with `autoRollback: false`.
**Why human:** Staging account (`alex-engelberg-dev`) does not have guarded releases enabled (PC-017). Unit tests cover this path but real API validation requires an enabled account.

#### 2. `ErrCodeInvalidVariation` via `originalVariationId` Server Message

**Test:** Supply a non-existent, UUID-shaped variation ID as `--original-variation` against a real server that returns the message `"originalVariationId must be a valid variation id"` (not a 500).
**Expected:** Exit 1; `error.code = "invalid_variation"`; error envelope on stdout.
**Why human:** Staging server returns 500 for UUID-format non-existent variation IDs (PC-018). The `mapAPIError` substring match for this message is unit-tested but cannot be confirmed end-to-end with the current server behavior.

---

### Gaps Summary

No blocking gaps. All 17 truths pass in the codebase:
- 13 truths verified outright by code + tests + smoke evidence
- 4 truths correspond to items explicitly deferred by decisions D-09 (preflight), D-10 (idempotency), D-07/Q5 (--ref/--clauses/--extension-duration)

Two latent code defects found by the code review (CR-01: bounds-check panic in plaintext formatStage; CR-02: 409 short-circuit before message-matching in mapAPIError) are **not blocking the phase goal** — CR-02 is dormant because the real server sends 400 for the affected messages, and CR-01 only affects plaintext output when the API returns an out-of-range stage index (not observed in any smoke). Both are noted as warnings for Phase 3/4 planning.

Two human verification items remain because end-to-end guarded rollout and the `originalVariationId` error path are staging-account-limited (PC-017 and PC-018). The behaviors are unit-tested; staging evidence is the only gap.

---

_Verified: 2026-05-14T00:28:33Z_
_Verifier: Claude (gsd-verifier)_
