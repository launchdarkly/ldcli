---
phase: 01-list-foundation-first-end-to-end-slice
plan: 02
subsystem: cli
tags: [rollouts, retryablehttp, httptest, status-mapping, error-code, dto-converter]

# Dependency graph
requires:
  - phase: 1
    plan: 01
    provides: "rollouts package skeleton (Client interface, RolloutsClient with retryablehttp, models, error skeleton, envelope, mock, idempotency)"
provides:
  - "Real HTTP path in RolloutsClient.List against /internal/projects/{p}/flags/{f}/automated-releases"
  - "Real HTTP path in RolloutsClient.Get against /internal/projects/{p}/environments/{e}/automated-releases/{id}"
  - "rawRolloutList / rawRollout / rawStage DTO layer with toRolloutList / toRollout / toStage converters"
  - "13-state status mapping (rollouts.MapStatus + DeriveStatusBlock) covering all documented raw API statuses with sub-condition discrimination (in_progress x4, reverted x3)"
  - "Full FOUND-08 status-code -> error.code taxonomy (401/403/404/409/400/429/5xx + transport) in mapAPIError / mapTransportError"
  - "AGENT-04 timestamp + duration conversion (int64 unix-millis -> time.Time UTC; durationMillis -> Go-style string)"
  - "httptest.NewServer round-trip test suite (18 sub-tests) covering happy path, retry envelope, no-retry-on-4xx, header construction, URL/query assembly, error mapping, conversion correctness, status decoration, Get path"
  - "Three hand-crafted JSON fixtures: list_progressive_in_progress.json, list_guarded_regressed.json, get_guarded_completed.json"
  - "PAPERCUT source anchors at PC-002 / PC-003 / PC-005 / PC-011 / PC-013 / PC-014 ready for Plan 03 to materialize API-PAPERCUTS.md"
affects: [03-flag-surface-and-papercuts, 04-stop-and-dismiss]

# Tech tracking
tech-stack:
  added: []  # no new dependencies — retryablehttp already on the tree from Plan 01
  patterns:
    - "Raw API DTO layer with explicit converter methods (rawT.toT()) for the API/CLI shape boundary — no time.Time field ever appears in a raw type"
    - "retryablehttp.PassthroughErrorHandler so retry-exhaustion routes through resp.StatusCode + mapAPIError rather than the default 'giving up after N attempts' wrapped error"
    - "Test-only constructor NewClientWithRetryWaitsForTest to shorten retry waits without exposing a knob to production callers"
    - "Source-code papercut anchors (// PAPERCUT: PC-NNN) at every Phase 1 workaround site so the eventual API-PAPERCUTS.md cross-references stay in sync with code reality"
    - "Threat-mitigation by struct-tag: RolloutError fields all json:\"-\" so the error type cannot accidentally serialize sensitive upstream body data (T-02-02)"
    - "Generic 5xx message in mapAPIError (does not echo upstream body) versus 4xx (passes through API message text) — informed by T-02-03 disposition"

key-files:
  created:
    - internal/rollouts/status_mapping.go
    - internal/rollouts/status_mapping_test.go
    - internal/rollouts/testdata/list_progressive_in_progress.json
    - internal/rollouts/testdata/list_guarded_regressed.json
    - internal/rollouts/testdata/get_guarded_completed.json
  modified:
    - internal/rollouts/client.go      (Plan 01 stub bodies replaced with real HTTP)
    - internal/rollouts/client_test.go (Plan 01 sanity test replaced with 18 round-trip sub-tests)
    - internal/rollouts/errors.go      (mapAPIError filled; RawBody given json:"-"; suggestionOrFallback helper added)
    - internal/rollouts/models.go      (Rollout gets ExtensionDurationMillis; MetricConfiguration gets MinSampleSize/AutoRollback/Status; Event gets StageIndex/MetricKey; raw* DTOs + converters appended)

