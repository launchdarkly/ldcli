---
phase: 02-start-a-rollout
plan: 02
subsystem: rollouts
tags: [rollouts, cobra, semantic-patch, two-step-start, error-mapping, smoke]
dependency_graph:
  requires: [02-01]
  provides: [rollouts-start-command]
  affects: [cmd/flags/rollouts, internal/rollouts]
tech_stack:
  added: []
  patterns:
    - two-step PATCH+GET pattern for semantic-patch mutations (PC-001 workaround)
    - message-substring error code mapping for server-side validation errors
    - StringArray flags with viper.GetStringSlice for repeatable CLI args
key_files:
  created:
    - internal/rollouts/start.go
    - internal/rollouts/start_test.go
    - internal/rollouts/testdata/start_success.json
    - cmd/flags/rollouts/start.go
    - cmd/flags/rollouts/start_test.go
    - .planning/phases/02-start-a-rollout/02-SMOKE.md
  modified:
    - internal/rollouts/client.go
    - internal/rollouts/errors.go
    - internal/rollouts/envelope.go
    - internal/rollouts/mock_client.go
    - internal/rollouts/errors_test.go
    - cmd/flags/rollouts/rollouts.go
    - cmd/flags/rollouts/plaintext.go
    - .planning/API-PAPERCUTS.md
decisions:
  - D-02: stages allocation is percent int; CLI multiplies by 1000 for basis points
  - D-03: stages duration is Go duration string with mandatory unit suffix
  - D-04: pause-on-regression/revert-on-regression are repeatable; mutex-rejected on overlap
  - D-05: releaseKind inferred from presence of pause/revert flags (no --release-kind)
  - D-07: JSON-mode errors emitted to stdout not stderr (AGENT-04)
  - D-11: two-step PATCH+GET-with-filter+limit=1 pattern locked
  - D-12: error-code mapping by server message substring; no pre-fetch
metrics:
  duration: ~2 hours (context-window continuation)
  completed: 2026-05-13
  tasks_completed: 4
  tasks_total: 4
  files_changed: 14
  tests_added: 21
  tests_baseline: 17
  tests_total: 38
---

# Phase 02 Plan 02: Start-a-Rollout Summary

Shipped the end-to-end vertical slice for `ldcli flags rollouts-beta start` using the semantic-patch two-step PATCH+GET pattern, with message-based error code mapping and real-staging smoke validation (Smokes A-E).

## What Was Shipped

### Task 1: internal/rollouts/ extension

**`internal/rollouts/client.go`** — `Client` interface widened from 2 to 3 methods. Added `Start(ctx, accessToken, baseURI, projKey, flagKey, envKey string, instr StartInstruction) (*Rollout, error)`. Added `setStartHeaders` method with `Content-Type: application/json; domain-model=launchdarkly.semanticpatch` (semantic-patch middleware gate, Pitfall 1).

**`internal/rollouts/start.go`** (new) — `RolloutsClient.Start` implementation:
- Captures `beforePatch := time.Now().UTC()` before PATCH for staleness detection
- PATCH to `/api/v2/flags/{projKey}/{flagKey}` with `setStartHeaders`
- GET to `/internal/projects/{projKey}/flags/{flagKey}/automated-releases?filter=environmentKey:{envKey}&limit=1` with `setStandardHeaders`
- Retries GET up to 3 times (100ms, 250ms, 500ms) on empty items
- Returns `ErrCodeUnknownUpstream` after exhausting retries

**`internal/rollouts/errors.go`** — Three new error code constants: `ErrCodeFlagNotConfiguredForRollout`, `ErrCodeInvalidVariation`, `ErrCodeRolloutAlreadyRunning`. Extended `mapAPIError` switch with 6 message-substring cases BEFORE the `StatusBadRequest` branch.

**`internal/rollouts/envelope.go`** — Added `NewRolloutEnvelope(r *Rollout) Envelope` returning `Kind: "Rollout"` with `SchemaVersionV1Beta1`.

**`internal/rollouts/mock_client.go`** — Hand-written `Start` method with nil-safe pointer extraction. Compile-time `var _ Client = &MockClient{}` assertion passes.

**`internal/rollouts/testdata/start_success.json`** — Single-item list fixture with `int64` millis timestamps (matching real-staging wire shape from 01-SMOKE.md).

### Task 2: cmd/flags/rollouts/start.go

New Cobra command with:
- Required flags: `--flag`, `--project`, `--environment`, `--stages`, `--target-variation`, `--original-variation`, `--randomization-unit`
- Optional repeatable (StringArray): `--pause-on-regression`, `--revert-on-regression`
- Optional single: `--rule-id`
- `parseStages(raw string)`: validates allocation as percent integer (rejects decimals via `strconv.Atoi`), duration via `time.ParseDuration` (rejects bare integers), range [1,100]
- `releaseKind` inferred from len(pauseMetrics)+len(revertMetrics): zero → `"progressive"`, nonzero → `"guarded"`
- Mutex check for metric keys in both flags BEFORE HTTP call
- `emitStartError`: writes JSON error envelope to stdout in JSON mode; returns short sentinel for stderr (D-07/AGENT-04)
- Wired into `NewRolloutsCmd` alongside `NewListCmd`

### Task 3: Tests

