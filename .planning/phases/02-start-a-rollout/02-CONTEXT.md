# Phase 2: Start a rollout - Context

**Gathered:** 2026-05-13
**Status:** Ready for planning

<domain>
## Phase Boundary

Ship `ldcli flags rollouts-beta start --flag <key> --environment <env> ...` end-to-end. The operator (human or agent) composes a `startAutomatedRelease` semantic-patch from CLI flags, the CLI mutates the flag, re-fetches the new rollout, and emits a `rollouts.v1beta1` envelope carrying the new rollout's ID and initial state.

**Scope (Phase 2 after discussion-driven reductions):**
1. Compose `startAutomatedRelease` instruction body from CLI flags (stages, variations, randomization unit, metrics with per-metric regression behavior, targeting).
2. PATCH `/api/v2/flags/{p}/{flagKey}` with the `domain-model=launchdarkly.semanticpatch` content type and `LD-API-Version: beta` (the standard rollouts-beta headers from Phase 1).
3. Two-step re-fetch via GET `/internal/projects/{p}/flags/{flagKey}/automated-releases?filter=environmentKey:{ek}&limit=1` (PC-001 workaround locked in Phase 1).
4. Emit success envelope (`schemaVersion: "rollouts.v1beta1"`, `kind: "Rollout"`, `data: <the rollout>`, `meta`) on stdout. Errors go through the same Phase 1 envelope contract (JSON-mode errors on stdout via D-07; exit 1 on any error via D-01).
5. Add `Start(ctx, accessToken, baseURI, projKey, flagKey, envKey, instr StartInstruction) (*Rollout, error)` to `internal/rollouts.Client` per D-08; regenerate mocks.

**Two significant scope reductions came out of discussion:**
- **Preflight (`recommended-duration` check + `--skip-health-checks`)** is deferred to a future phase **within this project**. It's a domain-specific safety feature of automated releases (validates metric/randomization-unit compatibility), so it belongs in the rollouts surface — just not in Phase 2. Phase 2 does NOT call `recommended-duration` and does NOT register `--skip-health-checks`.
- **Idempotency-Key is out of scope for this project entirely.** Per a user preference applied during this discussion: generic CLI-robustness features (idempotency, exit-code taxonomies, structured-retry contracts) that aren't already adopted elsewhere in ldcli should NOT be scoped into a single subtree. They should be tackled CLI-wide outside this project. Phase 2 does NOT set the `Idempotency-Key` header, does NOT register `--idempotency-key`, and does NOT echo a `meta.idempotencyKey`. START-06 and the idempotency clauses of FOUND-05 / AGENT-03 should be **struck from REQUIREMENTS.md** (not moved to a later phase). The dormant `internal/rollouts/idempotency.go:SetIdempotencyKey` helper Phase 1 wired should be deleted as a cleanup task.

Both reductions need follow-up updates to `.planning/REQUIREMENTS.md` and `.planning/ROADMAP.md` — see **Required REQUIREMENTS / ROADMAP follow-ups** in the Deferred Ideas section.

</domain>

<decisions>
## Implementation Decisions

### Stages flag UX
- **D-01:** Stages are expressed as a single compact-list flag: `--stages 25:60m,50:60m,100:60m`. Colon separates allocation from duration; comma separates stages. No `--stage` repeatable form, no `--stages-file`, no single-stage shorthand. Rationale: matches the example already in REQUIREMENTS START-01, ergonomic in shell quoting, leaves room for `--stage` later if real users hit pain.
- **D-02:** Allocation is a percent integer in `[0, 100]` (e.g. `25` = 25%). The CLI multiplies by 1000 internally to produce the API's basis-points field. Decimals (e.g. `12.5`) are rejected with a usage error. Document this in `--help` as a CLI-side translation; this is the user-facing form for PC-014 (API uses int64 millis + basis points; humans want percent + duration string).
- **D-03:** Duration is a Go duration string (`60m`, `1h30m`, `2h`, `300s`, etc.) parsed with `time.ParseDuration`. The CLI converts to milliseconds for the API's `durationMillis` field. Plain integers (no unit suffix) are rejected — there is no millis-passthrough form.

