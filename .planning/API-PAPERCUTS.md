# API Papercuts: gonfalon `automated-releases`

> Living document of known gaps, awkward shapes, and missing features in the upstream
> API. Each active workaround in code carries a `// PAPERCUT: PC-NNN` comment that
> cross-references the anchor below. When the API team resolves an item, move its
> entry to `## Resolved` with a date and delete the workaround in the same PR.

**Last updated:** 2026-05-14
Active count: 21
Resolved count: 0

**End-of-milestone review completed: 2026-05-14.** This doc is now the milestone v1.0's API-team-facing deliverable per DOC-03. All 21 active entries have been reviewed: every entry has all 7 template fields, every `// PAPERCUT: PC-NNN` source-code annotation has been grep-verified to point at live Go code (12 anchors across 6 files: `cmd/flags/rollouts/{start,dismiss}.go`, `internal/rollouts/{client,instructions,models,status_mapping,start,stop,dismiss}.go`), and no entries were upstream-resolved during the milestone. PC-003 was amended with a Phase 4 empirical update noting the server cap of `limit=100`.

Seeded during the **Phase 1: List foundation** milestone (`ldcli flags rollouts-beta list`).
The catalog is derived from the architecture research in `.planning/research/ARCHITECTURE.md`
(papercuts P1..P16). Later phases will append new entries here as they're encountered.

## Active Index

| Anchor | One-line                                                                       | Discovered | Affected commands       |
| ------ | ------------------------------------------------------------------------------ | ---------- | ----------------------- |
| PC-001 | Start mutation returns updated FeatureFlag, not new AutomatedRelease           | 2026-05-11 | start (Phase 2)         |
| PC-002 | `filter` accepts array but only honors element [0]                             | 2026-05-11 | list                    |
| PC-003 | No pagination on list endpoint (limit only, no offset/cursor)                  | 2026-05-11 | list                    |
| PC-004 | GET-by-ID requires environment in path despite globally-unique UUID            | 2026-05-11 | status (Phase 3)        |
| PC-005 | Status enum mixes lifecycle + action-required + meta states                    | 2026-05-11 | list, status            |
| PC-006 | `waiting` status semantics undocumented                                        | 2026-05-11 | status (Phase 3), watch |
| PC-007 | `dismiss_regression` returns 204 instead of new state                          | 2026-05-11 | dismiss (Phase 4)       |
| PC-008 | No dedicated preflight validation endpoint                                     | 2026-05-11 | start (Phase 2)         |
| PC-009 | RBAC errors don't name the missing action                                      | 2026-05-11 | all                     |
| PC-010 | Metric monitoring preferences in parallel side-car map                         | 2026-05-11 | start (Phase 2)         |
| PC-011 | `/internal/` URL prefix is access-control-irrelevant                           | 2026-05-11 | observability           |
| PC-012 | `kind` vs `releaseKind` vs `rolloutType` terminology mismatch                  | 2026-05-11 | start (Phase 2)         |
| PC-013 | `controlVariationId` (legacy) -> `originalVariationId` (unified) inconsistency | 2026-05-11 | start, status           |
| PC-014 | Stage durations only as int64 millis (no Go-style duration string)             | 2026-05-11 | start (Phase 2), status |
| PC-015 | No documented status enum transitions (state machine implicit)                 | 2026-05-11 | watch (Phase 3)         |
| PC-016 | `recommended-duration` requires `finalStageAllocation` even for progressive    | 2026-05-11 | start (Phase 2)         |
| PC-017 | `startAutomatedRelease` does not support guarded releases on staging           | 2026-05-13 | start (Phase 2)         |
| PC-018 | Non-existent variation UUID in start instruction returns 500 instead of 400    | 2026-05-13 | start (Phase 2)         |
| PC-019 | Rollout response returns `environmentId` (opaque), not `environmentKey`        | 2026-05-14 | status (Phase 3), list  |
| PC-020 | `probabilityOfMismatch` is Sample-Ratio-Mismatch and lives in wrong endpoint   | 2026-05-14 | status (Phase 3)        |
| PC-021 | `Status.Kind` taxonomy omits `"regressed"`; regression hidden in `status.label` | 2026-05-14 | dismiss-regression (Phase 4) |