- **`internal/rollouts/start_test.go`**: 7 client-layer tests — happy path two-step, header assertions, body round-trip, empty-refetch retry exhaustion (5 calls: 1 PATCH + 4 GET), error-message mapping (7 table rows), transport error, 5xx retry
- **`internal/rollouts/errors_test.go`**: Extended with `TestMapAPIErrorPhase2MutationErrors` (7 rows for new mutation error codes) and `TestMapAPIError403` (3 rows)
- **`cmd/flags/rollouts/start_test.go`**: 12 command-layer tests — progressive happy path, guarded with pause/revert, mixed metrics, mutex validation, stages parser edge cases (decimal, no-unit, out-of-range), JSON error envelope on stdout not stderr, plaintext error, rule-id flow-through, `TestParseStages` (8 table cases)

Total tests in rollouts packages: 38 (up from 17 before this plan, +21 new).

### Task 4: Real-Staging Smoke Tests

All 5 smokes ran against `ld-stg.launchdarkly.com`:

| Smoke | Scenario | Verdict |
|---|---|---|
| A | Progressive happy path (3 stages) | PASS — `data.id` = `07fe1deb...`, `kind=progressive`, `status.kind=active` |
| B | Guarded with `--pause-on-regression` | CONDITIONAL — server rejected with `PC-017` (guarded not enabled on account) |
| C | Already-running error | PASS — `error.code=rollout_already_running`, envelope on stdout |
| D | Flag-off error | PASS — `error.code=flag_not_configured_for_rollout`, envelope on stdout |
| E | Invalid variation UUID | CONDITIONAL — `invalid_variation` fires for same-variation IDs; non-existent UUID returns `PC-018` (500) |

## Deviations from Plan

### Auto-fixed Issues

None — plan executed as written.

### Known Smoke Limitations

**PC-017 — Guarded releases not enabled on staging account**
- **Found during:** Task 4 Smoke B
- **Issue:** `startAutomatedRelease` with `releaseKind: "guarded"` returns HTTP 400: `"instruction kind startAutomatedRelease is not enabled for guarded releases"`
- **CLI behavior:** Correctly propagates as `error.code: "bad_request"` (D-08 fallthrough). No bug.
- **Coverage:** Guarded-rollout behavior covered by unit tests (TestStartCmd_Guarded*) but not confirmable end-to-end on staging.

**PC-018 — Non-existent variation UUID returns 500**
- **Found during:** Task 4 Smoke E
- **Issue:** A UUID-shaped string not matching any flag variation triggers a server 500 rather than 400 `"originalVariationId must be a valid variation id"`. The `ErrCodeInvalidVariation` mapping for this substring is correct code but cannot be exercised from staging.
- **CLI behavior:** 500 is correctly mapped to `ErrCodeUpstreamUnavailable`. No bug.
- **Coverage:** The `"originalVariationId"` substring case covered by unit test in `errors_test.go` but not confirmable end-to-end.

## Known Stubs

None — all code paths are wired. The `NewRolloutEnvelope` result is fully populated from the real rollout returned by the two-step re-fetch.

## Deferred Items

Per plan decisions (D-09, D-10, D-06, D-07 partial):
- No `--skip-health-checks` / preflight (D-09 deferred)
- No `--idempotency-key` (D-10 out of scope)
- No metric groups / `--metric-group` (D-06 deferred to v1.1)
- No `--ref` / `--clauses` (D-07 deferred)
- No `--extension-duration` (RESEARCH Q5 omit)
- No `--comment` (CONTEXT.md Claude's Discretion default: omit)

## API Papercuts Discovered

| ID | Summary |
|---|---|
| PC-017 | `startAutomatedRelease` instruction does not support guarded releases on staging account |
| PC-018 | Non-existent variation UUID in start instruction returns HTTP 500 instead of 400 |

Both logged to `API-PAPERCUTS.md`. Confluence update to page 4875452435 needed (to be done by human per fetch-first protocol — page content must be read first before update).

## Test Coverage Summary

| Layer | Tests Added | Key Behaviors |
|---|---|---|
| client (internal/rollouts/start_test.go) | 7 | Two-step PATCH+GET, header assertions, body round-trip, empty-refetch retry, error mapping, transport error, 5xx retry |
| error mapping (errors_test.go) | 9 new rows | Phase 2 mutation codes + 403 passthrough |
| command (cmd/flags/rollouts/start_test.go) | 12 | Progressive/guarded inference, mutex validation, stages parser, error envelope routing (AGENT-04) |

## Phase 3 Integration Notes

- `data.id` from Smoke A (`07fe1deb-5a61-4117-b6e1-ba12d77a280a`) is the addressable rollout ID for Phase 3 `status` and `watch` verbs
- `data.status.kind` / `data.status.label` are already populated by Phase 1's status-mapping path
- `_links.self` on the start response (`/internal/projects/.../environments/test/automated-releases/<id>`) is a candidate for Phase 3's direct GET path (avoid another list+filter round-trip)

## Self-Check: PASSED

Files created verified present:
- `internal/rollouts/start.go` ✓
- `internal/rollouts/start_test.go` ✓
- `internal/rollouts/testdata/start_success.json` ✓
- `cmd/flags/rollouts/start.go` ✓
- `cmd/flags/rollouts/start_test.go` ✓
- `.planning/phases/02-start-a-rollout/02-SMOKE.md` ✓

Commits verified:
- `9b8fbfd` feat(02-02): Task 1 ✓
- `7e83460` feat(02-02): Task 2 ✓
- `661e443` test(02-02): Task 3 ✓
- `38a8860` chore(02-02): Task 4 ✓

`go build ./...` exits 0 ✓
`go test ./internal/rollouts/ ./cmd/flags/rollouts/ -count=1 -race` exits 0 ✓
`gofmt -l internal/rollouts/ cmd/flags/rollouts/` produces no output ✓
