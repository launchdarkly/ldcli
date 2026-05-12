# Roadmap: ldcli — Automated Rollouts via CLI

**Created:** 2026-05-12
**Core Value:** An AI agent (or human, or CI/CD pipeline) can take a merged feature behind a flag, kick off an automated rollout, monitor it through to completion, and respond to regressions — without ever needing the LaunchDarkly UI.

**Mode:** MVP / vertical slices — every phase delivers an end-to-end, user-visible capability against the `automated-releases` API. Foundation work (Client skeleton, output envelope, exit codes, papercuts doc) lands inside Phase 1 alongside the first real command so something works end-to-end from the very first phase.

**Granularity:** coarse (4 phases, 1-3 plans each).

**Coverage:** 39 / 39 v1 requirements mapped.

## Cross-Cutting Constraints

These are not phases — they are constraints every phase must honor. They are listed once here and referenced from each phase's success criteria.

- **AGENT-01**: every command supports `--output json` and produces parseable output regardless of TTY state.
- **AGENT-02**: every command's exit codes follow the FOUND-04 taxonomy.
- **AGENT-03**: every mutating command sends an `Idempotency-Key` and documents per-command guarantees.
- **AGENT-04**: timestamps are RFC 3339 UTC; durations are explicit unit-bearing strings in JSON.
- **AGENT-05**: list outputs have a deterministic sort order documented in `--help`.
- **DOC-02**: papercuts discovered during implementation are appended to `.planning/API-PAPERCUTS.md` with a `// PAPERCUT: PC-NNN` cross-reference at every workaround site, in every phase.

## Phases

- [ ] **Phase 1: List (foundation + first end-to-end slice)** — Operator can enumerate every rollout (current + past) for a flag from the CLI; ships the package skeleton, JSON envelope, exit-code taxonomy, retry/idempotency layer, TTY-aware output, beta banner, and the seeded papercuts doc.
- [ ] **Phase 2: Start a rollout** — Operator can kick off a guarded or progressive rollout from the CLI with full option surface, default-on preflight, re-fetch for rollout ID, and idempotency.
- [ ] **Phase 3: Status & Watch** — Operator can inspect the most-recent (or a specific) rollout with UI-parity detail, and can watch a running rollout for actionable events via NDJSON.
- [ ] **Phase 4: Stop, Dismiss, & Finalize papercuts** — Operator can manually stop a rollout to a chosen final variation and dismiss an active regression; papercuts doc is reviewed and circulated.

## Phase Details

