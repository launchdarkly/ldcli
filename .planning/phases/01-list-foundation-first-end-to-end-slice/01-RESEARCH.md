# Phase 1: List (foundation + first end-to-end slice) - Research

**Researched:** 2026-05-12
**Domain:** Cobra CLI extension on top of an unstable internal REST API (`gonfalon` automated-releases), with agent-friendly JSON output, hand-rolled types, retry layer, and TTY-aware formatting
**Confidence:** HIGH overall — patterns and integration points are already validated in upstream research (`SUMMARY.md`, `ARCHITECTURE.md`, `STACK.md`, `PITFALLS.md`); only items flagged inline drop to MEDIUM/LOW

## Summary

Phase 1 is the foundation phase **and** the first vertical slice: it ships `ldcli flags rollouts-beta list --flag <key>` end-to-end against `GET /internal/projects/{p}/flags/{flagKey}/automated-releases` and establishes the contract surface every later phase reuses — `internal/rollouts/` package, versioned JSON envelope (`schemaVersion: "rollouts.v1beta1"`), structured `error.code` taxonomy, `go-retryablehttp` retry layer, `Idempotency-Key` plumbing, TTY-aware output, beta banner, and the seeded `.planning/API-PAPERCUTS.md`. The 8 locked decisions in CONTEXT.md (D-01 through D-08) eliminate most architectural ambiguity; this research fills in the concrete file structure, function signatures, retry policy spec, status-mapping table, and pattern-analog mapping the planner needs.

The phase is unusual in that the *infrastructure* and the *first slice* land together. That's intentional — a working `list` proves the JSON envelope, retry layer, TTY detection, and error taxonomy are correctly wired end-to-end. The plumbing has no test fixture without `list`; `list` has no contract without the plumbing. The planner should treat the walking skeleton sequence (Wave 0 in this research) as a single atomic deliverable that ends with `make test && ./ldcli flags rollouts-beta list --flag foo` returning a valid envelope.

**Primary recommendation:** Ship Phase 1 as one slice — `internal/rollouts/` skeleton with `List` + `Get` + retryable HTTP + envelope types → `cmd/flags/rollouts/` Cobra subtree with `list` only → `cmd/root.go` wiring → seeded papercuts doc → tests against `httptest.NewServer`. Defer `--idempotency-key` user flag to Phase 2 (no mutation exists to exercise it). Defer transparent pagination behind a documented "list saturated upstream limit" warning (Papercut P3) — Phase 1 truncates at the API's default limit and surfaces the warning.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Exit codes & error contract
- **D-01:** Exit codes stay consistent with the rest of ldcli — any error returns exit `1`. **No numeric taxonomy.** This explicitly reframes REQ-AGENT-02 and FOUND-04 from "numeric exit-code taxonomy" to "structured `error.code` taxonomy in the JSON envelope."
  - Rationale: The JSON envelope already carries `error.code` + `error.nextAction`, which is richer than any exit code. Agents read JSON. Adding a numeric taxonomy means more code, more places to keep consistent, and minimal added value over what's in the envelope.
  - Downstream impact: FOUND-04 collapses to "documented `error.code` enum on the JSON envelope." SIGINT during `--watch` (Phase 3) still uses exit `130` per Go stdlib convention since that's emitted by `signal.NotifyContext`, not by our code.

#### Status display model
- **D-02:** Every rollout-describing response carries a **three-field status model**:
  ```json
  {
    "status": "<raw API status>",
    "kind": "active|regressed|reverted|paused|completed",
    "label": "<human-readable string with reason inline>"
  }
  ```
  - `status` is the raw API value (`not_started`, `in_progress`, `waiting`, `monitoring_regressed`, `completed`, `reverted`, `manually_completed`, `manually_reverted`, `srm_stopped`, `monitoring_stopped`, `archived`).
  - `kind` is a 5-bucket lifecycle classifier derived from UI `guardedRolloutUIStates`. **UI's `neutral` is renamed to `paused`** (operationally accurate; UI copy uses "paused at N%").
  - `label` is the human-readable string with contextual reason inline (e.g. `"rolled back automatically after detecting a regression for latency-p99"`). Mirrors UI labels for parity (REQ-UX-01).
- **D-03:** **Structured `reason` object is deferred.** Phase 1 emits `status` + `kind` + `label` only. `label` is the agent-parseable stop-gap.
- **D-04:** **`--state` filter is dropped from v1.** REQ-LIST-03 modified — only `--environment` filter is shipped.

#### `list` command shape
- **D-05:** **Default scope:** most recent 20 rollouts (`--limit 20`), reverse-chronological. `--all` returns the full history. Stable ordering documented in `--help`.
- **D-06:** **Plaintext layout:** narrow 5-column table by default — `ID`, `kind`, `environment`, `state/label`, `started`. `--detailed` adds variations, ended-at, current stage index, raw API status.
- **D-07:** **JSON output always emits the full field set** regardless of `--detailed`.

#### `Client` interface scope
- **D-08:** **Grow the `internal/rollouts/Client` interface incrementally.** Phase 1 ships only:
  ```go
  type Client interface {
      List(ctx, token, baseURI, projKey, flagKey, opts ListOpts) (*RolloutList, error)
      Get(ctx, token, baseURI, projKey, envKey, rolloutID string) (*Rollout, error)
  }
  ```
  Phase 2 adds `Start`. Phase 4 adds `Stop` + `DismissRegression`.

### Claude's Discretion