## Entries

### PC-001 - Start mutation returns wrong resource

**Title:** `startAutomatedRelease` returns updated FeatureFlag, not new AutomatedRelease
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** `PATCH /api/v2/flags/{p}/{flagKey}` with a `startAutomatedRelease` instruction returns the standard updated `FeatureFlag` resource. The newly-created AutomatedRelease ID is nowhere in the response body or in any response header.
**CLI workaround:** After every `start` call, issue a follow-up `GET /internal/projects/{p}/flags/{flagKey}/automated-releases?filter=environmentKey:{env}&limit=1` and return the first item. Doubles round-trips for the most common mutation.
**What we'd prefer:** Return `{ flag: FeatureFlag, automatedRelease: AutomatedRelease }` for the `startAutomatedRelease` and `stopAutomatedRelease` instruction kinds, OR add an `X-LD-AutomatedReleaseId` response header.
**Status:** active (no Phase 1 source-code annotation; surfaces in Phase 2 `start` implementation)
**Removal criteria:** API returns rollout ID either in body or header; CLI integration test confirms; the follow-up GET in `RolloutsClient.Start` is deleted.

### PC-002 - `filter` array drops elements beyond [0]

**Title:** `GET .../automated-releases?filter=...` accepts a string array but only honors element [0]
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** `GET .../automated-releases` accepts `filter` as a string array (per OpenAPI) but explicitly takes only the first element internally (`internal_get_automated_releases.go:66`: `(*request.Params.Filter)[0]`). Extra filters are silently dropped. Filter syntax: comma-separated `field:value` pairs with supported fields `environmentKey`, `status`, `kind`.
**CLI workaround:** Send exactly one filter element (`filter=environmentKey:{env}`); document that intuitive AND-style filtering is not supported. Code annotation lives at `internal/rollouts/client.go` `List` method, above `q.Set("filter", ...)`.
**What we'd prefer:** Reject when `len(filter) > 1`, OR honor multiple elements, OR switch to discrete query params (`environmentKey=`, `status=`, `kind=`).
**Status:** active (`// PAPERCUT: PC-002` in `internal/rollouts/client.go`)
**Removal criteria:** API switches to discrete query params or properly honors multi-element filter arrays; CLI converts back to natural filter expressions.

### PC-003 - No pagination on list endpoint