### Metric declaration & guarded vs progressive
- **D-04:** **Drop `--metric` entirely.** Per-metric behavior on regression is declared by which verb-flag the metric appears under:
  - `--pause-on-regression <metricKey>` (repeatable) → `{key: metricKey}` in `metrics`, `{autoRollback: false}` in `metricMonitoringPreferences[metricKey]`. On regression the rollout halts at `monitoring_regressed` and waits for human dismissal.
  - `--revert-on-regression <metricKey>` (repeatable) → `{key: metricKey}` in `metrics`, `{autoRollback: true}` in `metricMonitoringPreferences[metricKey]`. On regression the rollout auto-rolls back.
  - The monitored set is the union of both flags. A metric appearing in BOTH is a usage error.
  - Rationale: self-documenting (flag name describes the behavior), eliminates the parallel-list sync trap PC-010 in CLI form, and rides the vocabulary already locked in Phase 1's D-02 (`paused`, `reverted`).
- **D-05:** **Drop `--release-kind` entirely.** Progressive vs guarded is purely inferred:
  - Zero pause/revert flags → progressive (sends `releaseKind: "progressive"` in the instruction).
  - One or more pause/revert flags → guarded (sends `releaseKind: "guarded"` in the instruction, plus the constructed `metrics[]` and `metricMonitoringPreferences{}`).
  - Rationale: the API rejects guarded-with-zero-metrics anyway; explicit `--release-kind` would only let users declare something the server rejects. Inference is unambiguous.
- **D-06:** **Metric groups (`isGroup: true`) are deferred to v1.1.** v1 supports individual metrics only via the pause/revert flags. If a user passes a metric-group key, the API will reject it server-side — that error surfaces verbatim; CLI does not pre-validate metric vs metric-group identity.

### Targeting scope
- **D-07:** Phase 2 supports **fallthrough** (no targeting flags → empty `ruleId`, empty `ref`, empty `clauses`) and **existing rule by ID** (`--rule-id <uuid>` → sends `ruleId`). Both `--ref` and `--clauses` (new-rule creation) are deferred.
  - Rationale for dropping `--ref`: `ref` is only useful when adding a rule AND starting a rollout on it in the same patch — Phase 2 is start-only, so the rule must already exist, which means the operator has the ruleId.
  - Rationale for dropping `--clauses`: clause objects are LaunchDarkly-shaped JSON (attribute/op/values/contextKind/negate) — hard for agents to construct correctly and a large surface to test. Defer until real-world demand surfaces.
- **D-08:** Fallthrough rollouts are dogfood-gated server-side (`disable-automated-rollouts-on-default-rule`). When the server rejects fallthrough with `"Automated releases cannot be created on the default rule"`, the CLI maps it to `error.code: "unknown_upstream"` (the generic bucket) — **no dedicated `error.code`**. Rationale: keeping the taxonomy small; this is a server-policy condition, not a CLI-actionable failure mode, and the verbatim server message is descriptive enough.

### Preflight (deferred)
- **D-09:** **Preflight is removed from Phase 2.** No `recommended-duration` GET call; no `--skip-health-checks` flag; no TTY prompt path; no `meta.skippedHealthChecks` audit field. START-04 and the related Phase 2 Success Criterion #3 move to a future "Preflight & health checks" phase. See Deferred Ideas for the REQUIREMENTS / ROADMAP follow-up that captures this.

### Idempotency (out of scope for this project)
- **D-10:** **Idempotency-Key is out of scope for the entire rollouts milestone, not just Phase 2.** No `Idempotency-Key` header set on the PATCH; no `--idempotency-key` flag; no `meta.idempotencyKey` echo. START-06 and the idempotency clauses of FOUND-05 / AGENT-03 should be **struck from REQUIREMENTS.md** via a `/gsd-phase` follow-up (not moved to a later phase). Rationale: per user preference, generic CLI-robustness features (idempotency, exit-code taxonomies, structured-retry contracts) that aren't already adopted elsewhere in ldcli should be tackled CLI-wide outside this project — shipping the half-version inside one subtree creates inconsistency. Additionally: the gonfalon `automated-releases` API does not document Idempotency-Key support, no other ldcli command sends it, and server-side `"Flag must not have ongoing rollout"` already guards against accidental double-mutation from retries. The dormant `SetIdempotencyKey` helper at `internal/rollouts/idempotency.go` should be deleted as a follow-up cleanup task (not a Phase 2 deliverable).

