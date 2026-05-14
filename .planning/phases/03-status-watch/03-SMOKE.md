# Phase 3 — Real-Staging Smoke Tests (03-01)

Last updated: 2026-05-14

Validates that `./ldcli flags rollouts-beta status` talks to real LaunchDarkly
staging end-to-end after the four 03-01 commits landed on `ae/cli-gr`
(`c273882`, `8c0587e`, `0985984`, `05de336`). Mirrors the structure of
`01-SMOKE.md` and `02-SMOKE.md`.

## Environment

- **Base URI:** `https://ld-stg.launchdarkly.com` (LaunchDarkly staging)
- **Access token:** Writer-scoped staging token (loaded via `~/.config/ldcli/config.yml`;
  literal token bytes redacted from every captured Command block below — the token
  matches `~/secret/ld-staging-token`).
- **Binary:** `./ldcli` built from this branch (`make build` on commit `05de336`).
- **Project:** `alex-engelberg-dev` (carried forward from Phase 1 + Phase 2 staging
  fixtures).
- **Environment:** `test`.
- **Flag fixtures (reused from Phase 1):**
  - `ldcli-blitz-2-guarded-rollouts` — has a real, paused guarded rollout from
    2026-05-13 (rg-simulator-errors regression).
  - `ldcli-blitz-1-no-rollout` — empty flag, no rollouts. Used for the
    `no_rollouts_found` path.

## Smoke A — most-recent / happy path

**Command:**
```
./ldcli flags rollouts-beta status \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-2-guarded-rollouts \
  --output json
```
(access token + base URI sourced from `~/.config/ldcli/config.yml`; both
redacted from this log.)

**Exit code:** 0