**Title:** `GET .../automated-releases` supports `limit` but no offset/cursor/pageToken
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** The list endpoint exposes only `limit`. Once a flag has more than the default 20 historical rollouts, callers cannot fetch older ones without setting `limit` to a large value (and hoping it isn't capped server-side). **Phase 4 empirical update (2026-05-14, 04-SMOKE.md secondary findings):** the server now caps `limit` at 100 (rejects larger values with `bad_request: "Limit must be less than or equal to 100"`); the CLI's `--all` flag currently requests `limit=1000` and therefore returns a `bad_request` envelope on real staging.
**CLI workaround:** `--all` issues a single request with `limit=1000`. The list command's runE compares `len(items)` against the requested limit; when equal, the envelope's `meta.warnings` is decorated with a hint pointing at this papercut: `"List returned exactly N items; results may be truncated upstream (see API-PAPERCUTS.md PC-003)"`. Source anchor at `cmd/flags/rollouts/list.go` and `internal/rollouts/client.go`. **Phase 4 follow-up needed:** lower the `--all` request to `limit=100` (or whatever the server reports as max via an introspection call if one exists), so `--all` stops returning a `bad_request` against current staging. Tracked as a CLI follow-up; the underlying pagination gap is the same as originally captured.
**What we'd prefer:** Add a `_links.next` cursor following the standard LD pagination pattern; the CLI's `--all` becomes a transparent multi-call fetch.
**Status:** active (`// PAPERCUT: PC-003` in `internal/rollouts/client.go`; `PC-003` reference in `cmd/flags/rollouts/list.go` saturation-warning text)
**Removal criteria:** API exposes a pagination cursor; CLI `--all` becomes a multi-call fetch; the saturation warning is deleted; an integration test confirms paging through > 25 rollouts works.

### PC-004 - GET-by-ID requires environment in path

**Title:** `GET .../environments/{environmentKey}/automated-releases/{automatedReleaseId}` requires `environmentKey` despite globally-unique rollout ID
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** The `automatedReleaseId` is a globally unique UUID. The path nevertheless requires `environmentKey` as a separate segment. Callers receiving a rollout ID from a webhook or log line must do an extra lookup to discover the environment.
**CLI workaround:** Phase 1 `Get` is not yet user-facing (Phase 3 wires the `status` verb), but the call site already requires `--environment` alongside the rollout ID. The CLI surfaces this redundancy in help text.
**What we'd prefer:** Add `GET .../automated-releases/{automatedReleaseId}` (project-scoped or account-scoped). Keep the env-scoped path for cache-invalidation use cases.
**Status:** active (`// PAPERCUT: PC-004` at the Phase 1 call site; surfaces in Phase 3 user-facing `status`)
**Removal criteria:** API exposes a non-env-scoped GET-by-ID; CLI `status` verb stops requiring `--environment`.

### PC-005 - Status enum mixes lifecycle + action-required + meta states

**Title:** A single `status` enum encodes lifecycle (`not_started`, `in_progress`, terminal), action-required (`monitoring_regressed`), and meta states (`waiting`, `archived`)
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** A single `status` enum mixes three orthogonal axes. Consumers (CLI, UI, agents) must hardcode which status values mean "still going", "stop and ask the human", or "done".
**CLI workaround:** `internal/rollouts/status_mapping.go` `mapStatusToKind` collapses the 13 documented raw statuses into 5 lifecycle buckets (`active`, `regressed`, `reverted`, `paused`, `completed`). The mapping also produces human-readable labels with reason info inline (D-03: no separate `Reason` field). Source anchor: `internal/rollouts/status_mapping.go` doc comment on `mapStatusToKind`.
**What we'd prefer:** Either split into `phase` (active/terminal) + `attention` (ok|action_required) + `outcome` (success|failure|null), OR document the status categories in the OpenAPI `x-extensions`.
**Status:** active (`// PAPERCUT: PC-005` in `internal/rollouts/status_mapping.go`)
**Removal criteria:** API returns the status as a structured object with the three axes, or documents the partition; CLI status-mapping table is removed and consumers branch directly on the structured fields.

### PC-006 - `waiting` status semantics undocumented

**Title:** The enum includes `waiting` but the README and transformations file don't explain when it fires
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** `waiting` is one of the 13 documented status values, but there is no documentation explaining when the API server returns it (post-stage cooldown? pre-start grace? deferred start?).
**CLI workaround:** Treat `waiting` as the `paused` lifecycle bucket in `mapStatusToKind`; surface "Waiting" in the human-readable label until empirical observation clarifies the trigger.
**What we'd prefer:** Document the trigger condition (e.g., "fires when the rollout is scheduled but not yet started; transitions to `in_progress` at startedAt") OR remove the value if it's unreachable.
**Status:** active (no source-code annotation in Phase 1; will surface in Phase 3 `status` and `watch` once we observe the trigger condition empirically)
**Removal criteria:** API team documents the `waiting` semantics OR removes the value; CLI status-mapping comment is updated to reflect the documented meaning.

### PC-007 - `dismiss_regression` returns 204 instead of new state

**Title:** `PATCH .../metric-states/{mk}` returns 204 No Content with a self-described "Hack" comment
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** `PATCH .../metric-states/{mk}` (the dismissal endpoint) returns 204 No Content. The API server-side code comment (`internal_patch_measured_rollout_metric_state.go:54`) acknowledges: "Hack: We should return the new status. For now, we are not." Callers must re-GET the release to confirm dismissal landed.
**CLI workaround:** Phase 4 `dismiss` verb will issue a follow-up `GET` immediately after the dismissal succeeds; the typed Rollout in the envelope reflects the post-dismissal state.
**What we'd prefer:** Return the updated `AutomatedRelease` (or just the affected `metricConfigurations[i]`) with status 200.
**Status:** active (no Phase 1 source-code annotation; surfaces in Phase 4 `dismiss`)
**Removal criteria:** API returns the updated resource on dismissal; CLI eliminates the follow-up GET.

### PC-008 - No dedicated preflight validation endpoint

**Title:** Server-side validation (metrics, randomization unit, stage shape) only runs inside `startAutomatedRelease`
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** There is no `POST .../automated-releases:validate` or equivalent. The CLI's `--skip-health-checks` flag has no clean inverse: "run validation but don't start". Callers either start-and-fail (and must clean up partial state) or piggyback on the `recommended-duration` endpoint (which also computes a duration we don't need just to validate inputs).
**CLI workaround:** Phase 2 `start` verb will offer `--dry-run` that calls `recommended-duration` as a stand-in validator, accepting the awkward shape per PC-016.
**What we'd prefer:** Add `POST /internal/projects/{p}/flags/{fk}/automated-releases:validate` that accepts the same body as the start instruction and returns `{ valid: true } | { valid: false, errors: [...] }`.
**Status:** active (no Phase 1 source-code annotation; surfaces in Phase 2 `start`)
**Removal criteria:** API exposes a dedicated validation endpoint; CLI `start --dry-run` routes through it directly.

### PC-009 - RBAC errors don't name the missing action

**Title:** When the caller's role lacks `updateRulesWithMeasuredRollout` (start) or `stopMeasuredRolloutOnFlagRule` (stop), the 403 response just says "Access denied"
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** A 403 response carries a generic message without naming the specific action identifier the caller is missing. Custom-role debuggers must go fishing through the action list to figure out which permission to add.
**CLI workaround:** `errors.go` `mapAPIError` 403 branch surfaces `ErrCodeForbidden` with `NextAction = "Verify your access token's role includes the required permission/scope on the target project"`. The next-action text cannot name the specific action because the API response doesn't include it.
**What we'd prefer:** Include the failing `actionIdentifier` in the 403 body, so the CLI can surface the exact missing permission name.
**Status:** active (no Phase 1 source-code annotation; the generic 403 text in `errors.go` is the current best effort)
**Removal criteria:** API 403 body includes the failing action; CLI `NextAction` text incorporates it verbatim.

### PC-010 - Metric monitoring preferences in parallel side-car map

**Title:** `metrics: [{key, isGroup}]` carries no per-metric configuration; `metricMonitoringPreferences: {<key>: {autoRollback}}` is a separate map
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** Two parallel collections must stay in sync. Setting `autoRollback` for a metric you didn't include in `metrics` is silently ignored. Callers easily diverge the two collections during refactors.
**CLI workaround:** Phase 2 `start` will reconcile the two during input parsing; passing `--auto-rollback metric-key` adds the metric to BOTH collections atomically.
**What we'd prefer:** Inline the configuration into the metric source: `metrics: [{key, isGroup, autoRollback?}]`. The side-car map becomes redundant.
**Status:** active (no Phase 1 source-code annotation; surfaces in Phase 2 `start`)
**Removal criteria:** API consolidates the two collections; CLI flattens to a single argument shape.

### PC-011 - `/internal/` URL prefix is access-control-irrelevant

**Title:** The `/internal/...` paths look private but `EhttpWithSessionOrToken` accepts ordinary account tokens
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** The `/internal/...` path prefix is purely an "endpoint maturity" signal — it does not restrict access beyond what `viewProject` already enforces. First-time integrators see the prefix and assume the endpoints will be removed or require special auth; neither is true.
**CLI workaround:** `internal/rollouts/client.go` builds the URL with the `/internal/` prefix in both `List` and `Get`; both call sites carry a `// PAPERCUT: PC-011` anchor above the `path := fmt.Sprintf(...)` line.
**What we'd prefer:** Rename to `/api/v2/projects/{p}/flags/{fk}/automated-releases` once stable, OR document explicitly that the `/internal/` prefix is for endpoint maturity, not access control.
**Status:** active (`// PAPERCUT: PC-011` in `internal/rollouts/client.go` at both `List` and `Get`)
**Removal criteria:** API renames the path to `/api/v2/...`; CLI updates the URL builder; integration test verifies new path works.

### PC-012 - Mismatched terminology: `kind` vs `releaseKind` vs `rolloutType`

**Title:** The same concept ("guarded" vs "progressive") is called four different things across the API surface
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** `releaseKind` in the `startAutomatedRelease` instruction body; `kind` in the `AutomatedRelease` response; `rolloutType` in legacy `MeasuredRollout` / `MeasuredRolloutDesign` responses; `kind` in the list-filter query param. Easy to typo; hard to write generic helpers.
**CLI workaround:** `internal/rollouts/models.go` standardizes on `kind` on the CLI-facing struct; the converter (`raw.toRollout`) maps from whichever name the response uses. Phase 2 `start` will accept `--release-kind` as the CLI flag (matching the instruction body field) but emit `kind` in the envelope.
**What we'd prefer:** Standardize on `kind` everywhere on the API side, including instruction bodies and legacy endpoints.
**Status:** active (no Phase 1 source-code annotation; surfaces in Phase 2 `start`)
**Removal criteria:** API unifies on `kind`; CLI flag is renamed to `--kind` (with `--release-kind` as a hidden alias for one release cycle).

### PC-013 - `controlVariationId` (legacy) -> `originalVariationId` (unified) renamed mid-stream

**Title:** Legacy `MeasuredRolloutDesign.controlVariationId` was renamed to `AutomatedRelease.originalVariationId`; same for `treatmentVariationId` -> `targetVariationId`
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** The instruction body uses the new names; diagnostic responses and exemplar errors still reference the legacy field names. CLI output may switch terminology depending on which endpoint produced the data.
**CLI workaround:** `internal/rollouts/models.go` raw DTO uses the unified `originalVariationId` / `targetVariationId` only; the converter does not need a legacy-name fallback path until we observe one in a fixture. Source anchor: `// PAPERCUT: PC-013` on `rawRollout.Kind` (a related rename per P12) and on the converter's variation-ID fields.
**What we'd prefer:** Pick one name (suggest the unified pair) and rename throughout — including in returned events, diagnostics, and legacy endpoints.
**Status:** active (`// PAPERCUT: PC-013` in `internal/rollouts/models.go`)
**Removal criteria:** API unifies the names everywhere; CLI converter strips the (currently unused) legacy-name fallback hook.

### PC-014 - Stage durations are int64 millis; humans want `1h30m`

**Title:** `durationMillis` is the only stage-duration field exposed by the API
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** Stage durations come back as `int64` unix-millis only. Every caller (CLI, UI, docs) does its own millis-to-human conversion; off-by-one in unit conversion is a real risk.
**CLI workaround:** `internal/rollouts/models.go` `toStage` converter computes `Duration = (time.Duration(DurationMillis) * time.Millisecond).String()` so the CLI envelope carries both forms (per AGENT-04). Source anchor: `// PAPERCUT: PC-014` above the conversion line in `toStage`.
**What we'd prefer:** Accept and return `duration: "1h30m"` (Go-style) alongside `durationMillis`. CLI input parsing already accepts both shapes, so symmetry would simplify the round-trip.
**Status:** active (`// PAPERCUT: PC-014` in `internal/rollouts/models.go` `toStage`)
**Removal criteria:** API accepts and returns a `duration` string field; CLI converter prefers it when present and falls back to millis only for backwards compatibility.

### PC-015 - No documented status enum transitions

**Title:** The status enum lists values but doesn't say which transitions are legal
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** Documentation lists the enum values but does not describe the state machine. (Can `monitoring_regressed` go back to `in_progress` after dismissal? Can `paused` reach `completed` without passing through `in_progress`?) The comment in the source file links to a Confluence page; that's not enough for CLI implementers writing watch loops.
**CLI workaround:** Phase 3 `watch` will be conservative — treat any transition as legal but flag unexpected ones in the human-readable label. Tests will discover the state machine empirically.
**What we'd prefer:** Add an `x-state-transitions` OpenAPI extension, or document inline.
**Status:** active (no Phase 1 source-code annotation; surfaces in Phase 3 `watch`)
**Removal criteria:** API documents the legal transitions; CLI `watch` uses the documented graph instead of the empirical permissive model.

### PC-016 - `recommended-duration` requires `finalStageAllocation` even for progressive

**Title:** Progressive rollouts don't have a "final stage allocation" in the same sense, yet `finalStageAllocation` is required by the `recommended-duration` endpoint
**Discovered:** 2026-05-11 (architecture research; Phase 1 milestone)
**API behavior:** Progressive rollouts' final stage allocation is the rollout completion itself (i.e., 100% of traffic on the target variation). The required `finalStageAllocation` parameter is awkward for the progressive case — callers must pass an essentially meaningless `100000` (or 100%).
**CLI workaround:** Phase 2 `start --dry-run` (and any caller of `recommended-duration` for progressive rollouts) will pass `100000` and document the workaround in CLI help text.
**What we'd prefer:** Make `finalStageAllocation` optional for progressive rollouts; OR split the endpoint per kind.
**Status:** active (no Phase 1 source-code annotation; surfaces in Phase 2 `start --dry-run`)
**Removal criteria:** API makes `finalStageAllocation` optional or splits the endpoint; CLI removes the dummy-value workaround.

### PC-017 - `startAutomatedRelease` does not support guarded releases on staging

**Title:** `startAutomatedRelease` instruction rejects `releaseKind: "guarded"` on the `alex-engelberg-dev` staging account
**Discovered:** 2026-05-13 (Phase 2 smoke test B)
**API behavior:** When `startAutomatedRelease` is submitted with `releaseKind: "guarded"`, the server returns HTTP 400 with message: `"instruction kind startAutomatedRelease is not enabled for guarded releases"`. Progressive rollouts work correctly.
**CLI workaround:** The CLI correctly propagates this error via `mapAPIError` → `bad_request` code (D-08 fallthrough for unrecognized policy messages). No dedicated `ErrCode` added. Operators see the server's message verbatim.
**What we'd prefer:** Either enable guarded rollouts uniformly for accounts with the `automated-releases` beta enabled, OR document that guarded rollouts require a separate feature gate.
**Status:** active (no source-code annotation; falls through to `bad_request` per D-08; CLI behavior is correct)
**Removal criteria:** API enables guarded rollouts via `startAutomatedRelease`; Smoke B re-run confirms `data.kind = "guarded"`.

### PC-018 - Non-existent variation UUID in start instruction returns 500 instead of 400

**Title:** Passing a UUID-format string that is not a real variation ID causes server 500
**Discovered:** 2026-05-13 (Phase 2 smoke test E)
**API behavior:** When `targetVariationId` is a UUID-shaped string not matching any variation in the flag (e.g., `00000000-0000-0000-0000-000000000000`), the server returns HTTP 500 Internal Server Error. The expected behavior is HTTP 400 with `"originalVariationId must be a valid variation id '<value>'"`.
**CLI workaround:** The CLI maps the 500 to `ErrCodeUpstreamUnavailable` per the Phase 1 5xx branch — the user sees `"LaunchDarkly returned 500 Internal Server Error"`. This is a degraded experience compared to the `ErrCodeInvalidVariation` code that would fire on a 400.
**What we'd prefer:** The server should return HTTP 400 with a descriptive error message for any UUID-shaped input that does not match a flag variation, rather than panicking.
**Status:** active (CLI correctly handles 500 but cannot surface `invalid_variation` code for this case)
**Removal criteria:** API returns HTTP 400 with a descriptive message for non-existent variation UUIDs; Smoke E re-run confirms `error.code = "invalid_variation"`.

### PC-019 - Rollout response returns `environmentId` (opaque), not `environmentKey`

**Title:** `automated-releases` GET (and List → items[]) surface `environmentId` (24-char hex ObjectId) on the rollout payload, with no `environmentKey` field
**Discovered:** 2026-05-14 (Phase 3 smoke test A; verified via raw curl)
**API behavior:** A `GET /internal/projects/{projKey}/environments/{envKey}/automated-releases/{id}` request returns a rollout body whose only environment-identifier field is `"environmentId": "64e3e188a9dedd13411006f8"` — no `environmentKey` is present. The env key is encoded in `_links.self.href` (`.../environments/test/automated-releases/...`), so a consumer can recover it by string-parsing the link, but there is no first-class field on the resource. List → items[] has the same shape.
**CLI workaround:** None in Phase 3 — the CLI passes `envKey` into the request URL (path parameter) but renders the API's `environmentId` verbatim in the JSON envelope. The plaintext renderer surfaces `Env: —` when the operator didn't pass `--environment` because the renderer has no env key to display (see CLI-LEARNINGS CL-009). No `// PAPERCUT: PC-019` annotation added; the gap is purely on the read side.
**What we'd prefer:** Either include `environmentKey` alongside `environmentId` on every rollout payload, or drop `environmentId` in favor of `environmentKey` (the operator-meaningful identifier — every other `automated-releases` URL path uses `envKey`, not `envId`). Consistency with the path-parameter convention would be more useful than the opaque ObjectId echo.
**Status:** active (no CLI workaround required; the gap surfaces as a plaintext rendering limitation tracked in CLI-LEARNINGS CL-009)
**Removal criteria:** API response includes `environmentKey` as a first-class field; Phase 3 plaintext renderer can populate the `Env:` line from the wire payload without parsing `_links.self.href`.

### PC-020 - `probabilityOfMismatch` is Sample-Ratio-Mismatch and lives in wrong endpoint

**Title:** Per-metric `metric-results` response includes a `probabilityOfMismatch` field that (a) is actually rollout-level (identical across every metric) and (b) is misnamed — it pertains specifically to Sample Ratio Mismatch (SRM), an edge case usually not relevant to operators
**Discovered:** 2026-05-14 (Phase 3 status enhancement — plaintext review surfaced that every metric reported the same value; user pointed out the SRM semantics)
**API behavior:** `GET /internal/projects/{p}/flags/{f}/environments/{e}/automated-releases/{id}/metric-results/{metricKey}` includes a top-level `probabilityOfMismatch` field on every metric's response. Empirically (verified by curling all three metrics on `eb858e8b-cb92-474b-9666-5b82ac8dcdb5`) the value is identical for every metric on the same rollout — it is a *rollout-level* signal, not a per-metric one. Worse, the field name reads as "probability of metric mismatch" (which would be useful and per-metric) when it actually means "probability of Sample Ratio Mismatch" — i.e. the probability that the control vs treatment user allocation diverged from the configured split. SRM is a rollout-health-monitoring edge case (matters when there's a bug in randomization), not a routine status signal an operator wants to read on every status call.
**CLI workaround:** `internal/rollouts/client.go:GetMetricResult` returns the value as a separate return — `(*MetricResult, *float64, error)` — and `cmd/flags/rollouts/status.go:fetchMetricResults` takes the first non-nil observation and lifts it to `Rollout.ProbabilityOfMismatch` for JSON consumers. The plaintext renderer intentionally does NOT surface it (per Phase 3 user feedback 2026-05-14) — JSON-mode consumers can branch on the field if they care about SRM, but human-readable plaintext stays focused on the actionable signals (per-metric control/treatment/difference). The public `MetricResult` type intentionally omits the field so it can't be confused as per-metric.
**What we'd prefer:** (1) Rename the field to `probabilityOfSampleRatioMismatch` (or `probabilityOfSRM`) so the SRM semantics is unambiguous. (2) Move it to the rollout-level GET endpoint (`/internal/projects/{p}/environments/{e}/automated-releases/{id}`) where it semantically belongs. (3) Drop it from per-metric responses, or document explicitly that it is replicated. Replicated per-metric is wire waste and invites consumers to render the same number N times under a misleading name.
**Status:** active (CLI lifts to rollout root and omits from plaintext; SRM nuance documented above)
**Removal criteria:** API renames the field to make SRM semantics explicit AND moves it to the rollout-level GET endpoint; CLI's `fetchMetricResults` no longer needs the `*float64` second return.

