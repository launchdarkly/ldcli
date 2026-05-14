# CLI / UX Learnings: ldcli flags rollouts-beta

> Prototype-era learnings doc that catalogues open CLI/UX questions surfaced by the
> rollouts-beta prototype. Sibling to API-PAPERCUTS.md; that doc captures API gaps the
> upstream team should fix, this doc captures CLI/UX design questions the future
> production CLI build should revisit. Feeds production CLI build's design discussions per
> REQ-LEARN-01 (PROJECT.md) and LEARN-01..03 (REQUIREMENTS.md).

**Last updated:** 2026-05-14
Active count: 15
Resolved count: 0

Seeded during Phase 3 plan-phase (2026-05-14). Entries are open CLI/UX questions about
how a production CLI for automated rollouts should be shaped — envelope vs raw, classifier
fields, exit-code taxonomy, etc. They are NOT API gaps (those go to `.planning/API-PAPERCUTS.md`
+ Confluence page `4875452435`). Production CLI build revisits this doc per LEARN-03. Append
new entries inline as Phase 3 implementation and real-staging smoke surface them (LEARN-02).

## Active Index

| Anchor | One-line                                                                              | Discovered | Affected commands |
| ------ | ------------------------------------------------------------------------------------- | ---------- | ----------------- |
| CL-001 | JSON envelope vs raw-resource wire shape (gh/kubectl style)                           | 2026-05-14 | all rollouts-beta |
| CL-002 | AGENT-04 timestamp format: RFC 3339 vs raw int64 millis pass-through                  | 2026-05-14 | all rollouts-beta |
| CL-003 | Phase 1 D-03 structured `reason` lift vs single `label` string                        | 2026-05-14 | status, list      |
| CL-004 | Exit-code taxonomy richness (exit 1 + error.code vs distinct numeric codes)           | 2026-05-14 | all rollouts-beta |
| CL-005 | Watch-shaped use cases after --watch removal (poll cadence, event-driven monitoring)  | 2026-05-14 | status            |
| CL-006 | "Most recent" semantics (createdAt DESC vs most-recent-running)                       | 2026-05-14 | status, list      |
| CL-007 | `--rollout-id` requiring `--environment` (PC-004 surface)                             | 2026-05-14 | status            |
| CL-008 | Typed Go structs strip wire fields on read (`omitempty` + missing struct fields)      | 2026-05-14 | all rollouts-beta |
| CL-009 | Plaintext `Env: —` when env is implicit (no `environmentKey` on wire, PC-019)         | 2026-05-14 | status            |
| CL-010 | Plaintext stage marker "in progress" while overall State is "paused"                  | 2026-05-14 | status            |
| CL-011 | Single-stage rollout plaintext rendering vs multi-stage reference doc                 | 2026-05-14 | status            |
| CL-012 | Plaintext `auto-rollback: false` for every metric (downstream of CL-008)              | 2026-05-14 | status            |
| CL-013 | Dismiss pre-read gates on Status.Kind="regressed" but upstream never emits that Kind  | 2026-05-14 | dismiss-regression |
| CL-014 | `stop --to-variation` accepts any variation UUID (no validation against orig/target)  | 2026-05-14 | stop              |
| CL-015 | `meta.uiURL` is flag-level (`/features/{key}/targeting`), not rollout-level           | 2026-05-14 | stop, dismiss     |

## Entries

### CL-001 — JSON envelope vs raw-resource wire shape

**Question:** Should `--output json` emit the `rollouts.v1beta1` envelope (`{schemaVersion, kind, data, meta}`) or the raw resource directly (gh/kubectl style)? The envelope buys us versioning, error-on-stdout routing, and meta affordances (`fetchedAt`, `warnings`); the raw shape is what most modern CLIs ship and is closer to "JSON is API-passthrough" intuition.

**What we did in prototype:** Phase 1 picked the envelope (D-07). All rollouts-beta verbs emit `{schemaVersion: "rollouts.v1beta1", kind: "<Kind>", data: ..., meta: {fetchedAt}}` on success; errors emit `{schemaVersion, kind: "Error", error: {code, message, nextAction}}` on stdout (D-07 routing). Phase 2 + Phase 3 reused this verbatim (D-05).