**Stdout (full envelope):**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Rollout",
  "data": {
    "id": "eb858e8b-cb92-474b-9666-5b82ac8dcdb5",
    "flagKey": "ldcli-blitz-2-guarded-rollouts",
    "kind": "guarded",
    "environmentId": "[REDACTED]",
    "originalVariationId": "62f05f48-2e90-42d1-a56c-b1e89f70e027",
    "targetVariationId": "cebc44a4-ce02-4975-b121-398cd99496ec",
    "randomizationUnit": "user",
    "ruleIdOrFallthrough": "fallthrough",
    "status": {
      "status": "monitoring_stopped",
      "kind": "paused",
      "label": "the default rule paused at 50%: regressions detected for rg-simulator-errors"
    },
    "createdAt": "2026-05-13T19:39:12.042Z",
    "startedAt": "2026-05-13T19:39:12.227Z",
    "endedAt": "2026-05-14T01:40:03.47Z",
    "latestStageIndex": 0,
    "extensionDurationMillis": 21600000,
    "stages": [
      {
        "stageIndex": 0,
        "allocation": 50000,
        "durationMillis": 21600000,
        "duration": "6h0m0s",
        "startedAt": "2026-05-13T19:39:12.227Z",
        "safeRollForward": true
      }
    ],
    "events": [
      {"kind": "stage_started",                   "createdAt": "2026-05-13T19:39:12.227Z"},
      {"kind": "safe_roll_forward",               "createdAt": "2026-05-13T19:40:06.307Z"},
      {"kind": "regression_detected",             "createdAt": "2026-05-13T19:40:16.309Z", "metricKey": "rg-simulator-errors"},
      {"kind": "minimum_monitoring_window_expired","createdAt": "2026-05-14T01:40:03.47Z"}
    ],
    "metricConfigurations": [
      {"metricKey": "rg-simulator-latency",            "minSampleSize": 10, "status": "ok"},
      {"metricKey": "rg-simulator-errors",             "minSampleSize": 10, "status": "regressed"},
      {"metricKey": "rg-simulator-latency-percentile", "minSampleSize": 10, "status": "ok"}
    ],
    "_links": {
      "self": {
        "href": "/internal/projects/alex-engelberg-dev/environments/test/automated-releases/eb858e8b-cb92-474b-9666-5b82ac8dcdb5",
        "type": "application/json"
      }
    }
  },
  "meta": {
    "fetchedAt": "2026-05-14T18:53:18.959694Z"
  }
}
```

**Stderr:** (empty)

**Verdict:** ✅ Envelope shape exactly matches D-05: top-level `schemaVersion` +
`kind: "Rollout"` + `data` + `meta.fetchedAt`. `data.status` carries all three
fields (`status` raw upstream value, `kind` derived enum, `label` human string)
per D-02 / D-06. Phase 1's existing `Client.List → items[0]` + Phase 1's
`mapAPIError` were reused verbatim — no Phase 3-specific code paths were hit.

**Contract observations:**
- `data.status.kind == "paused"` and `data.status.status == "monitoring_stopped"`
  exercise the active/regressed/reverted/paused/completed mapping introduced
  in Phase 1 D-02 — the "monitoring_stopped" raw value isn't in
  PC-005's enumerated list, so we now have empirical proof the raw enum has
  more values than the original Phase 1 capture noted. **No new papercut** — PC-005
  already calls out the enum-coverage gap; this smoke just adds a value
  (`monitoring_stopped`) to the in-the-wild list.
- **CLI fidelity surprise**: the CLI's envelope omits `metricConfigurations[].autoRollback`
  and `metricConfigurations[].differenceEstimateType`, even though raw curl
  against the same endpoint **does** return both
  (`{"autoRollback": false, "differenceEstimateType": "absolute", "metricKey": "...", "minSampleSize": 10, "status": "ok"}`).
  Root cause is in the CLI, not the API: `internal/rollouts/models.go` defines
  `MetricConfiguration` with five fields (`metricKey`, `kind`, `minSampleSize`,
  `autoRollback`, `status`), and `autoRollback` uses `json:"autoRollback,omitempty"`
  — so the `false` zero-value is stripped on re-marshal, and `differenceEstimateType`
  is dropped entirely because the struct has no field for it. This violates
  the "JSON passthrough" principle (memory `feedback_json_api_passthrough.md`)
  and is the biggest learning from this smoke run. → **Task 3 — new CL-008.**
- `data.environmentId` is the **opaque environment ID** (24-char hex
  ObjectId), not the **environment key** (`test`). Raw curl confirms the API
  literally does not send `environmentKey` at all (`raw env field: None 64e3e188a9dedd13411006f8`).
  The env key is reachable only by parsing `data._links.self.href`
  (`/internal/.../environments/test/...`) — fragile for consumers. → **Task 3 — new PC-019.**
- `data._links.self.href` confirms the rollout lives under `/internal/...` and
  encodes both `project` + `environment` keys in the path, but **not** the
  rollout-id-only shortcut PC-004 already covers.
- `meta.fetchedAt` is RFC 3339 with microsecond precision (`2026-05-14T18:53:18.959694Z`)
  — consistent with Phase 1 D-04 / AGENT-04.
- `data.stages` has only ONE stage entry (`stageIndex: 0`, `allocation: 50000`).
  The Phase 3 plaintext renderer's reference shape (Stages: 25% / 50% / 75%)
  assumed multi-stage rollouts; this one was configured as a single 50% stage
  that paused on regression. **Not a papercut** — single-stage guarded rollouts
  are a valid configuration. → CLI-LEARNINGS.md candidate.

## Smoke B — specific rollout via `--rollout-id`

**Command:**
```
./ldcli flags rollouts-beta status \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-2-guarded-rollouts \
  --rollout-id eb858e8b-cb92-474b-9666-5b82ac8dcdb5 \
  --environment test \
  --output json
```

**Exit code:** 0

**Stdout (full envelope):** Identical to Smoke A's `data` block (verified via
`diff <(jq .data A-stdout.json) <(jq .data B-stdout.json)` → empty diff). Only
`meta.fetchedAt` differs (later timestamp).

**Stderr:** (empty)

**Verdict:** ✅ `Get(envKey, rolloutID)` returned **identical** `data` to
`List(flagKey, Limit:1)→items[0]` — no eventual-consistency window, no field-
shape divergence between the two API paths. D-04's "most-recent semantics fall
out of Phase 1's existing List sort" is empirically valid.

**Contract observations:**
- `Get` and `List → items[0]` return the same wire shape. No divergence
  papercut to file.
- The CLI-side validation requiring `--environment` when `--rollout-id` is set
  (D-03 / PC-004 surface) worked correctly — the operator was prompted via help
  text and Smoke D below exercises the failure path.

## Smoke C — no rollouts found

**Command:**
```
./ldcli flags rollouts-beta status \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-1-no-rollout \
  --output json