### PC-021 - `Status.Kind` taxonomy omits `"regressed"`; regression hidden in `status.label`

**Title:** Guarded rollouts that have hit a regression surface as `status.kind == "paused"` (not `"regressed"`); the regression signal is encoded in `status.label`
**Discovered:** 2026-05-14 (Phase 4 dismiss-regression smoke; see `.planning/phases/04-stop-dismiss-finalize-papercuts/04-SMOKE.md` Smoke D + history-sweep observations)
**API behavior:** When a guarded rollout's monitor detects a regression, the upstream response shape is:
```json
"status": {
  "status": "monitoring_stopped",
  "kind": "paused",
  "label": "the default rule paused at 50%: regressions detected for rg-simulator-errors"
}
```
There is no `Status.Kind == "regressed"` enum value visible in any observed response. Across 12 rollouts in 5 flags surveyed during the Phase 4 smoke, the kinds seen were `{paused, reverted, completed, active}` — `"regressed"` never appeared. The `events[]` array does contain a `regression_detected` event with the offending `metricKey`, but the top-level Kind classifier collapses regression-paused, manually-paused, and other paused-reasons into a single `paused` bucket.
**CLI workaround:** The dismiss-regression command's pre-read in `cmd/flags/rollouts/dismiss.go` gates on `current.Status.Kind != "regressed"` — under this taxonomy that gate rejects every real regression scenario observed on staging. Workaround for the prototype: leave the gate as-is and document the gap. Source anchor at `cmd/flags/rollouts/dismiss.go` (the no-active-regression check) with `// PAPERCUT: PC-021` annotation.
**What we'd prefer:** (a) Add `"regressed"` (or `"paused_regressed"`) as a first-class `Status.Kind` value, OR (b) add an explicit `data.activeRegression: bool` (or `data.dismissibleRegression: bool`) predicate to the rollout payload. Either lets downstream consumers detect the dismissible state without substring-parsing `label` or scanning `events[]`.
**Status:** active (`// PAPERCUT: PC-021` in `cmd/flags/rollouts/dismiss.go`)
**Removal criteria:** Upstream exposes a stable predicate for "is this rollout currently in an unresolved regression?"; CLI dismiss pre-read reads from that predicate; Phase 4 dismiss smoke can be re-run with a real regressed fixture and exercise the polling-budget path + PC-007 timeout warning.

## Resolved

*(empty)*