### Phase 1: List (foundation + first end-to-end slice)
**Goal**: Operator (human or agent) can run `ldcli flags rollouts-beta list --flag <key>` and get a deterministic JSON or plaintext enumeration of every rollout on the flag, with proper exit codes, beta signaling, and the agent-friendly output envelope already locked in.
**Mode:** mvp
**Depends on**: Nothing (first phase).
**Requirements**: FOUND-01, FOUND-02, FOUND-03, FOUND-04, FOUND-05, FOUND-06, FOUND-07, FOUND-08, DOC-01, LIST-01, LIST-02, LIST-03, AGENT-01, AGENT-02, AGENT-03, AGENT-04, AGENT-05
**Success Criteria** (what must be TRUE):
  1. The operator can run `ldcli flags rollouts-beta list --flag <key>` and receive a non-empty, deterministically ordered list of rollouts (current + past) for any flag that has rollout history, with stable reverse-chronological ordering documented in `--help`.
  2. The operator can pass `--output json` (or pipe stdout) and receive a single well-formed envelope `{schemaVersion: "rollouts.v1beta1", kind: "RolloutList", data: [...], meta: {...}}` where every timestamp is RFC 3339 UTC, every duration is a unit-bearing string, no ANSI sequences leak to stdout, and human chrome (beta banner, spinner) is suppressed.
  3. The operator can filter results with `--environment <key>` and `--state running|completed|failed|stopped`, and the CLI handles upstream pagination transparently (or fails with a documented exit code if the result set exceeds the upstream limit and pagination is unsupported — see papercut P3).
  4. An agent can branch on outcome without parsing stderr: a 4xx / 5xx / auth / transient / unknown-error response from upstream maps to a distinct documented exit code from the FOUND-04 taxonomy, and JSON-mode errors are emitted as a structured envelope on stdout with `error.code` and (where applicable) `error.nextAction`.
  5. `.planning/API-PAPERCUTS.md` exists, follows the structured template (anchor ID, discovered, API behavior, CLI workaround, what we'd prefer, status, removal criteria), is seeded with the 16 cataloged papercuts from architecture research, and every workaround introduced in Phase 1 code is annotated with `// PAPERCUT: PC-NNN`.
**Plans:** 3 plans

Plans:
- [x] 01-01-PLAN.md — Walking Skeleton: scaffold internal/rollouts/ package + cmd/flags/rollouts/ Cobra subtree + root wiring with stub HTTP path
- [ ] 01-02-PLAN.md — Real HTTP via go-retryablehttp + 13-state status mapping + full error.code taxonomy + httptest round-trip tests
- [ ] 01-03-PLAN.md — Flag surface (--environment/--limit/--all/--detailed) + plaintext table + sort + saturation warning + seed API-PAPERCUTS.md

### Phase 2: Start a rollout
**Goal**: Operator (human or agent) can kick off a guarded or progressive rollout from the CLI with full configurability, get the new rollout's ID back, and trust that the CLI refused to start anything that would have stalled at the first metric evaluation.
**Mode:** mvp
**Depends on**: Phase 1 (Client skeleton, semantic-patch envelope helper from FOUND-01, exit codes, output envelope, re-fetch helper from FOUND-06, idempotency layer from FOUND-05).
**Requirements**: START-01, START-02, START-03, START-04, START-05, START-06, START-07
**Success Criteria** (what must be TRUE):
  1. The operator can run `ldcli flags rollouts-beta start --flag <key> --environment <env> --target-variation <vid> --original-variation <vid> --randomization-unit <u> --stages 25:60m,50:60m,100:60m` and receive a JSON envelope containing the new rollout's ID and initial state — progressive by default, guarded when one or more `--metric <key>` flags are supplied.
  2. The operator can configure every existing API option from the CLI — stages (allocation + duration), metrics + per-metric auto-rollback, randomization unit, rule/clauses/ref targeting, extension duration — and the resulting rollout reflects exactly what was requested (or fails fast with a structured error if upstream validation rejects it; no silent substitution).
  3. In a non-TTY context (or when `--output json` is set), the CLI runs the metric/randomization-unit preflight via `recommended-duration` before any mutation and exits with the dedicated preflight-failed exit code on rejection; in an interactive TTY the operator is prompted with the specific failure; `--skip-health-checks` bypasses the preflight and the success envelope includes an audit entry naming what was skipped.
  4. After the patch mutation succeeds, the CLI follows up with a GET (env-filtered, `limit=1`) and surfaces the new rollout's ID + initial state in stdout — and an agent running `start --output json | jq -r .data.id` always gets a non-empty ID.
  5. Distinct documented exit codes / `error.code` values fire for: preflight failure, flag-off, "rollout already running on this flag/env", invalid variation, auth scope missing, and unknown upstream error — and `--idempotency-key <uuid>` (or the auto-generated UUID) produces a coherent outcome when the same start is retried after a transient failure.
**Plans**: TBD

### Phase 3: Status & Watch
**Goal**: Operator (human or agent) can inspect any rollout with full UI-parity detail and can watch a running rollout for actionable events (regressions, stage transitions, action-required) via diff-based NDJSON streaming — the agent's primary feedback loop.
**Mode:** mvp
**Depends on**: Phase 1 (Client + output envelope + exit codes), Phase 2 (rollouts to status/watch exist; semantic-patch helper not used, but realistic rollouts created during Phase 2 enable end-to-end status testing).
**Requirements**: STATUS-01, STATUS-02, STATUS-03, STATUS-04, STATUS-05, STATUS-06, STATUS-07, STATUS-08, STATUS-09
**Success Criteria** (what must be TRUE):
  1. The operator can run `ldcli flags rollouts-beta status --flag <key>` and receive everything the LD UI surfaces for an automated release — stage progression (current stage index, allocations, durations), latest metric results per monitored metric, monitoring state, action-required reasons, and regression detail if present — for the most-recent rollout by default, or for a specific rollout when `--rollout-id <id>` is passed.
  2. Human-mode output uses terminology consistent with the LaunchDarkly UI's labels for rollout states (documented when divergent), while JSON-mode output exposes both a stable bucketed `state` (`running` / `paused` / `succeeded` / `failed` / `regression-detected`) and the raw upstream `status` value alongside it.
  3. The operator can run `ldcli flags rollouts-beta status --flag <key> --watch` and see actionable events (regression detected, stage advanced, action required, terminal) — not just terminal states — at the documented default poll interval (~15s), configurable via `--watch-interval`, with diff-based transition detection so an inter-poll `running → regression_detected → rolled_back` sequence surfaces the regression event rather than only the terminal state.
  4. With `--watch --output json`, the CLI emits NDJSON (one JSON object per line, each carrying the schema-versioned envelope) with a final `terminal: true` record; when piped to an agent the stream parses cleanly line-by-line, and SIGINT during watch exits 130 with no partial JSON object on stdout.
  5. `--watch` has a hard `--timeout` (configurable; reasonable default for hour-scale rollouts) and exits with the dedicated watch-timeout exit code when the timeout fires while the rollout is still running — distinct from terminal-failure and from SIGINT — so an agent can re-watch or fall back to scheduled `status` polls for multi-day rollouts.
**Plans**: TBD

### Phase 4: Stop, Dismiss, & Finalize papercuts
**Goal**: Operator (human or agent) can manually stop a rollout to a chosen final variation and dismiss an active regression so the rollout can resume; the milestone's `API-PAPERCUTS.md` deliverable is reviewed and circulated as input for the API team.
**Mode:** mvp
**Depends on**: Phase 2 (semantic-patch helper, re-fetch pattern, idempotency layer), Phase 3 (state pre-read pattern, `meta.availableActions` next-action hints inform error responses).
**Requirements**: STOP-01, STOP-02, STOP-03, STOP-04, DOC-03
**Success Criteria** (what must be TRUE):
  1. The operator can run `ldcli flags rollouts-beta stop --flag <key> --to-variation <key>` to manually stop the current rollout, rolling out to either the original (control) or target (test) variation — `--to-variation` is required (no implicit default), and the CLI pre-reads the rollout state and refuses to stop a rollout that's already terminal, exiting with the conflict exit code and a structured error naming the current state.
  2. The operator can run `ldcli flags rollouts-beta dismiss-regression --flag <key>` to dismiss a current regression so the rollout can resume; the CLI pre-reads state, re-fetches after the upstream 204 with bounded backoff until the dismissal is reflected, and returns the post-dismiss state in the response envelope.
  3. `stop` and `dismiss-regression` handle the "nothing to do" cases gracefully — already-terminal rollout, no active regression, no current rollout — with distinct exit codes, distinct `error.code` values, and agent-friendly `nextAction` hints, so an agent never sees a generic "failed" for a state it can recover from.
  4. The operator running either mutation with `--output json` always receives a confirmation envelope containing the affected rollout's ID, the effective parameters the API accepted, a permalink to the UI (`meta.uiURL`), and the post-mutation state — no silent transformation, no `OK`-only success.
  5. `.planning/API-PAPERCUTS.md` is reviewed end-of-milestone, every active workaround has a documented removal criterion and a verified source-code cross-reference, resolved papercuts have been moved to the resolved section with a date, and the doc is circulated to the API team as input for the API stabilization work before public release.
**Plans**: TBD

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. List (foundation + first slice) | 0/3 | Not started | - |
| 2. Start a rollout | 0/? | Not started | - |
| 3. Status & Watch | 0/? | Not started | - |
| 4. Stop, Dismiss, & Finalize papercuts | 0/? | Not started | - |

## Coverage Summary

39 v1 requirements mapped:

| Category | Count | Phase(s) |
|----------|-------|----------|
| FOUND-* (foundations, folded into Phase 1) | 8 | Phase 1 |
| DOC-* (papercuts) | 3 | DOC-01 → Phase 1; DOC-02 cross-cutting; DOC-03 → Phase 4 |
| START-* | 7 | Phase 2 |
| LIST-* | 3 | Phase 1 |
| STATUS-* (one-shot + watch) | 9 | Phase 3 |
| STOP-* (stop + dismiss) | 4 | Phase 4 |
| AGENT-* (cross-cutting affordances) | 5 | Phase 1 (introduced); referenced in every subsequent phase's success criteria |

No orphans. No duplicates.

## Notes for Planning

- **Phase 3 (Watch) may benefit from extra planning depth.** Diff-based transition detection, NDJSON event taxonomy stability, SIGINT discipline, and the "watch until next actionable event vs. until terminal" default are all novel design surfaces. `/gsd-plan-phase 3` should expect to spend more cycles here than on Phase 4.
- **Phase 2 is the highest-risk phase.** Preflight + semantic-patch mutation + re-fetch + idempotency + flag-state pre-checks all converge. Expect rework after first staging contact.
- **Coarse granularity assumption.** Each phase is intentionally broad (1-3 plans). If during `/gsd-plan-phase` a phase fails the must-haves test, prefer creating a follow-on plan within the phase over splitting the phase.

---
*Roadmap created: 2026-05-12*
