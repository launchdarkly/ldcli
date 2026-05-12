---
phase: 01-list-foundation-first-end-to-end-slice
plan: 01
subsystem: cli
tags: [cobra, viper, retryablehttp, json-envelope, beta-banner, walking-skeleton]

# Dependency graph
requires:
  - phase: 0
    provides: existing ldcli baseline (cobra/viper, internal/* Client pattern, internal/output, internal/analytics, internal/errors)
provides:
  - "internal/rollouts/ package skeleton with Client interface (List+Get only, D-08)"
  - "v1beta1 JSON envelope DTOs (Envelope, EnvelopeError, EnvelopeMeta, RolloutList, Rollout, StatusBlock) with nested status shape (D-02) and no Reason field (D-03)"
  - "error.code enum constants + RolloutError type + skeleton mapAPIError/mapTransportError"
  - "Idempotency-Key helper (wired but not exercised in Phase 1)"
  - "Stub SemanticPatch / StartInstruction / StopInstruction / DismissRegressionInstruction types"
  - "testify-based MockClient with nil-safe pointer extraction"
  - "cmd/flags/rollouts/ Cobra subtree (parent + list verb + initListFlags + plaintext placeholder)"
  - "Beta banner emission on stderr gated by TTY AND non-JSON output kind"
  - "cmd/root.go wiring: APIClients.RolloutsClient field, rollouts.NewClient(version), rolloutscmd.NewRolloutsCmd under flags subtree"
  - "End-to-end stub-mode invocation: ldcli flags rollouts-beta list --flag X --project Y --output json returns valid envelope on stdout, exit 0"
affects: [02-real-http, 03-flag-surface-and-papercuts, 04-stop-and-dismiss]

# Tech tracking
tech-stack:
  added: [github.com/hashicorp/go-retryablehttp v0.7.8]
  patterns:
    - "Typed Client interface + concrete struct + var _ Client = ... compile-time assertion (mirrors internal/flags/)"
    - "Versioned JSON envelope with schemaVersion / kind / data / meta / error"
    - "Nested status block {status, kind, label} to resolve the kind-collision between rollout kind and lifecycle bucket"
    - "Banner emission gated by TTY AND non-JSON output (stderr only, never stdout)"
    - "RunE closure reads Viper at request time, NOT constructor time (ldcli CONVENTIONS.md anti-pattern guard)"
    - "Direct json.MarshalIndent for the typed envelope rather than routing through output.CmdOutput (which assumes flat resource maps)"

key-files:
  created:
    - internal/rollouts/client.go
    - internal/rollouts/models.go
    - internal/rollouts/envelope.go
    - internal/rollouts/errors.go
    - internal/rollouts/idempotency.go
    - internal/rollouts/instructions.go
    - internal/rollouts/mock_client.go
    - internal/rollouts/client_test.go
    - cmd/flags/rollouts/rollouts.go
    - cmd/flags/rollouts/list.go
    - cmd/flags/rollouts/flags.go
    - cmd/flags/rollouts/plaintext.go
  modified:
    - cmd/root.go
    - go.mod
    - go.sum

key-decisions:
  - "D-01/D-08 honored: Client interface ships exactly List + Get; no numeric exit code taxonomy; any error returns exit 1"
  - "D-02 honored: Rollout.Status is a nested StatusBlock {status, kind, label}; Rollout.Kind is the top-level rollout kind (guarded|progressive); the kind-collision is resolved by nesting"
  - "D-03 honored: NO Reason field on Rollout or StatusBlock; agent-facing reason info goes through status.label only"
  - "D-07 honored: JSON output always emits the full envelope (schemaVersion, kind, data, meta)"
  - "Beta banner suppressed when --output json OR when stderr is not a TTY"
  - "--idempotency-key user-facing flag NOT exposed in Phase 1 (no mutations to exercise it); SetIdempotencyKey helper exists but is unused"
  - "RolloutError.RawBody is intentionally not serialized into the envelope (T-01-02 threat mitigation)"
  - "newRetryableClient sets Logger=nil so go-retryablehttp does not log request URLs or Authorization headers (T-01-08)"
  - "Vendor directory not regenerated: the repo dropped its vendor/ in commit 5cd5a84; CI builds from go.sum. The plan's mention of vendor/modules.txt is vestigial — go.mod/go.sum cover the dependency lockfile."

patterns-established:
  - "Typed envelope marshaling: command layer marshals rollouts.Envelope via json.MarshalIndent (does NOT route through internal/output/CmdOutput which is designed for flat maps)"
  - "Error envelope path: errors.As(err, &*RolloutError) extracts code/message/nextAction; falls back to ErrCodeUnknownUpstream when the error is not a typed RolloutError"
  - "Banner gating: shouldPrintBetaBanner returns false when --output json OR stderr is not a TTY; the helper centralizes the rule so future verbs do not re-implement"
  - "Stub method bodies in client.go return non-nil zero values (&RolloutList{Items: []Rollout{}}, &Rollout{}) so the envelope shape is provable end-to-end without inventing fake data"

requirements-completed:
  - FOUND-01
  - FOUND-02
  - FOUND-03
  - FOUND-06
  - FOUND-07
  - FOUND-08
  - AGENT-01
  - LIST-01

# Metrics
duration: ~25min
completed: 2026-05-12
---

# Phase 1 Plan 1: Walking Skeleton (List foundation) Summary

**JWT-style v1beta1 envelope, retryablehttp wiring, Cobra subtree, and stub end-to-end pipeline for `ldcli flags rollouts-beta list` — all in one atomic deliverable, ready for Plan 02 to swap stub bodies for real HTTP.**

## Performance

- **Duration:** ~25 minutes
- **Started:** 2026-05-12T21:22:00Z (approx)
- **Completed:** 2026-05-12T21:47:51Z
- **Tasks:** 2 / 2
- **Files created:** 12 (8 in internal/rollouts/, 4 in cmd/flags/rollouts/)
- **Files modified:** 3 (cmd/root.go, go.mod, go.sum)

## Accomplishments

- Locked the v1beta1 envelope contract (`schemaVersion`, `kind`, `data`, `meta`, `error`) with a working end-to-end stub: `./ldcli flags rollouts-beta list --flag X --project Y --output json` returns a syntactically valid envelope on stdout, exit 0, empty stderr.
- Locked the architectural decisions in source: D-01 (exit code 1 for any error, no numeric taxonomy), D-02 (nested three-field status), D-03 (no Reason field), D-07 (full JSON field set), D-08 (Client interface = List + Get only).
- Wired the entire pipeline so Plan 02 only needs to fill in the HTTP body + status mapping + error classification inside `internal/rollouts/`, and Plan 03 only needs to add the flag surface + plaintext table + papercuts doc. No further architectural churn expected on the envelope or interface shape.

## Task Commits

1. **Task 1: Add go-retryablehttp dependency and scaffold internal/rollouts/ package** — `95064c3` (feat)
   - 10 files changed, 493 insertions; tdd RED→GREEN cycle (test written before source, RED run confirmed package didn't exist, GREEN run confirmed all subtests pass)
2. **Task 2: Build cmd/flags/rollouts/ Cobra subtree and wire into cmd/root.go** — `ccca81b` (feat)
   - 5 files changed, 253 insertions, 1 deletion; end-to-end smoke verified against built binary

**Plan metadata commit:** (this SUMMARY.md commit follows below)

## Files Created/Modified

**New files (internal/rollouts/):**
- `internal/rollouts/client.go` — Client interface (List + Get per D-08), RolloutsClient struct with *retryablehttp.Client field, NewClient(version), unexported newRetryableClient() helper with RetryMax=4, RetryWaitMin=500ms, RetryWaitMax=8s, Logger=nil
- `internal/rollouts/models.go` — Rollout, RolloutList, Stage, Event, MetricConfiguration, Link, StatusBlock, Envelope, EnvelopeError, EnvelopeMeta types and `SchemaVersionV1Beta1 = "rollouts.v1beta1"` const
- `internal/rollouts/envelope.go` — `NewListEnvelope(*RolloutList) Envelope` (sets meta.fetchedAt = time.Now().UTC()) and `NewErrorEnvelope(code, message, nextAction string) Envelope`
- `internal/rollouts/errors.go` — RolloutError type with Error() / Is() methods; ErrCode* enum constants (10 of them); skeleton mapAPIError + mapTransportError returning ErrCodeUnknownUpstream / ErrCodeNetworkError (Plan 02 fills mapping)
- `internal/rollouts/idempotency.go` — SetIdempotencyKey(req, key) returning the effective key; generates UUIDv4 when key is empty
- `internal/rollouts/instructions.go` — SemanticPatch + StartInstruction + StopInstruction + DismissRegressionInstruction struct skeletons (bodies in Phase 2 / Phase 4)
- `internal/rollouts/mock_client.go` — testify-based MockClient with nil-safe pointer extraction for List / Get returns
- `internal/rollouts/client_test.go` — sanity test verifying stub envelope shape (5 subtests; Plan 02 replaces with httptest.NewServer round-trip)

**New files (cmd/flags/rollouts/):**
- `cmd/flags/rollouts/rollouts.go` — `NewRolloutsCmd(client rollouts.Client, analyticsTrackerFn analytics.TrackerFn)`; PersistentPreRun emits `flags-rollouts-beta` analytics event and (if TTY && not JSON) the two-line beta banner to stderr
- `cmd/flags/rollouts/list.go` — `NewListCmd(client rollouts.Client)` and runE closure; reads accessToken/baseURI/project/flag from Viper at RunE time; marshals envelope via json.MarshalIndent; error path uses errors.As + ErrCodeUnknownUpstream fallback
- `cmd/flags/rollouts/flags.go` — `initListFlags(cmd)` registering --flag and --project as required (reuses existing cliflags.FlagFlag and cliflags.ProjectFlag; no new constants in Plan 01)
- `cmd/flags/rollouts/plaintext.go` — `RenderRolloutListPlaintext(list, detailed bool)` placeholder returning "No rollouts found.\n" or one tab-separated line per item (Plan 03 replaces with a real 5-column table)

**Modified:**
- `cmd/root.go` — added `rolloutscmd` and `internal/rollouts` imports; added `RolloutsClient rollouts.Client` to APIClients struct; added `RolloutsClient: rollouts.NewClient(version)` to Execute(); added `c.AddCommand(rolloutscmd.NewRolloutsCmd(clients.RolloutsClient, analyticsTrackerFn))` inside the existing `if c.Name() == "flags"` branch
- `go.mod`, `go.sum` — added `github.com/hashicorp/go-retryablehttp v0.7.8` as a direct dependency (and its transitive `github.com/hashicorp/go-cleanhttp v0.5.2`)

## A2 Investigation Result (per plan output spec)

**A2: Does the upstream API response include `environmentKey`?**

**Outcome: DEFERRED to Plan 02.** Plan 01 ships a stub `List` / `Get` that returns hardcoded zero-value `RolloutList` / `Rollout`; no real HTTP call exists yet against staging or any environment, so we cannot empirically confirm whether the upstream `automated-releases` API response body carries `environmentKey` directly or only `environmentId` (UUID).

The `Rollout` struct defensively includes BOTH fields (`EnvironmentID string` + `EnvironmentKey string` with `omitempty`) so the DTO is forward-compatible with either upstream behavior:
- If the API returns `environmentKey` directly → the converter (Plan 02) populates `Rollout.EnvironmentKey` from the response field.
- If `environmentKey` is missing → the converter (Plan 02) parses it from `_links.self.href` and logs a new papercut `PC-NEW-environmentKey-missing-in-list` for the Plan 03 papercuts doc.

**Action for Plan 02:** First task should hit a staging fixture (or the live staging API via `httptest`-replay) to capture the actual response shape, then choose the converter path and update `internal/rollouts/models.go`'s field comment to record the outcome.

## CLI Version Flowing Into RolloutsClient (per plan output spec)

The `cliVersion` field on `RolloutsClient` is populated from `cmd/root.go`'s `Execute(version string)` argument, which is in turn set by `main.go` from the `-X 'main.version={{.Version}}'` linker flag (per GoReleaser config). At runtime in this worktree it resolves to `"dev"` (when invoked from a non-release build via `go build` without `-ldflags`); in test contexts (`cmd.CallCmd`) it resolves to `"test"`. Plan 02's eventual User-Agent header should follow the existing `internal/resources/client.go` convention: `fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion)`.

## Decisions Made

- **Vendor directory NOT regenerated.** The repo dropped `vendor/` in commit `5cd5a84` ("chore: remove vendor dir, fix release action, CI for dev server UI") and CI now builds from `go.sum`. The plan's `files_modified` lists `vendor/modules.txt` as a vestige; updating go.mod + go.sum is the actual deliverable. No deviation flagged because the plan's success criterion is dependency lockfile correctness, which go.mod + go.sum satisfy.
- **Envelope marshaling bypasses `internal/output/CmdOutput`.** That dispatcher operates on flat `map[string]interface{}`-style resource bodies and would lose the typed envelope shape. The list verb uses `json.MarshalIndent(env, "", "  ")` directly. This is documented in the plan's `<action>` section so it's not a deviation.
- **Stub `List`/`Get` return non-nil zero values.** `List` returns `&RolloutList{Items: []Rollout{}}` and `Get` returns `&Rollout{}`. Both are nil-safe for downstream marshaling. Documented in the plan's `<behavior>` section.

## Deviations from Plan

**Auto-fixed Issues:**

**1. [Rule 3 - Blocking] gofmt reordering of `cmd/root.go` import block**

- **Found during:** Task 2 (immediately after editing the import block).
- **Issue:** The pre-existing `cmd/root.go` import block had `sdk_active` listed before `resources` — wrong alphabetical order. `gofmt -l` did not flag the file in isolation (since the imports were already in a consistent state), but `gofmt -d` and `gofmt -w` both reorder it. Adding my new `rolloutscmd` import triggered re-running gofmt on the file, which exposed the pre-existing inconsistency.
- **Fix:** Ran `gofmt -w cmd/root.go cmd/flags/rollouts/*.go`. This reordered the existing `sdkactivecmd` / `resourcecmd` lines AND added my new `rolloutscmd` import in the alphabetically correct slot.
- **Files modified:** `cmd/root.go` (one extra line-reorder beyond the planned three modification sites).
- **Verification:** `gofmt -l cmd/root.go cmd/flags/rollouts/*.go` returns empty; `go build ./...` succeeds; `make test` passes.
- **Committed in:** `ccca81b` (rolled into Task 2 commit since the gofmt run is part of the standard "format before commit" step).

This is a Rule 3 (blocking) auto-fix because pre-commit hooks (`golangci-lint`) and CI would otherwise reject a file that gofmt wants to rewrite. Scope: limited to the import block reorder; no other pre-existing issues in the file were touched.

**No other deviations.** Both tasks executed exactly as planned. Banner copy, envelope shape, Client interface scope, error.code enum, retry constants, threat-model mitigations (T-01-01 through T-01-08) all landed as specified.

## Known Stubs (intentional — replaced by Plan 02 / Plan 04)

These stubs are part of the Walking Skeleton contract; they prove the plumbing without inventing fake data. Each is replaced by a later plan.

| Location | Stub | Replacement plan |
|---|---|---|
| `internal/rollouts/client.go` (List) | Returns `&RolloutList{Items: []Rollout{}}, nil` — no HTTP, no retry exercised | Plan 02: real GET /internal/projects/.../automated-releases + DTO conversion |
| `internal/rollouts/client.go` (Get) | Returns `&Rollout{}, nil` — no HTTP | Plan 02: real GET /internal/projects/.../automated-releases/{id} |
| `internal/rollouts/errors.go` (mapAPIError) | Returns `RolloutError{Code: ErrCodeUnknownUpstream, Message: "Phase 2 will refine; upstream returned <status>"}` | Plan 02: full status-code → error.code mapping table per FOUND-08 enum |
| `internal/rollouts/errors.go` (mapTransportError) | Returns `RolloutError{Code: ErrCodeNetworkError, Message: err.Error()}` | Plan 02: distinguish timeouts / DNS / TLS errors |
| `internal/rollouts/idempotency.go` (SetIdempotencyKey) | Defined but never called anywhere in Plan 01 | Plan 02 (Start instruction) calls it on the PATCH path |
| `internal/rollouts/instructions.go` (StartInstruction/StopInstruction/DismissRegressionInstruction) | All have only a `Kind string` field | Plan 02 fleshes `StartInstruction`; Plan 04 fleshes the rest |
| `cmd/flags/rollouts/plaintext.go` (RenderRolloutListPlaintext) | Tab-separated lines, no alignment | Plan 03: real 5-column aligned table per D-06 |

These are documented in the plan's `<done>` section as expected end-state for Plan 01.

## Threat Surface Scan

No new threat surface beyond what is already in the plan's `<threat_model>`. Mitigations verified in source:

- T-01-01 (banner info disclosure): banner copy contains only the literal "beta unstable" line plus the CLI version interpolated from `cmd.Root().Version`. No tokens, project keys, or environment values appear in the banner code path. ✓
- T-01-02 (error envelope info disclosure): `RolloutError.RawBody` has no JSON tag and is never marshaled into the envelope. ✓
- T-01-03 (dependency tampering): `github.com/hashicorp/go-retryablehttp v0.7.8` is pinned exactly in `go.mod`; `go.sum` carries the lockfile hashes. ✓
- T-01-04 (analytics spoofing): event `flags-rollouts-beta` uses the existing analytics tracker pattern; `analytics-opt-out` continues to gate it. ✓
- T-01-05 (retry DoS): `RetryMax=4`, `RetryWaitMax=8s` cap total retry wall time at ~16s. ✓
- T-01-07 (idempotency): SetIdempotencyKey helper generates UUIDv4 via `google/uuid`; Phase 2 exercises it. ✓
- T-01-08 (token leakage in retryablehttp logs): `c.Logger = nil` in `newRetryableClient()`. ✓

No new flags to file.

## Verification

**End-to-end smoke test (from plan `<verification>`):**

```text
$ ./ldcli flags rollouts-beta list --flag X --project Y --access-token T --output json --base-uri https://example.test
exit=0
stdout:
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "RolloutList",
  "data": { "items": [] },
  "meta": { "fetchedAt": "2026-05-12T21:46:53.967169Z" }
}
stderr: (empty)
```

**Architecture lock-in checks (from plan `<verification>`):**

- `grep -cE "^\s*(List|Get)\(" internal/rollouts/client.go` → 2 (D-08 satisfied)
- `grep -rE "os\.Exit\([2-9]\)" cmd/flags/rollouts/ internal/rollouts/` → (no matches; D-01 satisfied)
- `grep -c "Status\s*StatusBlock" internal/rollouts/models.go` → 1 (A1 / D-02 nested shape satisfied)

**Regression:**

- `make test` → all packages pass (38 packages, 0 failures).
- `go build ./...` → succeeds (zero compile errors).
- `make build` → produces working `./ldcli` binary.

## Self-Check: PASSED

Files created exist:
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/client.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/client_test.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/envelope.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/errors.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/idempotency.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/instructions.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/mock_client.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/internal/rollouts/models.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/cmd/flags/rollouts/rollouts.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/cmd/flags/rollouts/list.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/cmd/flags/rollouts/flags.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a69181e2bc9424779/cmd/flags/rollouts/plaintext.go` — FOUND

Commits exist in git log:
- `95064c3` (Task 1) — FOUND
- `ccca81b` (Task 2) — FOUND
