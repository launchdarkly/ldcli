# Architecture Research — gonfalon `automated-releases` API

**Domain:** ldcli integration with the unified Release Guardian / Automated Releases API in gonfalon
**Researched:** 2026-05-11
**Confidence:** HIGH (primary sources: gonfalon source code at `/Users/alex/code/launchdarkly/gonfalon/`)

---

## Executive Summary

The unified `automated-releases` API is built on top of the existing `releaseguardian` (measured rollout) infrastructure. From the CLI's perspective there are **two distinct surfaces**:

1. **Mutations** (start / stop) — performed as **flag-patch semantic-patch instructions** against the existing public flag-patch endpoint (`PATCH /api/v2/flags/{projectKey}/{flagKey}` with `Content-Type: application/json; domain-model=launchdarkly.semanticpatch`). Instruction kinds: `startAutomatedRelease`, `stopAutomatedRelease`.
2. **Queries / observability** (list, get, diagnostics, dismiss-regression, recommended-duration, exemplar-errors) — performed as direct REST calls under **`/internal/projects/...`**. These are mounted on gonfalon's internal router, but accept ordinary account-token (or session) auth via the `EhttpWithSessionOrToken` middleware — so an ldcli access token works.

The unified `automated-releases` REST endpoints **delegate** to the legacy `measured-rollouts` handlers and re-name the response shape (`MeasuredRollout` → `AutomatedRelease`, `treatmentVariationId` → `targetVariationId`, etc.). The `AutomatedRelease` response is the **flattened** representation the CLI should target — it merges design + state + stageStates into one object.

**Critical caveat:** None of these `/internal/...automated-releases/` endpoints appear in the public `ld-openapi.json` ldcli ships with. The mutation endpoint (`PATCH /api/v2/flags/...`) is public, but the `startAutomatedRelease` and `stopAutomatedRelease` *instruction kinds* are not yet in the public OpenAPI either. This means the CLI must **hand-roll types and routes** for everything in this milestone — no code generation is possible from the LD public spec today.

**Gating:** `startAutomatedRelease` / `stopAutomatedRelease` are gated by the `release-guardian` dogfood flag (`e.Flags.ReleaseGuardian()`) **except** for flags with `Purpose == "ai"`, which bypass the gate. The CLI must surface meaningful errors when the account is not yet enabled.

---

