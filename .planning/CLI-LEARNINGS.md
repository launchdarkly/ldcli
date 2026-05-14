# CLI / UX Learnings: ldcli flags rollouts-beta

> Prototype-era learnings doc that catalogues open CLI/UX questions surfaced by the
> rollouts-beta prototype. Sibling to API-PAPERCUTS.md; that doc captures API gaps the
> upstream team should fix, this doc captures CLI/UX design questions the future
> production CLI build should revisit. Feeds production CLI build's design discussions per
> REQ-LEARN-01 (PROJECT.md) and LEARN-01..03 (REQUIREMENTS.md).

**Last updated:** 2026-05-14
Active count: 12
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

**What we did in prototype:** Shipped the typed-struct approach (Phase 1). The struct has roughly the right shape for the API's documented payload, but the upstream is in beta — any new field the API team adds will be invisible to envelope consumers until a CLI release adds the struct field. And boolean fields with `omitempty` lose the distinction between "field absent from wire" and "field present and false."

**What's open for production CLI build:** Switch the envelope's `data` to `json.RawMessage` (or `map[string]interface{}`) so the wire shape is preserved bit-for-bit; only decode to the typed struct when the plaintext renderer or business logic needs typed access. Or keep the typed struct but remove `omitempty` on every boolean and add `AdditionalFields map[string]any` for forward compatibility. Pick one and apply uniformly across all rollouts-beta verbs.

**Severity:** high

### CL-009 — Plaintext `Env: —` when env is implicit

**Question:** When the operator runs `status --flag <key>` without `--environment`, the most-recent path returns a rollout payload whose only env identifier is `environmentId` (opaque ObjectId, see PC-019). The plaintext renderer has no human-readable env key to display, so it shows `Env: —`. This is visually surprising — the rollout *does* belong to a specific env, and the env key is recoverable from `_links.self.href`.

**What we did in prototype:** Phase 3 plaintext renders `Env: —` when neither the operator nor the API supplies an env key. No parsing of `_links.self.href`. No echoing of `environmentId` (which is operator-meaningless).

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

---

## Resolved

*(empty)*
