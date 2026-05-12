# Phase 1: List (foundation + first end-to-end slice) - Context

**Gathered:** 2026-05-12
**Status:** Ready for planning

<domain>
## Phase Boundary

Ship `ldcli flags rollouts-beta list --flag <key>` end-to-end, plus the shared infrastructure every later phase will reuse:

- `internal/rollouts/` package skeleton (hand-rolled types, `Client` interface, mock)
- Versioned JSON output envelope (`schemaVersion: "rollouts.v1beta1"`, `kind`, `data`, `meta`)
- Structured `error.code` taxonomy in the JSON envelope (exit code stays `1` for any error — see D-01)
- `go-retryablehttp` retry layer (4 retries, 500ms–8s backoff, 4xx never retried)
- `Idempotency-Key` header on all mutations (no mutations in Phase 1; the layer is wired but exercised in Phase 2)
- TTY-aware output: TTY → plaintext default; non-TTY or `--output json` → JSON
- Beta banner on TTY (suppressed when piped or JSON)
- `.planning/API-PAPERCUTS.md` seeded with the 16 entries from architecture research

</domain>

<decisions>
## Implementation Decisions

### Exit codes & error contract
- **D-01:** Exit codes stay consistent with the rest of ldcli — any error returns exit `1`. **No numeric taxonomy.** This explicitly reframes REQ-AGENT-02 and FOUND-04 from "numeric exit-code taxonomy" to "structured `error.code` taxonomy in the JSON envelope."
  - Rationale: The JSON envelope already carries `error.code` + `error.nextAction`, which is richer than any exit code. Agents read JSON. Adding a numeric taxonomy means more code, more places to keep consistent, and minimal added value over what's in the envelope.
  - Downstream impact: FOUND-04 collapses to "documented `error.code` enum on the JSON envelope." SIGINT during `--watch` (Phase 3) still uses exit `130` per Go stdlib convention since that's emitted by `signal.NotifyContext`, not by our code.

### Status display model
- **D-02:** Every rollout-describing response carries a **three-field status model**:
  ```json
  {
    "status": "<raw API status>",      // exact API passthrough
    "kind": "active|regressed|reverted|paused|completed",
    "label": "<human-readable string with reason inline>"
  }
  ```
  - `status` is the raw API value (`not_started`, `in_progress`, `waiting`, `monitoring_regressed`, `completed`, `reverted`, `manually_completed`, `manually_reverted`, `srm_stopped`, `monitoring_stopped`, `archived`).
  - `kind` is a 5-bucket lifecycle classifier derived from UI `guardedRolloutUIStates`. **UI's `neutral` is renamed to `paused`** (operationally accurate; UI copy uses "paused at N%").
  - `label` is the human-readable string with contextual reason inline (e.g. `"rolled back automatically after detecting a regression for latency-p99"`). Mirrors UI labels for parity (REQ-UX-01).
- **D-03:** **Structured `reason` object is deferred.** Phase 1 emits `status` + `kind` + `label` only. `label` is the agent-parseable stop-gap; if usage shows agents struggling to parse reasons, we lift them to a structured `reason: { type, metrics, rule, trafficAllocation }` field in a later phase.
- **D-04:** **`--state` filter is dropped from v1.** REQ-LIST-03's filter-by-state is not essential and the API's status enum is messy (11+ values across mixed axes — see Papercut P5). Filter by raw `status` values directly via API only when/if needed. `--environment` filter is kept.

### `list` command shape
- **D-05:** **Default scope:** most recent 20 rollouts (`--limit 20`), reverse-chronological. `--all` returns the full history. Stable ordering documented in `--help`.
- **D-06:** **Plaintext layout:** narrow 5-column table by default — `ID`, `kind`, `environment`, `state/label`, `started`. `--detailed` adds variations, ended-at, current stage index, raw API status.
- **D-07:** **JSON output always emits the full field set** regardless of `--detailed`. JSON is for machines; truncation is a plaintext-only concern.

### `Client` interface scope
- **D-08:** **Grow the `internal/rollouts/Client` interface incrementally.** Phase 1 ships only:
  ```go
  type Client interface {
      List(ctx, token, baseURI, projKey, flagKey, opts ListOpts) (*RolloutList, error)
      Get(ctx, token, baseURI, projKey, envKey, rolloutID string) (*Rollout, error)
  }
  ```
  `Get` is included because `List` may need it as a follow-up (some endpoints return summaries; `Get` returns full detail). Phase 2 adds `Start`. Phase 4 adds `Stop` + `DismissRegression`. Mocks regenerated per phase via `mockgen`.

