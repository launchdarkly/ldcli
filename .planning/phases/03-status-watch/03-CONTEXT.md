# Phase 3: Status & Watch - Context

**Gathered:** 2026-05-14
**Status:** Ready for planning
**Framing:** Prototype-first (learnings-vehicle, not production CLI). Demo due end-of-day 2026-05-14.

<domain>
## Phase Boundary

Ship `ldcli flags rollouts-beta status --flag <key> [--environment <env>] [--rollout-id <id>]` end-to-end as a working prototype. Single-snapshot inspection of a rollout with full UI-parity detail. Agents poll by re-invoking `status` periodically; the CLI does not do continuous monitoring.

**Major scope reduction:** `--watch` mode is **REMOVED** from Phase 3 and from the project entirely. `STATUS-05`, `STATUS-06`, `STATUS-07`, `STATUS-08`, `STATUS-09` are dropped. The roadmap success criteria #3, #4, #5 (all watch-related) are dropped. Polling is the agent's responsibility — invoke `status` periodically with whatever cadence makes sense.

**Project framing (carry into every Phase 3 plan + task):** This work is a prototype to surface learnings, not a production CLI build. The high-value output is two artifacts:
1. **(Primary)** API papercuts — `.planning/API-PAPERCUTS.md` + Confluence page `4875452435`. Every API gotcha encountered during status implementation gets a new PC-NNN entry. Goal: take to the API team to make the `automated-releases` API more consumable by CLIs / agents / programmatic clients.
2. **(Secondary)** CLI / UX complexities — `.planning/CLI-LEARNINGS.md` (NEW doc, prototype-era). Catalog open CLI/UX questions surfaced by the prototype that the production CLI build will revisit.

Architecture choices don't need to be 100% right. Ship working code, capture learnings. Minimize blocking discussion gates downstream.

</domain>

<decisions>
## Implementation Decisions

### Scope (locked)

- **D-01:** `--watch` is removed from Phase 3 and from the project. Strike `STATUS-05`..`STATUS-09` from REQUIREMENTS.md (incl. traceability rows). Strike ROADMAP Phase 3 SC#3, #4, #5. See `<deferred>` Required REQUIREMENTS / ROADMAP follow-ups.
- **D-02:** Status command surface is exactly: `status --flag <key> [--environment <env>] [--rollout-id <id>]`. No additional flags in this phase (no `--detailed`, no `--short`, no polling-cadence hints).

### "Most-recent" rollout resolution

- **D-03:** Resolution rules:
  - **With `--rollout-id` provided:** `--environment` is also required. Validator rejects if either is missing when `--rollout-id` is set. Rationale: PC-004 (GET-by-ID requires environmentKey in the URL path despite globally-unique rollout UUID). Auto-resolving env via a list-and-filter call would add complexity and a CLI-side workaround that papers over the API gap — the API gap is the learning we want to surface (PC-004 already filed; help text references it).
  - **Without `--rollout-id`:** `Client.List(projKey, flagKey, opts{environment, limit:1})` → take `items[0]` → that is the most recent. Phase 1 list already sorts by `createdAt DESC, ID ASC` so the most-recent semantics fall out of the existing client. `--environment` is optional; if provided, it filters the List.
  - **Zero rollouts on the flag:** error with `error.code: "no_rollouts_found"`. New code constant added in `internal/rollouts/errors.go`. Exit 1 per Phase 1 D-01.

- **D-04:** "Most recent by `createdAt`" semantics is honored verbatim from Phase 1 list. Whether this surprises users (e.g., they expected "most recent running" or "most recent active") is a CLI-LEARNINGS topic, not a prototype blocker.

### Output shape — JSON

- **D-05:** Reuse Phase 1's envelope verbatim. `NewRolloutEnvelope(*Rollout)` from `internal/rollouts/envelope.go` produces `{schemaVersion: "rollouts.v1beta1", kind: "Rollout", data: <full Rollout>, meta: {fetchedAt}}` — same shape as Phase 2's `start` success path. `meta` carries only `fetchedAt`; no `uiURL`, no `availableActions`. **The bigger envelope question** (is the wrapper the right contract, or should we be raw-resource like `gh`/`kubectl`?) is captured as a CLI-LEARNINGS topic; not a Phase 3 decision.
- **D-06:** ROADMAP Phase 3 SC#2 vocabulary (`running/paused/succeeded/failed/regression-detected`) is reconciled by reusing Phase 1 D-02's `status.kind` (`active/regressed/reverted/paused/completed`). ROADMAP SC#2 wording is softened in the follow-up (see `<deferred>`). No new `state` field is added.