key-decisions:
  - "D-01 honored end-to-end: every error path returns a typed *RolloutError with one of the documented error.code constants; no os.Exit or numeric taxonomy"
  - "D-02 honored end-to-end: every Rollout emitted by the converter has its nested Status block populated via DeriveStatusBlock; raw passthrough on Status.Status guarantees the API enum value is preserved"
  - "D-03 honored: no Reason field introduced; reason info flows through Status.Label only (e.g. 'rolled back automatically after detecting a regression for latency-p99')"
  - "retryablehttp.PassthroughErrorHandler installed so 5xx-exhaustion maps to ErrCodeUpstreamUnavailable rather than ErrCodeNetworkError"
  - "Get URL path: /internal/projects/{p}/environments/{e}/automated-releases/{id} (environment in path even though rollout ID is globally unique — PC-004 documented at the call site)"
  - "RolloutError fields all use json:\"-\" tags (not just RawBody) so the struct is never directly marshaled into the envelope; the EnvelopeError shape is the user-facing surface"
  - "NewClientWithRetryWaitsForTest is exported only because Go test access requires it; production code uses NewClient which sets the documented 500ms..8s envelope"

patterns-established:
  - "Test helper makeFlakyServer returns failureStatus for first N calls then successStatus — pattern reusable for Phase 2's mutation retry tests"
  - "Fixtures are hand-crafted (not captured live) because the staging API is unstable; fixture shape conforms to the field set RESEARCH.md §'Field Mapping API -> CLI' documents"
  - "DeriveStatusBlock requires the rollout's Status.Status to be set BEFORE the call; the converter sets it as a one-field StatusBlock first, then re-assigns the full block from DeriveStatusBlock(&r)"

requirements-completed:
  - FOUND-04
  - FOUND-05
  - LIST-02
  - AGENT-03
  - AGENT-04
# FOUND-08 was completed in Plan 01 per the SUMMARY there; this plan exercises the contract.

# Metrics
duration: 9m
completed: 2026-05-12
---

# Phase 1 Plan 2: Real HTTP for List + Get (with 13-state status mapping) Summary

**`./ldcli flags rollouts-beta list` now hits a real upstream (or httptest fixture) and returns a fully-decorated v1beta1 envelope with the nested 3-field status block and RFC 3339 timestamps; every documented HTTP error mode maps to a stable `error.code` value.**

## Performance

- **Duration:** ~9 minutes
- **Started:** 2026-05-12T21:53:49Z
- **Completed:** 2026-05-12T22:03:02Z
- **Tasks:** 2 / 2
- **Files created:** 5 (1 source, 1 test, 3 fixtures)
- **Files modified:** 4 (`client.go`, `client_test.go`, `errors.go`, `models.go`)

## Accomplishments

- Replaced both Plan 01 stub bodies (`List` returning `&RolloutList{Items: []Rollout{}}, nil` and `Get` returning `&Rollout{}, nil`) with the real retryablehttp request path against the documented endpoint URLs.
- Locked the 13-state status mapping table from CONTEXT.md `<specifics>` into source — `MapStatus` produces the correct `(kind, label)` tuple for every documented raw API status, including the 4 sub-cases for `in_progress` and the 3 sub-cases for `reverted`. The converter calls it on every emitted Rollout so every list item is decoration-complete.
- Filled the FOUND-08 error.code taxonomy: 401 → `unauthorized`, 403 → `forbidden`, 404 → `not_found`, 409 → `conflict`, 400 → `bad_request`, 429 → `rate_limited`, 5xx → `upstream_unavailable`, transport → `network_error`, unknown → `unknown_upstream`. Each error.code carries a curated `NextAction` hint (sourced from `errors.SuggestionForStatus` where parity exists; falling back to RESEARCH-specified strings otherwise).
- Wired the int64 unix-millis → `time.Time` UTC conversion (RFC 3339 on marshal) and the `durationMillis` → Go-duration-string conversion (`"15m0s"` for 900000ms) per AGENT-04.
- Replaced Plan 01's 5-subtest sanity check in `client_test.go` with 18 `httptest.NewServer` round-trip sub-tests that exercise: happy path, 5xx-retry-then-200 (3 requests), 5xx-exhaustion-then-fail (5 requests + `upstream_unavailable`), 4xx-no-retry (exactly 1 request), every documented `error.code` mapping (401/403/404/409/400/429), URL/query assembly with `--environment`/`--limit`/`--all`/`limit=20` default, header construction (Authorization/Content-Type/User-Agent; no Idempotency-Key on GET), timestamp + duration conversion against fixtures, status decoration via `DeriveStatusBlock`, and `Get` URL path verification + single-rollout parsing.

