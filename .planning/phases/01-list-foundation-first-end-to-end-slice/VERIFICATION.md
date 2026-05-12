---
phase: 01-list-foundation-first-end-to-end-slice
verified: 2026-05-12T22:26:36Z
status: passed
verdict: PASS
score: 17/17 must-haves verified (architectural lock-ins + requirement IDs all satisfied; two ROADMAP success criteria reframed by CONTEXT.md decisions D-01 and D-04, which is the documented and intended scope)
re_verification:
  previous_status: none
  initial_verification: true
notes:
  - "ROADMAP Phase 1 Success Criterion #3 mentions `--state running|completed|failed|stopped`; CONTEXT.md D-04 explicitly drops `--state` from v1. This is a planner-locked deviation, not a regression — see Plan 03 test `TestListStateFlagNotRecognized` which actively asserts the flag is rejected."
  - "ROADMAP Phase 1 Success Criterion #4 mentions a `distinct documented exit code` per upstream failure mode; CONTEXT.md D-01 explicitly reframes this to a structured `error.code` taxonomy in the JSON envelope with exit code always 1. This is also a planner-locked deviation."
  - "Plan 03 SUMMARY's table maps FOUND-06 to `Logger=nil`, which is wrong (Logger=nil is the T-02-01 mitigation, not the refetch helper). FOUND-06's actual Phase 1 scope per Plan 01 is `Get` on the Client interface as the re-fetch building block; the helper itself is exercised in Phase 2/4 when start/stop/dismiss exist. The substance is correct; the Plan 03 table cell is a SUMMARY copy-paste artifact."
overrides: []
gaps: []
human_verification: []
---

# Phase 1: List (foundation + first end-to-end slice) Verification Report

**Phase Goal (ROADMAP.md):** Operator (human or agent) can run `ldcli flags rollouts-beta list --flag <key>` and get a deterministic JSON or plaintext enumeration of every rollout on the flag, with proper exit codes, beta signaling, and the agent-friendly output envelope already locked in.

**Verified:** 2026-05-12T22:26:36Z
**Status:** PASS
**Re-verification:** No — initial verification
**Plans completed:** 3 / 3

---

## Verdict: PASS

Every architectural lock-in from CONTEXT.md (D-01 through D-08, banner gating, sort, saturation, idempotency, status mapping) is observably enforced in source. Every Phase 1 requirement ID (FOUND-01..08, DOC-01, LIST-01..03, AGENT-01..05) maps to concrete source / test evidence. `make build` and `make test` both succeed. The CLI end-to-end smoke test produces a well-formed envelope.

Two ROADMAP success criteria are intentionally reframed by CONTEXT.md decisions (D-04 drops `--state`; D-01 collapses exit-code taxonomy to envelope-level `error.code`). These are planner-approved deviations documented at phase planning time, locked into the source, and have negative-assertion test coverage (`TestListStateFlagNotRecognized`, `TestListIdempotencyKeyFlagNotRecognized`). Treating these as failures would contradict the locked decisions agreed at context-gathering time.

---

## Goal Coverage: Architectural Lock-Ins (CONTEXT.md decisions)

### Observable Truths