- Internal layout under `internal/rollouts/` (file split for `client.go`, `models.go`, `instructions.go`, `mock_client.go`) — follow `internal/flags/` convention.
- Exact field names inside the rollout model — follow API names where unambiguous; rename only when API name is misleading (e.g. UI's `treatmentVariationId` → `targetVariationId` happened upstream, so we use `targetVariationId`).
- Retry policy specifics within the 4 retries / 500ms–8s envelope (e.g. retry-after honoring, jitter percentage).
- Beta banner exact copy and placement (stderr-only when TTY; suppressed when piped or `--output json`).
- Whether to expose `--idempotency-key` user-facing flag in Phase 1 (no mutations to exercise it) or wait until Phase 2 — researcher/planner pick. **Research recommends: defer to Phase 2.**

### Deferred Ideas (OUT OF SCOPE)

- **`--state` filter on `list`** — not essential for v1.
- **Structured `reason` object on the status model** — Phase 1 ships `status` + `kind` + `label`.
- **`--idempotency-key` user-facing flag** — infrastructure is wired in Phase 1; user-facing flag held until Phase 2.
- **Pagination as a user-facing concern** — D-05's bounded default (20) sidesteps pagination for the common case. `--all` may need transparent pagination handling (Papercut P3 territory).
- **Cross-environment list behavior** — `list --flag <key>` without `--environment` returns rollouts across all envs; pure reverse-chronological by `startedAt` regardless of env.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| FOUND-01 | New package `internal/rollouts/` with `Client` interface and hand-rolled types | §"Standard Stack" + §"Recommended Project Structure" + §"`internal/rollouts/` Skeleton" |
| FOUND-02 | Command tree `ldcli flags rollouts-beta` registered under `flags`; beta banner on TTY | §"`cmd/flags/rollouts/` Cobra subtree" + §"Beta Banner Copy & Placement" |
| FOUND-03 | Versioned JSON output envelope `schemaVersion: "rollouts.v1beta1"`, `kind`, `data`, `error` | §"JSON Envelope: Concrete Go Types" |
| FOUND-04 | Documented `error.code` taxonomy (per D-01: in JSON envelope, not exit codes) | §"`error.code` Enum & Mapping" |
| FOUND-05 | Retry/idempotency layer with `go-retryablehttp` and `Idempotency-Key` UUID | §"Retry Layer Wiring" + §"`Idempotency-Key` Plumbing" |
| FOUND-06 | Re-fetch helper: every mutation does follow-up GET — interface present, exercised in Phase 2 | §"Re-fetch helper" (interface scoping only — `Get` is the building block) |
| FOUND-07 | TTY-aware output: human-readable in TTY, JSON when piped / `--output json` | §"TTY Detection & Output Defaults" |
| FOUND-08 | Errors include stable `error.code` and (where applicable) `nextAction` hint | §"`error.code` Enum & Mapping" |
| DOC-01 | `.planning/API-PAPERCUTS.md` seeded with 16 papercuts (P1–P16) and template | §"API-PAPERCUTS.md: Seeded Content" |
| LIST-01 | `ldcli flags rollouts-beta list --flag <key>` with deterministic ordering | §"Walking Skeleton Sequence" + §"`list` Command Shape" |
| LIST-02 | Output includes ID, kind, environment, state, variations, started/ended (RFC 3339), stage index | §"`Rollout` Model" + §"Field Mapping API → CLI" |
| LIST-03 | Filterable by `--environment` (per D-04, `--state` dropped); transparent pagination if API requires | §"`list` Command Shape" + §"Pagination Strategy" |
| AGENT-01 | Every command supports `--output json` regardless of TTY state | Honored via root command's existing TTY default → JSON behavior (already wired in `cmd/root.go:222-227`) |
| AGENT-02 | Exit codes follow FOUND-04 — superseded by D-01 (structured `error.code` instead) | §"Exit Code Contract (per D-01)" |
| AGENT-03 | Mutating commands return coherent response on retry — wired in Phase 1, exercised Phase 2 | §"`Idempotency-Key` Plumbing" |
| AGENT-04 | Timestamps RFC 3339 UTC; durations explicit unit-bearing strings | §"`Rollout` Model" (time conversion at DTO boundary) |
| AGENT-05 | List outputs have deterministic sort order documented in `--help` | §"`list` Command Shape" (sort spec) |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Cobra command parsing + flag binding | Command Layer (`cmd/flags/rollouts/`) | — | Mirrors existing `cmd/flags/toggle.go`; all command boundaries live in `cmd/` |
| HTTP request construction + retry | Domain Client (`internal/rollouts/`) | Infrastructure (`internal/resources/`) | New domain owns its own HTTP via `retryablehttp.Client` since the existing `resources.Client.MakeRequest` does not retry; `resources.Client` is unchanged for other commands |
| JSON envelope marshaling | Domain Client (`internal/rollouts/`) returns typed structs | Output Layer (`internal/output/`) renders | DTOs live with the domain; output layer dispatches kind-based renderers |
| Status → kind/label mapping | Domain Client (`internal/rollouts/`) | — | UI-parity logic is rollouts-specific; isolated in one file (e.g. `status_mapping.go`) so future state additions touch one place |
| TTY detection | Root command (`cmd/root.go`) — **already wired** at `cmd/root.go:222-227` | — | The persistent `--output` default already flips to `json` when `!isTerminal()`; Phase 1 does NOT re-implement this |
| Beta banner emission | Command Layer (`cmd/flags/rollouts/rollouts.go`) | — | Banner is rollouts-specific; printed at the subtree's `PersistentPreRun` |
| Plaintext table rendering | Output Layer (`internal/output/`) — new rollout-specific function | — | Mirrors existing per-resource plaintext functions |
| Error normalization (`error.code`) | Domain Client (`internal/rollouts/`) | Infrastructure (`internal/errors/`) | `internal/errors/` carries the typed error; rollouts client maps API responses → `error.code` enum |
| Analytics tracking | Command Layer (`PersistentPreRun` in `cmd/flags/rollouts/`) | `cmd/analytics/` | Existing pattern; new event name `flags-rollouts-beta-list` |

## Standard Stack

### Core (already in `go.mod` — confirmed)

| Library | Version (verified) | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/spf13/cobra` | v1.9.1 | Command/subcommand routing | [VERIFIED: ldcli go.mod] Existing baseline |
| `github.com/spf13/viper` | v1.21.0 | Flag/env/config precedence | [VERIFIED: ldcli go.mod] Existing baseline |
| `github.com/google/uuid` | v1.6.0 | Generate `Idempotency-Key` UUIDs | [VERIFIED: ldcli go.mod line 12] Already vendored (used for analytics) |
| `golang.org/x/term` | v0.33.0 | TTY detection (already in `cmd/root.go:303`) | [VERIFIED: ldcli go.mod line 34] Already a direct dep |
| `github.com/stretchr/testify` | v1.11.1 | Test assertions | [VERIFIED: ldcli go.mod] Existing |

### Net-New (single addition)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/hashicorp/go-retryablehttp` | **v0.7.8** (latest, June 2025) | HTTP retries with exponential backoff, request-body rewinding | [VERIFIED: `proxy.golang.org/.../@latest` returns v0.7.8, 2025-06-18] Used by Terraform/Vault. Note: STACK.md recommended v0.7.7; latest is v0.7.8 with no breaking changes. **Recommend v0.7.8.** |

**Installation:**
```bash
go get github.com/hashicorp/go-retryablehttp@v0.7.8
make vendor
```

**Version verification:** `proxy.golang.org/github.com/hashicorp/go-retryablehttp/@latest` → `{"Version":"v0.7.8","Time":"2025-06-18T14:25:10Z"}` [VERIFIED]

### Existing infrastructure reused (no install needed)

| Component | Purpose | Notes |
|-----------|---------|-------|
| `internal/output/output.go` `CmdOutput` / `Outputter` | JSON vs plaintext dispatch | Phase 1 adds rollouts-specific renderer; **does not replace** dispatch |
| `internal/errors/errors.go` `NewError`, `NewErrorWrapped`, `APIError` | Error type | Phase 1 wraps API errors with `error.code` mapping |
| `cmd/cliflags/flags.go` | Flag-name constants | Phase 1 appends `FlagFlag` (already exists), `EnvironmentFlag` (already exists), and adds `DetailedFlag`, `LimitFlag`, `AllFlag` |
| `cmd/analytics/analytics.go` | Analytics tracking | New event `flags-rollouts-beta-list`; uses existing `PersistentPreRun` pattern |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `go-retryablehttp` | `cenkalti/backoff/v5` | Backoff primitive only — doesn't handle body rewinding, 5xx detection, or Retry-After. Would mean reimplementing the HTTP wrapper. **Reject.** |
| `go-retryablehttp` | `avast/retry-go` | Generic retry not HTTP-specialized. Less ecosystem traction. **Reject.** |
| `golang.org/x/term.IsTerminal` | `mattn/go-isatty` | adds a new dep for capability already covered. ldcli's existing pattern in `cmd/root.go` uses `x/term`. **Reject `go-isatty`.** |
| Hand-rolled `httptest`-based mock | `gomock`/`mockgen` for `Client` interface | testify's `mock.Mock` (used in `internal/flags/mock_client.go`) is the established pattern. Use it. **Reject mockgen for rollouts.** |
| Adding rollouts to `ld-openapi.json` and using the OpenAPI generator | Hand-roll types in `internal/rollouts/models.go` | Generator templates assume `/api/v2/` paths and standard pagination; the `/internal/` URL family doesn't fit. API is unstable. **Hand-roll. [VERIFIED: ARCHITECTURE.md Anti-Pattern 1]** |

## `internal/rollouts/` Skeleton

### Recommended Project Structure

```
internal/rollouts/                       # NEW: typed client + DTOs for automated-releases API
├── client.go                            # Client interface + RolloutsClient impl + NewClient(cliVersion); retryablehttp wiring
├── models.go                            # AutomatedRelease, Stage, Event, MetricConfiguration, status enums, RolloutList, Envelope types
├── status_mapping.go                    # 13 raw statuses → 5 kind buckets + label formulation (D-02)
├── instructions.go                      # SemanticPatch + StartInstruction + StopInstruction stubs (Phase 2 fleshes Start; Phase 4 fleshes Stop)
├── errors.go                            # error.code enum, mapping from APIError.Body() → typed error
├── idempotency.go                       # Idempotency-Key header helper (UUID v4 via google/uuid)
├── mock_client.go                       # testify-based MockClient mirroring internal/flags/mock_client.go
├── client_test.go                       # httptest.NewServer round-trip tests for List + Get
├── status_mapping_test.go               # Table-driven test: 13 raw statuses → expected (kind, label) tuples
└── testdata/                            # Golden response fixtures (List + Get) captured from staging
    ├── list_progressive_in_progress.json
    ├── list_guarded_regressed.json
    └── get_guarded_completed.json

cmd/flags/rollouts/                      # NEW: command package for `flags rollouts-beta`
├── rollouts.go                          # parent cmd (returns *cobra.Command for "rollouts-beta"); beta banner via PersistentPreRun
├── list.go                              # `rollouts-beta list`
├── flags.go                             # shared flag registration (--detailed, --limit, --all, --environment, --flag)
├── plaintext.go                         # rollouts-specific plaintext table rendering
├── list_test.go                         # table-driven tests using cmd.CallCmd + MockClient
└── rollouts_test.go                     # banner-suppression test (TTY off vs on)

cmd/cliflags/flags.go                    # APPEND: DetailedFlag, LimitFlag, AllFlag constants + descriptions
cmd/root.go                              # MODIFY: APIClients adds RolloutsClient; flags subcommand gets rollouts-beta child
.planning/API-PAPERCUTS.md               # NEW: seeded with P1–P16 from ARCHITECTURE.md
```

**Structure rationale:**
- **`internal/rollouts/`** is a sibling of `internal/flags/`, `internal/environments/`, etc. Mirrors existing pattern (`.planning/codebase/ARCHITECTURE.md` "Domain Client Layer"). Hand-rolled, NOT generated.
- **`cmd/flags/rollouts/`** is a subdirectory under `cmd/flags/`. Justification: command path is `ldcli flags rollouts-beta <verb>`, so the source tree mirrors that hierarchy. Existing siblings: `cmd/flags/toggle.go`, `cmd/flags/archive.go` (flat). The rollouts subtree is large enough (multiple verbs across Phases 1–4) that a subdirectory is warranted.

### `Client` Interface (Phase 1 scope only — D-08)

```go
// internal/rollouts/client.go

package rollouts

import (
    "context"
    "net/http"
    "time"

    "github.com/hashicorp/go-retryablehttp"
)

type ListOpts struct {
    Environment string // optional; maps to filter=environmentKey:<env>
    Limit       int    // default 20 (D-05); if All=true, ignored
    All         bool   // fetch full history; may invoke pagination if API gains it (today: best-effort to API limit; warning on saturation)
}

type Client interface {
    List(ctx context.Context, accessToken, baseURI, projKey, flagKey string, opts ListOpts) (*RolloutList, error)
    Get(ctx context.Context, accessToken, baseURI, projKey, envKey, rolloutID string) (*Rollout, error)
}

type RolloutsClient struct {
    cliVersion string
    httpClient *retryablehttp.Client
}

var _ Client = RolloutsClient{}

func NewClient(cliVersion string) RolloutsClient {
    return RolloutsClient{
        cliVersion: cliVersion,
        httpClient: newRetryableClient(),
    }
}

func newRetryableClient() *retryablehttp.Client {
    c := retryablehttp.NewClient()
    c.RetryMax = 4
    c.RetryWaitMin = 500 * time.Millisecond
    c.RetryWaitMax = 8 * time.Second
    c.CheckRetry = retryablehttp.DefaultRetryPolicy // retries 5xx + network errors; never 4xx
    c.Logger = nil                                  // ldcli uses stdlib log minimally; no spam during retries
    return c
}
```

**Design notes:**
- The `httpClient` lives on the struct (not constructed per-call) so connection pooling works.
- `Logger = nil` matches ldcli's logging convention (sparse stdlib `log`).
- `RetryMax = 4` + `500ms..8s` honors D-05/CONTEXT.md envelope; total worst-case backoff sequence: 500ms, 1s, 2s, 4s, 8s (capped) = ~16s.
- `CheckRetry = DefaultRetryPolicy` retries 5xx and network errors, **never** 4xx. This is the safe default for an unstable API where 4xx means "you did it wrong" and 5xx means "try again." [CITED: hashicorp/go-retryablehttp README]

### `Rollout` Model (Phase 1 — DTOs match API exactly)

```go
// internal/rollouts/models.go

package rollouts

import "time"

// Rollout is the CLI-side representation of an AutomatedRelease.
// Field names match the API where unambiguous; renames documented in comments.
type Rollout struct {
    ID                      string                 `json:"id"`
    AccountID               string                 `json:"accountId,omitempty"`
    ProjectID               string                 `json:"projectId,omitempty"`
    EnvironmentID           string                 `json:"environmentId,omitempty"`
    EnvironmentKey          string                 `json:"environmentKey,omitempty"` // derived from path or _links; see Field Mapping
    FlagKey                 string                 `json:"flagKey"`
    Kind                    string                 `json:"kind"`                     // "guarded" | "progressive"
    OriginalVariationID     string                 `json:"originalVariationId"`
    TargetVariationID       string                 `json:"targetVariationId"`
    RandomizationUnit       string                 `json:"randomizationUnit"`
    RuleIDOrFallthrough     string                 `json:"ruleIdOrFallthrough"`

    // Status: three-field model per D-02
    Status                  string                 `json:"status"`                   // raw API value
    StatusKind              string                 `json:"kind,omitempty"`           // 5-bucket; renamed in JSON to avoid collision with Rollout.Kind → see note
    StatusLabel             string                 `json:"label,omitempty"`

    // Timestamps: API returns int64 unixMillis; CLI emits RFC 3339 UTC (AGENT-04)
    CreatedAt               time.Time              `json:"createdAt"`
    StartedAt               *time.Time             `json:"startedAt,omitempty"`
    EndedAt                 *time.Time             `json:"endedAt,omitempty"`

    LatestStageIndex        int                    `json:"latestStageIndex"`
    ExtensionDurationMillis *int64                 `json:"extensionDurationMillis,omitempty"`
    Stages                  []Stage                `json:"stages"`
    Events                  []Event                `json:"events,omitempty"`           // omitted from list view in Phase 1; present in Get for Phase 3
    MetricConfigurations    []MetricConfiguration  `json:"metricConfigurations,omitempty"`
    Links                   map[string]Link        `json:"_links,omitempty"`
}

type Stage struct {
    StageIndex      int        `json:"stageIndex"`
    Allocation      int        `json:"allocation"`        // 0–100000 (basis points; API papercut to document)
    DurationMillis  int64      `json:"durationMillis"`
    Duration        string     `json:"duration"`          // human form "15m0s" — AGENT-04 mandates unit-bearing duration
    StartedAt       *time.Time `json:"startedAt,omitempty"`
    SafeRollForward *bool      `json:"safeRollForward,omitempty"` // guarded only
}

type Event struct {
    Kind        string    `json:"kind"`
    StageIndex  int       `json:"stageIndex"`
    MetricKey   string    `json:"metricKey,omitempty"`
    Description string    `json:"description,omitempty"`
    CreatedAt   time.Time `json:"createdAt"`
}

type MetricConfiguration struct {
    MetricKey     string `json:"metricKey"`
    MinSampleSize int    `json:"minSampleSize"`
    AutoRollback  bool   `json:"autoRollback"`
    Status        string `json:"status"` // "ok" | "regressed" | "regression_dismissed"
}

type Link struct {
    Href string `json:"href"`
}

// RolloutList is the response shape for List.
type RolloutList struct {
    Items []Rollout       `json:"items"`
    Links map[string]Link `json:"_links,omitempty"`
}
```

**Important naming collision:** `Rollout` has both a top-level `Kind` field (`"guarded"|"progressive"` — API name) and the *status* `StatusKind` field (the 5-bucket lifecycle — D-02). The CLI JSON envelope MUST present them as two distinct fields. The cleanest resolution is to wrap status in a nested object on output:

```go
// Final JSON envelope shape (preferred):
//
// "data": {
//   "id": "...",
//   "kind": "guarded",            // ← rollout kind (guarded|progressive)
//   "status": {                   // ← nested status block
//     "status": "monitoring_regressed",  // raw API
//     "kind":   "regressed",             // 5-bucket
//     "label":  "Regressions detected on rule X for latency-p99"
//   },
//   ...
// }
```

This nesting:
1. Eliminates the `kind`-meaning-two-things ambiguity.
2. Matches D-02 exactly (`status`, `kind`, `label` are the three fields *of the status object*).
3. Makes the JSON readable: `data.kind` is "what kind of rollout?", `data.status.kind` is "what lifecycle bucket is it in?".

Update `Rollout`'s status fields accordingly:
```go
type Rollout struct {
    // ...
    Kind   string       `json:"kind"`    // guarded|progressive
    Status StatusBlock  `json:"status"`
    // ...
}

type StatusBlock struct {
    Status string `json:"status"` // raw API value
    Kind   string `json:"kind"`   // 5-bucket lifecycle
    Label  string `json:"label"`  // human string
}
```

**[ASSUMED]** that the planner and discuss-phase will confirm this nesting approach. The alternative — flattening with renamed keys like `lifecycleKind`/`statusLabel` — is uglier and contradicts D-02's wording ("three-field status model"). Logging this as Assumption A1 in the Assumptions Log.

### Field Mapping API → CLI

| API field (`AutomatedRelease`) | CLI field | Transformation |
|--------------------------------|-----------|----------------|
| `id` | `Rollout.ID` | passthrough |
| `flagKey` | `Rollout.FlagKey` | passthrough |
| `kind` | `Rollout.Kind` | passthrough (`"guarded"\|"progressive"`) |
| `originalVariationId` | `Rollout.OriginalVariationID` | passthrough |
| `targetVariationId` | `Rollout.TargetVariationID` | passthrough |
| `randomizationUnit` | `Rollout.RandomizationUnit` | passthrough |
| `ruleIdOrFallthrough` | `Rollout.RuleIDOrFallthrough` | passthrough |
| `status` | `Rollout.Status.Status` | passthrough; raw API enum |
| (derived) | `Rollout.Status.Kind` | 5-bucket via `mapStatusToKind(status)` |
| (derived) | `Rollout.Status.Label` | human string via `formatLabel(rollout)` |
| `createdAt` (int64 ms) | `Rollout.CreatedAt` | `time.UnixMilli(v).UTC()` |
| `startedAtMillis` (int64) | `Rollout.StartedAt` | `time.UnixMilli(v).UTC()` (nil if zero) |
| `endedAtMillis` (int64) | `Rollout.EndedAt` | `time.UnixMilli(v).UTC()` (nil if zero) |
| `latestStageIndex` | `Rollout.LatestStageIndex` | passthrough |
| `stages[].allocation` | `Stage.Allocation` | passthrough (basis points 0–100000) |
| `stages[].durationMillis` | `Stage.DurationMillis` + `Stage.Duration` | DurationMillis = raw; Duration = `time.Duration(ms*time.Millisecond).String()` |
| `_links.self.href` | `Rollout.Links["self"].Href` | passthrough |
| n/a (path-derived) | `Rollout.EnvironmentKey` | for items returned by list-by-flag (no env in path), parse from `_links.self.href` or leave empty — see Papercut P4 territory |

**Note on `environmentKey` derivation:** The list endpoint is `/internal/projects/{p}/flags/{flagKey}/automated-releases` — flag-scoped, returns rollouts across all envs. The response `AutomatedRelease` includes `environmentId` (a UUID) but **may not** include `environmentKey`. Verify against a staging fixture in Wave 0 of Phase 1; if missing, the CLI parses `environmentKey` from `_links.self.href` (path component) and documents this as a new papercut (`PC-NEW-environmentKey-missing-in-list`). [ASSUMED] — A2.

## Walking Skeleton Sequence

The minimum sequence that gets `list` working end-to-end. The planner should treat these as a single deliverable; partial completion produces nothing testable.

| Step | Deliverable | Validates |
|------|-------------|-----------|
| 1 | `go.mod` adds `go-retryablehttp@v0.7.8`; `make vendor` | Dep wiring |
| 2 | `internal/rollouts/models.go` — types compile | Type shape |
| 3 | `internal/rollouts/status_mapping.go` — `mapStatusToKind`, `formatLabel` + unit tests pass | D-02 mapping |
| 4 | `internal/rollouts/errors.go` — `error.code` enum + `mapAPIError(body, statusCode) → typed error` | D-01 / FOUND-08 |
| 5 | `internal/rollouts/client.go` — `Client` interface + `RolloutsClient.List` + `RolloutsClient.Get` against `internal/resources/`-free retryablehttp.Client | Retry + HTTP plumbing |
| 6 | `internal/rollouts/mock_client.go` — testify mock | Test injection |
| 7 | `internal/rollouts/client_test.go` — `httptest.NewServer` round-trip tests for List + Get | End-to-end client correctness |
| 8 | `cmd/cliflags/flags.go` — append `DetailedFlag`, `LimitFlag`, `AllFlag` constants | Flag naming |
| 9 | `cmd/flags/rollouts/rollouts.go` — parent `rollouts-beta` cobra cmd + `PersistentPreRun` banner | FOUND-02 |
| 10 | `cmd/flags/rollouts/flags.go` — shared flag registration helper | DRY |
| 11 | `cmd/flags/rollouts/plaintext.go` — `renderRolloutsTable` (5-col) + `renderRolloutDetailed` | D-06 |
| 12 | `cmd/flags/rollouts/list.go` — `NewListCmd(client) *cobra.Command` + `runE` | LIST-01..03 |
| 13 | `cmd/root.go` — `APIClients.RolloutsClient`, `rollouts.NewClient(version)`, wire under `flags` subcommand | Wiring |
| 14 | `cmd/flags/rollouts/list_test.go` — `cmd.CallCmd` table-driven tests (plaintext, JSON, --detailed, --environment, error cases) | All success criteria |
| 15 | `.planning/API-PAPERCUTS.md` — seeded with P1–P16 from ARCHITECTURE.md | DOC-01 |
| 16 | `make test && make build && ./ldcli flags rollouts-beta list --flag <real-flag>` against staging | Real end-to-end |

Each step is a candidate for a separate task in PLAN.md, though steps 2–4, 5–7, and 9–11 are tightly coupled and may merge.

## Retry Layer Wiring

### Architecture decision: `retryablehttp.Client` lives **inside** `internal/rollouts/`

The `internal/rollouts/RolloutsClient` constructs and owns its own `retryablehttp.Client`. It does NOT route through `internal/resources/Client.MakeRequest`. Rationale:

1. **`resources.Client` has no retry.** Adding retry to `resources.Client` would change behavior for every existing ldcli command — out of scope and risky.
2. **`/internal/projects/...automated-releases/...` is a distinct API family.** It does not share base path semantics with `/api/v2/...`. Routing it through the same client gains nothing.
3. **Idempotency-Key header is rollouts-specific.** Adding it generically to `resources.Client` would send it on every request from every command — pollution.

The trade-off: `internal/rollouts/` re-implements a small amount of HTTP plumbing (auth header, User-Agent header, response error parsing). This is documented in §"Error response parsing" below.

**Alternative considered:** create `internal/retryhttp/` as a shared helper. Rejected for Phase 1 — no second consumer exists. Phase 2's `start` command also lives in `internal/rollouts/` and uses the same client. If a third consumer emerges, refactor.

### Retry policy spec

```go
func newRetryableClient() *retryablehttp.Client {
    c := retryablehttp.NewClient()
    c.RetryMax = 4
    c.RetryWaitMin = 500 * time.Millisecond
    c.RetryWaitMax = 8 * time.Second
    c.CheckRetry = retryablehttp.DefaultRetryPolicy
    c.Backoff = retryablehttp.DefaultBackoff       // exponential with jitter, honors Retry-After
    c.Logger = nil
    return c
}
```

**Behavior:**
- Retries on: connection errors, request timeouts, 500, 502, 503, 504, 429. **Never** retries 4xx (except 429).
- Max retries: 4 (so up to 5 total attempts).
- Backoff sequence: 500ms → 1s → 2s → 4s → 8s (capped). Total worst-case wall time: ~16s. [CITED: hashicorp/go-retryablehttp DefaultBackoff source]
- `DefaultBackoff` honors `Retry-After` response headers when present.
- Request body rewinding is automatic for POST/PATCH — important for Phase 2 mutations, but Phase 1 uses only GET (still safe).

### Request flow inside `RolloutsClient.List`

```go
func (c RolloutsClient) List(ctx context.Context, accessToken, baseURI, projKey, flagKey string, opts ListOpts) (*RolloutList, error) {
    path := fmt.Sprintf("%s/internal/projects/%s/flags/%s/automated-releases",
        strings.TrimRight(baseURI, "/"),
        url.PathEscape(projKey),
        url.PathEscape(flagKey),
    )

    q := url.Values{}
    if opts.Environment != "" {
        q.Set("filter", "environmentKey:"+opts.Environment) // Papercut P2: only first element honored
    }
    limit := opts.Limit
    if limit <= 0 {
        limit = 20 // D-05 default
    }
    if opts.All {
        limit = 1000 // best-effort to API limit; Papercut P3 (no pagination)
    }
    q.Set("limit", strconv.Itoa(limit))

    req, err := retryablehttp.NewRequestWithContext(ctx, "GET", path+"?"+q.Encode(), nil)
    if err != nil {
        return nil, errors.NewErrorWrapped("failed to build request", err)
    }
    req.Header.Set("Authorization", accessToken)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
    // No Idempotency-Key on GETs — it's a mutation-only header (Phase 2 wires it on PATCH/POST)

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, mapTransportError(err) // network failure after retries exhausted
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, errors.NewErrorWrapped("failed to read response body", err)
    }

    if resp.StatusCode >= 400 {
        return nil, mapAPIError(body, resp.StatusCode) // → typed error with error.code
    }

    var raw rawRolloutList // raw API shape with int64 timestamps
    if err := json.Unmarshal(body, &raw); err != nil {
        return nil, errors.NewErrorWrapped("failed to parse response", err)
    }

    return raw.toRolloutList(), nil // converts timestamps, derives Status block
}
```

**Note on `rawRolloutList` vs `RolloutList`:** The raw API shape uses `int64` unix-millis for timestamps. The CLI shape uses `time.Time` (so JSON marshals to RFC 3339 — AGENT-04). The DTO boundary lives in `models.go` with explicit `raw*` types and `.toRolloutList()` / `.toRollout()` converters. This is the "thin DTO layer" pitfall #1 prevention.

## `Idempotency-Key` Plumbing

### Phase 1: wired but not exercised

Phase 1 has no mutations to exercise the `Idempotency-Key` header. The infrastructure is still wired so Phase 2's `start` and Phase 4's `stop`/`dismiss` get the header for free.

**File:** `internal/rollouts/idempotency.go`

```go
package rollouts

import (
    "github.com/google/uuid"
    "net/http"
)

// SetIdempotencyKey attaches an Idempotency-Key header to a mutation request.
// If key is empty, generates a UUIDv4. Returns the effective key for logging/echo.
func SetIdempotencyKey(req *http.Request, key string) string {
    if key == "" {
        key = uuid.NewString()
    }
    req.Header.Set("Idempotency-Key", key)
    return key
}
```

**Internal usage (Phase 2+):**
```go
// in RolloutsClient.Start (Phase 2):
req, _ := retryablehttp.NewRequest("PATCH", url, body)
effectiveKey := SetIdempotencyKey(req.Request, opts.IdempotencyKey) // opts is empty-string → UUID generated
// effectiveKey is echoed back in the JSON envelope's meta.idempotencyKey field
```

**Recommendation for `--idempotency-key` user-facing flag:** **DEFER TO PHASE 2.** Rationale:
- Phase 1 has no mutations; the flag would do nothing.
- Adding the flag in Phase 1 means documenting a flag with no observable behavior — confusing.
- Phase 2 introduces `start`, the first command where idempotency matters; the flag debuts there.

The header is sent on every mutation regardless; the user-facing flag merely lets the caller pin a specific key for deterministic retries.

## TTY Detection & Output Defaults

### Existing infrastructure — DO NOT re-implement

ldcli's root command already wires TTY detection:

```go
// cmd/root.go:222-227 (EXISTING — Phase 1 changes nothing here)
defaultOutput := "plaintext"
if !forceTTYDefaultOutput(getenv) && !isTerminal() {
    defaultOutput = "json"
}
cmd.PersistentFlags().StringP(cliflags.OutputFlag, "o", defaultOutput, cliflags.OutputFlagDescription)
```

Key behaviors already present:
- `--output` defaults to `plaintext` when stdout is a TTY.
- `--output` defaults to `json` when stdout is NOT a TTY (piped, CI, agent).
- `FORCE_TTY` / `LD_FORCE_TTY` env vars override.
- `cliflags.GetOutputKind(cmd)` is the canonical accessor inside `RunE` (gives precedence to `--json` over `--output`).

**Phase 1 implication:** Cobra commands under `rollouts-beta` call `cliflags.GetOutputKind(cmd)` exactly like `cmd/flags/toggle.go:81` does. No new TTY logic in Phase 1.

### Where TTY check happens for **banner/spinner** decisions

The output-kind check above handles stdout dispatch. For the **beta banner** (which writes to *stderr*), the check is independent. Pattern:

```go
// in cmd/flags/rollouts/rollouts.go (PersistentPreRun):
import "golang.org/x/term"

func shouldPrintBanner(cmd *cobra.Command) bool {
    if cliflags.GetOutputKind(cmd) == "json" {
        return false // never print banner in JSON mode
    }
    // stderr-side TTY check (banner goes to stderr)
    return term.IsTerminal(int(os.Stderr.Fd()))
}
```

The `cmd.ErrOrStderr()` accessor is used for writing (test-friendly). The `term.IsTerminal` check uses the actual file descriptor.

## Beta Banner Copy & Placement

### Copy (recommended)

```
⚠ flags rollouts-beta is unstable; the command surface and JSON schema may change.
  Pin to ldcli vX.Y.Z for production use. See: <docs URL>
```

(Replace `vX.Y.Z` with the current version via `cmd.Root().Version` at runtime.)

Per CONTEXT.md's "Discretion" — researcher recommends:
- One leading `⚠` U+26A0 character (visible in any locale; degrades to `?` if terminal can't render — acceptable since the banner is human-only).
- Two lines, second line indented for visual hierarchy.
- No ANSI color (keeps the banner readable on non-color terminals; CI logs are cleaner).

### Placement

Beta banner prints **once per command invocation**, at the beginning, on **stderr**.

```go
// cmd/flags/rollouts/rollouts.go
func NewRolloutsCmd(client rollouts.Client) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "rollouts-beta",
        Short: "Manage automated rollouts (beta)",
        Long:  `Manage guarded and progressive automated rollouts on feature flags.

THIS COMMAND TREE IS BETA. The surface and output schema may change between releases.`,
        PersistentPreRun: func(c *cobra.Command, args []string) {
            if shouldPrintBanner(c) {
                fmt.Fprintln(c.ErrOrStderr(), betaBanner(c.Root().Version))
            }
        },
    }
    cmd.AddCommand(NewListCmd(client))
    return cmd
}
```

Suppressed when:
- `--output json` is set
- stderr is NOT a TTY (piped, CI)
- Help is being shown (Cobra short-circuits `PersistentPreRun` before help — verified by existing pattern)

## JSON Envelope: Concrete Go Types

### Envelope shape (per FOUND-03 + D-07)

Every command emits exactly one envelope object on stdout (NDJSON deferred to Phase 3's `--watch`).

```go
// internal/rollouts/models.go

type Envelope struct {
    SchemaVersion string      `json:"schemaVersion"`           // "rollouts.v1beta1"
    Kind          string      `json:"kind"`                    // "RolloutList" | "Rollout" | "Error"
    Data          interface{} `json:"data,omitempty"`
    Error         *EnvelopeError `json:"error,omitempty"`
    Meta          *EnvelopeMeta  `json:"meta,omitempty"`
}

type EnvelopeError struct {
    Code       string                 `json:"code"`               // stable enum, see error.code §
    Message    string                 `json:"message"`            // human-readable
    NextAction string                 `json:"nextAction,omitempty"`
    Details    map[string]interface{} `json:"details,omitempty"`  // optional structured detail
}

type EnvelopeMeta struct {
    FetchedAt        time.Time `json:"fetchedAt,omitempty"`        // RFC 3339 UTC
    UIUrl            string    `json:"uiURL,omitempty"`            // permalink to LD UI
    Warnings         []string  `json:"warnings,omitempty"`         // non-fatal issues (e.g., "list saturated at 20 items; use --all for full history")
    AvailableActions []string  `json:"availableActions,omitempty"` // populated in Phase 2+ for single-rollout responses
}

const SchemaVersionV1Beta1 = "rollouts.v1beta1"
```

**Per D-07:** `Data` is the **full** rollout shape regardless of `--detailed`. `--detailed` affects plaintext rendering only.

### Example envelope for `list`

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "RolloutList",
  "data": {
    "items": [
      {
        "id": "abc-123",
        "flagKey": "checkout-v2",
        "kind": "guarded",
        "originalVariationId": "...",
        "targetVariationId": "...",
        "randomizationUnit": "user",
        "ruleIdOrFallthrough": "fallthrough",
        "status": {
          "status": "monitoring_regressed",
          "kind": "regressed",
          "label": "Regressions detected on default rule for latency-p99"
        },
        "createdAt": "2026-05-10T15:00:00Z",
        "startedAt": "2026-05-10T15:00:05Z",
        "endedAt": null,
        "latestStageIndex": 1,
        "stages": [
          {"stageIndex": 0, "allocation": 5000, "durationMillis": 900000, "duration": "15m0s", "startedAt": "2026-05-10T15:00:05Z"},
          {"stageIndex": 1, "allocation": 25000, "durationMillis": 3600000, "duration": "1h0m0s", "startedAt": "2026-05-10T15:15:05Z"}
        ]
      }
    ]
  },
  "meta": {
    "fetchedAt": "2026-05-12T12:00:00Z"
  }
}
```

### Example envelope for error

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "not_found",
    "message": "Feature flag \"checkout-v2\" not found in project \"prod\"",
    "nextAction": "Verify --flag and --project values, then retry"
  }
}
```

Exit code is `1` regardless (per D-01). The envelope is written to stdout.

## `error.code` Enum & Mapping

Per D-01, all error signalling is via the JSON envelope. Phase 1 introduces the enum and maps known HTTP responses to it.

### Enum (Phase 1)

```go
// internal/rollouts/errors.go

const (
    ErrCodeUnauthorized       = "unauthorized"           // HTTP 401
    ErrCodeForbidden          = "forbidden"              // HTTP 403
    ErrCodeNotFound           = "not_found"              // HTTP 404 (flag, project, rollout missing)
    ErrCodeConflict           = "conflict"               // HTTP 409 (e.g. rollout already running — Phase 2)
    ErrCodeBadRequest         = "bad_request"            // HTTP 400 (invalid filter, etc.)
    ErrCodeRateLimited        = "rate_limited"           // HTTP 429
    ErrCodeUpstreamUnavailable = "upstream_unavailable"  // HTTP 5xx after retries
    ErrCodeNetworkError       = "network_error"          // transport-level failure after retries
    ErrCodeBetaGateClosed     = "beta_gate_closed"       // release-guardian dogfood flag off (Phase 2 surface; here for completeness)
    ErrCodeUnknownUpstream    = "unknown_upstream"       // 4xx/5xx with body we don't recognize — last-resort sentinel
)

type Error struct {
    Code       string
    Message    string
    NextAction string
    StatusCode int
    RawBody    []byte
}

func (e *Error) Error() string { return e.Message }
```

### Mapping function

```go
// internal/rollouts/errors.go

func mapAPIError(body []byte, statusCode int) error {
    e := &Error{StatusCode: statusCode, RawBody: body}

    // Try to extract structured error from body (gonfalon returns {"code": "...", "message": "..."})
    var apiBody struct {
        Code    string `json:"code"`
        Message string `json:"message"`
    }
    _ = json.Unmarshal(body, &apiBody) // best-effort

    switch {
    case statusCode == http.StatusUnauthorized:
        e.Code = ErrCodeUnauthorized
        e.Message = "Access token rejected by LaunchDarkly"
        e.NextAction = "Run `ldcli config --set access-token=<token>` or `ldcli login`"
    case statusCode == http.StatusForbidden:
        e.Code = ErrCodeForbidden
        e.Message = "Access denied; token may lack required scope"
        e.NextAction = "Verify role includes `viewProject` on the target project"
    case statusCode == http.StatusNotFound:
        e.Code = ErrCodeNotFound
        e.Message = humanizeNotFound(apiBody.Message)
        e.NextAction = "Verify --flag, --project, and --environment values"
    case statusCode == http.StatusConflict:
        e.Code = ErrCodeConflict
        e.Message = apiBody.Message
    case statusCode == http.StatusBadRequest:
        e.Code = ErrCodeBadRequest
        e.Message = apiBody.Message
    case statusCode == http.StatusTooManyRequests:
        e.Code = ErrCodeRateLimited
        e.Message = "Rate limited by LaunchDarkly"
        e.NextAction = "Retry after the Retry-After interval"
    case statusCode >= 500:
        e.Code = ErrCodeUpstreamUnavailable
        e.Message = fmt.Sprintf("LaunchDarkly returned %d %s", statusCode, http.StatusText(statusCode))
        e.NextAction = "Retry; if persistent, check LaunchDarkly status page"
    default:
        e.Code = ErrCodeUnknownUpstream
        e.Message = fmt.Sprintf("Unexpected upstream response: %d", statusCode)
    }
    return e
}

func mapTransportError(err error) error {
    return &Error{
        Code:       ErrCodeNetworkError,
        Message:    fmt.Sprintf("Network error: %v", err),
        NextAction: "Check connectivity and retry",
    }
}
```

### Relationship to `internal/errors`

The rollouts-specific `Error` type is a sibling of `internal/errors.Error`. Reasons to NOT extend `internal/errors.Error`:
- `error.code` enum is rollouts-specific (no other ldcli surface has a structured enum).
- Other commands' error handling continues unchanged.

The rendering path is: rollouts `Error` → marshaled into `Envelope.Error` field → written to stdout via `output.CmdOutput`. Exit code is still 1 (D-01).

For interop with existing `errors.As`:
```go
func (e *Error) Is(target error) bool { _, ok := target.(*Error); return ok }
```

Callers in `cmd/flags/rollouts/list.go` use `errors.As(err, &rolloutErr)` to unwrap.

## Status Mapping: 13 UI States → 5 Kinds

Per CONTEXT.md `<specifics>`, here is the canonical table for `status_mapping.go`:

| Raw API `status` | Sub-condition | `kind` | `label` template |
|------------------|---------------|--------|------------------|
| `not_started` | — | `active` | `"Monitoring {rule}"` |
| `waiting` | — | `active` | `"Monitoring {rule}"` |
| `in_progress` | min sample size NOT reached (guarded) | `active` | `"Monitoring {rule} for regressions… (not enough data)"` |
| `in_progress` | min sample size reached (guarded) | `active` | `"Monitoring {rule} for regressions…"` |
| `in_progress` | extension active (guarded) | `active` | `"Monitoring extended by {duration}"` |
| `in_progress` | progressive (no metrics) | `active` | `"Monitoring {rule}"` |
| `monitoring_regressed` | — | `regressed` | `"Regressions detected on {rule} for {metric names}"` |
| `monitoring_stopped` | — | `paused` | `"{rule} paused at {N}%: regressions detected for {metric names}"` |
| `srm_stopped` | — | `paused` | `"{rule} paused at {N}%: sample ratio mismatch detected"` |
| `completed` | — | `completed` | `"Monitoring completed on {rule}"` |
| `manually_completed` | — | `completed` | `"{rule} rolled forward manually"` |
| `manually_reverted` | — | `reverted` | `"{rule} rolled back manually"` |
| `reverted` | insufficient sample size (detected via events) | `reverted` | `"{rule} rolled back due to insufficient sample size"` |
| `reverted` | SRM event in events list | `reverted` | `"{rule} rolled back automatically"` |
| `reverted` | regression event in events list | `reverted` | `"{rule} rolled back automatically after detecting a regression for {metric names}"` |
| `archived` | — | `paused` | `"Monitoring of {rule} stopped early"` |

**Disambiguation rules for sub-conditions:**

- **`in_progress` variants** (4 sub-cases) — discriminated by:
  - `kind == "progressive"` → progressive label, no "for regressions" suffix
  - `extensionDurationMillis != nil && currentStage.StartedAt.Add(currentStage.Duration).Before(now)` → extended
  - `MetricConfigurations[*].Status` shows `minSampleSize` not yet hit → "not enough data"
  - Otherwise → standard guarded monitoring

- **`reverted` variants** (3 sub-cases) — discriminated by inspecting `Events[*].Kind`:
  - `events` contains `srm_detected` (and not `regression_detected`) → "SRM" label
  - `events` contains `regression_detected` → regression label (extract metric names from event)
  - Otherwise (most likely `minimum_monitoring_window_expired` without enough samples) → "insufficient sample size"

**`{rule}` placeholder:** derived from `RuleIDOrFallthrough` — `"fallthrough"` → `"the default rule"`; otherwise `"rule {ruleId}"`. **[ASSUMED]** that the CLI does not fetch the rule name (separate API call); the planner may opt to fetch rule names if it becomes a UX problem. A3.

**`{N}%` placeholder:** computed as `currentStage.Allocation / 1000` (basis points → percent).

**`{metric names}` placeholder:** extracted from `MetricConfigurations[*].MetricKey` where `Status == "regressed"`. Comma-separated. If none found (e.g., for `monitoring_regressed` on a regression event in the events array), fall back to extracting from `Events[*].MetricKey` where `Events[*].Kind == "regression_detected"`.

### Function signatures

```go
// internal/rollouts/status_mapping.go

func DeriveStatusBlock(r *Rollout) StatusBlock {
    return StatusBlock{
        Status: r.Status.Status,                          // raw passthrough — but see Note
        Kind:   mapStatusToKind(r),
        Label:  formatLabel(r),
    }
}

func mapStatusToKind(r *Rollout) string {
    switch r.Status.Status {
    case "not_started", "waiting", "in_progress":
        return "active"
    case "monitoring_regressed":
        return "regressed"
    case "monitoring_stopped", "srm_stopped", "archived":
        return "paused"
    case "completed", "manually_completed":
        return "completed"
    case "reverted", "manually_reverted":
        return "reverted"
    default:
        return "active" // safest fallback — log as papercut
    }
}

func formatLabel(r *Rollout) string {
    // 16-case switch as documented in the table; see status_mapping.go in implementation
}
```

**Note:** `DeriveStatusBlock` is called by the DTO converter `raw.toRollout()`. The raw API gives us `status` (string) directly; CLI wraps it into the nested `StatusBlock`.

## `list` Command Shape

### Signature

```bash
ldcli flags rollouts-beta list --flag <key> [--environment <env>] [--limit N] [--all] [--detailed]
```

### Flag spec

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--flag` | string | yes | — | Feature flag key (reuses `cliflags.FlagFlag`) |
| `--project` | string | yes (persistent or via config) | from config | Project key (reuses `cliflags.ProjectFlag`) |
| `--access-token` | string | yes (persistent or env) | from config / env | Reuses existing |
| `--environment` | string | no | — | Filter rollouts to this env (reuses `cliflags.EnvironmentFlag`) |
| `--limit` | int | no | 20 | Max rollouts to return (D-05) |
| `--all` | bool | no | false | Return full history; overrides `--limit` (D-05) |
| `--detailed` | bool | no | false | Plaintext only: expanded columns (D-06); ignored in JSON (D-07) |
| `--output` | string | no | TTY-dependent | Inherited from root; `--json` shorthand also inherited |

**Help text snippet for `--help`:**
```
Rollouts are returned in reverse-chronological order by createdAt timestamp,
with rollout ID as the deterministic tiebreaker.

By default, the 20 most recent rollouts are returned. Use --all to fetch the
full history (subject to upstream API limits — see API-PAPERCUTS.md PC-003).
```

(Satisfies AGENT-05 — "deterministic sort order documented in `--help`".)

### Sorting

The API does not guarantee order ([VERIFIED: ARCHITECTURE.md §"LIST"]). The CLI sorts client-side:
1. Primary: `CreatedAt` descending (newest first).
2. Tiebreaker: `ID` ascending (deterministic).

```go
sort.Slice(items, func(i, j int) bool {
    if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
        return items[i].CreatedAt.After(items[j].CreatedAt)
    }
    return items[i].ID < items[j].ID
})
```

### Pagination Strategy

[VERIFIED: ARCHITECTURE.md Papercut P3] — the API has no `offset` or cursor; only `limit`. Phase 1 strategy:
- `--limit 20` (default) → request `limit=20`.
- `--all` → request `limit=1000` (best-effort; we believe upstream caps somewhere).
- If response `len(items) == requested_limit`, surface a warning in `meta.warnings`: `"List returned exactly {N} items; results may be truncated upstream (see API-PAPERCUTS.md PC-003)"`.

Transparent multi-call pagination is **deferred** until the API exposes cursors. Phase 1 makes one HTTP call.

### Plaintext default (5-column table — D-06)

```
ID                                    KIND         ENVIRONMENT  STATE              STARTED
abc-123                               guarded      production   regressed          2026-05-10T15:00:05Z
def-456                               progressive  staging      active             2026-05-09T11:30:00Z
ghi-789                               guarded      production   completed          2026-05-08T08:00:00Z
```

5 columns: `ID`, `KIND` (rollout kind), `ENVIRONMENT`, `STATE` (the lifecycle `status.kind`), `STARTED` (RFC 3339).

### Plaintext `--detailed` (D-06)

```
ID:           abc-123
Kind:         guarded
Environment:  production
State:        regressed
Label:        Regressions detected on default rule for latency-p99
Started:      2026-05-10T15:00:05Z
Ended:        —
Stage:        1 of 3 (25%)
Target var:   {targetVariationId}
Original var: {originalVariationId}
Raw status:   monitoring_regressed
---
ID:           def-456
...
```

Multi-record records separated by `---`.

### JSON (always full per D-07)

See §"JSON Envelope: Concrete Go Types" — `--detailed` has no effect on JSON output.

## Exit Code Contract (per D-01)

Phase 1 inherits ldcli's existing behavior:
- Success → exit 0.
- Any error → exit 1 (Cobra default; `cmd/root.go:331` calls `os.Exit(1)`).

There is **no** numeric taxonomy. All error signal lives in the `error.code` field of the JSON envelope. The envelope is emitted on **stdout** even in error cases (so agents always parse stdout, never stderr — Pitfall #7 prevention).

Future exception, locked at roadmap level: Phase 3's `--watch` will use exit `130` on SIGINT, which is emitted by `signal.NotifyContext` (Go stdlib convention, not by ldcli code).

## API-PAPERCUTS.md: Seeded Content

### Template (per DOC-01)

```markdown
# API Papercuts: gonfalon `automated-releases`

> Living document of known gaps, awkward shapes, and missing features in the upstream
> API. Each active workaround in code carries a `// PAPERCUT: PC-NNN` comment that
> cross-references the anchor below. When the API team resolves an item, move its
> entry to `## Resolved` with a date and delete the workaround in the same PR.

**Last updated:** 2026-05-12
**Active count:** 16
**Resolved count:** 0

## Active Index

| Anchor | One-line | Discovered | Affected commands |
|--------|----------|------------|-------------------|
| PC-001 | Start mutation returns updated FeatureFlag, not new AutomatedRelease | 2026-05-11 | start (Phase 2) |
| PC-002 | `filter` accepts array but only honors element [0] | 2026-05-11 | list |
| PC-003 | No pagination on list endpoint (limit only, no offset/cursor) | 2026-05-11 | list |
| PC-004 | GET-by-ID requires environment in path despite globally-unique UUID | 2026-05-11 | status (Phase 3) |
| PC-005 | Status enum mixes lifecycle + action-required + meta states | 2026-05-11 | list, status |
| PC-006 | `waiting` status semantics undocumented | 2026-05-11 | status (Phase 3), watch |
| PC-007 | `dismiss_regression` returns 204 instead of new state | 2026-05-11 | dismiss (Phase 4) |
| PC-008 | No dedicated preflight validation endpoint | 2026-05-11 | start (Phase 2) |
| PC-009 | RBAC errors don't name the missing action | 2026-05-11 | all |
| PC-010 | Metric monitoring preferences in parallel side-car map | 2026-05-11 | start (Phase 2) |
| PC-011 | `/internal/` URL prefix is access-control-irrelevant | 2026-05-11 | observability |
| PC-012 | `kind` vs `releaseKind` vs `rolloutType` terminology mismatch | 2026-05-11 | start (Phase 2) |
| PC-013 | `controlVariationId` (legacy) → `originalVariationId` (unified) inconsistency | 2026-05-11 | start, status |
| PC-014 | Stage durations only as int64 millis (no Go-style duration string) | 2026-05-11 | start (Phase 2), status |
| PC-015 | No documented status enum transitions (state machine implicit) | 2026-05-11 | watch (Phase 3) |
| PC-016 | `recommended-duration` requires `finalStageAllocation` even for progressive | 2026-05-11 | start (Phase 2) |

## Entries

### PC-001 — Start mutation returns wrong resource

**Title:** `startAutomatedRelease` returns updated FeatureFlag, not new AutomatedRelease
**Discovered:** 2026-05-11 (architecture research)
**API behavior:** `PATCH /api/v2/flags/{p}/{flagKey}` with a `startAutomatedRelease` instruction returns the standard updated `FeatureFlag` resource. The newly-created AutomatedRelease ID is nowhere in the response.
**CLI workaround:** After every `start` call, issue a follow-up `GET /internal/projects/{p}/flags/{flagKey}/automated-releases?filter=environmentKey:{env}&limit=1` and return the first item. Doubles round-trips for the most common mutation.
**What we'd prefer:** Return `{ flag: FeatureFlag, automatedRelease: AutomatedRelease }` for these two instruction kinds, or add an `X-LD-AutomatedReleaseId` response header.
**Status:** active
**Removal criteria:** API returns rollout ID either in body or header; CLI integration test confirms; the follow-up GET in `RolloutsClient.Start` is deleted.

### PC-002 — `filter` array drops elements beyond [0]
... (full text per ARCHITECTURE.md §P2)

### PC-003 — No pagination
... (full text per ARCHITECTURE.md §P3)

### PC-004 .. PC-016
... (one section per papercut, each ~10 lines, full text in ARCHITECTURE.md §"API Papercut Catalog")

## Resolved

*(empty)*
```

### Phase 1 papercut annotations in code

Of the 16 papercuts, these surface in Phase 1 code (need `// PAPERCUT: PC-NNN` comments at the workaround site):

| Papercut | Phase 1 site | Comment placement |
|----------|--------------|-------------------|
| **PC-002** | `RolloutsClient.List` building the filter string | Above `q.Set("filter", "environmentKey:"+opts.Environment)` |
| **PC-003** | `RolloutsClient.List` handling `--all` and saturation detection | Above the `if opts.All` branch + the saturation-warning emission |
| **PC-005** | `status_mapping.go` 13 → 5 mapping function | Doc comment on `mapStatusToKind` |
| **PC-011** | `RolloutsClient.List` building the `/internal/...` URL | Above the `path := fmt.Sprintf(...)` line |
| **PC-013** | DTO converter `raw.toRollout` if any legacy field appears | Wherever a renamed field is touched |
| **PC-014** | `Stage.Duration` derivation in `raw.toStage` | Above the `time.Duration(...).String()` call |

Other papercuts (PC-001, PC-004, PC-007, PC-008, PC-009, PC-010, PC-012, PC-015, PC-016) surface in Phase 2–4 code.

## Testing Approach

### Test patterns to mirror

| New test file | Mirror | Pattern |
|---------------|--------|---------|
| `internal/rollouts/client_test.go` | `internal/resources/client_test.go` | `httptest.NewServer` returning canned JSON; assert URL path, headers, query string, parsed response |
| `internal/rollouts/status_mapping_test.go` | n/a — table-driven only | `[]struct{ raw Rollout; wantKind, wantLabel string }`; assert each case |
| `cmd/flags/rollouts/list_test.go` | `cmd/flags/toggle_test.go` | `cmd.CallCmd(t, APIClients{RolloutsClient: mockClient}, ...)`; assert stdout matches expected plaintext or JSON |
| `cmd/flags/rollouts/rollouts_test.go` | n/a — banner-only | `t.Run("suppresses banner with --output json"...)` and `t.Run("suppresses banner when stderr is not TTY"...)` |
| `internal/rollouts/testdata/*.json` | n/a — golden fixtures | Captured from staging during Wave 0 manual validation; checked into repo |

### Mock pattern

The `internal/flags/mock_client.go` testify-mock pattern is the precedent (verified in CLAUDE.md and `.planning/codebase/CONVENTIONS.md`):

```go
// internal/rollouts/mock_client.go
package rollouts

import (
    "context"
    "github.com/stretchr/testify/mock"
)

type MockClient struct {
    mock.Mock
}

var _ Client = &MockClient{}

func (c *MockClient) List(ctx context.Context, accessToken, baseURI, projKey, flagKey string, opts ListOpts) (*RolloutList, error) {
    args := c.Called(accessToken, baseURI, projKey, flagKey, opts)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*RolloutList), args.Error(1)
}

func (c *MockClient) Get(ctx context.Context, accessToken, baseURI, projKey, envKey, rolloutID string) (*Rollout, error) {
    args := c.Called(accessToken, baseURI, projKey, envKey, rolloutID)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*Rollout), args.Error(1)
}
```

Note: this differs from `internal/flags/MockClient` which returns `[]byte`. The rollouts mock returns typed structs because the rollouts `Client` interface returns typed structs (not `[]byte`). The reason: rollouts' DTO conversion happens inside `RolloutsClient`, not in the command handler. The command handler receives `*Rollout` / `*RolloutList` ready-to-render.

### `cmd.CallCmd` test harness — `APIClients` must include `RolloutsClient`

The existing harness (referenced by `cmd/flags/toggle_test.go:34`) accepts an `APIClients` struct. Phase 1 adds `RolloutsClient rollouts.Client` to that struct in `cmd/root.go`:

```go
// cmd/root.go (MODIFICATION)
type APIClients struct {
    DevClient          dev_server.Client
    EnvironmentsClient environments.Client
    FlagsClient        flags.Client
    MembersClient      members.Client
    ProjectsClient     projects.Client
    ResourcesClient    resources.Client
    RolloutsClient     rollouts.Client      // NEW
}
```

And the construction in `Execute()`:
```go
clients := APIClients{
    // ... existing
    RolloutsClient: rollouts.NewClient(version),
}
```

The wiring inside `NewRootCommand`:
```go
// in the for-loop that finds the existing "flags" subcommand:
if c.Name() == "flags" {
    c.AddCommand(flagscmd.NewToggleOnCmd(clients.ResourcesClient))
    c.AddCommand(flagscmd.NewToggleOffCmd(clients.ResourcesClient))
    c.AddCommand(flagscmd.NewArchiveCmd(clients.ResourcesClient))
    c.AddCommand(rolloutscmd.NewRolloutsCmd(clients.RolloutsClient)) // NEW
}
```

### Test cases (list command — minimum coverage)

| Test name | Behavior verified | Requirement |
|-----------|-------------------|-------------|
| `succeeds with plaintext output` | Default `--output` in TTY shows 5-col table | D-06, FOUND-07 |
| `succeeds with JSON output` | `--output json` emits envelope with `schemaVersion: "rollouts.v1beta1"`, full fields | FOUND-03, D-07, AGENT-01 |
| `succeeds with --json shorthand` | `--json` flag equivalent to `--output json` | AGENT-01 |
| `succeeds with --detailed` | Plaintext shows expanded record format; JSON unchanged | D-06, D-07 |
| `filters by --environment` | URL contains `filter=environmentKey:<env>` | LIST-03 |
| `respects --limit` | URL contains `limit=<N>`; default 20 | LIST-01, D-05 |
| `--all sends large limit` | URL contains `limit=1000` (best-effort) | D-05, PC-003 |
| `surfaces saturation warning when len(items) == limit` | `meta.warnings` includes the truncation hint | PC-003 |
| `sorts client-side by createdAt desc, id asc` | API returns unsorted; CLI emits sorted | LIST-01, AGENT-05 |
| `propagates 401 as error.code unauthorized` | JSON error envelope with code = "unauthorized" + nextAction | D-01, FOUND-08 |
| `propagates 404 as error.code not_found` | JSON error envelope with code = "not_found" | D-01, FOUND-08 |
| `propagates 5xx as error.code upstream_unavailable after retries` | Retried 4 times; final error envelope | FOUND-05, FOUND-08 |
| `suppresses beta banner with --output json` | stderr does NOT contain "rollouts-beta is unstable" | FOUND-02 |
| `prints beta banner on TTY` | stderr contains banner (with FORCE_TTY shim in test) | FOUND-02 |
| `status mapping table` (in `status_mapping_test.go`) | Each of 13 raw statuses + sub-conditions produces expected `kind`/`label` | D-02 |

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTTP retry with exponential backoff | Custom retry loop on `net/http` | `hashicorp/go-retryablehttp@v0.7.8` | Handles body rewinding, Retry-After, 5xx detection, context cancellation. Custom loops always miss one. |
| UUID generation for `Idempotency-Key` | Custom UUID code | `google/uuid` (already vendored) | Already in `go.mod`; one line. |
| TTY detection | Custom isatty | `golang.org/x/term.IsTerminal` (already used in `cmd/root.go`) | Established pattern in this codebase. |
| Output format dispatch | New dispatch table | `internal/output/CmdOutput` + `Outputter` interface | Existing pattern; extend with rollouts-specific renderer, don't replace |
| Flag-name constants | Inline string literals | `cmd/cliflags/flags.go` constants | Existing pattern; allows Viper env-var binding (`LD_DETAILED`, etc.) |
| Mock generation | Hand-write mocks from scratch | `testify/mock` (already used in `internal/flags/mock_client.go`) | Established pattern; mockgen used elsewhere in repo but flags package uses testify; mirror flags. |
| Error normalization | Custom error type hierarchy | Wrap existing `internal/errors.Error` + add rollouts-specific typed `Error` with `error.code` | Mirrors `errors.APIError` pattern; lets rollouts errors interop with `errors.As` |
| Status state machine | Custom enum-walker | Static table in `status_mapping.go` | The 13→5 mapping is a pure function; no state machine needed for read-only `list` |
| JSON marshaling | Custom marshaller | `encoding/json` with struct tags | Stdlib; sufficient for envelope shape |

**Key insight:** Phase 1 reuses ldcli's established patterns aggressively. The only net-new dependency is `go-retryablehttp`. Every other Phase 1 concern has a precedent in this codebase — the planner should resist the urge to invent new patterns.

## Common Pitfalls

### Pitfall 1: Implementing JSON envelope without `schemaVersion` from day one

**What goes wrong:** Phase 1 ships `{"items": [...]}` and Phase 2 needs to add `meta.uiURL` and the schema retroactively becomes "whatever struct serializes." Agents that pinned to Phase 1's shape break.

**Why it happens:** Easiest path is `json.Marshal(rolloutList)`. The envelope feels like premature abstraction.

**How to avoid:** Define `Envelope`, `EnvelopeError`, `EnvelopeMeta` types **before** writing any command. Every `RunE` builds an `Envelope{SchemaVersion: SchemaVersionV1Beta1, Kind: "RolloutList", Data: ...}` and marshals that.

**Warning signs:** JSON output that doesn't contain `schemaVersion` at the top level.

### Pitfall 2: Forgetting timestamp normalization

**What goes wrong:** API returns `int64` unix-millis. If the DTO converter is missed, the CLI emits raw `int64` in JSON instead of RFC 3339 — violates AGENT-04.

**Why it happens:** The raw API shape is convenient to unmarshal directly into types with `int64` fields. Going through a converter feels like extra work.

**How to avoid:** Two-tier types — `rawRolloutList` for unmarshaling (with `int64` timestamp fields and `*Millis` suffix), `RolloutList` for CLI output (with `time.Time` fields). `.toRolloutList()` is the only path between them. Lint rule: no `time.Time` field in raw types.

**Warning signs:** JSON output contains `"createdAt": 1715692800000` instead of `"createdAt": "2024-05-14T12:00:00Z"`.

### Pitfall 3: ANSI codes leaking to stdout in piped mode

**What goes wrong:** `cmd/flags/rollouts/plaintext.go` adds color codes for the `STATE` column. Without TTY-gating, `ldcli ... list | jq` shows `\x1b[31mregressed\x1b[0m` in the parsed output.

**Why it happens:** Adding color is "one line per render call" — easy to skip the global TTY check.

**How to avoid:** No ANSI codes at all in Phase 1 plaintext rendering. If color is desired later, gate it behind `term.IsTerminal(int(os.Stdout.Fd()))` AND `cliflags.GetOutputKind(cmd) != "json"`.

**Warning signs:** Test that pipes stdout to a buffer and asserts no `\x1b[` sequences fails.

### Pitfall 4: Two `kind` meanings colliding in JSON

**What goes wrong:** `Rollout.Kind` is `"guarded"|"progressive"` (API name). `StatusBlock.Kind` is the 5-bucket. Marshaling both at the same level with key `"kind"` creates ambiguity.

**Why it happens:** D-02 introduces a second meaning for the word "kind."

**How to avoid:** Nest the status block (see §"`Rollout` Model"). The envelope's top-level data has `"kind": "guarded"` (rollout kind) and `"status": {"kind": "regressed"}` (lifecycle bucket). [ASSUMED A1 — pending discuss-phase confirmation]

**Warning signs:** Any flattened envelope where the same key appears with two different value sets across rollouts.

### Pitfall 5: API-PAPERCUTS.md seeded as a brain dump

**What goes wrong:** The doc is created with 16 entries but no template, no anchor IDs, no "removal criteria" field. Future workarounds get appended freeform; cross-referencing breaks.

**Why it happens:** Seeding feels like a chore; cutting corners on doc format saves an hour.

**How to avoid:** Use the exact template structure in §"API-PAPERCUTS.md: Seeded Content". Each entry has the same six fields. Anchor IDs are stable. PR review checks for `// PAPERCUT: PC-NNN` matching an anchor.

**Warning signs:** Workaround code lacks `// PAPERCUT:` comment; new entries miss the "removal criteria" field; index table out of sync with sections.

### Pitfall 6: Beta banner pollutes JSON parse

**What goes wrong:** Banner prints unconditionally to stderr, an agent's `2>/dev/null` works, but an agent that captures stderr (e.g., for diagnostics) sees the banner and tries to parse it.

**Why it happens:** "stderr is for humans" assumption.

**How to avoid:** Suppress banner when `cliflags.GetOutputKind(cmd) == "json"` even on TTY. Agents using JSON output get clean stderr.

**Warning signs:** Banner shown when `--output json` is passed.

### Pitfall 7: Status `Get` returned by `--rollout-id` flag in Phase 1

**What goes wrong:** Per D-08, `Get` is in the Client interface in Phase 1, but the `list` command doesn't need it. A planner might add a `--rollout-id` flag to `list` to invoke `Get` — but that's the `status` command's job (Phase 3).

**Why it happens:** D-08 mentions `Get` as part of Phase 1 scope; easy to interpret as "expose it in `list`."

**How to avoid:** Phase 1's `list` command uses only `Client.List`. `Client.Get` exists for: (a) future use by `status` in Phase 3, (b) the Phase 2 re-fetch pattern (FOUND-06). Phase 1 tests `Get` via `client_test.go` but no command uses it.

**Warning signs:** `cmd/flags/rollouts/list.go` calls `client.Get(...)` or has a `--rollout-id` flag.

## Code Examples

### Example 1: Cobra command constructor (analog: `cmd/flags/toggle.go`)

```go
// cmd/flags/rollouts/list.go

package rollouts

import (
    "context"
    "fmt"

    "github.com/spf13/cobra"
    "github.com/spf13/viper"

    "github.com/launchdarkly/ldcli/cmd/cliflags"
    "github.com/launchdarkly/ldcli/cmd/validators"
    "github.com/launchdarkly/ldcli/internal/output"
    "github.com/launchdarkly/ldcli/internal/rollouts"
)

func NewListCmd(client rollouts.Client) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "list",
        Short: "List automated rollouts on a flag",
        Long: `List all current and past automated rollouts for a feature flag.

Rollouts are returned in reverse-chronological order by createdAt, with
rollout ID as the deterministic tiebreaker. By default the 20 most recent
are returned; use --all for full history.`,
        Args: validators.Validate(),
        RunE: runListE(client),
    }
    initListFlags(cmd)
    return cmd
}

func runListE(client rollouts.Client) func(*cobra.Command, []string) error {
    return func(cmd *cobra.Command, args []string) error {
        ctx := context.Background()
        opts := rollouts.ListOpts{
            Environment: viper.GetString(cliflags.EnvironmentFlag),
            Limit:       viper.GetInt(cliflags.LimitFlag),
            All:         viper.GetBool(cliflags.AllFlag),
        }
        list, err := client.List(ctx,
            viper.GetString(cliflags.AccessTokenFlag),
            viper.GetString(cliflags.BaseURIFlag),
            viper.GetString(cliflags.ProjectFlag),
            viper.GetString(cliflags.FlagFlag),
            opts,
        )
        if err != nil {
            return emitErrorEnvelope(cmd, err)
        }

        env := buildListEnvelope(list)
        return emitEnvelope(cmd, env)
    }
}
```

### Example 2: HTTP request with retry layer (analog: none — net-new)

```go
// internal/rollouts/client.go

func (c RolloutsClient) List(
    ctx context.Context,
    accessToken, baseURI, projKey, flagKey string,
    opts ListOpts,
) (*RolloutList, error) {
    // PAPERCUT: PC-011 — /internal/ prefix is access-control-irrelevant despite the name
    path := fmt.Sprintf("%s/internal/projects/%s/flags/%s/automated-releases",
        strings.TrimRight(baseURI, "/"),
        url.PathEscape(projKey),
        url.PathEscape(flagKey),
    )
    q := url.Values{}
    if opts.Environment != "" {
        // PAPERCUT: PC-002 — only the first filter element is honored by the API
        q.Set("filter", "environmentKey:"+opts.Environment)
    }
    limit := opts.Limit
    if limit <= 0 {
        limit = 20
    }
    if opts.All {
        // PAPERCUT: PC-003 — no cursor pagination; best-effort to API limit
        limit = 1000
    }
    q.Set("limit", strconv.Itoa(limit))

    req, err := retryablehttp.NewRequestWithContext(ctx, "GET", path+"?"+q.Encode(), nil)
    if err != nil {
        return nil, errors.NewErrorWrapped("failed to build request", err)
    }
    req.Header.Set("Authorization", accessToken)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, mapTransportError(err)
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, errors.NewErrorWrapped("failed to read response", err)
    }
    if resp.StatusCode >= 400 {
        return nil, mapAPIError(body, resp.StatusCode)
    }
    var raw rawRolloutList
    if err := json.Unmarshal(body, &raw); err != nil {
        return nil, errors.NewErrorWrapped("failed to parse response", err)
    }
    return raw.toRolloutList(), nil
}
```

### Example 3: testify mock (analog: `internal/flags/mock_client.go`)

```go
// internal/rollouts/mock_client.go
package rollouts

import (
    "context"
    "github.com/stretchr/testify/mock"
)

type MockClient struct {
    mock.Mock
}

var _ Client = &MockClient{}

func (c *MockClient) List(ctx context.Context, accessToken, baseURI, projKey, flagKey string, opts ListOpts) (*RolloutList, error) {
    args := c.Called(accessToken, baseURI, projKey, flagKey, opts)
    var list *RolloutList
    if v := args.Get(0); v != nil {
        list = v.(*RolloutList)
    }
    return list, args.Error(1)
}

func (c *MockClient) Get(ctx context.Context, accessToken, baseURI, projKey, envKey, rolloutID string) (*Rollout, error) {
    args := c.Called(accessToken, baseURI, projKey, envKey, rolloutID)
    var r *Rollout
    if v := args.Get(0); v != nil {
        r = v.(*Rollout)
    }
    return r, args.Error(1)
}
```

### Example 4: command test (analog: `cmd/flags/toggle_test.go`)

```go
// cmd/flags/rollouts/list_test.go
package rollouts_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/launchdarkly/ldcli/cmd"
    "github.com/launchdarkly/ldcli/internal/analytics"
    "github.com/launchdarkly/ldcli/internal/rollouts"
)

func TestList_JSON(t *testing.T) {
    mockClient := &rollouts.MockClient{}
    mockClient.On("List", "abcd1234", "https://app.launchdarkly.com", "test-proj", "test-flag",
        rollouts.ListOpts{Limit: 20}).
        Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
            {ID: "r-1", FlagKey: "test-flag", Kind: "guarded",
             Status: rollouts.StatusBlock{Status: "in_progress", Kind: "active", Label: "Monitoring the default rule"}},
        }}, nil)

    args := []string{
        "flags", "rollouts-beta", "list",
        "--access-token", "abcd1234",
        "--flag", "test-flag",
        "--project", "test-proj",
        "--output", "json",
    }
    out, err := cmd.CallCmd(t,
        cmd.APIClients{RolloutsClient: mockClient},
        analytics.NoopClientFn{}.Tracker(),
        args,
    )
    require.NoError(t, err)
    assert.Contains(t, string(out), `"schemaVersion":"rollouts.v1beta1"`)
    assert.Contains(t, string(out), `"kind":"RolloutList"`)
    assert.Contains(t, string(out), `"id":"r-1"`)
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Exit-code taxonomy for agent signal (FEATURES.md proposal, STACK.md sysexits alignment) | Single exit code 1, all signal in `error.code` JSON field (D-01) | 2026-05-12 (CONTEXT.md) | Phase 1 does NOT define `cmd/exit.go` or numeric error constants; planner avoids that work |
| Stack research recommended `go-retryablehttp@v0.7.7` | Latest is `v0.7.8` (June 2025; no breaking changes) | 2025-06-18 (upstream release) | Use v0.7.8 |
| Generic resource client `resources.Client.MakeRequest` for all commands | Rollouts uses its own `retryablehttp.Client` directly in `internal/rollouts/` | This phase | `resources.Client` remains unchanged; other commands unaffected |
| Bubbletea for any TUI need (vendored for quickstart) | NOT used for rollouts; explicitly rejected for future `--watch` | Stack research (2026-05-11) | Phase 3 watch will use simple alternate-screen + redraw, but that's Phase 3 |
| `mattn/go-isatty` (industry default for TTY detection) | `golang.org/x/term.IsTerminal` (already in ldcli) | Existing ldcli convention | Do not introduce `go-isatty` |

**Deprecated/outdated (do NOT do):**
- Numeric exit-code taxonomy (sysexits or sequential) — superseded by D-01.
- Adding rollouts paths to `ld-openapi.json` — anti-pattern, [VERIFIED: ARCHITECTURE.md Anti-Pattern 1].
- Reusing legacy `measured-rollouts` REST endpoints — explicitly forbidden by ARCHITECTURE.md Anti-Pattern 2.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Nested `status` block in the envelope (avoiding `kind` ambiguity) is the right shape per D-02 | "`Rollout` Model" | If flat shape is preferred, JSON output structure changes — affects every command's envelope. Low risk; pattern is clearly D-02 intent. |
| A2 | List response items may not include `environmentKey` field; CLI derives from `_links.self.href` if missing | "Field Mapping API → CLI" | If `environmentKey` IS in the response, no impact (CLI just uses it directly). If it's missing AND the link path doesn't carry it either, the CLI's `environment` column on list output would be empty. Workaround: file as new papercut. Validate in Wave 0. |
| A3 | `{rule}` placeholder in labels uses `"the default rule"` for fallthrough and `"rule {ruleId}"` for custom rules; CLI does NOT fetch rule names | "Status Mapping" | UI parity weakened (UI shows rule names). If users find rollout IDs in labels unhelpful, Phase 3 could add a rule-name lookup. Low priority for Phase 1. |
| A4 | `go-retryablehttp` v0.7.8 has no breaking changes vs. v0.7.7 (only the version STACK.md recommended) | "Standard Stack" | Highly unlikely; semver patch bump. If wrong, fall back to v0.7.7. |
| A5 | The existing `cmd.CallCmd` test harness in this repo accepts `APIClients{RolloutsClient: mockClient}` once `APIClients` is extended; no separate test-harness change is needed | "Testing Approach" | If harness needs explicit registration of new clients (beyond struct field), a small `cmd/testhelpers.go` change is added. Low risk — same pattern as adding `EnvironmentsClient` would have required. |
| A6 | The `/internal/...automated-releases` family accepts `access-token` auth with the same value ldcli already passes for `/api/v2/...` | "Retry Layer Wiring" | [VERIFIED: ARCHITECTURE.md §"Authentication / RBAC Mapping" — `EhttpWithSessionOrToken` middleware accepts account tokens]. Promoted from assumption — but reconfirm in staging during Wave 0. |
| A7 | Phase 1 does NOT need to expose `--idempotency-key` user-facing flag | "Idempotency-Key Plumbing" | If reviewers prefer the flag visible from Phase 1, add it; behavior is no-op until Phase 2 anyway. |

**If this table needs reconfirmation:** A1 is the highest-impact (changes JSON envelope shape). A2 and A3 affect output completeness. A4–A7 are lower risk.

## Open Questions

1. **`environmentKey` presence in list-by-flag response.**
   - What we know: API returns `environmentId` (UUID); `environmentKey` presence unverified.
   - What's unclear: whether the response includes `environmentKey` or only the ID.
   - Recommendation: validate against staging during Wave 0 (Step 16 of skeleton sequence). If missing, add new papercut `PC-NEW-environmentKey-missing-in-list` and derive from `_links.self.href`.

2. **Concrete rule labels (UI parity).**
   - What we know: UI shows the human-readable rule description; CLI has only `ruleIdOrFallthrough`.
   - What's unclear: whether agents/operators find rule IDs sufficient or whether rule-name lookup is needed.
   - Recommendation: Phase 1 ships with IDs in labels; defer rule-name lookup. Revisit if usage shows confusion.

3. **API saturation behavior at `limit=1000`.**
   - What we know: API supports `limit`; no documented maximum.
   - What's unclear: whether 1000 is accepted or capped server-side.
   - Recommendation: implement saturation warning when `len(items) == requested_limit`; document upstream behavior in PC-003 once validated.

4. **`raw API status` column in `--detailed` plaintext.**
   - D-06 specifies `raw API status` as a column in `--detailed`. Should the raw `status.status` appear redundantly when `status.kind` is also visible?
   - Recommendation: yes — `--detailed` is for operators debugging; show both. Plaintext is verbose by design.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Build/test | ✓ | 1.23.5 (verified via `go version`) | — |
| CGO compiler (gcc/clang) | SQLite (dev-server; irrelevant to Phase 1 binary path) | ✓ | system gcc/clang | — |
| `make` | Build/vendor/test commands | ✓ | system | — |
| `npm` (for dev-server UI) | Out-of-scope for Phase 1 | n/a | — | — |
| Network access to LaunchDarkly staging API | Manual Wave 0 end-to-end validation | assumed ✓ | — | Use recorded fixtures if staging unreachable |
| `proxy.golang.org` for fetching `go-retryablehttp@v0.7.8` | `go get` step | ✓ (verified during research: version check returned successfully) | — | Vendor module manually |

**Missing dependencies with no fallback:** none.

**Missing dependencies with fallback:** none.

## Security Domain

> Skipped — `.planning/config.json` has `workflow.nyquist_validation: false` AND no `security_enforcement` key. Phase 1 inherits the existing access-token auth surface unchanged (PROJECT.md constraint) and does not introduce new auth or persistence concerns. Token handling continues per CLAUDE.md's existing pattern: read via Viper from `--access-token` flag / `LD_ACCESS_TOKEN` env / `$XDG_CONFIG_HOME/ldcli/config.yml`.

**Phase 1 security notes (informational only):**
- Existing `access-token` is sent via `Authorization` header. New rollouts client uses the same header pattern as `internal/resources/client.go:54`.
- No new persistence (no file writes, no SQLite). 
- New `Idempotency-Key` UUID values are not sensitive — they're random per-request identifiers, safe to log.
- Beta banner copy contains no secrets. Error envelope `Details` field MUST NOT include raw request URLs with embedded tokens (`Authorization` is in the header, not URL — verified by `resources.Client` pattern).

## Pattern Analogs (for each new file)

| New file | Closest existing analog | Pattern to mirror |
|----------|-------------------------|-------------------|
| `internal/rollouts/client.go` | `internal/flags/client.go` | `Client` interface + `<Domain>Client` struct + `var _ Client = ...` compile-time assertion + `NewClient(cliVersion)` constructor |
| `internal/rollouts/models.go` | `internal/dev_server/model/*.go` (typed structs with `json:` tags) | Plain structs with json tags; converter functions for raw → CLI shape |
| `internal/rollouts/mock_client.go` | `internal/flags/mock_client.go` | `testify/mock.Mock` embed; per-method `args := c.Called(...); return args.Get(0).(*T), args.Error(1)` |
| `internal/rollouts/client_test.go` | `internal/resources/client_test.go` | `httptest.NewServer` with canned responses; assert request URL, headers, parsed result |
| `internal/rollouts/errors.go` | `internal/errors/errors.go` | Typed error with `Code`, `Message`, `NextAction`; `Is()` method for `errors.As` interop |
| `internal/rollouts/status_mapping.go` | n/a (no precedent in ldcli) | Pure functions; table-driven; one switch statement per derivation |
| `internal/rollouts/idempotency.go` | n/a (no precedent) | Single helper function; god-mode simple |
| `cmd/flags/rollouts/rollouts.go` | `cmd/dev_server/dev_server.go` (parent cmd pattern) | `NewRolloutsCmd(client)` returns `*cobra.Command`; `PersistentPreRun` for cross-cutting behavior (banner) |
| `cmd/flags/rollouts/list.go` | `cmd/flags/toggle.go` | `NewListCmd(client) *cobra.Command` + `runListE(client) func(...)` closure + `initListFlags(cmd)`; reads Viper at `RunE` time, not constructor time |
| `cmd/flags/rollouts/flags.go` | `cmd/flags/toggle.go:94-111` (`initFlags`) | Per-flag: `cmd.Flags().X(...) → MarkFlagRequired → SetAnnotation → viper.BindPFlag` |
| `cmd/flags/rollouts/plaintext.go` | `internal/output/plaintext_fns.go` (existing renderers) | Function takes rollouts data, returns formatted string; called from `RunE` after dispatching on `cliflags.GetOutputKind` |
| `cmd/flags/rollouts/list_test.go` | `cmd/flags/toggle_test.go` | `cmd.CallCmd(t, APIClients{RolloutsClient: mockClient}, ...)`; assertions on output and on `mockClient.AssertExpectations` |
| `cmd/flags/rollouts/rollouts_test.go` | n/a (no banner test precedent) | New: assert banner suppression with `--output json`; use `cmd.CallCmd` capture for stderr verification |
| `.planning/API-PAPERCUTS.md` | n/a (new doc) | Template specified in §"API-PAPERCUTS.md: Seeded Content"; structured per DOC-01 requirements |

## Sources

### Primary (HIGH confidence)

- `.planning/research/ARCHITECTURE.md` — gonfalon API inventory, status enum source-of-truth, 16 papercuts, file-path-resolved references to gonfalon source code
- `.planning/research/STACK.md` — JSON envelope shape, retry policy spec, TTY detection pattern, idempotency-key pattern
- `.planning/research/SUMMARY.md` — phase ordering rationale, key data flows, confidence assessment
- `.planning/research/PITFALLS.md` — 16 pitfalls; Phase 1 honors anti-pattern #7 (output contract early), #8 (exit codes), #4 (papercut format)
- `.planning/research/FEATURES.md` — feature landscape and CLI survey (note: D-01 supersedes the FEATURES.md numeric exit-code proposal)
- `.planning/codebase/CONVENTIONS.md` — naming patterns, error handling, DI pattern, cobra patterns
- `.planning/codebase/ARCHITECTURE.md` — component table, layer boundaries
- `cmd/flags/toggle.go` — direct read; Cobra subcommand pattern; verified flag registration pattern at lines 94–111
- `cmd/flags/toggle_test.go` — direct read; testify + `cmd.CallCmd` test pattern at lines 14–80
- `internal/flags/client.go` — direct read; Client interface + concrete struct + `var _ Client = ...` pattern at lines 20–43
- `internal/flags/mock_client.go` — direct read; testify mock pattern at lines 1–53
- `internal/output/output.go` and `outputters.go` — direct read; `OutputKind`, `Outputter`, `CmdOutput` dispatch
- `internal/errors/errors.go` — direct read; `Error`, `NewError`, `NewLDAPIError`, `APIError` types
- `internal/resources/client.go` — direct read; existing HTTP client (which we do NOT route through for rollouts)
- `cmd/root.go` — direct read; lines 40–47 (`APIClients`), 109–280 (`NewRootCommand`), 282–351 (`Execute`), 222–238 (TTY default for `--output`)
- `cmd/cliflags/flags.go` — direct read; existing flag constants
- [VERIFIED via `proxy.golang.org/github.com/hashicorp/go-retryablehttp/@latest`] — current version v0.7.8 (2025-06-18)
- [VERIFIED via `go version`] — toolchain is go1.23.5

### Secondary (MEDIUM confidence)

- `.planning/CONTEXT.md` (locked decisions D-01..D-08) — user-confirmed, treated as fact
- `.planning/REQUIREMENTS.md` — requirement IDs and phase mapping
- `.planning/ROADMAP.md` — phase ordering and success criteria

### Tertiary (LOW confidence — flagged for Wave 0 validation)

- Exact API response shape of list-by-flag endpoint — to be verified against staging fixtures (A2)
- API behavior at `limit=1000` — to be empirically determined (Open Question 3)
- Whether `recommended-duration` and other endpoints honor `Idempotency-Key` — out of Phase 1 scope; relevant in Phase 2

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — single net-new dep, version verified via proxy.golang.org
- Architecture & file layout: HIGH — mirrors existing patterns exactly; no novel architectural decisions
- Status mapping table: HIGH — derived directly from CONTEXT.md `<specifics>` (which derives from gonfalon `GuardedRolloutUIStates.tsx`)
- API endpoint shape: HIGH — verified in ARCHITECTURE.md from gonfalon source
- JSON envelope nesting (A1): MEDIUM — recommended shape but pending discuss-phase confirmation
- Field mapping (A2): MEDIUM — `environmentKey` presence unverified
- `error.code` enum: HIGH — direct mapping from HTTP status codes; pattern matches existing `internal/errors`
- Beta banner copy & placement: HIGH — pattern is industry-standard (gcloud, kubectl precedent)
- Testing approach: HIGH — direct mirror of existing `cmd/flags/toggle_test.go` pattern

**Research date:** 2026-05-12
**Valid until:** 2026-06-11 (30 days — stack and API surface are stable; revalidate `go-retryablehttp` version if planning slips)