### Two-step start & error mapping (open for planner)
- **D-11:** The two-step pattern (PATCH → re-fetch via list+filter+limit=1) is locked from Phase 1 / architecture research / PC-001. **Re-fetch robustness specifics** (eventual-consistency backoff, stale-detection by comparing `createdAt`, behavior when GET returns empty after a successful PATCH) are **planner discretion**, informed by Phase 2 plan-research's staging probes.
- **D-12:** **Error-code taxonomy** for the new mutation failure modes is planner discretion within the existing Phase 1 `error.code` enum. Likely additions: a `flag_not_configured_for_rollout` or `flag_off` code for the server's "flag is off" rejection; a `rollout_already_running` code for the "Flag must not have ongoing rollout" rejection; an `invalid_variation` code for variation-not-found. Map by matching the server's error-message strings; fail open to `unknown_upstream`. **Do NOT** pre-fetch flag state to detect these conditions client-side — the server is authoritative and a pre-fetch adds latency for the happy path.

### Claude's Discretion

- Whether to expose `--comment <string>` for the semantic-patch `comment` field (architecture research mentions it as optional metadata). Default: omit unless the planner finds a clear use case.
- The exact `--help` copy for stages syntax (parsing rules, examples). Reference D-01/D-02/D-03 verbatim where possible so help text stays in sync with this CONTEXT.md.
- Whether `--target-variation` and `--original-variation` accept variation **keys**, variation **IDs**, or **either**. The API takes UUIDs. Most likely answer: accept the key (more ergonomic) and resolve to ID via a flag GET — but this means a pre-state read for the happy path, contradicting D-12's rationale. Planner should validate against staging: does the API accept variation keys directly, and if not, is the GET+resolve cost acceptable?
- File split inside `internal/rollouts/` — likely add `start.go` for the Start method and grow `instructions.go` for the fleshed-out `StartInstruction`. Mirror Phase 1's organization.
- Whether the success envelope's `kind` is `"Rollout"` (one item) or `"RolloutCreate"` (a verb-flavored kind, like Kubernetes' object/event distinction). Default: `"Rollout"` — same kind as `status` will emit in Phase 3, so consumers don't have to special-case envelope kinds across commands.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase 1 carry-forward (locked decisions Phase 2 must honor)
- `.planning/phases/01-list-foundation-first-end-to-end-slice/01-CONTEXT.md` — D-01 (any error → exit 1; taxonomy via `error.code`), D-02 (three-field status block), D-07 (JSON-mode errors on stdout), D-08 (Client grows incrementally — Phase 2 adds Start), envelope shape (`rollouts.v1beta1`)
- `.planning/phases/01-list-foundation-first-end-to-end-slice/01-SMOKE.md` — real-staging gotchas Phase 2 must avoid: `LD-API-Version: beta` is required; `int64 unix millis` is the on-the-wire shape for timestamps (not RFC 3339); error envelopes go to stdout in JSON mode