| #   | Invariant                                                                   | Status     | Evidence                                                                                                                                                                                                                  |
| --- | --------------------------------------------------------------------------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **D-01**: Any error from rollouts subtree exits with code 1, no numeric taxonomy. Error info via envelope `error.code` only. | VERIFIED   | `grep -rE "os\.Exit\([2-9]\)" cmd/flags/rollouts/ internal/rollouts/` → empty. `cmd/flags/rollouts/list.go:emitError` returns `errors.NewError(...)` whose Cobra path produces exit 1. Comment on line 138 of list.go documents D-01 explicitly. |
| 2   | **D-02**: Each `Rollout.Status` is nested `{status, kind, label}` AND a top-level `kind` on Rollout. | VERIFIED   | `internal/rollouts/models.go:14`: `Kind string `json:"kind"` // rollout kind: "guarded" | "progressive"`. `internal/rollouts/models.go:23`: `Status StatusBlock `json:"status"` // NESTED — D-02 three-field model`. `StatusBlock` has `Status`, `Kind`, `Label` fields. |
| 3   | **D-03**: NO `Reason` field on `Rollout` or `StatusBlock`. Reason info flows via `status.label` only. | VERIFIED   | `grep -E "Reason" internal/rollouts/models.go` returns only the doc comment explicitly stating "no `Reason` field". No struct field anywhere named `Reason`.                                                              |
| 4   | **D-04**: `--state` flag dropped. `--environment` kept.                      | VERIFIED   | `grep -E "StateFlag|\"state\"" cmd/flags/rollouts/flags.go` returns only the comment. `cmd/flags/rollouts/list_test.go:TestListStateFlagNotRecognized` asserts `--state running` is rejected as unknown.                  |
| 5   | **D-05**: `--limit N` default 20; `--all` for full history.                  | VERIFIED   | `cmd/flags/rollouts/flags.go:31` `cmd.Flags().Int(cliflags.LimitFlag, 20, ...)`. `cmd/flags/rollouts/flags.go:34` registers `--all`. `internal/rollouts/client.go:113-121` honors the default 20 and lifts to 1000 on `--all`. |
| 6   | **D-06**: Plaintext default 5-col table (ID, KIND, ENVIRONMENT, STATE, STARTED); `--detailed` expands. | VERIFIED   | `cmd/flags/rollouts/plaintext.go:44` `fmt.Fprintln(w, "ID\tKIND\tENVIRONMENT\tSTATE\tSTARTED")`. `renderDetailed()` adds Target var, Original var, Raw status, Stage progress, Label, Ended, Started.                       |
| 7   | **D-07**: `--detailed` MUST NOT change JSON output.                          | VERIFIED   | `cmd/flags/rollouts/list.go:emitSuccess` checks `GetOutputKind == "json"` first and emits the full envelope unconditionally; `detailed` is only consulted on the plaintext branch. `TestListDetailedDoesNotAffectJSON` asserts this end-to-end. |
| 8   | **D-08**: `Client` interface ships only `List` + `Get` in Phase 1.           | VERIFIED   | `grep -cE "^\s*(List|Get|Start|Stop|DismissRegression)\(" internal/rollouts/client.go` → 2 (only List + Get). `instructions.go` has `Start/Stop/DismissRegression` instruction types but no Client methods.              |
| 9   | **Banner**: stderr-only, gated by TTY AND `--output != json`.                | VERIFIED   | `cmd/flags/rollouts/rollouts.go:shouldPrintBetaBanner` returns false if `--output json` OR stderr not a TTY (FORCE_TTY override accepted). `printBetaBanner` writes to `cmd.ErrOrStderr()`. Three integration tests cover all gating combinations. |
| 10  | **Idempotency-Key wired but NOT a CLI flag in Phase 1.**                      | VERIFIED   | `internal/rollouts/idempotency.go:SetIdempotencyKey` exists with UUIDv4 generation. Zero call sites in production code (verified by grep). `cmd/flags/rollouts/flags.go` does not register `--idempotency-key`. `TestListIdempotencyKeyFlagNotRecognized` actively asserts rejection. |
| 11  | **Sort**: client-side `CreatedAt DESC` then `ID ASC`, applied in both production and command layer. | VERIFIED   | `internal/rollouts/client.go:159-165` and `cmd/flags/rollouts/list.go:127-134` both implement the comparator. `TestListSortOrder` asserts CreatedAt DESC, ID ASC tiebreaker via mock. Help text documents the order: `./ldcli flags rollouts-beta list --help` outputs "reverse-chronological order by createdAt timestamp, with rollout ID as the deterministic tiebreaker." |
| 12  | **Saturation**: `len(items) == limit` ⇒ warning in `meta.warnings` pointing at PC-003. | VERIFIED   | `cmd/flags/rollouts/list.go:88-98` constructs the warning. `TestListSaturationWarning` asserts the warning contains "PC-003" or "truncated" when `len(items) == limit` and `--all` is unset.                                |
| 13  | **Status mapping**: all 13 raw API statuses → (kind, label) with sub-condition discrimination. | VERIFIED   | `internal/rollouts/status_mapping.go` has switch arms for all 13 documented statuses (`not_started`, `waiting`, `in_progress` x4 sub-cases, `monitoring_regressed`, `monitoring_stopped`, `srm_stopped`, `completed`, `manually_completed`, `manually_reverted`, `reverted` x3 sub-cases, `archived`) plus an `unknown status` defensive arm. `TestStatusMapping` has 17 sub-cases, all PASS. |
| 14  | **Retry/error semantics**: 4 retries, 500ms..8s, 4xx never retried, retry-exhaustion → `upstream_unavailable`. | VERIFIED   | `internal/rollouts/client.go:newRetryableClient` sets `RetryMax=4`, `RetryWaitMin=500ms`, `RetryWaitMax=8s`, `ErrorHandler=PassthroughErrorHandler`. `internal/rollouts/client_test.go` has 18 round-trip sub-tests including 5xx-retry-then-200, 5xx-exhaustion, 4xx-no-retry, and per-status-code error.code mappings (401/403/404/409/400/429). |
| 15  | **Living papercuts doc**: `.planning/API-PAPERCUTS.md` seeded with PC-001..PC-016 + source anchors. | VERIFIED   | `grep -cE '^### PC-' .planning/API-PAPERCUTS.md` → 16. Every PC anchor in source (PC-002/003/004/005/011/013/014) cross-references a doc entry. Doc carries the structured template (Title/Discovered/API behavior/CLI workaround/What we'd prefer/Status/Removal criteria). |
| 16  | **Envelope shape**: `schemaVersion: "rollouts.v1beta1"`, `kind`, `data`, `meta`, `error`. | VERIFIED   | `internal/rollouts/models.go:93-117` defines `Envelope` with the four named fields. `SchemaVersionV1Beta1 = "rollouts.v1beta1"`. Smoke test confirms: `./ldcli flags rollouts-beta list ...` returns `{"schemaVersion": "rollouts.v1beta1", "kind": "RolloutList|Error", ...}`. |
| 17  | **TTY-aware output, no ANSI leak to stdout/JSON.**                            | VERIFIED   | The rollouts subtree introduces zero ANSI-emitting code paths (`grep` for `ansi|color\.|fatih/color|lipgloss` in `internal/rollouts/`, `cmd/flags/rollouts/` → empty). Banner is stderr-only and never JSON-mode. Plaintext renderer uses `text/tabwriter` (no color). |

**Architectural-invariant score: 17 / 17 verified.**

---

## Requirement Coverage

| Requirement | Source Plan(s) | Evidence (code / test) | Status |
| ----------- | -------------- | ---------------------- | ------ |
| **FOUND-01**: `internal/rollouts/` package + `Client` interface | 01 | `internal/rollouts/client.go:31-34` defines `Client` interface; `RolloutsClient` concrete impl with `var _ Client = RolloutsClient{}` compile-time assertion at line 44. | VERIFIED |
| **FOUND-02**: `ldcli flags rollouts-beta` subtree with beta indicator | 01 (subtree) + 03 (banner tests) | `cmd/root.go:271` `c.AddCommand(rolloutscmd.NewRolloutsCmd(...))` inside the `flags` branch. `cmd/flags/rollouts/rollouts.go:Use: "rollouts-beta"` + `printBetaBanner`. Three banner tests in `rollouts_test.go`. | VERIFIED |
| **FOUND-03**: Versioned JSON envelope | 01 | `internal/rollouts/models.go:93-117` (`Envelope`, `EnvelopeMeta`, `EnvelopeError`); `SchemaVersionV1Beta1 = "rollouts.v1beta1"`. Used by every list response, smoke-tested. | VERIFIED |
| **FOUND-04**: Stable error contract (reframed by D-01 to envelope `error.code` taxonomy) | 02 | `internal/rollouts/errors.go:14-25` enumerates 10 `ErrCode*` constants. `mapAPIError` covers 401/403/404/409/400/429/5xx + default. Test coverage in `client_test.go` for each. | VERIFIED (per D-01 reframe) |
| **FOUND-05**: Retry/idempotency layer (`go-retryablehttp` + Idempotency-Key helper) | 02 (retry tested) | `internal/rollouts/client.go:newRetryableClient` with `RetryMax=4`, `RetryWaitMin=500ms`, `RetryWaitMax=8s`, `PassthroughErrorHandler`. `internal/rollouts/idempotency.go:SetIdempotencyKey` (wired; Phase 2 exercises). | VERIFIED |
| **FOUND-06**: Re-fetch helper (building block for start/stop/dismiss) | 01 (Get as building block) | `internal/rollouts/client.go:174-213` `Get(ctx, token, baseURI, projKey, envKey, rolloutID)` — the env-scoped GET-by-ID is the re-fetch primitive. Plan 01 PLAN.md frames FOUND-06 explicitly as "Get method on Client interface — re-fetch building block." Phase 2 will compose Get with semantic-patch as `Start → Get`. | VERIFIED (building block in place; consumer wiring deferred to Phases 2/4 by D-08, which is the intended scope) |
| **FOUND-07**: TTY-aware output | 01 (banner gating) + 03 (full surface) | `cmd/flags/rollouts/rollouts.go:shouldPrintBetaBanner` honors `term.IsTerminal(os.Stderr.Fd())` + `--output` kind. `cliflags.GetOutputKind` already drives plaintext/JSON dispatch in root command. ANSI usage is zero in rollouts subtree. | VERIFIED |
| **FOUND-08**: Stable `error.code` + `nextAction` hint | 01 (enum) + 02 (taxonomy) | `internal/rollouts/errors.go` defines `ErrCode*` constants. `mapAPIError` populates `Code` + `NextAction` for each. `internal/errors/suggestions.go.SuggestionForStatus` reused for 401/403/404/409/429. RolloutsError fields all `json:"-"` to prevent accidental info leakage. | VERIFIED |
| **DOC-01**: `API-PAPERCUTS.md` seeded with 16 cataloged papercuts | 03 | `.planning/API-PAPERCUTS.md` has 16 `### PC-NNN` entries, all with the structured template (Title / Discovered / API behavior / CLI workaround / What we'd prefer / Status / Removal criteria). Source anchors PC-002/003/004/005/011/013/014 verified to cross-reference doc entries. | VERIFIED |
| **LIST-01**: `list --flag <key>` returns deterministically ordered rollouts | 01 (verb) + 02 (real HTTP) + 03 (sort) | `cmd/flags/rollouts/list.go` exists; production sort lives in `internal/rollouts/client.go:159-165` and is mirrored at command layer. `TestListSortOrder` proves CreatedAt DESC, ID ASC end-to-end. | VERIFIED |
| **LIST-02**: Per-rollout identifying info (ID, kind, environment, state, variations, started/ended, stage index) | 02 (DTO + converter) | `Rollout` struct has `ID`, `FlagKey`, `Kind`, `EnvironmentID/Key`, `OriginalVariationID`, `TargetVariationID`, `Status (3-field)`, `CreatedAt`, `StartedAt`, `EndedAt`, `LatestStageIndex`. Plaintext detailed renderer surfaces every field. | VERIFIED |
| **LIST-03**: Filterable by `--environment` (and `--state` per ROADMAP; D-04 drops `--state` in v1) | 03 | `cmd/flags/rollouts/flags.go:28-29` registers `--environment`. `internal/rollouts/client.go:107-111` sends `filter=environmentKey:<value>` as a URL query parameter (PC-002 anchor). `--state` is explicitly rejected per D-04 — test `TestListStateFlagNotRecognized` proves it. Pagination handled via `--all` (PC-003 anchor + saturation warning). | VERIFIED (per D-04 reframe) |
| **AGENT-01**: Every command supports `--output json` regardless of TTY | 01 | `--output` is a root persistent flag inherited by every subcommand; `GetOutputKind(cmd)` resolves it. `TestListJSONOutput`, `--json` shorthand test, and TTY-banner suppression test all cover JSON-mode. | VERIFIED |
| **AGENT-02**: Exit codes follow FOUND-04 taxonomy (reframed by D-01 to `error.code` in envelope; exit code always 1) | 02 + 03 | `emitError` in `cmd/flags/rollouts/list.go` always returns an `errors.NewError(...)` which Cobra renders as exit 1; the envelope carries `error.code`. `TestListErrorEnvelope` confirms the envelope shape. | VERIFIED (per D-01 reframe) |
| **AGENT-03**: Mutating commands send `Idempotency-Key` (no mutations in Phase 1; helper wired) | 01 (helper) | `SetIdempotencyKey` exists; GET path explicitly does not send the header (`client.go:129` comment + test "List sends required headers and no Idempotency-Key on GET"). Phase 2 will exercise on Start. | VERIFIED (building block in place) |
| **AGENT-04**: RFC 3339 UTC timestamps + unit-bearing durations in JSON | 02 (converter) | `models.go:millisToTimePtr` returns `time.Time` UTC; `time.Time` marshals to RFC 3339. `Stage.Duration` is `(time.Duration * ms).String()` → "15m0s". Plaintext also uses `RFC3339` (`plaintext.go:83`). Round-trip test in `client_test.go` asserts createdAt millis → RFC 3339. | VERIFIED |
| **AGENT-05**: Deterministic sort documented in `--help` | 03 | Help text from `./ldcli flags rollouts-beta list --help`: `"Rollouts are returned in reverse-chronological order by createdAt timestamp, with rollout ID as the deterministic tiebreaker."` Sort implemented at production client AND command layer. | VERIFIED |

**Requirement score: 17 / 17 verified.**

---

## ROADMAP Success Criteria Coverage

| # | Criterion (paraphrased) | Status | Note |
| - | ----------------------- | ------ | ---- |
| 1 | `list --flag <key>` returns deterministically ordered rollouts; sort documented in `--help`. | VERIFIED | Help text + production sort + TestListSortOrder. |
| 2 | `--output json` produces well-formed envelope with RFC 3339 timestamps, unit-bearing durations, no ANSI leak, suppressed human chrome. | VERIFIED | Envelope shape, AGENT-04 conversion, banner suppression on json, no ANSI in rollouts subtree. |
| 3 | Filterable by `--environment` AND `--state running|completed|failed|stopped`; pagination transparent OR documented exit code on overflow. | RE-FRAMED | `--environment` works. `--state` explicitly dropped by CONTEXT.md D-04 with a negative-assertion test (`TestListStateFlagNotRecognized`). `--all` plus saturation warning replaces transparent pagination (PC-003). |
| 4 | 4xx/5xx/auth/transient/unknown maps to **distinct documented exit code** + JSON `error.code` + `nextAction`. | RE-FRAMED | Distinct envelope `error.code` per status (401/403/404/409/400/429/5xx/network/unknown) — verified. Distinct **exit code** per failure mode is explicitly dropped by CONTEXT.md D-01 ("Exit codes stay consistent with the rest of ldcli — any error returns exit 1. No numeric taxonomy"); error info lives in envelope's `error.code` instead. |
| 5 | `.planning/API-PAPERCUTS.md` exists with 16 entries + every workaround annotated with `// PAPERCUT: PC-NNN`. | VERIFIED | 16 PC entries in doc; 7 distinct PC anchors in source (PC-002/003/004/005/011/013/014), each cross-referenced. |

Criteria 3 and 4 are intentionally narrowed by planner-locked decisions in CONTEXT.md (D-01, D-04). These deviations are:
- Visible in source (active negative-assertion tests for `--state` rejection, exit-code consistency)
- Documented in CONTEXT.md `<decisions>` section
- Reflected in REQUIREMENTS.md interpretation (D-01: "FOUND-04 collapses to documented `error.code` enum"; D-04: "filter by raw `status` values directly via API only when/if needed")

These are not phase failures — they are scope decisions ratified at phase planning time.

---

## Build & Test

| Check | Result |
| ----- | ------ |
| `make build` | PASS — produces `./ldcli` binary |
| `make test` | PASS — all 27 packages green |
| `go test ./internal/rollouts/...` | PASS — `TestStatusMapping` (17 sub-tests) + `TestDeriveStatusBlockMatchesMapStatus` + `TestRolloutsClient` (18 round-trip sub-tests) |
| `go test ./cmd/flags/rollouts/...` | PASS — 14 list-verb integration sub-tests + 3 banner-suppression sub-tests |
| `./ldcli flags rollouts-beta list --help` | Shows long description with sort contract + all 4 new flags (--environment, --limit, --all, --detailed) + PC-003 reference |
| `./ldcli flags rollouts-beta list --flag X --project Y --access-token T --base-uri https://invalid.example.test --output json` | Returns valid envelope: `{schemaVersion: "rollouts.v1beta1", kind: "Error", error: {code: "network_error", message: "...", nextAction: "..."}}`; exit code 1 |

---

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Help text mentions sort contract | `./ldcli flags rollouts-beta list --help | grep -E "reverse-chronological|tiebreaker"` | Two lines match | PASS |
| Network error returns well-formed envelope with exit 1 | `./ldcli flags rollouts-beta list --flag X --project Y --access-token T --base-uri https://invalid.example.test --output json; echo $?` | Envelope JSON + exit 1 | PASS |
| Rollouts-beta subtree visible | `./ldcli flags rollouts-beta --help` | Lists `list` subcommand and beta warning | PASS |
| All cross-package tests pass | `make test` | 27 packages green | PASS |

---

## Anti-Patterns Scan

| File | Pattern | Severity | Impact |
| ---- | ------- | -------- | ------ |
| (none) | No `TBD`/`FIXME`/`XXX` markers in any rollouts-subtree file modified by this phase | N/A | Clean — `grep -rE "TBD|FIXME|XXX" internal/rollouts/ cmd/flags/rollouts/` returns no debt markers. |
| `internal/rollouts/idempotency.go` | `SetIdempotencyKey` has no production call site | Info | Intentional — wired for Phase 2 mutations per CONTEXT.md and D-08. Not a stub: helper is callable and tested implicitly via the GET test asserting no Idempotency-Key on GETs. |
| `internal/rollouts/instructions.go` | `StartInstruction`/`StopInstruction`/`DismissRegressionInstruction` have only `Kind` field | Info | Intentional — D-08 ships Client interface with `List`+`Get` only; instruction structs are Phase 2 / Phase 4 scaffolding (Plan 01 explicitly documents this). |
| `01-03-SUMMARY.md` table mapping FOUND-06 to "Logger=nil" | Documentation hygiene | Info | Cosmetic — the FOUND-06 requirement is "re-fetch helper, encoded once, used by start/stop/dismiss." Plan 01 PLAN.md correctly frames FOUND-06 as `Get` on the Client (the re-fetch primitive). Plan 03 SUMMARY's table mis-attributes evidence; the substantive claim that FOUND-06 is covered in scope is correct. |

No blockers, no warnings.

---

## Gaps

None.

---

## Recommendations

For the next phase (Phase 2: Start a rollout) the verifier flags two scope items that will move from "wired but untested" to "exercised":

1. **`SetIdempotencyKey` exercise**: Phase 2's `start` verb must call `SetIdempotencyKey(req, key)` on the PATCH path. The helper is already production-ready; Phase 2 wires the call site and adds a CLI `--idempotency-key` flag.
2. **Re-fetch composition (FOUND-06 consumer)**: Phase 2 success criterion 4 ("after the patch mutation succeeds, the CLI follows up with a GET ... surfaces the new rollout's ID") will compose the existing `Get` method into a `Start → Get` flow. The building block is in place from Phase 1.

For the Phase 1 → Phase 2 hand-off:

- `RolloutError` JSON tags are stricter than necessary (`json:"-"` on every field instead of just `RawBody`). This is correct behavior — the user-facing surface is `EnvelopeError` — but Phase 2's start verb should continue to use `errors.As(err, &rerr)` to extract Code/Message/NextAction, not direct marshaling.
- `NewClientWithRetryWaitsForTest` is the test-only constructor used by `client_test.go`. Phase 2's mutation tests should reuse this pattern.

Cosmetic follow-up (non-blocking, would not change verdict):

- Plan 03's SUMMARY table cell mapping FOUND-06 to "Logger=nil" should be corrected to "`Get` method on Client interface — re-fetch building block" (Plan 01 PLAN.md's wording) on the next pass through the SUMMARY review.

---

## Verifier Notes

- This was a thorough goal-backward verification. Started from CONTEXT.md decisions (D-01..D-08, banner, sort, saturation, idempotency, status mapping), grepped source for each invariant, then walked the requirement IDs and ROADMAP success criteria with cross-reference to PLAN scope.
- SUMMARY.md claims were not trusted; every claim was checked against source. The one cosmetic error found (Plan 03's FOUND-06 → Logger=nil mis-attribution) does not change the verification outcome because the substantive claim that FOUND-06 is in scope is correct (Plan 01 PLAN.md's framing of FOUND-06 as `Get`-as-building-block is the authoritative one).
- ROADMAP success criteria 3 and 4 are explicitly re-framed by CONTEXT.md decisions D-04 and D-01 respectively. The verifier accepts these as scope decisions rather than failures, with negative-assertion tests confirming the dropped surface (`TestListStateFlagNotRecognized`) and an explicit-exit-1 implementation (no `os.Exit([2-9])` anywhere in the rollouts subtree).

---

_Verified: 2026-05-12T22:26:36Z_
_Verifier: Claude (goal-backward verification per `references/mandatory-initial-read.md` + `references/gates.md`)_