### Claude's Discretion

- Internal layout under `internal/rollouts/` (file split for `client.go`, `models.go`, `instructions.go`, `mock_client.go`) — follow `internal/flags/` convention.
- Exact field names inside the rollout model — follow API names where unambiguous; rename only when the API name is misleading (e.g. UI's `treatmentVariationId` → `targetVariationId` happened upstream, so we use `targetVariationId`).
- Retry policy specifics within the 4 retries / 500ms–8s envelope (e.g. retry-after honoring, jitter percentage).
- Beta banner exact copy and placement (stderr-only when TTY; suppressed when piped or `--output json`).
- Whether to expose `--idempotency-key` user-facing flag in Phase 1 (no mutations to exercise it) or wait until Phase 2 — researcher/planner pick.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project planning
- `.planning/PROJECT.md` — project overview, constraints, locked decisions
- `.planning/REQUIREMENTS.md` — FOUND-01..08, DOC-01, LIST-01..03, AGENT-01..05 (Phase 1's scope)
- `.planning/ROADMAP.md` — Phase 1 goal, success criteria, dependencies
- `.planning/STATE.md` — accumulated decisions, open questions, performance targets

### Research (drives implementation)
- `.planning/research/SUMMARY.md` — synthesis of cross-cutting decisions
- `.planning/research/ARCHITECTURE.md` — gonfalon `automated-releases` API inventory; 16 papercuts (P1–P16); status enum semantics; endpoint shapes; auth model
- `.planning/research/STACK.md` — JSON envelope shape, retry policy (`go-retryablehttp` v0.7.7), TTY detection, Idempotency-Key pattern, watch-pattern reference
- `.planning/research/FEATURES.md` — Exit code & JSON envelope proposals (note: D-01 supersedes the numeric exit-code taxonomy proposed here)
- `.planning/research/PITFALLS.md` — 16 anti-patterns to avoid (output contract designed late, --watch missing inter-poll transitions, hidden coupling to unstable shapes, etc.)

### Codebase patterns (existing ldcli)
- `.planning/codebase/ARCHITECTURE.md` — system overview, component table, layer boundaries
- `.planning/codebase/CONVENTIONS.md` — naming, error handling, dependency-injection pattern
- `.planning/codebase/STACK.md` — existing dependencies and runtime requirements
- `cmd/flags/toggle.go` — closest existing analog for new Cobra subcommand under `flags`
- `internal/flags/client.go` — existing `Client` interface pattern to mirror
- `internal/output/output.go` + `outputters.go` — existing output dispatch (`OutputKind`, `Outputter` interface) — D-07's full-fields-in-JSON contract layers on top of this
- `cmd/root.go:282` (`Execute`) and `cmd/root.go:109` (`NewRootCommand`) — where new clients get wired

### LD UI parity reference
- gonfalon: `static/ld/components/automated-rollouts/guarded-rollouts/results/GuardedRolloutResults/hooks/GuardedRolloutUIStates.tsx` — authoritative source for `kind` bucket (`active`/`regressed`/`reverted`/`paused`/`completed`) and `label` string formulation. The `guardedRolloutUIStates` array's predicates and `RolloutStatusLabel` components define what the CLI must mirror.
- gonfalon: `internal/experimentation/releaseguardian/internal/api/automated_release_transformations.go` — raw status enum source of truth; `domainStatusToAutomatedReleaseStatus` mapping.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`cmd/flags/toggle.go`**: Pattern for Cobra subcommand under `flags`; flag registration via `cmd/cliflags`; Viper-bound persistent flags. New `cmd/flags/rollouts/list.go` should mirror this.
- **`internal/output/output.go`** (`OutputKind`, `Outputter`, `CmdOutput`, `CmdOutputSingular`): Existing JSON/plaintext/markdown dispatch. The `rollouts.v1beta1` envelope wraps the `data` field but flows through this dispatch — extend, don't replace.
- **`internal/errors/errors.go`**: `errors.NewError`, `errors.NewLDAPIError`, `errors.APIError`. Existing error normalization. New `rollouts` errors hang off this with `error.code` taxonomy in the envelope.
- **`google/uuid` (v1.6.0)**: Already vendored. Use directly for `Idempotency-Key`.
- **`golang.org/x/term` (v0.33.0)**: Already a transitive dep. Use `term.IsTerminal(int(os.Stdout.Fd()))` for TTY detection. **Do not add `mattn/go-isatty`.**

### Established Patterns
- **`internal/<domain>/Client` interface + concrete `<Domain>Client` struct + `var _ Client = <Domain>Client{}` compile-time assertion**: Mirror in `internal/rollouts/`.
- **`mockgen`-generated mocks in same package**: `internal/rollouts/mock_client.go` follows existing pattern.
- **Reading Viper at `RunE` time, not constructor time**: Required by anti-pattern doc; CONVENTIONS.md captures this. Applies to new rollout commands.
- **`PersistentPreRun` analytics tracking** (`cmd/analytics/analytics.go`): New `rollouts-beta` commands need analytics events.
- **Existing CLI returns exit 1 for any error** (`cmd/root.go:331`): D-01 keeps consistency with this.

### Integration Points
- **`cmd/root.go:109` (`NewRootCommand`)**: Where new `RolloutsClient` is instantiated and where `flags rollouts-beta` subtree is wired. Pattern: client passed to `cmd/flags/rollouts.NewRolloutsCmd(rolloutsClient)`.
- **`cmd/flags/`**: New `rollouts-beta` subcommand registered here (sibling to `toggle.go`, `archive.go`). Probably as `cmd/flags/rollouts/` sub-package to scope the surface.
- **`cmd/cliflags/flags.go`**: New flag constants (`--detailed`, `--limit`, `--all`, `--environment` if not already shared, `--idempotency-key` if exposed in Phase 1).
- **`internal/output/`**: New plaintext rendering function for the rollouts list — register alongside existing per-resource plaintext functions.
- **`internal/resources/Client` (existing generic HTTP wrapper)**: New `RolloutsClient` does NOT reuse this — it has its own `retryablehttp.Client` wired in. Other ldcli commands continue to use `resources.Client` unchanged.

</code_context>

<specifics>
## Specific Ideas

- **UI parity is load-bearing for the status display model.** The `label` field's formulation should match the LD UI's `RolloutStatusLabel` output as closely as practical (REQ-UX-01). Researcher should extract the exact label strings from `GuardedRolloutUIStates.tsx`.
- **The 13 distinct displayable states from the UI inventory** are the target surface:
  - `not_started` / `waiting` → "Monitoring [rule]" (kind: active)
  - `in_progress` (min sample not reached) → "Monitoring [rule] for regressions… (not enough data)" (active)
  - `in_progress` (min sample reached) → "Monitoring [rule] for regressions…" (active)
  - `in_progress` (extended) → "Monitoring extended by [duration]" (active)
  - `monitoring_regressed` → "Regressions detected on [rule] for [metric names]" (regressed)
  - `monitoring_stopped` → "[rule] paused at [N]%: regressions detected for [metric names]" (paused)
  - `srm_stopped` → "[rule] paused at [N]%: sample ratio mismatch detected" (paused)
  - `completed` → "Monitoring completed on [rule]" (completed)
  - `manually_completed` → "[rule] rolled forward manually" (completed)
  - `manually_reverted` → "[rule] rolled back manually" (reverted)
  - `reverted` (insufficient sample) → "[rule] rolled back due to insufficient sample size" (reverted)
  - `reverted` (SRM event) → "[rule] rolled back automatically" (reverted)
  - `reverted` (regression) → "[rule] rolled back automatically after detecting a regression for [metric names]" (reverted)
  - `archived` → "Monitoring of [rule] stopped early" (paused)

</specifics>

<deferred>
## Deferred Ideas

- **`--state` filter on `list`** — not essential for v1; raw `status` filter via API can be added if usage demands it. Revisit after Phase 3 (when `status --watch` exists) to see if filter-by-state has become useful for operators.
- **Structured `reason` object on the status model** — Phase 1 ships `status` + `kind` + `label`. If agents struggle to parse reasons out of `label`, lift to `reason: { type, metrics, rule, trafficAllocation }`. Candidate for Phase 3 (`status` command) where richer detail matters more than in `list`.
- **`--idempotency-key` user-facing flag** — infrastructure is wired in Phase 1; whether the flag is user-facing in Phase 1 (no mutations to exercise it) or held until Phase 2 is at planner's discretion.
- **Pagination as a user-facing concern** — D-05's bounded default (20) sidesteps pagination for the common case. `--all` may need transparent pagination handling (Papercut P3 territory). Researcher/planner decides whether to ship transparent pagination in Phase 1 or defer until a flag actually has >upstream-limit rollouts.
- **Cross-environment list behavior** — `list --flag <key>` without `--environment` returns rollouts across all envs. Sort order across envs? Group by env? Currently: pure reverse-chronological by `startedAt` regardless of env. Revisit if operators find this confusing.

</deferred>

---

*Phase: 1-List (foundation + first end-to-end slice)*
*Context gathered: 2026-05-12*