## Standard Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        ldcli (this milestone)                            │
│                                                                          │
│   cmd/flags/rollouts/                  cmd/flags/                       │
│   ┌──────────────────┐                 ┌─────────────────┐              │
│   │ start.go         │                 │ toggle.go (ex.) │              │
│   │ stop.go          │                 └─────────────────┘              │
│   │ list.go          │                                                   │
│   │ status.go (get)  │                                                   │
│   │ dismiss_regr.go  │                                                   │
│   │ watch.go         │                                                   │
│   └────────┬─────────┘                                                   │
│            │                                                             │
│            ▼                                                             │
│   internal/rollouts/         (new package, follows existing pattern)    │
│   ┌──────────────────────────────────────────────────────────────┐      │
│   │ client.go      — Client interface + RolloutsClient impl       │      │
│   │ models.go      — AutomatedRelease, Stage, Event, ...          │      │
│   │ instructions.go — StartInstruction, StopInstruction structs   │      │
│   │ mock_client.go — generated mock                               │      │
│   └──────────────────────────────────────────────────────────────┘      │
│            │                              │                              │
│            │ (semantic-patch flag)         │ (direct REST)               │
│            ▼                              ▼                              │
│   internal/resources/                                                    │
│   ┌──────────────────────────────────────────────────────────────┐      │
│   │ ResourcesClient.MakeRequest(token, method, path, ct, ...)     │      │
│   │ (existing — handles auth, error normalization, suggestions)   │      │
│   └──────────────────────────────────────────────────────────────┘      │
└────────────────────────────────┬─────────────────────────────────────────┘
                                 │ HTTPS (Authorization: <api-token>)
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                  gonfalon (app.launchdarkly.com)                         │
│                                                                          │
│  PUBLIC ROUTER (/api/v2/...)        INTERNAL ROUTER (/internal/...)     │
│  ┌────────────────────────────┐    ┌─────────────────────────────────┐  │
│  │ PATCH /flags/{p}/{flagKey} │    │ /projects/{p}/flags/{fk}/       │  │
│  │  (with semantic-patch CT)  │    │   automated-releases  (LIST)    │  │
│  │   → instruction dispatch:  │    │ /projects/{p}/environments/{ek}/│  │
│  │    • startAutomatedRelease │    │   automated-releases/{id}  (GET)│  │
│  │    • stopAutomatedRelease  │    │   .../diagnostics         (GET) │  │
│  └────────────────────────────┘    │   .../metric-results/{mk} (GET) │  │
│                                    │   .../metric-states/{mk}  (PATCH│  │
│                                    │     — dismiss_regression)       │  │
│                                    │   .../exemplar-errors     (GET) │  │
│                                    │ /projects/{p}/environments/{ek}/│  │
│                                    │   automated-releases/recommended│  │
│                                    │   -duration               (GET) │  │
│                                    └─────────────────────────────────┘  │
│                                                                          │
│  Both routers run EhttpWithSessionOrToken → account token works         │
│  Public router additionally gates start/stop instructions behind        │
│    `ReleaseGuardian()` dogfood flag (see Gating section).               │
└─────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Implementation Hint |
|-----------|----------------|---------------------|
| `cmd/flags/rollouts/` (new) | Cobra subcommands: `start`, `stop`, `list`, `status`, `dismiss-regression`, `watch` | Each file mirrors `cmd/flags/toggle.go` style; thin handlers that delegate to `internal/rollouts/` |
| `internal/rollouts/Client` (new) | Typed interface for all 6 operations; mockable for tests | Same pattern as `internal/flags/Client` |
| `internal/rollouts/RolloutsClient` (new) | Concrete impl using `internal/resources/Client.MakeRequest` for HTTP | All 6 operations share one `resources.Client` — no `ldapi.APIClient` needed (endpoints aren't in the public OpenAPI) |
| `internal/rollouts/instructions.go` (new) | `StartInstruction`, `StopInstruction` structs that marshal to the semantic-patch body | Mirrors gonfalon's `StartAutomatedReleaseInstruction` struct exactly |
| `internal/rollouts/models.go` (new) | `AutomatedRelease`, `Stage`, `Event`, `MetricConfiguration`, status enums | Hand-rolled (gonfalon spec not in ldcli's `ld-openapi.json`) |
| `internal/output/` (existing) | Plaintext columns, table formatting, JSON pass-through | Add column registrations for `automated-releases` resource |
| `cmd/cliflags/` (existing) | Flag-name constants (e.g., `--release-kind`, `--target-variation`, `--randomization-unit`, `--stages`, `--metrics`) | Append new constants |

---

## API Surface Inventory

### Mutations — Flag Semantic-Patch Instructions

Both `start` and `stop` are issued via the **public** flag patch endpoint:

```
PATCH /api/v2/flags/{projectKey}/{flagKey}
Authorization: <api token>
Content-Type: application/json; domain-model=launchdarkly.semanticpatch
LD-API-Version: beta            (RECOMMENDED — semantic patch may evolve)

Body:
{
  "environmentKey": "<envKey>",
  "instructions": [
    { "kind": "startAutomatedRelease", ... }   // or stopAutomatedRelease
  ],
  "comment": "(optional human comment)"
}
```

#### `startAutomatedRelease`

**Source:** `gonfalon/internal/flags/instruction/instruction_start_automated_release.go`

| Field | Type | Required | Notes |
|---|---|---|---|
| `kind` | `"startAutomatedRelease"` | yes | discriminator |
| `releaseKind` | `"guarded"` \| `"progressive"` | yes | controls which set of behaviors |
| `originalVariationId` | UUID string | yes | the "control" variation (variation `_id`, not index) |
| `targetVariationId` | UUID string | yes | the "treatment" variation; must differ from original |
| `randomizationUnit` | string | yes | e.g. `"user"`; must match metrics' supported units |
| `stages` | `[{allocation: int, durationMillis: int64}]` | yes | ≥ 1 stage; `allocation > 0`; for guarded, `allocation ≤ 50000` (50%) per stage |
| `metrics` | `[{key: string, isGroup?: bool}]` | guarded: yes; progressive: ignored | metric or metric-group keys |
| `metricMonitoringPreferences` | `map[metricKey]{autoRollback: bool}` | optional | per-metric override; defaults to `autoRollback=false` |
| `extensionDurationMillis` | `int64` | optional | guarded only; extra time after the final stage's `durationMillis` |
| `ruleId` | string | optional | target an existing rule by ID |
| `ref` | string | optional | target an existing rule by ref (alternative to `ruleId`) |
| `clauses` | `[Clause]` | optional | if set → creates a NEW rule with those clauses |
| `description` | string | optional | new-rule description |
| `beforeRuleId` | string | optional | insert new rule before this one (default: append) |

**Target resolution:**
- `ruleId == "" && ref == "" && len(clauses) == 0` → **fallthrough** (default rule). Also subject to the `disable-automated-rollouts-on-default-rule` dogfood flag.
- `len(clauses) > 0` → **new rule** (with optional `beforeRuleId`)
- otherwise → **existing rule** identified by `ruleId` or `ref`

**Server-side validation errors the CLI will encounter:**
- `"instruction releaseKind must be \"guarded\" or \"progressive\""`
- `"instruction targetVariationId and originalVariationId must be different"`
- `"stage allocation must be greater than 0"`
- `"stage allocation must not exceed 50%"` (guarded only)
- `"flag <key> is off"` (server requires flag.on == true)
- `"Flag must not have ongoing guarded rollout"` / `"...progressive rollout"`
- `"instruction kind 'startAutomatedRelease' unsupported"` (when `ReleaseGuardian` dogfood flag is OFF and `flag.purpose != "ai"`)
- `"Automated releases cannot be created on the default rule"` (dogfood-gated)
- `"cannot start an automated release on a disabled rule"`
- `domain.ErrInvalidMeasuredRolloutConfiguration` wrapped — e.g. metrics missing, trace metrics not enabled, randomization unit incompatible with metrics, stats model unsupported

**Response:** the standard updated `FeatureFlag` resource (NOT an `AutomatedRelease` — see Papercut P1). To retrieve the newly-created automated release ID, the CLI must subsequently call `GET .../automated-releases` and pick the most recent.

#### `stopAutomatedRelease`

**Source:** `gonfalon/internal/flags/instruction/instruction_stop_automated_release.go`

| Field | Type | Required | Notes |
|---|---|---|---|
| `kind` | `"stopAutomatedRelease"` | yes | discriminator |
| `finalVariationId` | UUID string | yes | the variation to serve after stopping |
| `ruleId` | string | optional | target rule (omit for fallthrough) |
| `ref` | string | optional | alternative rule identifier |

**Target resolution:** same fallthrough vs. rule logic as `start`. **No `clauses` / new-rule support** (you can't stop a rollout while creating a new rule — only existing or fallthrough).

**Server-side validation errors:**
- `"instruction finalVariationId must be specified"`
- `"no active automated release found on this target"` (from `detectRolloutType` — fires when the rollout has already ended, or never existed)
- The rollout type is **inferred** from the flag's current variation/rollout (`AllocationTypeMeasuredRollout` → guarded; `AllocationTypeProgressiveRollout` → progressive) — caller does NOT pass it.

**Response:** updated `FeatureFlag` (not the stopped `AutomatedRelease`).

### Queries — Direct REST Endpoints

All paths are relative to `https://app.launchdarkly.com` (i.e., ldcli's `--base-uri`). All use `Authorization: <api token>`; no special `X-LD-*` headers required.

Source spec: `gonfalon/internal/experimentation/releaseguardian/internal/api/api.yaml`

| Operation | Method | Path | CLI Maps To |
|---|---|---|---|
| `internalGetAutomatedReleases` | GET | `/internal/projects/{projectKey}/flags/{flagKey}/automated-releases` | `rollouts list` |
| `internalGetAutomatedRelease` | GET | `/internal/projects/{projectKey}/environments/{environmentKey}/automated-releases/{automatedReleaseId}` | `rollouts status --id ...` (or as a sub-step of `rollouts status` after looking up most-recent) |
| `internalGetAutomatedReleaseDiagnostics` | GET | `/internal/projects/{projectKey}/environments/{environmentKey}/automated-releases/{automatedReleaseId}/diagnostics` | sub-step of `rollouts status` (shows context-kinds last received) |
| `internalGetAutomatedReleaseMetricResults` | GET | `/internal/projects/{projectKey}/flags/{flagKey}/environments/{environmentKey}/automated-releases/{automatedReleaseId}/metric-results/{metricKey}` | sub-step of `rollouts status` for guarded |
| `internalGetAutomatedReleaseRecommendedDuration` | GET | `/internal/projects/{projectKey}/environments/{environmentKey}/automated-releases/recommended-duration` | optional pre-flight; provides `milliseconds` |
| `internalPatchAutomatedReleaseMetricState` | PATCH | `/internal/projects/{projectKey}/environments/{environmentKey}/automated-releases/{automatedReleaseId}/metric-states/{metricKey}` | `rollouts dismiss-regression` |
| `internalGetAutomatedReleaseExemplarErrors` | GET | `/internal/projects/{projectKey}/environments/{environmentKey}/automated-releases/{automatedReleaseId}/exemplar-errors` | optional — show alongside regressed-metric details |

#### `LIST` — `internalGetAutomatedReleases`

**Query parameters:**
- `filter` (array of strings, only the **first** element is honored — see Papercut P2). Filter syntax: comma-separated `field:value` pairs with supported fields `environmentKey`, `status`, `kind`. Status accepts multiple values (e.g. `status:monitoring,monitoring_regressed`); environmentKey and kind accept only one each.
- `limit` (int32, default 25)
- No pagination cursors / page tokens exposed (Papercut P3).

**Response:** `{ items: AutomatedRelease[], _links: {self, parent} }`

#### `GET` (single rollout) — `internalGetAutomatedRelease`

Returns one `AutomatedRelease` (full shape — see below). **Requires `environmentKey`** in the path, even though rollouts are uniquely identified by `automatedReleaseId` (Papercut P4 — caller must know the env to look up by ID).

#### `AutomatedRelease` shape (response model)

Source: `internal/experimentation/releaseguardian/internal/api/automated_release_transformations.go`

```jsonc
{
  "id": "uuid",
  "accountId": "...", "projectId": "...", "environmentId": "...",
  "flagKey": "my-flag",
  "kind": "guarded" | "progressive",
  "originalVariationId": "uuid", "targetVariationId": "uuid",
  "randomizationUnit": "user",
  "ruleIdOrFallthrough": "<ruleId>" | "fallthrough",
  "createdAt": <unixMillis>,
  "startedAtMillis": <unixMillis>,        // optional (present once stage 0 started)
  "endedAtMillis":   <unixMillis>,        // optional (terminal state only)
  "status": "not_started" | "in_progress" | "monitoring_regressed" | "completed"
         | "reverted" | "manually_completed" | "manually_reverted"
         | "srm_stopped" | "monitoring_stopped" | "archived" | "waiting",
  "latestStageIndex": 0,
  "extensionDurationMillis": <int64>,     // guarded-only, when extension configured
  "stages": [
    { "stageIndex": 0, "allocation": 50000, "durationMillis": 900000,
      "startedAtMillis": <unixMillis>?,
      "safeRollForward": <bool>?           // guarded-only
    }, ...
  ],
  "events": [
    { "kind": "stage_started"|"regression_detected"|"regression_dismissed"|
              "monitoring_window_expired"|"safe_roll_forward"|
              "minimum_monitoring_window_expired"|"completed"|"reverted"|
              "manually_reverted"|"manually_rolled_out"|"rolled_out"|
              "archived"|"srm_detected",
      "stageIndex": 0,
      "metricKey": "..." (optional),
      "description": "...", "createdAt": <unixMillis> }, ...
  ],
  "metricConfigurations": [                // guarded-only
    { "metricKey": "...", "minSampleSize": 30, "autoRollback": false,
      "status": "ok"|"regressed"|"regression_dismissed" }, ...
  ],
  "metrics": [ /* MetricListingRep — expanded metric metadata */ ],  // guarded-only
  "_links": { "self": {...} }
}
```

**Status model — actually a 4-axis state machine** (Papercut P5: not labeled this way in docs):
- **Lifecycle**: `not_started` → `in_progress` → (terminal)
- **Terminal states**: `completed`, `reverted`, `manually_completed`, `manually_reverted`, `srm_stopped`, `monitoring_stopped`, `archived`
- **Action-required ("the UI shows a banner") state**: `monitoring_regressed` — rollout halted, awaiting user dismissal or stop.
- **`waiting`** is a quirk — see Papercut P6.

The unified `status` enum *consolidates* the legacy `monitoring` MeasuredRollout status into `in_progress` (`domainStatusToAutomatedReleaseStatus` in `automated_release_transformations.go:49`). The CLI should use the unified vocabulary exclusively.

#### `DISMISS REGRESSION` — `internalPatchAutomatedReleaseMetricState`

```
PATCH /internal/projects/{p}/environments/{ek}/automated-releases/{id}/metric-states/{metricKey}
Content-Type: application/json
{
  "action": "dismiss_regression"
}
```

- Returns **204 No Content** on success — does NOT return the new metric status (Papercut P7).
- Only valid action today is `dismiss_regression`.
- Only meaningful when `metricConfigurations[*].status == "regressed"`.

#### `RECOMMENDED DURATION` — optional pre-flight

```
GET /internal/projects/{p}/environments/{ek}/automated-releases/recommended-duration
    ?metricKeys=metric-a&metricKeys=metric-b
    &contextKind=user
    &finalStageAllocation=50000
    &flagKey=my-flag
→ { "milliseconds": 86400000 }
```

This is the closest thing to a "metric health-check" pre-flight (it implicitly fails if metrics are mis-configured, missing, or incompatible with the randomization unit). However:
- It is **not** a dedicated health-check endpoint; the underlying validation runs server-side as part of `startAutomatedRelease` regardless.
- It **doesn't return per-metric pass/fail** — just a single duration or an error.
- See Papercut P8: a dedicated pre-flight `POST /automated-releases:validate` would be much more useful.

The CLI's `--skip-health-checks` flag should map to "skip calling this endpoint before issuing start"; the actual server validation cannot be skipped.

#### `DIAGNOSTICS` — observability for a running rollout

```
GET /internal/projects/{p}/environments/{ek}/automated-releases/{id}/diagnostics
→ { "id": "...",
    "flag": { "key": "...", "contextKinds": [{"kind":"user", "lastReceivedMs": ...}] },
    "metrics": [{"key": "...", "contextKinds": [...]}] }
```

Shows which context kinds have been received on the flag eval stream and on metric events — i.e., "is data flowing?". Use as part of `status` output when the user wants to know why a rollout is stuck.

### Authentication / RBAC Mapping

| Surface | Auth | RBAC Action(s) checked |
|---|---|---|
| Public flag-patch (`PATCH /api/v2/flags/...`) | API token (existing ldcli `--access-token`) | `updateFallthroughWithMeasuredRollout` / `updateRulesWithMeasuredRollout` (start, on default vs. custom rule); `stopMeasuredRolloutOnFlagFallthrough` / `stopMeasuredRolloutOnFlagRule` (stop). When `deprecate-measured-rollout-rbac-actions` flag is on, falls back to plain `updateFallthrough` / `updateRules`. |
| `GET /internal/.../automated-releases*` | API token, session, **or** session cookie (`EhttpWithSessionOrToken`) | `viewProject` |
| `PATCH .../metric-states/{mk}` | same | `viewProject` (server uses access-checker on context) |

**Implication for ldcli:** the existing `--access-token` (or `LD_ACCESS_TOKEN` env, or config-file value) flows through unchanged. The CLI does not need any new auth surface. However, callers will need both **flag mutation** AND **project view** permissions — most existing `Writer` / `Admin` roles already grant both, but custom roles may break (Papercut P9: surface a friendly "missing scope" error).

---

## Architectural Patterns

### Pattern 1: Two-Step Start (Patch + Re-list)

**What:** Issuing `startAutomatedRelease` mutates the flag and returns the updated `FeatureFlag` — **not** the new `AutomatedRelease`. To return a useful result to the user (rollout ID, initial status), the CLI must do a second `GET .../automated-releases?filter=environmentKey:<ek>&limit=1` and pick the latest.

**When to use:** every `rollouts start` invocation.

**Trade-offs:**
- Pros: matches API today; no API changes required to ship a useful CLI.
- Cons: race window if another rollout is started concurrently on the same flag/env (essentially impossible in practice — server rejects with `"Flag must not have ongoing ..."` if one already exists). Latency: 2 round-trips.

**Example:**

```go
func (c RolloutsClient) Start(ctx context.Context, accessToken, baseURI, projKey, flagKey, envKey string, instr StartInstruction) (*AutomatedRelease, error) {
    // 1. POST the semantic-patch with the start instruction
    patchBody := SemanticPatch{
        EnvironmentKey: envKey,
        Instructions:   []any{instr},
    }
    if _, err := c.resources.MakeRequest(accessToken, "PATCH",
        baseURI + "/api/v2/flags/" + projKey + "/" + flagKey,
        "application/json; domain-model=launchdarkly.semanticpatch",
        nil, mustMarshal(patchBody), true /* isBeta */); err != nil {
        return nil, err
    }
    // 2. List rollouts, scoped to this env, limit=1
    listURL := baseURI + "/internal/projects/" + projKey + "/flags/" + flagKey + "/automated-releases"
    q := url.Values{"filter": {"environmentKey:" + envKey}, "limit": {"1"}}
    body, err := c.resources.MakeRequest(accessToken, "GET", listURL, "application/json", q, nil, false)
    // 3. unmarshal items[0] and return it.
    ...
}
```

### Pattern 2: Status as a Multi-Fetch Aggregation

**What:** the `rollouts status` command output combines (a) the `AutomatedRelease` object, (b) one `metric-results` call per metric for guarded rollouts, and (c) optionally diagnostics. There is no single endpoint that returns all of it.

**When to use:** `status`, `watch`, `list --detailed`.

**Trade-offs:**
- Pros: respects existing API; consistent with how the UI does it (it makes N+1 requests too).
- Cons: many round-trips for big metric sets. CLI should parallelize metric-results fetches.

### Pattern 3: Semantic-Patch Wrapper

**What:** Both start and stop reuse one helper that builds the `{ environmentKey, instructions: [...], comment? }` envelope and sets the `domain-model=launchdarkly.semanticpatch` content-type. Keep this in `internal/rollouts/instructions.go`.

**When to use:** any future automated-releases mutation; possibly reusable for other semantic-patch flag operations later.

**Example:**

```go
type SemanticPatch struct {
    EnvironmentKey string `json:"environmentKey"`
    Instructions   []any  `json:"instructions"`
    Comment        string `json:"comment,omitempty"`
}

type StartInstruction struct {
    Kind                        string                          `json:"kind"`  // "startAutomatedRelease"
    ReleaseKind                 string                          `json:"releaseKind"`
    OriginalVariationId         string                          `json:"originalVariationId"`
    TargetVariationId           string                          `json:"targetVariationId"`
    RandomizationUnit           string                          `json:"randomizationUnit"`
    Stages                      []Stage                         `json:"stages"`
    Metrics                     []MetricSource                  `json:"metrics,omitempty"`
    MetricMonitoringPreferences map[string]MetricPref           `json:"metricMonitoringPreferences,omitempty"`
    ExtensionDurationMillis     *int64                          `json:"extensionDurationMillis,omitempty"`
    RuleId                      string                          `json:"ruleId,omitempty"`
    Ref                         string                          `json:"ref,omitempty"`
    Clauses                     []Clause                        `json:"clauses,omitempty"`
    Description                 string                          `json:"description,omitempty"`
    BeforeRuleId                string                          `json:"beforeRuleId,omitempty"`
}
```

### Pattern 4: `--watch` Mode — Poll, Don't Subscribe

**What:** `gh pr checks --watch` style. Poll `GET .../automated-releases/{id}` every N seconds (e.g. 15s default, exponential backoff to 60s), maintain a running event cursor, and print **new** events. Terminate when `status` enters a terminal state OR an actionable state (`monitoring_regressed`).

**When to use:** `rollouts watch` (alias: `status --watch`).

**Trade-offs:**
- Pros: no streaming/SSE infra needed; works with existing HTTP client; agent-friendly.
- Cons: polling latency. For most rollouts (minutes-to-hours per stage) 15-60s is fine. Burns one API call per poll per env.

### Pattern 5: Models Hand-Rolled, Not Generated

**What:** Unlike most ldcli resource clients, do **not** add these endpoints to `ld-openapi.json` / regenerate `cmd/resources/resource_cmds.go`. The endpoints are not in the public spec; the spec format (`/internal/...`) won't survive the generator's URL templating. Hand-roll a small set of types in `internal/rollouts/models.go`.

**When to use:** entire milestone.

**Trade-offs:**
- Pros: no spec edits; isolates instability to one package.
- Cons: types can drift from server; mitigated by integration tests against a real account.

---

## Recommended Project Structure

```
ldcli/
├── cmd/
│   └── flags/
│       └── rollouts/                       # NEW: command package for `flags rollouts-beta`
│           ├── rollouts.go                 # parent cmd (returns *cobra.Command for "rollouts-beta")
│           ├── start.go                    # `rollouts-beta start`
│           ├── stop.go                     # `rollouts-beta stop`
│           ├── list.go                     # `rollouts-beta list`
│           ├── status.go                   # `rollouts-beta status` (+ --watch flag)
│           ├── dismiss_regression.go       # `rollouts-beta dismiss-regression`
│           ├── flags.go                    # shared flag registration (--release-kind, --target-variation, ...)
│           └── *_test.go                   # table-driven tests using mock client
├── internal/
│   └── rollouts/                           # NEW: typed client for the automated-releases API
│       ├── client.go                       # Client interface, RolloutsClient struct, NewClient(cliVersion)
│       ├── models.go                       # AutomatedRelease, Stage, Event, MetricConfiguration, status enums
│       ├── instructions.go                 # StartInstruction, StopInstruction, SemanticPatch envelope
│       ├── mock_client.go                  # generated via mockgen
│       └── client_test.go                  # HTTP round-trip tests (httptest.Server)
└── cmd/cliflags/
    └── flags.go                            # add new constants: ReleaseKindFlag, StagesFlag, MetricsFlag, ...
```

### Structure Rationale

- **`cmd/flags/rollouts/`** (under `cmd/flags/`, not top-level): matches the user-facing command path (`ldcli flags rollouts-beta ...`) and stays grouped with the existing flag-related commands (`toggle`, `archive`).
- **`internal/rollouts/`**: separate domain package, NOT nested under `internal/flags/`. Rationale: (a) the package owns its own data model (`AutomatedRelease`, not `FeatureFlag`); (b) it talks to two different endpoint families (semantic-patch + internal REST); (c) future automated-releases features (release policies) belong here too. This mirrors the existing pattern (`internal/environments/`, `internal/members/`, etc.).
- **No new `cmd/resources/` integration**: explicitly skip the OpenAPI generator pipeline — see Pattern 5.

---

## Data Flow

### `rollouts start` (most complex flow)

```
User: ldcli flags rollouts-beta start --flag my-flag --env production \
        --release-kind progressive --target-variation <vid> --original-variation <vid> \
        --randomization-unit user --stages 5000:60m,25000:60m,50000:60m

cmd/flags/rollouts/start.go:RunE
        │
        ├─ Optionally: GET /internal/.../recommended-duration  (pre-flight, unless --skip-health-checks)
        │     └─ on error in non-interactive: print error & exit 4
        │     └─ on error in interactive: prompt to override
        │
        ├─ PATCH /api/v2/flags/{p}/{flagKey}
        │     Content-Type: application/json; domain-model=launchdarkly.semanticpatch
        │     Body: { environmentKey, instructions: [{kind:"startAutomatedRelease", ...}] }
        │     └─ server validates RBAC, dogfood gate, instruction shape, metric configs
        │     └─ side-effect: server creates an AutomatedRelease row + applies the flag mutation in a single tx
        │     └─ Response: updated FeatureFlag (NOT the new AutomatedRelease)
        │
        ├─ GET /internal/projects/{p}/flags/{flagKey}/automated-releases?filter=environmentKey:{ek}&limit=1
        │     └─ Response: { items: [<the just-created AutomatedRelease>] }
        │
        └─ output.CmdOutput("create", outputKind, items[0]) → stdout
```

### `rollouts status --watch` (poll loop)

```
1. resolve --id (if given) OR GET .../automated-releases?env=...&limit=1 to find most recent
2. seenEventIds = {}
3. loop:
       release = GET .../automated-releases/{id}
       newEvents = [e for e in release.events if e.id not in seenEventIds]
       print(newEvents)  # actionable: stage_started, regression_detected, regression_dismissed, ...
       if release.status in {completed, reverted, manually_*, srm_stopped, monitoring_stopped, archived}:
           print final + exit 0 (completed) or exit non-zero (reverted, srm_stopped)
       if release.status == monitoring_regressed:
           print regression details + exit code 2 (action-required)
       sleep(pollInterval); pollInterval = min(pollInterval * 1.5, 60s)
```

### Key Data Flows

1. **Start flow:** CLI → `PATCH /api/v2/flags` → gonfalon validates + creates `AutomatedRelease` row → CLI re-fetches via `GET /internal/.../automated-releases`.
2. **Status flow:** CLI → `GET /internal/.../automated-releases/{id}` (single) or `.../flags/{flagKey}/automated-releases` (list) → render unified `AutomatedRelease`.
3. **Regression dismissal:** CLI → `PATCH /internal/.../metric-states/{mk}` with `{"action": "dismiss_regression"}` → server emits a `regression_dismissed` event → next `status` call shows updated metric `status: regression_dismissed`. (204 response means CLI must re-fetch to display the new state.)
4. **Stop flow:** CLI → `PATCH /api/v2/flags` with `stopAutomatedRelease` → server transitions release to `manually_completed` (if `finalVariationId == targetVariationId`) or `manually_reverted` (if `finalVariationId == originalVariationId`).

---

## Pre-Flight Health Checks — What Exists Today

The user asked specifically about a pre-flight metric/randomization-unit health-check endpoint. Findings:

- **No dedicated `POST /automated-releases:validate` exists.** Validation happens server-side as part of `startAutomatedRelease` (see `starterService.ValidateMetricsAndMetricSources` invoked from inside `instruction_start_automated_release.go:256`).
- The closest analog is `GET /internal/.../automated-releases/recommended-duration`, which runs much of the same validation pipeline to compute a duration. If this call fails with `domain.ErrInvalidMeasuredRolloutConfiguration` or `NotFound`, the same `start` call will also fail. **Use it as the pre-flight signal.**
- For an already-running rollout: `GET .../diagnostics` shows whether SDK flag-evaluation events and metric events are being received per context kind. This catches "rollout started but no exposures" scenarios.

**Recommendation:**
- `rollouts start` calls `recommended-duration` first when `--skip-health-checks` is unset; treats a non-2xx response as a pre-flight failure.
- Document clearly that pre-flight only validates *configuration*, not *event flow*. To check event flow, the user must start the rollout and inspect `diagnostics`.
- Open a papercut ticket for a dedicated `POST .../automated-releases:validate` endpoint (Papercut P8).

---

## API Papercut Catalog (seed for `.planning/API-PAPERCUTS.md`)

These will become the milestone's papercut deliverable. Numbered for cross-reference.

### P1 — Mutations return the wrong resource

**What:** `PATCH /api/v2/flags/...` with a `startAutomatedRelease` or `stopAutomatedRelease` instruction returns the updated `FeatureFlag`, not the affected `AutomatedRelease`.
**Why it hurts:** Every CLI/agent caller needs a follow-up GET to learn the rollout ID or terminal status. Doubles round-trips for the most common operation.
**Suggested fix:** Return `{ flag: <FeatureFlag>, automatedRelease: <AutomatedRelease> }` for these two instruction kinds, or add an `X-LD-AutomatedReleaseId` response header.

### P2 — `filter` accepts an array but only honors element [0]

**What:** `GET .../automated-releases?filter=...` accepts a string array but explicitly takes only the first element (`internal_get_automated_releases.go:66`: `(*request.Params.Filter)[0]`).
**Why it hurts:** Caller intuition is "multiple filter strings = AND". Bugs are silent (extra filters are dropped).
**Suggested fix:** Reject when `len(filter) > 1`, OR honor multiple elements, OR switch to discrete query params (`environmentKey=`, `status=`, `kind=`).

### P3 — No pagination on the list endpoint

**What:** `GET .../automated-releases` supports `limit` but no `offset` / `pageToken` / cursor.
**Why it hurts:** Once a flag has > 25 historical rollouts, the CLI cannot fetch older ones. `ldcli flags rollouts list --all` becomes impossible.
**Suggested fix:** Add a `_links.next` cursor following the standard LD pagination pattern.

### P4 — GET-by-ID requires environment in path

**What:** `GET .../environments/{environmentKey}/automated-releases/{automatedReleaseId}` — the `automatedReleaseId` is a UUID, globally unique. The `environmentKey` is redundant.
**Why it hurts:** When a caller has just a rollout ID (e.g. from a webhook), they must do a separate lookup to find the environment.
**Suggested fix:** Add `GET .../automated-releases/{automatedReleaseId}` (project-scoped or account-scoped); keep the env-scoped one for cache invalidation.

### P5 — Status enum mixes lifecycle + action-required + meta states

**What:** A single `status` enum encodes lifecycle (`not_started`, `in_progress`, terminal), action-required (`monitoring_regressed`), and meta (`waiting`, `archived`). Consumers must hardcode which statuses mean "still going", "stop & ask the human", or "done".
**Why it hurts:** Every consumer (CLI, UI, agent) re-implements the same `isTerminal(status)`, `isActionable(status)` logic.
**Suggested fix:** Either split into `phase` (active/terminal) + `attention: ok|action_required` + `outcome: success|failure|...|null`, OR document the categories in the OpenAPI x-extensions.

### P6 — `waiting` is undocumented in user-facing semantics

**What:** The enum includes `waiting` but the README doesn't explain when it fires (post-stage cooldown? pre-start grace?). The transformations file doesn't comment on it either.
**Why it hurts:** CLI status output / `--watch` will eventually surface "waiting" with no idea whether to keep waiting or surface a warning.
**Suggested fix:** Document it (or remove it if it's actually unreachable from the API surface).

### P7 — `dismiss_regression` returns 204 instead of the new resource state

**What:** `PATCH .../metric-states/{mk}` returns 204 No Content with a self-described "Hack: We should return the new status. For now, we are not." comment in the code (`internal_patch_measured_rollout_metric_state.go:54`).
**Why it hurts:** Callers must re-GET the release immediately to confirm dismissal landed. Adds a round-trip + a brief consistency window.
**Suggested fix:** Return the updated `AutomatedRelease` (or just the affected `metricConfigurations[i]`) with status `200`.

### P8 — No dedicated pre-flight validation endpoint

**What:** Server-side validation of metrics, randomization unit, and stage shape only runs inside `startAutomatedRelease`. The CLI's `--skip-health-checks` flag has no clean inverse: "run validation but don't start".
**Why it hurts:** Forces CLI/agent to either start-and-fail (and clean up partial state) or piggyback on `recommended-duration` (which also computes duration we don't need).
**Suggested fix:** Add `POST /internal/projects/{p}/flags/{fk}/automated-releases:validate` that accepts the same body as the start instruction and returns `{ valid: true } | { valid: false, errors: [...] }`.

### P9 — RBAC errors don't tell you which action you're missing

**What:** When the caller's role lacks `updateRulesWithMeasuredRollout` (start) or `stopMeasuredRolloutOnFlagRule` (stop), the 403 response just says "Access denied" without naming the action.
**Why it hurts:** Custom-role debuggers go fishing through the action list.
**Suggested fix:** Include the failing `actionIdentifier` in the 403 body.

### P10 — Metric monitoring preferences live in a side-car map

**What:** `metrics: [{key, isGroup}]` carries no per-metric configuration. `metricMonitoringPreferences: {<key>: {autoRollback}}` is a separate map keyed by metric key.
**Why it hurts:** Two parallel collections that must stay in sync; easy to set `autoRollback` for a metric you didn't include in `metrics` (silently ignored).
**Suggested fix:** Inline `autoRollback` into the metric source: `metrics: [{key, isGroup, autoRollback?}]`.

### P11 — `internal/` URL prefix signals "private", but auth works the same as public

**What:** The `/internal/...` paths look private but `EhttpWithSessionOrToken` accepts ordinary account tokens — same auth as `/api/v2/...`. Nothing about being mounted on the internal router actually restricts access beyond what `viewProject` already enforces.
**Why it hurts:** Confusing for first-time integrators; suggests these endpoints will be removed (they may be — see Beta volatility note below).
**Suggested fix:** Either rename to `/api/v2/.../automated-releases` once stable, or document explicitly that the `/internal/` prefix is for "endpoint maturity" not access control.

### P12 — Mismatched terminology: `kind` vs `releaseKind` vs `rolloutType`

**What:** The same concept ("guarded" vs "progressive") is called:
- `releaseKind` in the `startAutomatedRelease` instruction body
- `kind` in the `AutomatedRelease` response
- `rolloutType` in legacy `MeasuredRollout` / `MeasuredRolloutDesign` responses
- `kind` in the list-filter query param

**Why it hurts:** Easy to typo; hard to write generic helpers; the CLI's flag is `--release-kind` but the response field is `kind`.
**Suggested fix:** Standardize on `kind` everywhere.

### P13 — `controlVariationId` (legacy) → `originalVariationId` (unified) renamed mid-stream

**What:** Legacy `MeasuredRolloutDesign.controlVariationId` is renamed to `AutomatedRelease.originalVariationId`; same for `treatmentVariationId` → `targetVariationId`. The instruction body uses the new names. Diagnostics, exemplar-errors, etc. still reference legacy fields.
**Why it hurts:** CLI output may switch terminology depending on which endpoint produced the data.
**Suggested fix:** Pick one (suggest the unified names) and rename throughout, including in returned events.

### P14 — Stage durations are `int64` milliseconds; humans want `1h30m`

**What:** `durationMillis` is the only stage-duration field exposed.
**Why it hurts:** Every caller — CLI, UI, docs — does its own millis-to-human conversion. Off-by-one in unit conversion is a real risk.
**Suggested fix:** Either also accept/return `duration: "1h30m"` (Go-style), OR document a strict conversion helper. (CLI must accept both `--stages 50000:90m` and a millis form anyway.)

### P15 — No way to discover the unified status enum's transitions

**What:** The enum lists values but doesn't say which transitions are legal (e.g., can `monitoring_regressed` go back to `in_progress` after dismissal?).
**Why it hurts:** Tests have to discover the state machine empirically. Comment in the API file links to a Confluence page; that's not enough.
**Suggested fix:** Add an `x-state-transitions` extension in the OpenAPI, or document inline.

### P16 — `recommended-duration` requires `finalStageAllocation` even for progressive

**What:** Progressive rollouts don't have a "final stage allocation" in the same sense (the final stage's allocation is the rollout completion). The required `finalStageAllocation` parameter is awkward for the progressive case.
**Why it hurts:** Forces CLI to pass an essentially-meaningless `100000` for progressive callers.
**Suggested fix:** Make optional for progressive; or split the endpoint per kind.

---

## Anti-Patterns

### Anti-Pattern 1: Adding `automated-releases` to `ld-openapi.json`

**What people do:** Add the `/internal/...` paths to ldcli's bundled OpenAPI spec and regenerate `cmd/resources/resource_cmds.go` to get free CRUD commands.
**Why it's wrong:** (a) the public OpenAPI is the source-of-truth for what LD officially supports — modifying it locally diverges from prod; (b) the resource generator templates assume `/api/v2/` paths and standard pagination; (c) instability of the unstable API will create churn in 11k+ lines of generated code; (d) the spec is auto-updated from the LD API server via `make openapi-spec-update`, which would clobber local edits.
**Do this instead:** Hand-roll a small `internal/rollouts/` package; treat the `automated-releases` API as a separate, opaque dependency.

### Anti-Pattern 2: Re-implementing the legacy `measured-rollouts` and `progressive-rollouts` endpoints

**What people do:** Notice that the `measured-rollouts` REST endpoints have the same shape and ship CLI commands against them as a stepping stone.
**Why it's wrong:** The user explicitly forbade this. Those endpoints are being deprecated, and the unified `automated-releases` is the on-ramp. Building against the legacy surface would mean shipping deprecated code.
**Do this instead:** Use only the `automated-releases` instruction kinds and `/internal/.../automated-releases/...` REST endpoints. If a field is missing in the unified shape but present in the legacy shape, **file a papercut** instead of dropping back.

### Anti-Pattern 3: Treating `dismiss_regression` as synchronous

**What people do:** Print "regression dismissed" immediately after the 204 response and don't re-fetch.
**Why it's wrong:** The 204 confirms the event was queued; the metric status update is eventually-consistent (separate code path emits the dismissed status). Premature "success" output misleads agents.
**Do this instead:** After 204, re-fetch the release with a small backoff (e.g. 1s, 3s) until `metricConfigurations[*].status == "regression_dismissed"` for the targeted metric, then print success. Time out after ~10s and print a "dismissal queued; status may take a moment" warning.

### Anti-Pattern 4: Watch-mode that polls every second

**What people do:** Tight polling loop to feel responsive.
**Why it's wrong:** Rollouts last minutes-to-days; a 1s poll wastes API quota and burns the user's rate limit. Other LD commands in the same session will start failing.
**Do this instead:** Start at 15s, exponentially back off to 60s, reset on state change. Print events as they arrive; don't redraw the whole screen.

### Anti-Pattern 5: Inferring rollout type client-side

**What people do:** Look at the flag's existing rollout config to decide whether `stop` should send "stopMeasuredRolloutOnFallthrough" or "stopProgressiveRollout".
**Why it's wrong:** Those are legacy instructions; the unified `stopAutomatedRelease` infers the type server-side from the existing rollout (`detectRolloutType` in `instruction_stop_automated_release.go:193`). Caller passes only `finalVariationId`.
**Do this instead:** Always send `stopAutomatedRelease`; let the server figure out the type.

---

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---|---|---|
| gonfalon flag-patch (`PATCH /api/v2/flags/...`) | Semantic-patch via `internal/resources.Client.MakeRequest`; `Content-Type: application/json; domain-model=launchdarkly.semanticpatch`; `LD-API-Version: beta` recommended | Existing pattern in `cmd/flags/toggle.go` uses JSON Patch (RFC 6902), not semantic patch — do **not** copy that content-type. |
| gonfalon internal automated-releases REST (`/internal/projects/...`) | Plain JSON GET/PATCH via `internal/resources.Client.MakeRequest` | Auth = same access token. No special headers (`X-LD-Private`, `X-LD-AccountId`, etc.) needed — those are for the `/private/...` routes the orchestra service uses, not for human/CLI callers. |
| LD rate limiting | Inherited from gonfalon's existing rate-limit service | The release-guardian routes attach to the public route-collection rate-limit config. Watch-mode poll cadence must respect it (~60 req/min/token in many cases). |

### Internal Boundaries

| Boundary | Communication | Notes |
|---|---|---|
| `cmd/flags/rollouts/` ↔ `internal/rollouts/Client` | Direct method calls; client is constructed in `cmd/root.go` and injected | Mirrors existing `internal/flags.Client` pattern |
| `internal/rollouts/RolloutsClient` ↔ `internal/resources.Client` | The new client embeds (or accepts) `resources.Client` for all HTTP | Avoids duplicating auth/error/header logic |
| `internal/rollouts/Client` ↔ `internal/output` | Returns marshaled JSON `[]byte` (matches `internal/flags.Client.Get/Update` shape) | Lets `output.CmdOutput` handle JSON-vs-plaintext rendering |
| `cmd/analytics/` | Each command's `PersistentPreRun` fires `tracker.SendCommandRunEvent("flags-rollouts-beta-start", ...)` | Existing pattern; just register new event names |

---

## Suggested Build Order (feeds roadmap)

Based on dependencies and the fact that **`start` and `status` are the riskiest+highest-value**, suggested phasing:

1. **Phase R1 — Skeleton + Read-only**: scaffold `cmd/flags/rollouts/` + `internal/rollouts/`; implement `list` and `status` (read-only). Validates: client/output plumbing, types match server, auth works against staging. Surfaces papercuts P2-P6, P11-P15.
2. **Phase R2 — Start**: implement `start` (progressive + guarded), including the patch+re-fetch pattern and the optional `recommended-duration` pre-flight. Surfaces P1, P8, P10, P12-P14, P16. Highest API risk; expect rework.
3. **Phase R3 — Stop + Dismiss**: implement `stop` (with `--final-variation original|target`) and `dismiss-regression`. Smaller scope; depends on R1 (re-fetch) and R2 (semantic-patch helper). Surfaces P7, P9.
4. **Phase R4 — Watch**: add `--watch` to `status`. Pure CLI UX; no new endpoints. Test against a real staging rollout.
5. **Phase R5 — Polish**: JSON output schemas locked, exit codes documented, agent-mode (`--non-interactive`) verified, papercuts doc finalized.

R1 must come first; R2/R3 are mostly independent; R4 needs R1's status fetcher; R5 is cleanup.

---

## Architecture Decision Recommendations

| Question | Recommendation | Why |
|---|---|---|
| Generate types from OpenAPI or hand-roll? | **Hand-roll** in `internal/rollouts/models.go` | Spec isn't in ldcli's `ld-openapi.json`; gonfalon's spec uses `/internal/...` paths the generator can't handle; API is unstable |
| Where does the client live? | **`internal/rollouts/`** (new sibling package) | Follows existing pattern; isolates beta surface |
| Extend `internal/flags/` or new package? | **New package** | Different domain model, different endpoints, different stability lifecycle |
| Command names? | **`start` / `stop` / `list` / `status` / `dismiss-regression` / `watch` (alias for `status --watch`)** | Matches `gh pr` / `kubectl` mental model; `status` covers the common case better than `get` |
| One Cobra subcommand tree or flatten? | **Subcommand tree**: `ldcli flags rollouts-beta <verb>` | Matches PROJECT.md's stated command name; leaves room for future verbs (`pause`, `extend`) |
| Beta suffix in command name? | **Yes, `rollouts-beta`** | PROJECT.md requirement; signals instability matching the API |
| Use `LD-API-Version: beta` header? | **Yes** for the public flag-patch call | The `startAutomatedRelease` / `stopAutomatedRelease` instructions are beta-tier; this matches existing convention |
| Reuse `ldapi.APIClient` (Go SDK)? | **No** — use `internal/resources.Client.MakeRequest` directly | Endpoints aren't in the public Go client either |
| Dogfood-flag gating: detect and message? | **Yes**: catch the `"instruction kind 'startAutomatedRelease' unsupported"` error and add a suggestion to enable the `release-guardian` flag for the account | High likelihood the first user trying this hits the gate |

---

## Scaling Considerations

| Scale | Architecture Adjustments |
|---|---|
| Single user, single flag | Sequential calls; default poll cadence (15s) |
| CI/CD using watch on multiple flags | Run multiple `ldcli` processes; each respects its own rate limit |
| AI agents fanning out rollouts | Per-flag `start --no-watch`; one background `watch` per active rollout; agents respond to exit codes |
| Account with 10k+ historical rollouts on one flag | LIMITED by P3 (no pagination); CLI should warn when `items.length == limit` |

The CLI is a thin client; scaling is bounded by the server's rate limits, not ldcli internals.

---

## Sources

- `gonfalon/internal/flags/instruction/instruction_start_automated_release.go` — Start instruction definition, validation rules, target resolution
- `gonfalon/internal/flags/instruction/instruction_stop_automated_release.go` — Stop instruction definition, rollout-type inference
- `gonfalon/internal/flags/instruction/instruction.go` — Instruction-kind registry; dogfood gating
- `gonfalon/internal/experimentation/releaseguardian/internal/api/api.yaml` — Authoritative OpenAPI spec for all `/internal/...automated-releases/...` endpoints
- `gonfalon/internal/experimentation/releaseguardian/internal/api/gen_api_handler.go` — Generated route table; URL templates; auth middleware mapping
- `gonfalon/internal/experimentation/releaseguardian/internal/api/automated_release_transformations.go` — Domain → API translation rules (the canonical reference for what fields go where)
- `gonfalon/internal/experimentation/releaseguardian/internal/api/internal_get_automated_release.go` — List/get handler logic
- `gonfalon/internal/experimentation/releaseguardian/internal/api/internal_patch_measured_rollout_metric_state.go` — Dismiss-regression handler (incl. the "Hack" comment for P7)
- `gonfalon/internal/experimentation/releaseguardian/internal/api/internal_get_automated_release_diagnostics.go` — Diagnostics handler
- `gonfalon/internal/experimentation/releaseguardian/application/measuredrollout/starter/measured_rollout_starter_service.go` — Server-side validation that runs during start (informs P8)
- `gonfalon/internal/experimentation/releaseguardian/lifecycle/init.go` — Route binding + middleware (auth, dogfood-flag gating for `/api/v2/...measured-rollouts/...` legacy)
- `gonfalon/internal/routing/api_registration.go` — `/internal` URL prefix declaration
- `gonfalon/internal/foundation/ehttp.go` — `EhttpWithSessionOrToken` middleware (auth model for `/internal/...`)
- `gonfalon/internal/sempatch/middleware.go` — `domain-model=launchdarkly.semanticpatch` content-type requirement
- `gonfalon/internal/roles/action.go` — RBAC action identifiers for measured-rollout operations
- `ldcli/cmd/flags/toggle.go` — Existing JSON Patch flag-mutation pattern (compare: must use semantic-patch instead)
- `ldcli/internal/flags/client.go` — Existing typed-client pattern to mirror
- `ldcli/internal/resources/client.go` — Generic HTTP client with the right surface for `internal/rollouts/`
- `ldcli/ld-openapi.json` — Confirmed: contains `DependentMeasuredRolloutRep` only; no `automated-releases` paths

---

*Architecture research for: ldcli ↔ gonfalon automated-releases API integration*
*Researched: 2026-05-11*