### Output shape — Plaintext

- **D-07:** Plaintext layout is sectioned blocks (Overview / Stages / Metrics / Events), full detail by default. Reference shape (planner has flexibility on exact formatting / field order):
  ```
  Rollout: <id>
  Flag: <flagKey>            Env: <envKey>
  Kind: <guarded|progressive>   State: <status.kind>
  Label: <status.label>
  Started: <RFC 3339>           Ended: <RFC 3339 or —>
  Target var: <id>              Original var: <id>

  Stages:
    [✓] 25%  60m  completed
    [→] 50%  60m  in progress, 12m elapsed
    [ ] 75%  60m  pending

  Metrics:
    latency-p99    regressed       auto-rollback: false
    error-rate     ok              auto-rollback: true

  Events:
    10:45Z  regression_detected  latency-p99
    10:00Z  rollout_started
  ```
- **D-08:** No `--detailed` toggle in v1 (`status` is already a single rollout — the user explicitly asked for it, they want the detail).

### Error contract

- **D-09:** Reuse Phase 1's `error.code` + `nextAction` + envelope error shape exactly. One new code constant added: `ErrCodeNoRolloutsFound = "no_rollouts_found"` for the empty-list case (when no `--rollout-id` is given and the flag has zero rollouts). All other error mapping comes from Phase 1's `mapAPIError` / `mapTransportError`.
- **D-10:** Exit 1 for any error per Phase 1 D-01. No new exit codes. (The watch-timeout candidate disappears with `--watch`.)

### Phase 1 D-03 structured `reason` lift

- **D-11:** Do **not** lift Phase 1 D-03 in Phase 3. Stay with `status.label` only. Phase 1 D-03 named status as the candidate phase for lifting structured `reason` — but the prototype framing says we ship the simpler thing and capture "did agents struggle with `label`?" as a CLI-LEARNINGS topic.

### Client interface

- **D-12:** No new methods on `internal/rollouts/Client`. Phase 3 reuses existing `Get(ctx, token, baseURI, projKey, envKey, rolloutID)` and `List(ctx, token, baseURI, projKey, flagKey, opts)`. Phase 1 D-08 (incremental Client growth) honored — Phase 3 grows the surface by zero methods.

### CLI-LEARNINGS.md doc

- **D-13:** Create `.planning/CLI-LEARNINGS.md` as a Phase 3 deliverable. Structure: mirror `.planning/API-PAPERCUTS.md` (anchor table + one section per learning). Seed with topics from this CONTEXT (envelope vs raw, AGENT-04 timestamp format, structured-reason lift, exit-code taxonomy, watch-shaped use cases, most-recent semantics). Append new entries as Phase 3 implementation surfaces them.

### Claude's Discretion

- File split: new `cmd/flags/rollouts/status.go` mirroring `list.go` / `start.go`; add `RenderRolloutStatusPlaintext(*rollouts.Rollout) string` to `cmd/flags/rollouts/plaintext.go`.
- Section ordering, field-order within sections, ANSI color use (TTY-aware) — planner / executor discretion. Reasonable defaults are fine.
- Whether to write CLI-LEARNINGS.md skeleton early in the plan (first task) or populate as observations land — planner discretion; first-task skeleton is preferred so the doc exists for observations to land in.
- Help-text wording, especially around `--rollout-id` / `--environment` pairing (PC-004 reference) — executor discretion.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase 1 + 2 carry-forward (locked decisions Phase 3 must honor)
- `.planning/phases/01-list-foundation-first-end-to-end-slice/01-CONTEXT.md` — D-01 (any error → exit 1; structured `error.code`), D-02 (StatusBlock = status + kind + label), D-03 (no structured reason — Phase 3 keeps deferred), D-07 (JSON-mode errors on stdout), D-08 (Client grows incrementally; Phase 3 adds zero methods), envelope shape (`rollouts.v1beta1`)
- `.planning/phases/01-list-foundation-first-end-to-end-slice/01-RESEARCH.md` — status enum semantics, 13-row status mapping
- `.planning/phases/01-list-foundation-first-end-to-end-slice/01-SMOKE.md` — staging gotchas: `LD-API-Version: beta` required; int64 unix millis on the wire
- `.planning/phases/02-start-a-rollout/02-CONTEXT.md` — D-10 (idempotency out of project scope), D-12 (error.code taxonomy extension pattern), generic-CLI-robustness preference