```

**Exit code:** 1

**Stdout:**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "no_rollouts_found",
    "message": "No rollouts found for flag \"ldcli-blitz-1-no-rollout\"",
    "nextAction": "Verify the flag has at least one rollout, or pass --rollout-id <id> --environment <env> to address a specific rollout"
  }
}
```

**Stderr:** `rollouts status failed`

**Verdict:** ✅ Exit 1 + `kind: "Error"` envelope on **stdout** (per AGENT-04 / D-09).
The new `ErrCodeNoRolloutsFound = "no_rollouts_found"` constant added in 03-01
Task 1 is end-to-end wired and the empty-list detection branch in
`status.go:resolveRollout` triggered correctly. `data.status.kind` semantics
are **not** exercised in this path (envelope is the Error shape, not the Rollout shape).

**Contract observations:**
- Raw curl against the same `automated-releases?limit=1` endpoint for the
  rollout-less flag returns `{"_links": {...}, "items": []}` — empty array,
  not null. Wire shape is conventional. **No new papercut.**
- The stderr line `rollouts status failed` is from Cobra's `SilenceErrors:
  false` path on root command — the error message + nextAction live in the
  JSON envelope on stdout. Phase 1's D-01 single-exit-code-on-error contract holds.

## Smoke D — validation error (`--rollout-id` without `--environment`)

**Command:**
```
./ldcli flags rollouts-beta status \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-2-guarded-rollouts \
  --rollout-id eb858e8b-cb92-474b-9666-5b82ac8dcdb5 \
  --output json
```
(note: `--environment` deliberately omitted)

**Exit code:** 1

**Stdout:**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "bad_request",
    "message": "--environment is required when --rollout-id is set (API-PAPERCUTS.md PC-004 — GET-by-ID requires environmentKey in the URL path)",
    "nextAction": "Pass --environment alongside --rollout-id, or omit --rollout-id to get the most-recent rollout on the flag"
  }
}
```

**Stderr:** `rollouts status failed`

**Verdict:** ✅ Exit 1, error envelope on **stdout**, CLI-side validation
fired **before** any API call (the message references PC-004 directly, and
the failure happened instantly — clearly no HTTP round-trip occurred).
D-03's "validate locally before HTTP" contract holds.

**Contract observations:**
- This is the only error path in Phase 3 that surfaces a CLI workaround text
  (PC-004 reference). Future plaintext rendering of bad_request errors should
  consider whether the PC-004 reference is appropriate for end-users.
  → CLI-LEARNINGS.md candidate.

## Smoke E — plaintext sanity (with `--output plaintext`)

**Command:**
```
./ldcli flags rollouts-beta status \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-2-guarded-rollouts \
  --output plaintext
```

**Exit code:** 0

**Stdout:**
```
Rollout: eb858e8b-cb92-474b-9666-5b82ac8dcdb5
Flag: ldcli-blitz-2-guarded-rollouts            Env: —
Kind: guarded   State: paused
Label: the default rule paused at 50%: regressions detected for rg-simulator-errors
Created: 2026-05-13T19:39:12Z
Started: 2026-05-13T19:39:12Z           Ended: 2026-05-14T01:40:03Z
Target var: cebc44a4-ce02-4975-b121-398cd99496ec              Original var: 62f05f48-2e90-42d1-a56c-b1e89f70e027

Stages:
  [→]  50%  6h0m0s  in progress

Metrics:
  rg-simulator-latency             ok         auto-rollback: false
  rg-simulator-errors              regressed  auto-rollback: false
  rg-simulator-latency-percentile  ok         auto-rollback: false

Events:
  2026-05-13T19:39:12Z  stage_started                      —
  2026-05-13T19:40:06Z  safe_roll_forward                  —
  2026-05-13T19:40:16Z  regression_detected                rg-simulator-errors
  2026-05-14T01:40:03Z  minimum_monitoring_window_expired  —
```

**Stderr:** (empty)

**Verdict:** ✅ Sectioned plaintext (Overview / Stages / Metrics / Events)
renders end-to-end per D-07. Field padding and `[→]` stage marker work.

**Contract observations:**
- **`Env: —`** — the operator queried the most-recent path *without*
  `--environment`, so the renderer has no env key to display, even though
  the API response has both `environmentId` (opaque) and `_links.self.href`
  (encodes `test`). Plaintext misses the env. → **Task 3 — CLI-LEARNINGS
  CL-008 (or refine PC-020)**.