### Project planning
- `.planning/PROJECT.md` — milestone goals, constraints (real-server validation; API contract learnings → Confluence)
- `.planning/REQUIREMENTS.md` — START-01..07 (Phase 2 scope); note that **START-04 and START-06 have been deferred** out of this phase (see Deferred Ideas)
- `.planning/ROADMAP.md` — Phase 2 goal and success criteria (note: SC#3 about preflight no longer applies after D-09)
- `.planning/STATE.md` — accumulated decisions, open questions, performance targets

### Research (drives implementation)
- `.planning/research/SUMMARY.md` — synthesis of cross-cutting decisions
- `.planning/research/ARCHITECTURE.md` — `startAutomatedRelease` instruction field table; Pattern 1 (Two-Step Start); Pattern 3 (Semantic-Patch Wrapper); RBAC mapping; server-side validation error catalog; 16 papercuts (esp. PC-001, PC-010, PC-012, PC-013, PC-014)
- `.planning/research/STACK.md` — Phase 1 wired the stack; Phase 2 uses what's already there (retryablehttp, golang.org/x/term, JSON envelope helpers)
- `.planning/research/FEATURES.md` — feature catalog; note that exit-code taxonomy from FEATURES is superseded by Phase 1's D-01
- `.planning/research/PITFALLS.md` — anti-patterns; esp. #3 (CLI flags coupled to API field names — translate in `instructions.go`), #9 (silent fallbacks on start), #12 (metric/unit mismatch)

### Papercuts (active and unresolved at Phase 2 start)
- `.planning/API-PAPERCUTS.md` — PC-001 (start returns FeatureFlag not Rollout — drives the two-step), PC-010 (parallel sidecar map for autoRollback — D-04 sidesteps), PC-012 (kind/releaseKind terminology), PC-013 (controlVariationId vs originalVariationId), PC-014 (durationMillis ergonomics — D-03 translates), PC-016 (recommended-duration awkward for progressive — moot after D-09)
- Confluence: [Learnings: automated release API papercuts](https://launchdarkly.atlassian.net/wiki/spaces/~62435d09f6a26900695be8d7/pages/4875452435) — record any *new* API contract observations encountered during Phase 2 execution per PROJECT.md constraint. Fetch the page first (`mcp__mcp-atlassian__confluence_get_page` → `confluence_update_page`) to avoid clobbering concurrent human edits.

### Codebase patterns (existing ldcli)
- `internal/rollouts/client.go` — Phase 1's `RolloutsClient` skeleton; new `Start` method extends this. Mirror the GET-path structure for headers + retry wiring; the PATCH path needs the semantic-patch `Content-Type`.
- `internal/rollouts/instructions.go` — Phase 1 stub for `StartInstruction`; Phase 2 fleshes out the field set per D-01..D-04.
- `internal/rollouts/models.go` — existing `Rollout` and `RolloutList` DTOs Phase 2 reuses for the re-fetch decode.
- `internal/rollouts/errors.go` — error-code enum; Phase 2 extends with the new mutation-specific codes per D-12.
- `internal/rollouts/envelope.go` — `NewRolloutEnvelope` / `NewErrorEnvelope` helpers; Phase 2 emits via these.
- `cmd/flags/rollouts/list.go` — closest analog for the new `start.go` (RunE shape, emitSuccess/emitError split, plaintext-vs-JSON dispatch).
- `cmd/flags/rollouts/flags.go` — pattern for flag registration via `cmd/cliflags` constants.
- `cmd/flags/rollouts/rollouts.go` — `NewRolloutsCmd` adds `NewStartCmd(client)`; the beta banner / analytics PreRun is inherited.
- `cmd/cliflags/flags.go` — add new flag constants: `StagesFlag` (and description), `TargetVariationFlag`, `OriginalVariationFlag`, `RandomizationUnitFlag`, `PauseOnRegressionFlag`, `RevertOnRegressionFlag`, `RuleIdFlag` (and possibly `ExtensionDurationFlag`).
- `cmd/flags/toggle.go` — JSON Patch (RFC 6902) pattern. **Do NOT copy**; rollouts uses semantic patch (`application/json; domain-model=launchdarkly.semanticpatch`). Architecture INTEGRATION POINTS table calls this out explicitly.

### Gonfalon source (for instruction validation rules)
- `gonfalon/internal/flags/instruction/instruction_start_automated_release.go` — authoritative field list, validation rules, error messages the CLI may need to map for D-12

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/rollouts/RolloutsClient.setStandardHeaders`** (`client.go:223`) — already sets `Authorization`, `Content-Type: application/json`, `User-Agent`, `LD-API-Version: beta`. The new `Start` method needs a variant that overrides `Content-Type` to `application/json; domain-model=launchdarkly.semanticpatch` for the PATCH only. The re-fetch GET uses the existing `setStandardHeaders` unchanged.
- **`internal/rollouts/client.go:newRetryableClient`** — the retry envelope (4 retries, 500ms–8s, 4xx never retried, PassthroughErrorHandler) is reused as-is for the PATCH. Note: `retryablehttp.DefaultRetryPolicy` does not retry on 4xx, so a "rollout already running" response (likely 4xx) will NOT be retried — correct behavior.
- **`internal/rollouts/mapAPIError`** (`errors.go`) — Phase 1 mapping for 4xx/5xx → `*RolloutError` with `Code` / `Message` / `NextAction`. Phase 2 extends the message-matching switch with the new mutation-specific codes per D-12.
- **`internal/rollouts/envelope.go:NewRolloutEnvelope`** (or add `NewStartEnvelope` if the shape diverges) — wraps a `*Rollout` into the `rollouts.v1beta1` envelope. Reuse for the success path.
- **`google/uuid`** — already vendored; planner may still need it for misc UUID handling, just not for Idempotency-Key (D-10).

### Established Patterns
- **`internal/<domain>/Client` interface + `var _ Client = ...{}` compile-time assertion**: Phase 1 set this up; Phase 2 only widens the interface (adds `Start`).
- **`mockgen`-generated mocks**: rerun mockgen to regenerate `internal/rollouts/mock_client.go` after the interface change.
- **Reading Viper at `RunE` time, not constructor time** (CONVENTIONS.md): applies to the new `cmd/flags/rollouts/start.go`.
- **Semantic-patch envelope** (research Pattern 3): `{environmentKey, instructions: [<instruction>], comment?}` with `Content-Type: application/json; domain-model=launchdarkly.semanticpatch` and `LD-API-Version: beta`.
- **JSON-mode error envelope on stdout** (Phase 1 D-07): `emitError` in `list.go` is the canonical pattern. Phase 2's `start.go` should mirror it.

### Integration Points
- **`cmd/root.go` `NewRootCommand`**: `RolloutsClient` is already instantiated; the new `NewStartCmd` registers under `NewRolloutsCmd` automatically once added to the AddCommand list inside `cmd/flags/rollouts/rollouts.go`.
- **`cmd/flags/rollouts/rollouts.go:NewRolloutsCmd`**: add `cmd.AddCommand(NewStartCmd(client))` alongside the existing `NewListCmd`.
- **`internal/rollouts/Client` interface** (`client.go:31`): add `Start` per D-08. Compile-time assertion will catch incomplete mock regeneration.

</code_context>

<specifics>
## Specific Ideas

- **The `--pause-on-regression` / `--revert-on-regression` naming is deliberate and load-bearing.** The flag names describe operationally what happens, mirroring Phase 1's `kind: paused | reverted` lifecycle vocabulary (D-02). Do not abbreviate to `--pause`/`--revert` (less clear) or invert to `--auto-rollback` (the API field's name; less honest about what the user is actually opting into).
- **`--ref` is purposefully deferred** — it's only ergonomic when the user is also adding the rule in the same instruction batch, which Phase 2 isn't doing. If a future phase adds multi-instruction patches (e.g., create-rule-and-start-rollout), `--ref` becomes natural; revisit then.
- **Generic CLI-robustness features stay out of this project.** Stated by the user during this discussion: features that are general to mutative CLI commands (idempotency, numeric exit-code taxonomies, structured retry contracts) but NOT already adopted CLI-wide in ldcli should be tackled "in a general way outside of this project." Shipping the half-version inside one subtree creates inconsistency that has to be retrofitted later. This is the rationale for D-10 (idempotency drop) and reinforces Phase 1's D-01 (no numeric exit-code taxonomy beyond ldcli's existing "exit 1 on any error"). Domain-specific safety features (e.g. preflight via `recommended-duration`) are different — those belong to the rollouts surface and stay in scope (just deferred to a later phase, per D-09).
- **Two-step start safety:** the server already guards against "Flag must not have ongoing rollout" — a retried PATCH after a transient failure either succeeds OR fails with a conflict error. Without Idempotency-Key (D-10), the CLI relies entirely on this server-side guard for retry safety. The planner should validate empirically against staging during Phase 2 plan-research and document the actual retry behavior in API-PAPERCUTS.md (likely a new entry: "no idempotency support; retries may produce duplicate-attempt conflicts").

</specifics>

<deferred>
## Deferred Ideas

### Required REQUIREMENTS / ROADMAP follow-ups

Two scope reductions in this discussion need to be reflected in upstream planning docs **before** Phase 2 plan-phase runs (or as a parallel `/gsd-phase` task):

1. **Preflight phase (new phase within this project, between current Phase 2 and Phase 3, or appended to v1.x):**
   - Domain-specific safety feature of automated releases — belongs in this project.
   - Moves START-04 (preflight + `--skip-health-checks`) out of Phase 2.
   - Drops Phase 2 Success Criterion #3 (the "non-TTY runs preflight via `recommended-duration` before any mutation" line) from ROADMAP.md.
   - Adds a new phase that delivers: `recommended-duration` GET as the preflight proxy, `--skip-health-checks` flag, TTY prompt path on failure, non-TTY structured error, audit shape in success envelope, and resolution of the per-metric vs aggregate detail open question STATE.md flags.

2. **Idempotency — strike from REQUIREMENTS.md, do not create a phase for it:**
   - Generic CLI-robustness feature; not adopted elsewhere in ldcli; out of scope for this project entirely (per user preference; see Specifics).
   - Strike START-06 (idempotency-aware `start`) from REQUIREMENTS.md `## v1 Requirements > ### Start`.
   - Strike the `Idempotency-Key`-related clauses from FOUND-05 ("...and a generated `Idempotency-Key` UUID on every mutation...") and AGENT-03 ("Mutating commands return a coherent response on retry with the same idempotency key..."). The retry/backoff parts of FOUND-05 stay.
   - Strike "`google/uuid` for `Idempotency-Key`" from STATE.md's stack-research decision row (the rest of that row — `go-retryablehttp@v0.7.7`, `golang.org/x/term` — stays).
   - Cleanup task: delete `internal/rollouts/idempotency.go` (its sole exported helper `SetIdempotencyKey` is unreferenced and now planned to stay that way).
   - If LD CLI as a whole adopts an idempotency story in the future, rollouts inherits it for free — no per-feature design needed.

### Per-phase concerns

- **Metric groups (`isGroup: true`)** — deferred to v1.1 per D-06. Likely surface: `--pause-on-regression-group` / `--revert-on-regression-group` mirroring the metric pair, OR a typed prefix on the existing flags. Decide when first user demand surfaces.
- **`--ref` for existing-rule selection** — deferred per D-07 rationale. Natural to add when/if multi-instruction patches (create rule + start rollout in one call) are introduced.
- **`--clauses` for new-rule targeting** — deferred per D-07. Defer until real demand surfaces; likely needs a `--clauses-file` form rather than inline JSON to be agent-tractable.
- **`--comment` for the semantic-patch envelope** — Claude's discretion; default omit.
- **`--extension-duration` (guarded-only API field)** — Claude's discretion. If included in Phase 2, only valid when guarded (≥1 pause/revert flag); error fast if passed with progressive.
- **Per-metric pass/fail vs aggregate detail from `recommended-duration`** — STATE.md open question; moot for Phase 2 after D-09 but lives in the future Preflight phase.
- **Dedicated `error.code` for "default rule rollouts disabled" dogfood gate** (rejected as D-08, but worth revisiting) — if this surfaces in real usage as a common failure mode, lift it from `unknown_upstream` to its own code with a more actionable `nextAction`.

### Reviewed Todos (not folded)
None — no pending todos matched Phase 2 scope (`gsd-sdk query todo.match-phase 2` returned 0 matches).

</deferred>

---

*Phase: 2-Start a rollout*
*Context gathered: 2026-05-13*