### Project planning
- `.planning/PROJECT.md` — milestone goal, real-server validation constraint, API contract learnings → Confluence (fetch-first pattern)
- `.planning/REQUIREMENTS.md` — STATUS-01..04 (Phase 3 post-reduction). STATUS-05..09 to be struck per follow-ups.
- `.planning/ROADMAP.md` — Phase 3 goal + SC#1, #2 (post-reduction). SC#3, #4, #5 to be struck per follow-ups.
- `.planning/STATE.md` — accumulated decisions and open questions

### Research
- `.planning/research/ARCHITECTURE.md` — `automated-releases` API inventory; status enum semantics; 16 papercuts (esp. PC-004, PC-005, PC-006, PC-014, PC-015)
- `.planning/research/PITFALLS.md` — anti-patterns; #3 (CLI flags coupled to API field names — keep loose)

### Papercuts (active entering Phase 3)
- `.planning/API-PAPERCUTS.md` — PC-004 (GET-by-ID requires env in path; surfaces in Phase 3 user-facing `status` per its filed Affected commands), PC-005 (status enum mixes lifecycle + action-required + meta — `status.kind` derivation papers over it), PC-006 (`waiting` status undocumented — surfaces in Phase 3), PC-014 (durations as int64 millis only), PC-015 (no documented status enum transitions — moot post-D-01 watch removal but the documentation gap remains relevant for `status` itself). Phase 3 will append new entries as observed during real-staging exercise.
- Confluence: [Learnings: automated release API papercuts](https://launchdarkly.atlassian.net/wiki/spaces/~62435d09f6a26900695be8d7/pages/4875452435) — `page_id=4875452435`. Fetch first (`mcp__mcp-atlassian__confluence_get_page` → `confluence_update_page`) to avoid clobbering concurrent human edits.

### Codebase patterns (existing ldcli + Phase 1/2 output)
- `internal/rollouts/client.go` — existing `Get` (env-scoped per PC-004) and `List` (env-optional, sorts CreatedAt DESC) — Phase 3 wires these into the new command
- `internal/rollouts/models.go` — `Rollout`, `StatusBlock`, `Stage`, `Event`, `MetricConfiguration` types ready to consume
- `internal/rollouts/status_mapping.go` — `DeriveStatusBlock` already populates `kind` + `label` from raw status + sub-conditions; Phase 3 renderer reads `Status.Kind` for section state badges and `Status.Label` for the headline
- `internal/rollouts/envelope.go` — `NewRolloutEnvelope` (success) and `NewErrorEnvelope` (error) helpers; reuse verbatim
- `internal/rollouts/errors.go` — error-code enum; Phase 3 adds one constant (`ErrCodeNoRolloutsFound`)
- `cmd/flags/rollouts/list.go` — closest analog for `status` command body (RunE shape, `emitSuccess` / `emitError` split, JSON-mode-error-on-stdout pattern, Viper-at-RunE-time)
- `cmd/flags/rollouts/start.go` — Phase 2's single-rollout-result command; second analog (uses `NewRolloutEnvelope`)
- `cmd/flags/rollouts/plaintext.go` — `renderDetailed` is the closest existing layout reference; sectioned-block renderer is similar but with explicit section headers + per-section tables
- `cmd/flags/rollouts/rollouts.go` — `NewRolloutsCmd` registers child commands; add `NewStatusCmd(client)` here
- `cmd/cliflags/flags.go` — `FlagFlag`, `EnvironmentFlag` already defined; add `RolloutIdFlag` (and description)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/rollouts.Client.Get(ctx, token, baseURI, projKey, envKey, rolloutID)`** — exactly what `status --rollout-id` needs when env is also provided. Already exercises PC-004 path.
- **`internal/rollouts.Client.List(ctx, token, baseURI, projKey, flagKey, opts)`** — used for the "most-recent" path when `--rollout-id` is not provided. Phase 1's `CreatedAt DESC` sort makes `items[0]` the answer.
- **`internal/rollouts.DeriveStatusBlock`** — already populates the 5-bucket `kind` + reason-inline `label` from raw status + sub-condition discriminators (events / metric configs / extension duration). Phase 3 plaintext consumes both.
- **`internal/rollouts.NewRolloutEnvelope(*Rollout)`** — wraps a single rollout into `{schemaVersion, kind: "Rollout", data, meta: {fetchedAt}}`. Reuse verbatim.
- **`internal/rollouts.NewErrorEnvelope(code, message, nextAction)`** — JSON-mode error envelope. Phase 3 uses for the `no_rollouts_found` case and all upstream error mappings.

### Established Patterns
- `internal/<domain>/Client` interface + `var _ Client = ...{}` compile-time assertion: no Client interface change in Phase 3.
- `mockgen`-generated mocks: existing `internal/rollouts/mock_client.go` already covers `Get` and `List`; no mockgen rerun needed (no interface change).
- Reading Viper at `RunE` time, not constructor time (CONVENTIONS.md): applies to new `cmd/flags/rollouts/status.go`.
- `cliflags.GetOutputKind(cmd)` → "json" branch emits envelope; else branch emits plaintext via the renderer.
- JSON-mode error envelope on stdout (Phase 1 D-07): `emitError` pattern in `list.go` is the canonical model.

### Integration Points
- **`cmd/flags/rollouts/rollouts.go:NewRolloutsCmd`** — add `cmd.AddCommand(NewStatusCmd(client))` alongside existing `NewListCmd` and `NewStartCmd`.
- **`cmd/cliflags/flags.go`** — add `RolloutIdFlag = "rollout-id"` + `RolloutIdFlagDescription`.
- **`internal/rollouts/errors.go`** — add `ErrCodeNoRolloutsFound = "no_rollouts_found"` constant.

### What's NEW for this phase (small)
- `cmd/flags/rollouts/status.go` — Cobra command + RunE; resolves rollout via `Get` (with `--rollout-id`) or `List → items[0]` fallback; calls plaintext renderer or marshals JSON envelope
- `cmd/flags/rollouts/status_test.go` — happy + edge cases (most-recent path, --rollout-id path, no-rollouts, --rollout-id-without-env validation error)
- `cmd/flags/rollouts/plaintext.go` — add `RenderRolloutStatusPlaintext(*rollouts.Rollout) string`
- `internal/rollouts/errors.go` — one new code constant
- `cmd/cliflags/flags.go` — one new flag constant
- `.planning/CLI-LEARNINGS.md` — new doc, seeded with prototype-era CLI/UX topics (see D-13)

</code_context>

<specifics>
## Specific Ideas

### Demo-day framing (prototype-first)
- Demo due end-of-day 2026-05-14. Deliverable is a **working prototype**, not a polished production CLI. Architecture decisions that aren't 100% right are accepted; the rough edges are inputs to learnings, not blockers.
- Phase 3 plan should chain into execute-phase quickly. Plan-checker / verifier passes should focus on "does the prototype work end-to-end against real staging?" not "is every CLI shape perfect?"
- Real-staging exercise is required per PROJECT.md constraint. SMOKE.md from Phase 1/2 is the model.

### Two learnings streams to maintain
- **API papercuts (primary):** every API gotcha encountered during status implementation gets a PC-NNN entry in `.planning/API-PAPERCUTS.md` AND a Confluence update (page_id 4875452435; fetch-first per memory `feedback_confluence_fetch_first.md`). Expected new papercuts during Phase 3 work include:
  - Whether the API's empty-list response shape is consistent (`{items: []}` vs `null` vs `{}`).
  - Whether `Get` by env+rollout-id ever returns a stale snapshot relative to `List` (eventual consistency).
  - Status-enum sub-condition discriminators surfaced empirically vs documented (extends PC-006 / PC-015).
  - Metric-configuration field semantics: what's a metric's `status` enum's full enum set? Documented somewhere?

- **CLI / UX complexities (secondary, NEW):** `.planning/CLI-LEARNINGS.md` opens with these topics seeded:
  - Envelope vs raw-resource JSON shape (`gh` / `kubectl` style) — Phase 1 chose envelope, but the question is open for the production build
  - AGENT-04 timestamp format — RFC 3339 in JSON vs the API's raw int64 millis; what do agents actually prefer
  - Phase 1 D-03 structured `reason` lift — did agents struggle parsing the human-readable `label`?
  - Exit-code taxonomy — is exit 1 + structured `error.code` enough, or did consumers want richer numeric distinctions?
  - Watch-shaped use cases — with `--watch` deferred, where did agents end up needing periodic polling vs event-driven monitoring? What would the production CLI's monitoring story look like?
  - "Most recent" semantics — did `createdAt DESC` surprise users vs "most recent active" or "most recent running"?
  - `--rollout-id` requiring `--environment` (PC-004 surfaces in user-facing usage) — did this trip up CLI users?

### Why no `--watch` in this phase
- The user explicitly removed `--watch` mid-discussion: "I think watch is too complicated and I don't want it to be in scope for this project. only implement a way of getting the current status, with the idea that an agent can keep polling that periodically."
- Polling is the agent's job. The CLI's job is one snapshot per invocation. If demo feedback shows agents painfully reinventing watch, that's a CLI-LEARNINGS entry that informs the production CLI.

### Why we kept the envelope (for now)
- The user signaled mid-discussion that the envelope nuke was bigger than they wanted to take on in this phase: "let's simplify the approach. don't worry about nuking the envelope." Phase 3 reuses Phase 1's envelope verbatim; the deeper question (is wrapper-style right vs raw-resource?) is captured as a CLI-LEARNINGS topic for the production build.
- This is consistent with the prototype-first framing — ship working code with the existing wire contract, capture the question for later.

</specifics>

<deferred>
## Deferred Ideas

### Required REQUIREMENTS / ROADMAP follow-ups (apply BEFORE plan-phase 3 executes)

Two reductions to upstream planning docs:

1. **--watch removal — strike from REQUIREMENTS.md and ROADMAP.md:**
   - Strike `STATUS-05`, `STATUS-06`, `STATUS-07`, `STATUS-08`, `STATUS-09` from REQUIREMENTS.md `## v1 Requirements > ### Status`.
   - Strike the corresponding 5 rows from REQUIREMENTS.md `## Traceability` table.
   - Strike Success Criteria #3, #4, #5 from ROADMAP.md Phase 3 section.
   - Phase 3 ROADMAP Requirements line: `STATUS-01, STATUS-02, STATUS-03, STATUS-04` only.

2. **SC#2 vocabulary reconciliation:**
   - Soften ROADMAP.md Phase 3 SC#2 wording to: "JSON-mode output exposes the bucketed `status.kind` lifecycle classifier (per Phase 1 D-02) alongside the raw upstream `status` value, both inside the existing envelope's `data.status` block." No new top-level `state` field.

Both edits should land via a `/gsd-phase --edit` operation (or direct edit) before Phase 3 plan-phase, so the planner sees consistent requirements + success criteria.

### Phase 3 deferred ideas

- **`--watch` revisit** — out of project scope. If demo feedback shows agents painfully poll-thrashing or wanting event-driven semantics, a future milestone may add it. CLI-LEARNINGS.md captures the question.
- **Structured `reason` object (Phase 1 D-03 lift)** — deferred further. Revisit in production CLI build based on prototype feedback.
- **JSON envelope vs raw-resource wire contract** — captured in CLI-LEARNINGS.md; not a Phase 3 decision. Production CLI build revisits.
- **`uiURL` / `availableActions` in `meta`** — both rejected for Phase 3 (out of scope; smuggles watch-lite or duplicates state); CLI-LEARNINGS.md notes whether agents asked for either during the demo.
- **`--detailed` / `--short` toggles for status plaintext** — deferred. Phase 3 default is full sectioned blocks; if humans want a terse form, a future revision can add `--short`.
- **AGENT-04 (RFC 3339 + duration strings in JSON)** — kept as-is for Phase 3 (Phase 1 already wired it). The question "should JSON be raw int64 millis passthrough instead?" is a CLI-LEARNINGS topic.

### Reviewed Todos (not folded)
None — `gsd-sdk query todo.match-phase 3` returned 0 matches.

</deferred>

---

*Phase: 3-Status & Watch (reduced to just Status; Watch removed from project)*
*Context gathered: 2026-05-14*