## Task Commits

1. **Task 1: Status mapping** — `76a91e6` (feat)
   - 3 files changed (+509, -25); status_mapping.go + status_mapping_test.go created; models.go extended with `ExtensionDurationMillis` / `MinSampleSize` / `AutoRollback` / `Status` / `StageIndex` / `MetricKey` fields to support the discriminators.
   - 17 table-driven status mapping sub-tests + 1 DeriveStatusBlock=MapStatus parity sub-test all pass.
2. **Task 2: Real HTTP + tests + fixtures** — `5db91cc` (feat)
   - 7 files changed (+817, -102); raw API DTO layer + converters added to models.go; client.go rewritten with real HTTP path; errors.go filled; client_test.go rewritten with 18 sub-tests; 3 fixtures hand-crafted.
   - All 18 client sub-tests + 18 status mapping sub-tests pass; full regression (`make test`) green across all 26 ldcli packages.

**Plan metadata commit:** the SUMMARY.md commit follows below.

## Files Created/Modified

**New files:**
- `internal/rollouts/status_mapping.go` — `MapStatus(r *Rollout) StatusBlock`, `DeriveStatusBlock(r *Rollout) StatusBlock` (alias), `mapStatusToKind` (13 → 5 lifecycle bucket switch), `formatLabel` (16-row label producer), helpers (`formatRule`, `currentAllocationPct`, `formatMetricNames`, `metricNamesFromEvents`, `findEvent`, `anyMetricBelowMinSample`, `formatDuration`). PC-005 papercut anchor in two places.
- `internal/rollouts/status_mapping_test.go` — 17 table-driven sub-tests covering all 13 documented raw statuses + sub-conditions + 1 unknown-status defensive case; plus `TestDeriveStatusBlockMatchesMapStatus` parity test.
- `internal/rollouts/testdata/list_progressive_in_progress.json` — 1-item list with `kind:"progressive"`, `status:"in_progress"`, three stages 25%/50%/100%, no metricConfigurations, durationMillis=900000 (asserts the 15m0s conversion).
- `internal/rollouts/testdata/list_guarded_regressed.json` — 1-item list with `kind:"guarded"`, `status:"monitoring_regressed"`, two stages, one MetricConfiguration with `Status:"regressed"`, one Event with `Kind:"regression_detected"` + `MetricKey:"latency-p99"` (asserts the `Regressions detected on the default rule for latency-p99` label).
- `internal/rollouts/testdata/get_guarded_completed.json` — single rollout (not wrapped in items) with `kind:"guarded"`, `status:"completed"`, endedAtMillis set, used by the Get test.

