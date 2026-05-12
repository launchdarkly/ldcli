# API Papercuts: gonfalon `automated-releases`

> Living document of known gaps, awkward shapes, and missing features in the upstream
> API. Each active workaround in code carries a `// PAPERCUT: PC-NNN` comment that
> cross-references the anchor below. When the API team resolves an item, move its
> entry to `## Resolved` with a date and delete the workaround in the same PR.

**Last updated:** 2026-05-12
Active count: 16
Resolved count: 0

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
**API behavior:** The list endpoint exposes only `limit`. Once a flag has more than the default 20 historical rollouts, callers cannot fetch older ones without setting `limit` to a large value (and hoping it isn't capped server-side).
**CLI workaround:** `--all` issues a single request with `limit=1000`. The list command's runE compares `len(items)` against the requested limit; when equal, the envelope's `meta.warnings` is decorated with a hint pointing at this papercut: `"List returned exactly N items; results may be truncated upstream (see API-PAPERCUTS.md PC-003)"`. Source anchor at `cmd/flags/rollouts/list.go` and `internal/rollouts/client.go`.
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

## Resolved

*(empty)*