- **`Stages: [→] 50% 6h0m0s in progress`** — the marker says "in progress"
  but the overall State is "paused." The renderer derives stage state from
  `stageIndex == latestStageIndex` and absence of an explicit terminal flag,
  not from `data.status.kind == "paused"`. Visually contradicts the
  Overview block. → **Task 3 — CLI-LEARNINGS CL-009**.
- **`auto-rollback: false`** for every metric — same hardcoded-zero-value
  issue from Smoke A. Visually misleading in plaintext, same root cause as
  the wire-shape papercut PC-019.

## Status-mapping coverage from this smoke run

| Raw upstream `status` | Derived `status.kind` | Exercised |
|---|---|---|
| `monitoring_stopped` | `paused` | ✅ Smokes A + B + E |
| `active` | `active` | ❌ not exercised (no active rollout in fixtures) |
| `completed` | `completed` | ❌ not exercised |
| `regressed` | `regressed` | ❌ (metric-level regression hit, but rollout-level status remained `monitoring_stopped`) |
| `reverted` | `reverted` | ❌ not exercised |
| `waiting` | `waiting`? (PC-006) | ❌ not exercised |

→ Phase 3 unit tests cover the kind-mapping table comprehensively
(`status_test.go` rendering scenarios). Real staging only exercised the
`paused` path. The under-exercised values are listed for the production CLI
build to revisit (CL-006 / PC-006 / PC-015 traceability).

## Observations / Follow-ups

API contract observations (→ Task 3 — API-PAPERCUTS.md + Confluence):

1. **PC-019 — Rollout response surfaces `environmentId` (opaque ObjectId), not `environmentKey`.**
   Verified via raw curl: the API literally does not send `environmentKey` at
   all. Consumers identifying "which env is this rollout in?" must parse
   `_links.self.href` (fragile) or maintain a separate env-id → env-key map
   (requires an extra API call). The CLI passes `envKey` *in* (via path
   parameters), but the API echoes `envId` *out* — inconsistent.

CLI/UX observations (→ Task 3 — CLI-LEARNINGS.md):

2. **CL-008 — CLI's typed Go structs drop fields on read.** Biggest learning.
   Raw curl returns `metricConfigurations[].autoRollback: false` and
   `metricConfigurations[].differenceEstimateType: "absolute"`; the CLI envelope
   omits both because `MetricConfiguration.AutoRollback` uses `json:",omitempty"`
   (strips `false`) and there's no struct field for `differenceEstimateType`
   (silently dropped by `json.Unmarshal` → re-marshal). Violates the project's
   "JSON passthrough" principle (memory `feedback_json_api_passthrough.md`).
   Production CLI should use `json.RawMessage` or `map[string]interface{}`
   for the envelope's `data` payload to preserve wire fidelity.

3. **CL-009 — `Env: —` in plaintext when env is implicit.** The most-recent
   path returns `environmentId` (opaque) but the plaintext renderer can't show
   it as a human-readable env key. Two CLI options for production: (a) parse
   `_links.self.href` to extract the env key, (b) call out the API gap (PC-019).
   Prototype shipped neither — `Env: —` is the current visible state.

4. **CL-010 — Plaintext stage marker shows "in progress" while overall State
   is "paused."** Stage state should derive from `data.status.kind`, not
   purely from `stageIndex == latestStageIndex`. Visual contradiction.

5. **CL-011 — Single-stage rollouts render only one stage line.** The
   reference plaintext (CONTEXT.md D-07 example showing `25% / 50% / 75%`)
   assumed multi-stage progressive rollouts; this fixture was a single-stage
   guarded rollout. Renderer handled it correctly but the reference doc is
   misleading for first-time users.

6. **CL-012 — Plaintext renders `auto-rollback: false` for every metric**
   purely because of CL-008's struct stripping. Once CL-008 is fixed (raw
   passthrough), the renderer should consume the actual `autoRollback` value
   from the wire. Or omit the line entirely until the production CLI decides
   what "no auto-rollback configured" should look like.

Operator-confidence observations (no destination — for SUMMARY.md):

- Get vs List → items[0] return identical data. No eventual-consistency
  window observed. D-04 holds.
- Empty-collection wire shape is `items: []` (raw curl verified), not `null`.
  The CLI's `len(items) == 0` check is correct.
- Token never leaked into stdout or stderr in any of the 5 smokes (the
  `retryablehttp.Logger: nil` setting introduced in Phase 1 holds).
