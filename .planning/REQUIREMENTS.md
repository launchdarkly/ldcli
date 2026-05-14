# Requirements: ldcli — Automated Rollouts via CLI

**Defined:** 2026-05-12
**Core Value:** An AI agent (or human, or CI/CD pipeline) can take a merged feature behind a flag, kick off an automated rollout, monitor it through to completion, and respond to regressions — without ever needing the LaunchDarkly UI.

## v1 Requirements

### Foundations

Cross-cutting infrastructure shared by every command. Must land first.

- [ ] **FOUND-01**: New package `internal/rollouts/` exposes a `Client` interface (following the existing `internal/<domain>/Client` pattern) with hand-rolled types for the `automated-releases` API.
- [ ] **FOUND-02**: Command tree `ldcli flags rollouts-beta` is registered under the existing `flags` command, with a clear "beta — surface may change" indicator (TTY banner in human mode; metadata only in JSON mode).
- [ ] **FOUND-03**: Versioned JSON output envelope (e.g. `schemaVersion: "rollouts.v1beta1"`, `kind`, `data`, `error`) is defined once and reused by every rollouts-beta command.
- [ ] **FOUND-04**: Exit-code taxonomy that distinguishes user error, API client error, API unavailable, transient/retryable, auth, preflight-failed, regression-detected, and SIGINT. Documented as a stable contract.
- [ ] **FOUND-05**: Retry/idempotency layer for upstream calls — transient-failure retries with backoff and a generated `Idempotency-Key` UUID on every mutation (even if upstream currently ignores it; logged as papercut).
- [ ] **FOUND-06**: Re-fetch helper: every mutation that targets a rollout must follow up with a GET to surface the new/changed rollout ID and state. Encoded once, used by start/stop/dismiss.
- [ ] **FOUND-07**: TTY-aware output: human-readable in a TTY (with ANSI), structured JSON when piped or `--output json`; ANSI codes never leak into JSON or stderr.
- [ ] **FOUND-08**: Errors include a stable `error.code` field and (where applicable) a `nextAction` hint so agents can branch without parsing prose.

### Learnings Deliverables

This milestone's **primary outputs are two learnings artifacts**, not the CLI itself. The CLI is a prototype-shaped vehicle for surfacing what the API team and a future production-CLI build need to know. Both artifacts are first-class deliverables enforced via cross-cutting requirements.

#### API papercuts (artifact #1 — primary)