**What's open for production CLI build:** Did agents/CI consumers find the envelope helpful or did they immediately `jq .data` past it? Did `schemaVersion` ever pay off versioning-wise, or did anyone branch on `kind` programmatically? Would a raw-resource shape with versioning expressed via `Content-Type` / response headers be enough? Compare consumer ergonomics between envelope and raw on a real workflow.

**Severity:** medium

### CL-002 — AGENT-04 timestamp format: RFC 3339 vs raw int64 millis pass-through

**Question:** The upstream `automated-releases` API emits int64 unix-millis for every timestamp (PC-014 / PC-014-adjacent). The CLI converts to RFC 3339 in JSON output per AGENT-04. Did agents actually prefer RFC 3339, or did they want raw millis they could feed directly into `Date.now()` math without parsing? Same question for durations (Go-style `1h30m` vs raw millis).

**What we did in prototype:** Phase 1 wired AGENT-04 — every timestamp is RFC 3339 UTC in JSON; every stage duration carries both `durationMillis` (raw) and `duration` (Go-style string) per the converter in `internal/rollouts/models.go:toStage`. Event timestamps land as RFC 3339 only (no parallel millis field).

**What's open for production CLI build:** Did the RFC 3339 conversion add value for agents, or did it just force them to re-parse? Should JSON-mode be a literal API pass-through (raw millis), with the human-friendly form reserved for plaintext only? If so, drop the `duration` companion field on Stage and let agents compute it.

**Severity:** low

### CL-003 — Phase 1 D-03 structured `reason` lift vs single `label` string

**Question:** Phase 1 D-03 chose to keep status reason information inline in `status.label` (a single human-readable string like "Regression detected on metric latency-p99") rather than expose a structured `reason: {kind, metricKey, ...}` object alongside `status.kind`. Did agents struggle parsing the prose `label` to extract the regressing metric? Or did the `events` array (which carries `metricKey` on `regression_detected` entries) cover the structured-extraction case adequately?

**What we did in prototype:** Phase 1 D-03 deferred the structured reason. Phase 3 explicitly did NOT lift it (D-11). `status.label` stays the only human-readable reason carrier; `status.kind` is the 5-bucket classifier; agents extract metric specifics from `events[]` if needed. Plaintext renderer consumes `status.label` verbatim.

**What's open for production CLI build:** If we revisit, what does the structured `reason` shape look like? `{kind: "regression", metricKey: "..."} | {kind: "extension", extendedToMillis: ...} | {kind: "safe_roll_forward", ...}`? Or generic `{discriminator, details: {...}}`? Compare against real Phase 3 + Phase 4 dismiss workflows where agents need to know "which metric regressed" without prose parsing.

**Severity:** medium

### CL-004 — Exit-code taxonomy richness

**Question:** Phase 1 D-01 + Phase 3 D-10 chose "exit 1 for any error" + structured `error.code` in the JSON envelope, rather than a richer numeric exit-code taxonomy (e.g., sysexits-aligned 64/65/69/75/77 or sequential 0-9 with semantic meanings). Did consumers want richer numeric codes for shell-script branching, or was `jq -r .error.code` enough?

