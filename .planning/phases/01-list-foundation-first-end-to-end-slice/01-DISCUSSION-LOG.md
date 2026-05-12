# Phase 1: List (foundation + first end-to-end slice) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-12
**Phase:** 1-List (foundation + first end-to-end slice)
**Areas discussed:** Exit-code scheme, State enum / status display, `list` default + plaintext layout, `Client` interface scope

---

## Exit-code scheme

**Initial question:** Which exit-code scheme to lock for `rollouts-beta` (sysexits vs sequential vs hybrid)?

| Option | Description | Selected |
|--------|-------------|----------|
| Sequential (0/2/3/4/5/7/8/9/70/130) | SUMMARY.md synthesis; ergonomic, sysexits noted in code comments | |
| Sysexits-aligned (64/65/69/75/77) | STACK.md proposal; BSD convention | |
| Hybrid: sequential + transient bucket | Compromise with explicit 75 = transient/retryable | |

User pushed back, asking what existing ldcli does for consistency. Investigation found ldcli has **no exit-code taxonomy** today — every error is `os.Exit(1)` in `cmd/root.go:331` and `cmd/quickstart.go:51`. Reformulated:

| Option | Description | Selected |
|--------|-------------|----------|
| Sequential taxonomy, rollouts-beta only | Introduce small taxonomy scoped to rollouts-beta; rest of CLI unchanged | |
| Keep it simple: exit 1 for any error | Match existing convention; agents read JSON `error.code` | |
| Two-bucket minimum (0/1/2) | Compromise: one-bit retry-vs-fix signal | |

User questioned why a numeric taxonomy is needed for agent-friendliness. Acknowledged that REQ-AGENT-01 is actually about JSON output (not exit codes); REQ-AGENT-02 is the exit-code one. JSON `error.code` + `error.nextAction` carries the same information as a numeric exit code, just richer. Reformulated again:

| Option | Description | Selected |
|--------|-------------|----------|
| Match existing ldcli: exit 1 | Consistent; JSON envelope carries `error.code` + `error.nextAction` | ✓ |
| Two-bucket: 0 / 1 / 2 | Retry-or-not signal for shell scripts | |
| Full sequential taxonomy | Best for heavy CI/CD consumers | |

**User's choice:** Match existing ldcli — exit 1 for any error.
**Notes:** Reframes FOUND-04 from "numeric exit-code taxonomy" to "structured `error.code` taxonomy in the JSON envelope." Captured in CONTEXT.md as D-01.

---

## State enum / status display

**Initial question:** How should `list --state` work given the raw API has 11+ values across mixed axes (bucketed CLI vocab + raw / pass-through raw / bucketed only)?

User clarified two things:
1. **Punt `--state` filtering** — not essential for v1.
2. **Focus on displaying status** — every rollout-describing response needs a representation that's both programmatic and human-readable. Said: "I am imagining something like 'status' which maps exactly to the API's status, but another 'reason' or 'detail' that indicates more useful info." Directed Claude to inventory the UI's states and propose a categorization mirroring UI parity.

Devin query against `launchdarkly/gonfalon` surfaced `GuardedRolloutUIStates.tsx` as the authoritative source: the UI maps raw `status` + events + metric configs + flags to a `kind` bucket (`active`/`regressed`/`reverted`/`neutral`/`completed`) plus a `RolloutStatusLabel` human string. 13 distinct displayable states inventoried.

User asked what `neutral` means; Claude explained it covers `monitoring_stopped`, `srm_stopped`, `archived` (halted-in-place, neither rollback nor completion).

**User's choice:** Three-field model — `status` (raw API) + `kind` (5-bucket lifecycle, with **`neutral` renamed to `paused`**) + `label` (human-readable string with reason inline). Structured `reason` object deferred — `label` is the agent-parseable stop-gap. Captured as D-02, D-03, D-04 in CONTEXT.md.
**Notes:** UI's bucket names aren't a hard constraint; "paused" is operationally accurate (UI copy uses "paused at N%"). Structured `reason` is a candidate for Phase 3 (`status` command) where richer detail matters more.

---

## `list` default + plaintext layout

| Option (default scope) | Description | Selected |
|------------------------|-------------|----------|
| All rollouts, reverse-chronological, no limit | Matches REQ-LIST-01 wording literally | |
| Most recent N (e.g. 20), with `--all` for everything | `gh pr list` ergonomics | ✓ |
| Current only by default, `--all` for past too | Optimizes for "is anything running?" | |

| Option (plaintext layout) | Description | Selected |
|---------------------------|-------------|----------|
| Narrow table default + `--detailed` for full | 5 columns fit 80-col; `--detailed` adds variations + ended + stage | ✓ |
| Always-wide table, truncate to terminal width | `kubectl get` style | |
| Key-value blocks per rollout | Multi-line; nicer for few rollouts, awkward for many | |

**User's choice:** Bounded default (20) with `--all`; narrow table by default with `--detailed`. Captured as D-05, D-06, D-07. JSON output always emits the full field set regardless of `--detailed`.
**Notes:** Default columns: `ID`, `kind`, `environment`, `state/label`, `started`. `--detailed` adds variations, ended-at, current stage, raw API status.

---

## `Client` interface scope

| Option | Description | Selected |
|--------|-------------|----------|
| Grow incrementally (Phase 1: `List` + `Get`) | Avoids premature commitment to method signatures | ✓ |
| Stub full surface in Phase 1 | One-time `cmd/root.go` wiring; unimplemented methods return "not yet implemented" | |

**User's choice:** Grow incrementally. Phase 1 ships `List` and `Get` only. Captured as D-08.
**Notes:** `Get` is included because `List` may need it as a follow-up for full-detail records. Phase 2 adds `Start`. Phase 4 adds `Stop` + `DismissRegression`. Mocks regenerated per phase via `mockgen` — cheap.

---

## Claude's Discretion

- Internal layout under `internal/rollouts/` (file split for `client.go`, `models.go`, `instructions.go`, `mock_client.go`) — follow `internal/flags/` convention.
- Exact field names inside the rollout model — follow API names where unambiguous.
- Retry policy specifics within the 4 retries / 500ms–8s envelope (retry-after honoring, jitter).
- Beta banner exact copy and placement (stderr-only on TTY; suppressed when piped or `--output json`).
- Whether to expose `--idempotency-key` user-facing flag in Phase 1 (no mutations) or wait until Phase 2.

## Deferred Ideas

- `--state` filter on `list` — not essential for v1; revisit after Phase 3.
- Structured `reason` object on the status model — `label` is the stop-gap for Phase 1.
- `--idempotency-key` flag exposure timing — planner decides.
- Transparent pagination for `--all` — Papercut P3 territory; planner decides.
- Cross-environment list ordering — currently pure reverse-chronological by `startedAt`; revisit if operators find this confusing.