- [ ] **DOC-01**: `.planning/API-PAPERCUTS.md` is created early with a structured template (Discovered / API behavior / CLI workaround / What we'd prefer / Status / Removal criteria) and seeded with the 16 papercuts already cataloged by the architecture research.
- [ ] **DOC-02**: New papercuts discovered during implementation are appended throughout the milestone with a `// PAPERCUT: PC-NNN` source-code cross-reference at every workaround site. Cross-cutting — enforced every phase from Phase 1 onward.
- [ ] **DOC-03**: At milestone end, the doc is reviewed and circulated to the API team as input for the API stabilization work that precedes public release.
- [ ] **DOC-04**: For any API papercut that captures a *contract-shape* observation (confusing field names, missing data on responses, forced consumer workarounds, inconsistencies with the rest of the LD API surface), the matching Confluence doc at page_id 4875452435 is updated using the fetch-first pattern (`mcp__mcp-atlassian__confluence_get_page` → `confluence_update_page`) so concurrent human edits aren't clobbered. Cross-cutting — enforced every phase from Phase 1 onward.

#### CLI / UX learnings (artifact #2 — secondary)

- [x] **LEARN-01**: `.planning/CLI-LEARNINGS.md` is created with a structured template (anchor table + per-topic entries with Question / What we did in prototype / What's open for production CLI build / Severity) and seeded with the open CLI/UX questions surfaced by Phases 1+2 retroactively (JSON envelope vs raw-resource shape; AGENT-04 timestamp format; structured `reason` lift; exit-code taxonomy; watch-shaped use cases; "most recent" semantics; `--rollout-id` requiring `--environment`). — Phase 3 Plan 01
- [ ] **LEARN-02**: New CLI/UX complexities discovered during implementation are appended to `.planning/CLI-LEARNINGS.md` throughout the milestone. Includes: shape/contract questions, surprising behaviors agents would prefer differently, deferred decisions worth revisiting, ergonomic frictions surfaced by real-staging exercise. Cross-cutting — enforced every phase from Phase 3 onward (where CLI-LEARNINGS.md is first created).
- [ ] **LEARN-03**: At milestone end, `.planning/CLI-LEARNINGS.md` is reviewed alongside `.planning/API-PAPERCUTS.md` and circulated as input for the production CLI build's design discussions. Pairs with DOC-03.

### Start

- [ ] **START-01**: `ldcli flags rollouts-beta start` kicks off a guarded or progressive rollout. Progressive is the default; supplying `--metric` flags promotes it to guarded.
- [ ] **START-02**: All existing API options are configurable from the CLI: stages (allocation + duration), target variation, original variation, randomization unit, metrics, auto-rollback per-metric, rule/clauses/ref targeting.
- [ ] **START-03**: Environment is parameterized via `--environment` (or equivalent flag); any environment in the project is a valid target.
- [ ] **START-04**: Pre-flight health checks run by default before the start mutation: validates metric/randomization-unit compatibility against the upstream `recommended-duration` proxy. Failure modes:
  - In a TTY: prompt the user with the specific failure and offer to abort or proceed.
  - In CI / non-TTY / `--output json`: exit non-zero with a structured error (no prompt).
  - `--skip-health-checks` bypasses the preflight entirely.
- [ ] **START-05**: After the patch-instruction mutation succeeds, the CLI automatically re-fetches the new rollout (via the list endpoint with environment filter) and surfaces its ID + initial state in the output. Documents the API papercut.
- [ ] **START-06**: `start` is idempotency-aware: same `--idempotency-key` (or auto-generated UUID) returns a coherent outcome on retry rather than racing the API.
- [ ] **START-07**: Preflight failures, off-flag conditions, and "rollout already running" conditions surface as distinct error codes per FOUND-04, with `nextAction` hints.

### List

- [ ] **LIST-01**: `ldcli flags rollouts-beta list --flag <key>` returns all rollouts for a flag, current + past, with stable deterministic ordering (CLI sorts client-side if the API doesn't guarantee order).
- [ ] **LIST-02**: Output includes per-rollout identifying info: ID, kind (guarded/progressive), environment, current state, target/original variations, started/ended times in RFC 3339, current stage index.
- [ ] **LIST-03**: Filterable by `--environment` and `--state` (e.g. `running`, `completed`, `failed`, `stopped`); pagination handled transparently if the API requires it.

### Status

- [x] **STATUS-01**: `ldcli flags rollouts-beta status --flag <key>` returns the most-recent rollout's state by default (running now, or last completed/failed). — Phase 3 Plan 01
- [x] **STATUS-02**: Status output surfaces everything the LaunchDarkly UI shows for an automated release: stage progression (current stage, allocations, durations), latest metric results per monitored metric, monitoring state, action-required reasons, regression detail if present. — Phase 3 Plan 01
- [x] **STATUS-03**: A specific rollout can be addressed by `--rollout-id` to override the "most recent" default. — Phase 3 Plan 01
- [x] **STATUS-04**: Terminology in human output is consistent with the LaunchDarkly UI's labels for rollout states (nice-to-have where it makes sense; documented when divergent). — Phase 3 Plan 01
<!-- STATUS-05..09 (the entire `--watch` surface) were struck on 2026-05-14 during Phase 3
     discuss-phase per user decision: watch is too complicated for this project. Polling is
     the agent's job — invoke `status` periodically. See
     `.planning/phases/03-status-watch/03-CONTEXT.md` D-01. The watch-shaped use cases are
     captured in `.planning/CLI-LEARNINGS.md` for the production CLI build to revisit. -->

### Stop & Dismiss

- [ ] **STOP-01**: `ldcli flags rollouts-beta stop --flag <key> --to-variation <key>` manually stops the current rollout, rolling out to the chosen final variation. Required `--to-variation` accepts either the original (control) or target (test) variation key.
- [ ] **STOP-02**: Stop pre-reads the current rollout state (per FOUND-06) and refuses to stop something that's already terminal, with a clear error code.
- [ ] **STOP-03**: `ldcli flags rollouts-beta dismiss-regression --flag <key>` dismisses a current regression so the rollout can resume.
- [ ] **STOP-04**: Dismiss handles the "no active regression" case gracefully — distinct exit code, distinct error.code, agent-friendly `nextAction`.

### Agent Affordances

Cross-cutting agent-friendly behaviors that every command must honor. Stated explicitly so they aren't forgotten in any phase.

- [ ] **AGENT-01**: Every command supports `--output json` and produces parseable output regardless of TTY state.
- [ ] **AGENT-02**: Every command's exit codes follow FOUND-04 so agents can branch on retry/diagnose/escalate without parsing stderr.
- [ ] **AGENT-03**: Mutating commands return a coherent response on retry with the same idempotency key (best-effort given upstream limitations; documented per command).
- [ ] **AGENT-04**: Timestamps are RFC 3339 UTC in JSON output; durations are explicit unit-bearing strings (e.g. `"3600s"` or `"60m"`).
- [ ] **AGENT-05**: List outputs have a deterministic sort order documented in `--help`; agents can rely on "first entry = most relevant" semantics.

## v2 Requirements

Acknowledged future work; not in current roadmap.

### Release Policy Integration

- **POLICY-01**: When a release policy is set on the project, `start` can be invoked with no rollout options because the policy supplies defaults.
- **POLICY-02**: CLI surfaces which policy was used and which fields it overrode.

### Cross-Environment Workflows

- **PROMOTE-01**: Single command to promote a flag through environments sequentially (dev → staging → prod) with per-env guardrails.

### Richer Observability

- **METRICS-01**: Stream metric timeseries (chart data) for an in-progress rollout.
- **EXEMPLAR-01**: Surface exemplar errors / sample failed events for a regression.

## Out of Scope

Explicitly excluded.

| Feature | Reason |
|---------|--------|
| Timeseries / chart data for metric results | UI exists for this; CLI surfaces latest values only. v2 candidate (`METRICS-01`). |
| Release-policy-driven defaults | Future work once policies are GA. v2 candidate (`POLICY-01`). |
| Configuring metric definitions or randomization units | Pre-existing LD resources; CLI consumes them, doesn't create them. |
| "Notify human" as an explicit command | Escalation is the agent's choice; CLI doesn't ship a notification primitive. |
| Cross-environment promotion workflow | v1 targets a single env per invocation; multi-env composes from there. v2 (`PROMOTE-01`). |
| Code generation from gonfalon OpenAPI for these endpoints | API is unstable & undocumented; hand-rolled types are the right move until it stabilizes. |
| Inventing `pause` semantics | Upstream API doesn't expose pause; faking it would surprise agents. |

## Traceability

Every v1 requirement maps to exactly one phase. AGENT-* and DOC-02 are cross-cutting but anchored to the phase where they are first introduced; they are re-validated in every subsequent phase's success criteria.

| Requirement | Phase | Status |
|-------------|-------|--------|
| FOUND-01 | Phase 1 | Pending |
| FOUND-02 | Phase 1 | Pending |
| FOUND-03 | Phase 1 | Pending |
| FOUND-04 | Phase 1 | Pending |
| FOUND-05 | Phase 1 | Pending |
| FOUND-06 | Phase 1 | Pending |
| FOUND-07 | Phase 1 | Pending |
| FOUND-08 | Phase 1 | Pending |
| DOC-01 | Phase 1 | Pending |
| DOC-02 | Phase 1 (cross-cutting, enforced every phase) | Pending |
| DOC-03 | Phase 4 | Pending |
| DOC-04 | Phase 1 (cross-cutting, enforced every phase) | Pending |
| LEARN-01 | Phase 3 Plan 01 | Complete |
| LEARN-02 | Phase 3 (cross-cutting, enforced Phase 3 onward) | Pending |
| LEARN-03 | Phase 4 | Pending |
| LIST-01 | Phase 1 | Pending |
| LIST-02 | Phase 1 | Pending |
| LIST-03 | Phase 1 | Pending |
| AGENT-01 | Phase 1 (cross-cutting, enforced every phase) | Pending |
| AGENT-02 | Phase 1 (cross-cutting, enforced every phase) | Pending |
| AGENT-03 | Phase 1 (cross-cutting, enforced every phase) | Pending |
| AGENT-04 | Phase 1 (cross-cutting, enforced every phase) | Pending |
| AGENT-05 | Phase 1 (cross-cutting, enforced every phase) | Pending |
| START-01 | Phase 2 | Pending |
| START-02 | Phase 2 | Pending |
| START-03 | Phase 2 | Pending |
| START-04 | Phase 2 | Pending |
| START-05 | Phase 2 | Pending |
| START-06 | Phase 2 | Pending |
| START-07 | Phase 2 | Pending |
| STATUS-01 | Phase 3 Plan 01 | Complete |
| STATUS-02 | Phase 3 Plan 01 | Complete |
| STATUS-03 | Phase 3 Plan 01 | Complete |
| STATUS-04 | Phase 3 Plan 01 | Complete |
| STOP-01 | Phase 4 | Pending |
| STOP-02 | Phase 4 | Pending |
| STOP-03 | Phase 4 | Pending |
| STOP-04 | Phase 4 | Pending |

**Coverage:**
- v1 requirements: 38 total (STATUS-05..09 struck 2026-05-14 — watch removed from project; DOC-04 + LEARN-01..03 added 2026-05-14 — learnings deliverables baked in as first-class)
- Mapped to phases: 38
- Unmapped: 0

---
*Requirements defined: 2026-05-12*
*Last updated: 2026-05-14 after Phase 3 discuss-phase — STATUS-05..09 struck (watch removed from project); DOC-04 + LEARN-01..03 added (learnings deliverables baked in as cross-cutting requirements)*