**Modified:**
- `internal/rollouts/client.go` — Plan 01 stub bodies replaced; `newRetryableClient` takes wait-min/wait-max args and sets `ErrorHandler = retryablehttp.PassthroughErrorHandler`; `NewClientWithRetryWaitsForTest` exported for tests; `setStandardHeaders` helper extracted; PC-002/003/011 anchors at the workaround sites; `Logger=nil` preserved (T-02-01).
- `internal/rollouts/client_test.go` — entire Plan 01 test (`TestClientStubReturnsEmptyEnvelopeShape`) replaced with `TestRolloutsClient` containing 18 `httptest.NewServer` round-trip sub-tests + helpers (`recordedRequest`, `makeServer`, `makeFlakyServer`, `loadFixture`).
- `internal/rollouts/errors.go` — `mapAPIError` rewritten to switch on statusCode; `mapTransportError` produces a useful network-error message; `apiErrorBody` shape for best-effort body unmarshal; `suggestionOrFallback` helper threads existing `errors.SuggestionForStatus` (401/403/404/409/429) before falling back to rollouts-specific strings; ALL `RolloutError` fields are now `json:"-"` (not just `RawBody`) — the struct should never marshal directly; the envelope path goes through `EnvelopeError` instead.
- `internal/rollouts/models.go` — `Rollout` gains `ExtensionDurationMillis *int64`; `Event` gains `StageIndex int` and `MetricKey string`; `MetricConfiguration` gains `MinSampleSize int`, `AutoRollback bool`, `Status string`; new appendix section defines `rawRolloutList`, `rawRollout`, `rawStage`, `millisToTimePtr`, `(raw rawRolloutList) toRolloutList()`, `(raw rawRollout) toRollout()`, `(raw rawStage) toStage()`. PC-014 anchor on the duration-string derivation.

## A2 Investigation Result (per plan output spec)

**A2: Does the upstream API response include `environmentKey` directly?**

**Outcome: PLAUSIBLE — fixtures include both `environmentId` and `environmentKey`.** The hand-crafted fixtures (`list_*.json` and `get_*.json`) include both `environmentId` (UUID) and `environmentKey` (slug) on every rollout item, matching the field shape RESEARCH.md §"Field Mapping API → CLI" hypothesizes. The converter (`toRollout`) passes both through to the CLI shape via `omitempty`, so the typed `Rollout.EnvironmentKey` will be populated whenever the API surfaces it.

We have NOT yet confirmed this against a live staging response (the API is unstable per CONTEXT.md constraints; capturing a live fixture is deferred to Wave 0 of any future staging-validation effort). If staging turns out to omit `environmentKey`, the converter will need a fallback that parses it from `_links.self.href` (path component); document that as a new papercut `PC-NEW-environmentKey-missing-in-list` when the discrepancy is encountered.

**Action for Plan 03:** Optionally run a one-off `curl` (or LD MCP query if the unstable API surface is registered) against staging to confirm. Either way, Plan 03's papercuts doc should record the outcome under `Open Questions` or as an active papercut entry.

## Get URL Path Confirmation (per plan output spec)

**Plan output spec asks:** is the `Get` URL `/environments/{envKey}/automated-releases/{rolloutID}` or different?

**Used path:** `/internal/projects/{projKey}/environments/{envKey}/automated-releases/{rolloutID}` — exactly the shape RESEARCH.md ARCHITECTURE inventory documents (per PC-004 — env in path despite globally unique rollout ID). Verified against `testdata/get_guarded_completed.json` via the `Get sends correct URL path with environment in path and parses single rollout` sub-test.

For Plan 03's help-text examples this means a status / get verb (Phase 3+) needs `--environment` as a required input alongside `--project` and the rollout ID. Plan 03 should propagate that requirement to the CLI flag wiring.

## RawBody Disposition Confirmation (per plan output spec)

**Plan output spec asks:** was `RawBody` given `json:"-"` or left un-tagged?

**Outcome: explicit `json:"-"` on every `RolloutError` field, not just `RawBody`.** The original Plan 01 skeleton documented the intent ("never JSON-tagged") but the struct fields had no JSON tags at all, meaning a careless `json.Marshal(*RolloutError)` would have emitted them under their Go field names. Plan 02 tightens the contract — every field on `RolloutError` (`Code`, `Message`, `NextAction`, `StatusCode`, `RawBody`) carries `json:"-"`. The user-facing surface is `EnvelopeError` exclusively; `RolloutError` is internal and only readable via the `Error()` method or the typed-error inspection path (`errors.As(err, &rolloutErr)`).

This is a stricter implementation than the plan's acceptance criterion required (which only mandated `json:"-"` on `RawBody`), but the broader application is correct: nothing on `RolloutError` is meant for direct JSON output.

## NextAction Parity Notes (per plan output spec)

