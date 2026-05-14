---
phase: 03-status-watch
plan: 02
status: complete
completed_at: 2026-05-14
---

# Plan 03-02 — Real-staging smoke + papercut/learnings updates

## What shipped

- `.planning/phases/03-status-watch/03-SMOKE.md` — 5-scenario smoke log (A: most-recent, B: explicit `--rollout-id`, C: no-rollouts-found, D: validation error, E: plaintext sanity) executed against `https://ld-stg.launchdarkly.com` on commit `05de336`.
- `.planning/API-PAPERCUTS.md` — 1 new entry (PC-019); active count 18 → 19; `Last updated: 2026-05-14`.
- `.planning/CLI-LEARNINGS.md` — 5 new entries (CL-008..CL-012); active count 7 → 12; `Last updated: 2026-05-14`.
- Confluence page `4875452435` (v3 → v4) — Phase 3 confirmation appended to existing entry #1 (env-key gap also affects GET-by-id endpoint).

## Counts at a glance

| Artifact | Before | After | Delta |
|---|---|---|---|
| Smoke scenarios executed | 0 | 5 | +5 |
| API-PAPERCUTS.md entries | 18 | 19 | +1 (PC-019) |
| CLI-LEARNINGS.md entries | 7 | 12 | +5 (CL-008..CL-012) |
| Confluence page version | 3 | 4 | +1 (merged Phase 3 confirmation; no duplicate entry) |

## Staging exercise outcomes

All 5 smoke scenarios passed:

- **A** (most-recent / `ldcli-blitz-2-guarded-rollouts`): exit 0; envelope shape matches D-05 (`schemaVersion`, `kind: "Rollout"`, `data` with `status.{status,kind,label}` block, `meta.fetchedAt`).
- **B** (explicit `--rollout-id` + `--environment`): exit 0; `data` block byte-identical to Smoke A (verified via diff). No Get-vs-List divergence; D-04 most-recent semantics holds end-to-end.
- **C** (no rollouts / `ldcli-blitz-1-no-rollout`): exit 1; envelope `kind: "Error"`, `error.code: "no_rollouts_found"` on **stdout**; stderr `rollouts status failed`. New error constant from Plan 03-01 (`ErrCodeNoRolloutsFound`) wired correctly.
- **D** (validation: `--rollout-id` without `--environment`): exit 1; envelope `kind: "Error"`, `error.code: "bad_request"` on **stdout**; no API call made (returned instantly). D-03 client-side validation works.
- **E** (plaintext sanity): exit 0; sectioned Overview / Stages / Metrics / Events layout renders end-to-end.

## API contract observations surfaced

1. **PC-019** — Rollout response returns `environmentId` (opaque ObjectId), not `environmentKey`. Verified via raw curl: the API literally does not include `environmentKey` on either the list or single-rollout payloads. Same gap on both endpoints (list + GET-by-id). The Confluence page already had this filed for the list endpoint as of 2026-05-13; Phase 3 added the GET-by-id confirmation to the existing entry rather than duplicating.

No other new API papercuts surfaced — the empty-collection wire shape (`items: []`, verified via raw curl) is conventional; the missing-on-wire `metricConfigurations[].autoRollback` turned out to be **present** on the wire and stripped by our typed-struct decoder (which is a CLI fidelity issue, not an API gap — see CL-008).

## CLI/UX observations surfaced

The biggest learning is **CL-008** — the CLI's typed Go structs strip wire fields on read. Specifically:

- `MetricConfiguration.AutoRollback bool` with `json:",omitempty"` strips the zero-value `false` on re-marshal — so even when the API explicitly says `autoRollback: false`, the CLI envelope omits it.
- `MetricConfiguration` has no `differenceEstimateType` field, so the API's `"differenceEstimateType": "absolute"` is silently dropped during `json.Unmarshal` → typed struct → re-marshal.

This violates the project's "JSON output is API-passthrough" principle (memory `feedback_json_api_passthrough.md`). The production CLI build should consider switching the envelope's `data` field to `json.RawMessage` or `map[string]interface{}` to preserve wire fidelity.

Other CLI/UX observations:

- **CL-009** — `Env: —` rendered in plaintext when env is implicit (downstream of PC-019; could be papered over by parsing `_links.self.href` but Phase 3 didn't).
- **CL-010** — Stage marker shows "in progress" while overall State is "paused" — visual contradiction in plaintext.
- **CL-011** — Reference plaintext doc (CONTEXT.md D-07) shows multi-stage example; real-staging fixture was single-stage. Doc could be misleading for first-time users.
- **CL-012** — Plaintext `auto-rollback: false` for every metric (downstream of CL-008; self-fixes once CL-008 is addressed).

## Anomalies / surprises

- **No eventual-consistency window** observed between Get-by-id and List → items[0] for the same rollout. We had budgeted for a possible divergence; the wire shape was byte-identical.
- **Status enum has values beyond PC-005's list.** The staging fixture's status was `monitoring_stopped` (mapped to `kind: paused`), which wasn't in PC-005's enumerated list. The mapping logic in Plan 03-01 handled it correctly; this just adds an empirical data point that the raw enum has more values than docs cover. **No new papercut filed** — PC-005 already calls out the enum-coverage gap.
- **Token redaction held.** No bytes from the access token leaked into stdout, stderr, or any captured SMOKE.md text. The `retryablehttp.Logger: nil` setting from Phase 1 holds.

## Phase 3 completion recommendation

**Phase 3 is ready to mark complete.** No follow-up plan 03-03 needed. Specifically:

- All Phase 3 success criteria are met (SC#1 / SC#2 / SC#3 — see CONTEXT.md and ROADMAP.md).
- The `--watch` work is explicitly out of scope (removed from project on 2026-05-14; tracked in CL-005 for production CLI build).
- The big "CLI fidelity" learning (CL-008) is a production-CLI-build problem, not a Phase 3 blocker. It's a known prototype limitation we surfaced *intentionally* by exercising the smoke; the project's whole purpose is to surface learnings like this.
- All artifacts (SMOKE.md, API-PAPERCUTS.md, CLI-LEARNINGS.md, Confluence) are committed and consistent.

## Files modified

- `.planning/phases/03-status-watch/03-SMOKE.md` (new)
- `.planning/phases/03-status-watch/03-02-SUMMARY.md` (new; this file)
- `.planning/API-PAPERCUTS.md` (PC-019 + index + counters)
- `.planning/CLI-LEARNINGS.md` (CL-008..CL-012 + index + counters)
- Confluence page `4875452435` (v3 → v4; merged Phase 3 confirmation into existing entry #1)