**What we did in prototype:** Every error path exits 1. The JSON envelope carries `error.code` from a documented enum (`unauthorized`, `forbidden`, `not_found`, `bad_request`, `conflict`, `rate_limited`, `upstream_unavailable`, `network_error`, `beta_gate_closed`, `unknown_upstream`, plus Phase 2 mutation-specific codes + Phase 3's `no_rollouts_found`). Plaintext consumers see the error message on stderr.

**What's open for production CLI build:** Did anyone write a shell script that wanted to branch on numeric exit code without parsing JSON? Did anyone need a "transient vs permanent" distinction at the exit-code level (e.g., for CI retry-policy gating)? If yes, what numeric taxonomy makes sense — sysexits-aligned, sequential, or domain-specific?

**Severity:** low

### CL-005 — Watch-shaped use cases after `--watch` removal

**Question:** Phase 3 removed `--watch` entirely (D-01). Polling is now the agent's responsibility — re-invoke `status` periodically with whatever cadence makes sense. Did agents end up reinventing watch (sleep loops + diff comparison), did they want event-driven monitoring (webhooks, SSE), or was one-shot status sufficient for the common case?

**What we did in prototype:** Shipped `status` as a single-snapshot command. No polling helpers in the CLI. CLI-LEARNINGS captures the open question; the production CLI build decides whether to add `--watch` back, ship an `events` subcommand, or expose webhook config.

**What's open for production CLI build:** If watch returns: what's the right shape — `gh pr checks --watch` (alt screen + redraw)? NDJSON event stream when `--output json`? "Until next actionable event" vs "until terminal" default? Does poll cadence belong as a CLI flag or as a baked-in conservative default? If event-driven instead: do we need a `webhooks` subcommand surface?

**Severity:** high

### CL-006 — "Most recent" semantics

**Question:** Phase 1 D-02 / Phase 3 D-04 picked `createdAt DESC, ID ASC` as the "most recent" sort. Did this surprise users who expected "most recent running" or "most recent active"? A flag with a completed rollout from yesterday and a paused rollout from a week ago surfaces the *completed* one as "most recent" — is that the right default?

**What we did in prototype:** Phase 1 list and Phase 3 status both honor `createdAt DESC, ID ASC` verbatim. `status --flag <key>` (no `--rollout-id`) returns `items[0]` after that sort. No "currently-active" / "most-recent-running" filter is exposed.

**What's open for production CLI build:** Should `status` default to "most recent active" (e.g., status in {`in_progress`, `monitoring_regressed`, `waiting`})? Should it fall back to "most recent any" only when no active rollouts exist? Or is the createdAt-DESC default fine and users learn to pass `--rollout-id` when they want something specific? Compare against real demo feedback — did this trip anyone up?

**Severity:** medium

### CL-007 — `--rollout-id` requiring `--environment`

**Question:** PC-004 (GET-by-ID requires `environmentKey` in the URL path despite globally-unique rollout UUID) leaks into Phase 3 user-facing UX — `status --rollout-id <id>` requires `--environment <env>` too. Did CLI users find this redundant/annoying? Should the CLI silently auto-resolve env via list-and-filter when only `--rollout-id` is given, hiding the API gap?

**What we did in prototype:** Phase 3 D-03 chose to surface the requirement explicitly: CLI-side validation rejects `--rollout-id` without `--environment` BEFORE any API call (with `error.code: bad_request` + nextAction pointing at PC-004). Rationale: papering over the API gap with a list-and-filter call would add complexity and obscure the upstream issue — the gap is the learning we want to surface.

**What's open for production CLI build:** If PC-004 lands API-side (account-scoped GET-by-ID), this question disappears. If PC-004 stays unresolved, do we auto-resolve env in the production CLI to be friendly, or keep the explicit-required surface? Compare against demo feedback — did anyone get confused by the "two flags required for one ID" UX?

**Severity:** low

### CL-008 — Typed Go structs strip wire fields on read

**Question:** The CLI decodes upstream responses into `internal/rollouts/models.go` Go structs and re-marshals to the envelope's `data` field. Two failure modes empirically surfaced in the Phase 3 smoke run: (a) struct fields with `json:",omitempty"` strip zero-value primitives — e.g. `MetricConfiguration.AutoRollback: false` is silently dropped on re-marshal, so the operator never sees that auto-rollback was explicitly disabled; (b) fields the API returns that have no corresponding struct field are silently dropped — e.g. `metricConfigurations[].differenceEstimateType: "absolute"` exists in the raw curl response but is missing from the CLI envelope. Both violate the project's "JSON output is API-passthrough" principle (memory `feedback_json_api_passthrough.md`).

**What we did in prototype:** Shipped the typed-struct approach (Phase 1). The struct has roughly the right shape for the API's documented payload, but the upstream is in beta — any new field the API team adds will be invisible to envelope consumers until a CLI release adds the struct field. And boolean fields with `omitempty` lose the distinction between "field absent from wire" and "field present and false." **Phase 4 confirmation:** Smoke G's plaintext rendering for stop omits `data.status.status` (the `manually_completed` / `manually_reverted` server-side field), confirming the same struct-strip pattern in the post-stop renderer. The JSON envelope's `data.status.status` field IS preserved on the wire (visible in Smokes A and B captures), so the loss is renderer-specific, not envelope-wide.

**What's open for production CLI build:** Switch the envelope's `data` to `json.RawMessage` (or `map[string]interface{}`) so the wire shape is preserved bit-for-bit; only decode to the typed struct when the plaintext renderer or business logic needs typed access. Or keep the typed struct but remove `omitempty` on every boolean and add `AdditionalFields map[string]any` for forward compatibility. Pick one and apply uniformly across all rollouts-beta verbs.

**Severity:** high

### CL-009 — Plaintext `Env: —` when env is implicit

**Question:** When the operator runs `status --flag <key>` without `--environment`, the most-recent path returns a rollout payload whose only env identifier is `environmentId` (opaque ObjectId, see PC-019). The plaintext renderer has no human-readable env key to display, so it shows `Env: —`. This is visually surprising — the rollout *does* belong to a specific env, and the env key is recoverable from `_links.self.href`.

**What we did in prototype:** Phase 3 plaintext renders `Env: —` when neither the operator nor the API supplies an env key. No parsing of `_links.self.href`. No echoing of `environmentId` (which is operator-meaningless). **Phase 4 confirmation:** Same gap reaffirmed in the stop command's plaintext renderer (Smoke G output: `Stopped rollout c42efcad... (progressive) in environment —`). The operator passed `--environment test` but the renderer doesn't carry the operator-supplied env through to the response shape. Same fix candidates apply across both `status` and `stop` renderers.

**What's open for production CLI build:** Three options once PC-019 is filed: (a) parse `_links.self.href` to extract the env key, (b) echo `environmentId` with a note that it's an opaque ID, (c) require `--environment` always for `status` (more friction but no ambiguous renderings). Picks (a) and (c) require additional CLI logic; (b) is honest but unhelpful. If PC-019 lands API-side, this question disappears.

**Severity:** medium

### CL-010 — Plaintext stage marker contradicts overall State

**Question:** The Phase 3 plaintext renderer derives the per-stage state column from `stageIndex == latestStageIndex` and the presence/absence of a terminal flag — so a paused rollout still renders its current stage as `[→] in progress`. The Overview line above shows `State: paused`. Visual contradiction: stage line says "in progress" while the overall rollout state says "paused."

**What we did in prototype:** Phase 3 status_test.go covers the rendering happy paths but didn't catch this because the test fixtures use a multi-stage rollout where stage state and rollout state agree. The single-stage paused rollout on staging hit the edge case.

**What's open for production CLI build:** Either (a) propagate `data.status.kind` into the stage-state derivation (e.g., if rollout is paused, the latest stage renders `paused`, not `in progress`), or (b) drop the stage state column for the latest stage and rely on the Overview block's `State:` line. The first is more informative; the second is simpler.

**Severity:** low

### CL-011 — Single-stage rollouts vs multi-stage reference doc

**Question:** CONTEXT.md D-07's plaintext reference showed three stages (25% / 50% / 75% / pending / completed / in progress markers). Phase 3 staging surfaced a single-stage guarded rollout that the renderer correctly displayed as one stage line, but a new operator reading the reference doc would expect the multi-stage shape. Is the reference doc misleading?

**What we did in prototype:** Renderer handles 1, 2, or N stages uniformly — no bug. Reference doc kept as-is for the milestone.

**What's open for production CLI build:** Update onboarding/help text to show a single-stage example first (more common case), with the multi-stage shape as a secondary example. Or drop the in-line example entirely from `--help` and link to a doc.

**Severity:** low

### CL-012 — Plaintext `auto-rollback: false` for every metric

**Question:** The plaintext renderer surfaces `auto-rollback: false` for every metric configuration, even when the API response on the wire would say `auto-rollback: true`. Root cause is CL-008 — the typed-struct's `omitempty` strips the field on re-marshal, so the renderer sees zero-value `false`. Visible symptom is misleading rendering.

**What we did in prototype:** No production fix in Phase 3. The renderer faithfully shows what the typed struct holds; the typed struct happens to hold zero-value-stripped data. Once CL-008 is addressed, this fixes itself.

**What's open for production CLI build:** Resolved as a downstream effect of CL-008. If CL-008 stays unfixed for some reason, the alternative is to drop the `auto-rollback:` line from plaintext until we can render reliably.

**Severity:** low

### CL-013 — Dismiss pre-read gates on the wrong field

**Question:** How should the CLI detect "is this rollout currently in an unresolved regression?" — `Status.Kind`, `status.label`, scanning `events[]`, or a future explicit upstream predicate?

**What we did in prototype:** Phase 4 dismiss-regression gates on `current.Status.Kind == "regressed"` (see `cmd/flags/rollouts/dismiss.go`). Phase 4 real-staging smoke (`04-SMOKE.md` Smoke D + history-sweep observations) found that no rollout in any observed flag carries `Status.Kind == "regressed"` — regressed guarded rollouts surface as `Kind == "paused"` with the regression encoded in `status.label` ("the default rule paused at 50%: regressions detected for ..."). The CLI's gate therefore rejects every real regression scenario, and the bounded-backoff polling loop / PC-007 timeout warning path was never exercised end-to-end. **Phase 4 confirmation: same gap; cross-references PC-021.**

**What's open for production CLI build:** Reshape the pre-read to detect regression via `status.label` substring match OR by scanning `events[]` for a `regression_detected` event without a subsequent `regression_dismissed`/`safe_roll_forward`. Coordinate with the API team on PC-021 — the cleanest fix is a stable upstream predicate (`data.activeRegression: bool` or a `"regressed"` Kind value). Until upstream changes, prefer `events[]` scanning over `label` substring match — `label` text is not a stable contract. Re-run Phase 4 dismiss smoke once the pre-read is reshaped; this unblocks the empirical answers to Plan 04-02 open questions #1 (polling-budget rightness) and #2 (instruction body shape).

**Severity:** high

### CL-014 — `stop --to-variation` accepts any variation UUID

**Question:** Should the CLI validate that `--to-variation` matches either the rollout's `originalVariationId` or `targetVariationId`, or accept arbitrary UUIDs and let the server validate?

**What we did in prototype:** Pass-through; no validation. Phase 4 Smoke B used the correct original-variation UUID (`c0cf6728...`), but a typo'd or wrong-flag UUID would also pass the CLI pre-read and either fail server-side with a non-obvious error or, worse, silently stop the rollout to an unintended variation. The prototype trusts the operator to supply the right UUID.

**What's open for production CLI build:** (a) Validate `--to-variation` against the flag's variations list via a pre-flight `flags get` lookup (adds one round-trip; one extra failure mode if the lookup itself fails). (b) Ship higher-level `--rollback` / `--roll-forward` flags that resolve to the original/target UUID automatically (less footgun, matches operator intent more directly; the underlying `--to-variation` stays for advanced/agent usage). (c) Reject UUIDs that don't match either original or target with a friendly error before sending the PATCH. Option (b) is the most operator-friendly; option (a) is the safest backstop.

**Severity:** medium

### CL-015 — `meta.uiURL` path shape is flag-level, not rollout-level

**Question:** Should the UI permalink point at the flag's targeting tab (current behavior) or at a rollout-specific anchor (e.g. `/features/{flagKey}/automated-releases/{rolloutId}` or a query string anchor like `?rollout={rolloutId}`)?

**What we did in prototype:** `internal/rollouts/envelope.go:BuildUIURL` constructs `https://{base}/{projectKey}/{envKey}/features/{flagKey}/targeting`. Phase 4 real-staging smoke (Smokes A + B + G) verified the URL resolves to the flag's targeting page — operators can see the active rollout from there. Not wrong, just not maximally precise.

**What's open for production CLI build:** Investigate whether the LD UI exposes a stable per-rollout anchor — if so, switch `BuildUIURL` to emit it (the operator clicks once and lands on the exact rollout's view). Coordinate with the UI team. If no rollout anchor exists, file a UI feature request; in the meantime, the current flag-level URL is a defensible default.

**Severity:** low

---

## Resolved

*(empty)*