The `suggestionOrFallback` helper prefers `errors.SuggestionForStatus(statusCode, "")` when it returns a non-empty string; this keeps parity with existing CLI error envelopes for 401/403/404/409/429. RESEARCH.md-specified strings serve as fallbacks for codes `errors.SuggestionForStatus` does not cover (400 and 5xx).

- 401 NextAction includes "Run `ldcli login`" and reference to `settings/authorization` (from existing `errors.SuggestionForStatus`).
- 403 NextAction mentions role/permissions (existing `errors.SuggestionForStatus` text).
- 404 NextAction mentions project/flag/environment keys + `ldcli projects list`/`ldcli flags list` examples (existing text).
- 429 NextAction mentions rate limits + retry (existing text).
- 5xx NextAction is rollouts-specific: "Retry; if persistent, check the LaunchDarkly status page" (not in the central suggestions map; new content).

Plan 03's docs (help text, top-level command description) should reference the same `error.code` enum so the agent-facing surface stays consistent.

## Decisions Made

- **`retryablehttp.PassthroughErrorHandler` installed.** Without it, retry-exhaustion on 5xx returns `(nil, fmt.Errorf("...giving up after N attempts: %w", retryErr))` — which routes through `mapTransportError` and produces `ErrCodeNetworkError`. That's wrong: the failure is "upstream took my retries and still 5xx'd", which is `ErrCodeUpstreamUnavailable`. PassthroughErrorHandler delivers the final response to the caller so the standard `resp.StatusCode >= 400 → mapAPIError` branch handles it correctly.
- **`NewClientWithRetryWaitsForTest` exported.** Test code in the `rollouts_test` external package needs to construct a client with zero-wait retries so the 5xx-exhaustion sub-test completes in milliseconds. Naming the helper `*ForTest` signals its narrow purpose; the production constructor `NewClient` continues to set the documented 500ms..8s envelope.
- **Hand-crafted JSON fixtures rather than live capture.** The plan acknowledges the upstream is unstable; capturing live responses now would lock in a snapshot that may not match the API tomorrow. Hand-crafted fixtures that conform to the documented field shape are easier to maintain and let Plan 03 add new test cases without needing staging access. The trade-off is documented above under "A2 Investigation Result".
- **All `RolloutError` fields tagged `json:"-"` (not just `RawBody`).** Stricter than the plan required; rationale in the "RawBody Disposition Confirmation" section above. No deviation flagged because this is a strict-superset of the plan's threat-model intent.

## Deviations from Plan

**Auto-fixed Issues:**

**1. [Rule 3 — Blocking] Plan 01 models lacked discriminator fields for status mapping**

- **Found during:** Task 1, immediately after writing the failing test (compile errors on `MinSampleSize`, `Status`, `MetricKey`, `ExtensionDurationMillis`).
- **Issue:** Plan 01 shipped `Rollout`, `Event`, and `MetricConfiguration` with a minimal field set. Task 1's `<behavior>` spec explicitly requires sub-condition discrimination for `in_progress` (extension active, min sample reached/not reached) and `reverted` (regression event, SRM event, insufficient sample), which need `ExtensionDurationMillis` on `Rollout`, `MinSampleSize` + `Status` on `MetricConfiguration`, and `MetricKey` on `Event` to operate.
- **Fix:** Extended the three structs with the required fields (all `omitempty` JSON tags so absent values don't pollute the envelope). The Plan 01 zero-value behavior is preserved because the new fields are pointers / strings / ints with `omitempty`.
- **Files modified:** `internal/rollouts/models.go` (struct expansions, no breaking renames).
- **Verification:** `go build ./...` succeeds; status mapping tests pass; no other rollouts package tests broken.
- **Committed in:** `76a91e6` (rolled into Task 1).

This is a Rule 3 (blocking) auto-fix: without these fields the Task 1 spec literally cannot be implemented. Scope: limited to adding fields; no Plan 01 field renamed or removed.

**2. [Rule 2 — Missing critical functionality] `PassthroughErrorHandler` not in Plan 01's retryablehttp setup**

- **Found during:** Task 2, after the first GREEN run revealed two failing sub-tests (5xx-exhaustion mapped to `ErrCodeNetworkError` instead of `ErrCodeUpstreamUnavailable`; 429 mapped to network error after retries).
- **Issue:** Plan 01's `newRetryableClient` did not install an `ErrorHandler`; the default behavior is "after retries, drop the response and wrap the underlying error into 'giving up after N attempts'." That routes 5xx-exhaustion through `mapTransportError` instead of `mapAPIError`, breaking the FOUND-08 contract that says 5xx-exhaustion → `upstream_unavailable`.
- **Fix:** Added `c.ErrorHandler = retryablehttp.PassthroughErrorHandler` in `newRetryableClient`. The retry envelope still caps at `RetryMax=4` and `~16s` wall time; only the post-exhaustion routing changes.
- **Files modified:** `internal/rollouts/client.go`.
- **Verification:** All 18 client sub-tests pass; total wall-time of the retry-exhaustion test stays under 100ms with zero-wait config.
- **Committed in:** `5db91cc` (rolled into Task 2).

This is a Rule 2 (correctness) auto-fix: FOUND-08 mandates the 5xx-exhaustion mapping, and without `PassthroughErrorHandler` the mapping is wrong. The Plan 01 `newRetryableClient` was correct at the unit-of-skeleton level but lacked the routing nuance Plan 02 needs.

**3. [Rule 2 — Documentation hygiene] Plan 01 left `RolloutError` fields untagged**

- **Found during:** Task 2 errors.go rewrite.
- **Issue:** Plan 01's `RolloutError` struct had no JSON tags. The Plan 01 comment said `RawBody` should not serialize, but the code did not enforce it. A careless `json.Marshal(rolloutErr)` would have leaked all fields under Go names.
- **Fix:** Added `json:"-"` to every field on `RolloutError` (Code, Message, NextAction, StatusCode, RawBody). The user-facing envelope path goes through `EnvelopeError` exclusively.
- **Files modified:** `internal/rollouts/errors.go`.
- **Verification:** Plan 01's threat model (T-01-02 in Plan 01; T-02-02 in Plan 02) is now enforced by struct tag, not just by code review.
- **Committed in:** `5db91cc` (rolled into Task 2).

This is a Rule 2 (correctness) auto-fix: the threat-model disposition stated the intent, but the source did not enforce it.

**No other deviations.** Both tasks executed as planned; all acceptance-criteria source-grep checks pass; the regression suite is green across all 26 packages.

## Known Stubs (intentional — replaced by Plan 03 / Plan 04)

| Location | Stub | Replacement plan |
|---|---|---|
| `cmd/flags/rollouts/list.go` | List runE uses default `ListOpts{}` — `--environment` / `--limit` / `--all` flags not yet wired | Plan 03 |
| `cmd/flags/rollouts/plaintext.go` | Tab-separated lines, no alignment | Plan 03: real 5-column table per D-06 |
| `.planning/API-PAPERCUTS.md` | Not yet seeded; source-code `// PAPERCUT: PC-NNN` anchors are in place (PC-002/003/005/011/013/014) | Plan 03 seeds the doc with PC-001..PC-016 + the source anchors stay |
| `internal/rollouts/idempotency.go` (SetIdempotencyKey) | Helper exists, no call site exercises it yet | Phase 2 (Start instruction) |
| `internal/rollouts/instructions.go` | StartInstruction / StopInstruction / DismissRegressionInstruction have only Kind field | Phase 2 / Phase 4 |

These are documented in the plan's `<done>` section as expected end-state for Plan 02.

## Threat Surface Scan

No new threat surface beyond Plan 01 + Plan 02's `<threat_model>`. Mitigations verified in source:

- **T-02-01 (token leakage in retryablehttp logs):** `c.Logger = nil` preserved in `newRetryableClient`. `grep -c "c\\.Logger\\s*=\\s*nil" internal/rollouts/client.go` returns 1. ✓
- **T-02-02 (error envelope info disclosure):** `RolloutError` fields all carry `json:"-"`. The struct cannot accidentally serialize through `json.Marshal`. ✓
- **T-02-03 (verbose 5xx error messages):** `mapAPIError` 5xx branch produces a generic `"LaunchDarkly returned %d %s"` message; does NOT echo upstream body for 5xx. 4xx branches echo `apiBody.Message` because those are operator-actionable. ✓
- **T-02-04 (untrusted JSON unmarshal):** all upstream JSON is decoded into typed `rawRolloutList` / `rawRollout` / `rawStage` structs with strict field shapes; no `interface{}` or `map[string]any` for response data. ✓
- **T-02-05 (retry DoS):** `RetryMax=4`, `RetryWaitMax=8s` honored in production constructor; total wall time bounded at ~16s. Two test sub-cases (5xx-then-200 → 3 reqs; all-5xx → 5 reqs terminal) prove the cap. ✓
- **T-02-06 (Authorization header construction):** Access token set verbatim into Authorization (no Bearer prefix), matching `internal/resources/Client` precedent. ✓
- **T-02-07 (Idempotency-Key — deferred):** `SetIdempotencyKey` helper exists; not exercised in Phase 1. Plan 02 confirms no Phase 1 call site uses it (`grep -r "SetIdempotencyKey" internal/rollouts/ | grep -v "_test\\|idempotency.go"` returns no production call sites). Phase 2 wires it on Start. ✓
- **T-02-08 (RBAC failures masked):** 403 → `ErrCodeForbidden` with NextAction "Verify your access token's role includes the required permission/scope on the target project". PC-009 (RBAC errors don't name the missing action) is documented as a future papercut anchor for Plan 03. ✓

No new threat flags to file.

## Verification

**Status mapping verification (from plan):**

```text
$ go test ./internal/rollouts/... -run TestStatusMapping -count=1 -v
=== RUN   TestStatusMapping
... 17 sub-tests ... ALL PASS
--- PASS: TestStatusMapping (0.00s)
PASS
ok  	github.com/launchdarkly/ldcli/internal/rollouts	0.541s
```

**Round-trip HTTP verification (from plan):**

```text
$ go test ./internal/rollouts/... -run TestRolloutsClient -count=1 -v
=== RUN   TestRolloutsClient
... 18 sub-tests ... ALL PASS
--- PASS: TestRolloutsClient (0.01s)
PASS
ok  	github.com/launchdarkly/ldcli/internal/rollouts	0.628s
```

**Regression check (from plan):**

```text
$ go test ./... -count=1 -short
... 26 packages ... 0 failures
ok  	github.com/launchdarkly/ldcli/internal/rollouts	2.091s
```

**Architectural lock-in checks (from plan `<verification>`):**

- `grep -c "Items: \[\]Rollout{}" internal/rollouts/client.go` → 0 (Plan 01 stub is gone) ✓
- `grep -rE "PAPERCUT: PC-(002|003|005|011|014)" internal/rollouts/` → 8 anchors ✓ (PC-002 x1, PC-003 x1, PC-005 x2, PC-011 x2, PC-014 x1, plus PC-013 in models.go)

**Build check:**

```text
$ make build
go build -o ldcli
(success)
```

## Self-Check: PASSED

Files created exist:
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a8b8469d30edc64f8/internal/rollouts/status_mapping.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a8b8469d30edc64f8/internal/rollouts/status_mapping_test.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a8b8469d30edc64f8/internal/rollouts/testdata/list_progressive_in_progress.json` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a8b8469d30edc64f8/internal/rollouts/testdata/list_guarded_regressed.json` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a8b8469d30edc64f8/internal/rollouts/testdata/get_guarded_completed.json` — FOUND

Commits exist in git log:
- `76a91e6` (Task 1: status mapping) — FOUND
- `5db91cc` (Task 2: real HTTP + tests + fixtures) — FOUND
