# Phase 2: Start a Rollout - Research

**Researched:** 2026-05-13
**Domain:** `ldcli flags rollouts-beta start` — semantic-patch mutation via gonfalon + two-step re-fetch + structured output
**Confidence:** HIGH (primary sources: gonfalon source code, Phase 1 codebase already in place, 01-SMOKE.md real-staging data)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Stages flag UX**
- D-01: `--stages 25:60m,50:60m,100:60m` compact list. No repeatable `--stage`, no `--stages-file`.
- D-02: Allocation is percent int `[0,100]`. CLI multiplies by 1000 for API basis-points. Decimals rejected.
- D-03: Duration is a Go duration string (`60m`, `1h30m`, `300s`). Parsed by `time.ParseDuration`. Plain integers rejected.

**Metric declaration and guarded vs progressive**
- D-04: `--pause-on-regression <metricKey>` (repeatable) and `--revert-on-regression <metricKey>` (repeatable). No `--metric`. A metric in both is a usage error.
- D-05: No `--release-kind`. Inferred: zero pause/revert flags → progressive; ≥1 → guarded.
- D-06: Metric groups (`isGroup: true`) deferred to v1.1.

**Targeting scope**
- D-07: Fallthrough (no targeting flags) and existing rule by ID (`--rule-id`). `--ref` and `--clauses` deferred.
- D-08: Server error "Automated releases cannot be created on the default rule" → `unknown_upstream`. No dedicated code.

**Preflight (deferred)**
- D-09: No preflight. No `recommended-duration` GET. No `--skip-health-checks`. No `meta.skippedHealthChecks`.

**Idempotency (out of scope for project)**
- D-10: No `Idempotency-Key` header. No `--idempotency-key` flag. No `meta.idempotencyKey`. Delete `internal/rollouts/idempotency.go` as cleanup. START-06 and idempotency clauses of FOUND-05 / AGENT-03 to be struck from REQUIREMENTS.md.

**Two-step start and error mapping**
- D-11: Two-step pattern locked (PATCH + re-fetch GET). Re-fetch robustness specifics are planner discretion.
- D-12: Error-code taxonomy for mutation failures is planner discretion within existing enum. No pre-fetch flag state client-side. Likely new codes: `rollout_already_running`, `flag_not_configured_for_rollout` (or `flag_off`), `invalid_variation`.

### Claude's Discretion

- Whether to expose `--comment <string>` for the semantic-patch `comment` field.
- The exact `--help` copy for stages syntax.
- Whether `--target-variation` and `--original-variation` accept variation keys, IDs, or either. (**Answered by research — see Open Question 1 below.**)
- File split inside `internal/rollouts/` — likely `start.go` + fleshed-out `instructions.go`.
- Whether the success envelope's `kind` is `"Rollout"` or `"RolloutCreate"`. Default: `"Rollout"`.
- `--extension-duration` inclusion or omission in Phase 2.

### Deferred Ideas (OUT OF SCOPE)

- Preflight (`recommended-duration` + `--skip-health-checks` + TTY prompt + audit shape) → future phase.
- Idempotency-Key → out of scope for entire project. Strike from REQUIREMENTS.md via separate task.
- Metric groups (`isGroup: true`) → v1.1.
- `--ref` for existing-rule selection → future multi-instruction-patch phase.
- `--clauses` for new-rule targeting → future demand-driven.
- `--extension-duration` → Claude's discretion (researcher recommends: omit for Phase 2; see below).
- Generic CLI-robustness features (idempotency, numeric exit-code taxonomies, retry contracts) → outside this project entirely.

**Required upstream REQUIREMENTS / ROADMAP follow-up (separate `/gsd-phase` task):**
- Strike START-04, START-06 from REQUIREMENTS.md.
- Strike idempotency clauses from FOUND-05 / AGENT-03 in REQUIREMENTS.md.
- Drop Phase 2 Success Criterion #3 (preflight) from ROADMAP.md.
- Strike AGENT-03 idempotency reference from ROADMAP.md cross-cutting constraints.
- Delete `internal/rollouts/idempotency.go`.
- The planner should NOTE these follow-ups are pending but should NOT block Phase 2 plan execution on them.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| START-01 | `ldcli flags rollouts-beta start` kicks off a guarded or progressive rollout. Progressive by default; `--pause-on-regression` / `--revert-on-regression` flags → guarded. | Instruction shape confirmed from gonfalon source. `releaseKind` inference via D-05 is clean and aligns with server validation. |
| START-02 | All API options configurable from CLI: stages, target variation, original variation, randomization unit, metrics + auto-rollback, rule targeting. | Full field set confirmed. Note: phase scope limited per CONTEXT.md (metric groups deferred, `--ref`/`--clauses` deferred, `--extension-duration` is discretionary, `--comment` is discretionary). |
| START-03 | Environment parameterized via `--environment`. | Trivially satisfied; `environmentKey` is required in the semantic-patch envelope body, same pattern as Phase 1. |
| START-05 | After PATCH succeeds, CLI re-fetches via GET (env-filtered, limit=1) and surfaces new rollout's ID + initial state. | Two-step pattern confirmed. See Research Q2 for robustness specifics. |
| START-07 | Preflight failures (deferred per D-09), off-flag conditions, rollout-already-running, invalid-variation, auth-scope-missing surface as distinct error codes with `nextAction` hints. | Error code mapping researched. See Open Question 3. Note: "preflight failures" part of START-07 is moot after D-09 — the remaining cases are flag-off, rollout-already-running, invalid-variation, auth-scope. |
</phase_requirements>

---

## Summary

Phase 2 ships `ldcli flags rollouts-beta start` end-to-end. The Phase 1 infrastructure (retryablehttp client, error taxonomy, envelope helpers, mock pattern) is fully in place — Phase 2 extends it rather than building new foundation.

The core implementation is:
1. Parse CLI flags → build `StartInstruction` in `instructions.go`.
2. PATCH the flag with the semantic-patch body (new Content-Type override needed in `setStartHeaders`).
3. Re-fetch via `GET .../automated-releases?filter=environmentKey:{ek}&limit=1` (PC-001 workaround from Phase 1).
4. Emit the `rollouts.v1beta1` success envelope with `kind: "Rollout"` or the Phase 1 error envelope on failure.

The research resolves five previously-open questions and provides planner-ready answers for all ten questions posed in the task brief.

**Primary recommendation:** Hand-write a testify-based `Start` method addition to `MockClient` (same pattern as `List`/`Get`); do NOT use `go.uber.org/mock/mockgen` for the rollouts package — Phase 1 chose the hand-written pattern and Phase 2 must match it. The biggest implementation risk is the variation-ID-only requirement (users must pass UUIDs, not keys) — the planner should include this clearly in flag help text.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| CLI flag parsing + validation | cmd/flags/rollouts/start.go | — | Cobra command layer owns flag binding and input validation |
| Stages string parsing (25:60m,50:60m) | cmd/flags/rollouts/start.go | — | CLI-to-API translation layer; not in internal/rollouts per existing pattern |
| StartInstruction struct construction | internal/rollouts/instructions.go | — | Instruction shape belongs in the domain package |
| PATCH /api/v2/flags semantic-patch | internal/rollouts/client.go (Start method) | — | RolloutsClient owns all HTTP; cmd layer calls client.Start |
| Two-step re-fetch GET | internal/rollouts/client.go (Start method, part 2) | — | Bundled into Start so command layer gets back a *Rollout atomically |
| Error code mapping (flag-off, already-running, etc.) | internal/rollouts/errors.go (mapAPIError) | — | Single source of truth for error taxonomy per FOUND-08 |
| Success envelope construction | cmd/flags/rollouts/start.go | internal/rollouts/envelope.go | emitSuccess mirrors list.go pattern; NewRolloutEnvelope is the helper |
| Error envelope on stdout (JSON mode) | cmd/flags/rollouts/start.go | internal/rollouts/envelope.go | emitError mirrors list.go; D-07 contract: error envelope on stdout in JSON mode |
| Analytics tracking | cmd/flags/rollouts/rollouts.go (PersistentPreRun) | — | Inherited from parent command; no new wiring needed |

---

## Standard Stack

### Core (already in repo — no new deps)

| Library | Version | Purpose | Status |
|---------|---------|---------|--------|
| `github.com/hashicorp/go-retryablehttp` | v0.7.7 | HTTP retries for PATCH + GET | Already wired in Phase 1 |
| `github.com/spf13/cobra` | v1.9.1 | Cobra command + `StringArrayVar` for repeatable flags | Already in repo |
| `github.com/spf13/viper` | v1.21.0 | Config/flag value reading at RunE time | Already in repo |
| `github.com/stretchr/testify` | v1.11.1 | Assertions + mock | Already in repo; `MockClient` uses testify/mock |
| `golang.org/x/term` | v0.33.0 | TTY detection (banner suppression) | Already a transitive dep |

**No new dependencies required for Phase 2.** [VERIFIED: existing go.mod]

### Key Patterns Already Established (Phase 1)

| Pattern | File | Phase 2 reuse |
|---------|------|---------------|
| retryablehttp client with 4-retry/500ms–8s policy | `internal/rollouts/client.go:newRetryableClient` | Reuse as-is for both PATCH and GET |
| `setStandardHeaders` (Auth + Content-Type: application/json + UA + LD-API-Version: beta) | `internal/rollouts/client.go:setStandardHeaders` | Reuse for GET re-fetch; PATCH needs a variant with `Content-Type: application/json; domain-model=launchdarkly.semanticpatch` |
| `mapAPIError` — HTTP status → typed `RolloutError` | `internal/rollouts/errors.go` | Extend the message-matching switch for new mutation-specific codes |
| `mapTransportError` — network failures | `internal/rollouts/errors.go` | Reuse unchanged |
| `NewErrorEnvelope` / `NewListEnvelope` helpers | `internal/rollouts/envelope.go` | Add `NewRolloutEnvelope` (returns `kind: "Rollout"`) |
| `emitSuccess` + `emitError` split in RunE | `cmd/flags/rollouts/list.go` | Exact pattern for `start.go` |
| Hand-written `MockClient` with testify/mock | `internal/rollouts/mock_client.go` | Add `Start` method manually — DO NOT use mockgen |
| Viper read at RunE time, not constructor time | All cmd files | Required per CONVENTIONS.md |

---

## Architecture Patterns

### System Architecture Diagram

```
User invokes: ldcli flags rollouts-beta start --flag X --environment E --stages 25:60m,50:60m,100:60m
              --target-variation <uuid> --original-variation <uuid> --randomization-unit user

cmd/flags/rollouts/start.go : RunE
  │
  ├─ Parse & validate --stages string → []StageInput{allocation, durationMillis}
  ├─ Parse & validate --pause-on-regression / --revert-on-regression
  ├─ Infer releaseKind (zero pause/revert → "progressive"; ≥1 → "guarded")
  ├─ Build StartInstruction{kind:"startAutomatedRelease", releaseKind, ...}
  │
  ▼
internal/rollouts/RolloutsClient.Start(ctx, token, baseURI, projKey, flagKey, envKey, instr)
  │
  ├─ Step 1: PATCH /api/v2/flags/{projKey}/{flagKey}
  │   Body: SemanticPatch{environmentKey, instructions:[instr]}
  │   Headers: Authorization, Content-Type: application/json; domain-model=launchdarkly.semanticpatch
  │            User-Agent, LD-API-Version: beta
  │   4xx → mapAPIError → RolloutError (rollout_already_running / flag_off / etc.)
  │   5xx → retry → mapAPIError → upstream_unavailable
  │   Transport → mapTransportError → network_error
  │   Response: updated FeatureFlag (IGNORED — PC-001)
  │
  └─ Step 2: GET /internal/projects/{projKey}/flags/{flagKey}/automated-releases
             ?filter=environmentKey:{envKey}&limit=1
             Headers: standard (Content-Type: application/json)
             Response: {items: [Rollout]} → decode → items[0]
             Empty items → retry with backoff → error after timeout
             ▼
     return *Rollout

cmd/flags/rollouts/start.go : emitSuccess / emitError
  │
  ├─ JSON mode: marshal NewRolloutEnvelope(rollout) → stdout  (success)
  │             marshal NewErrorEnvelope(...)        → stdout  (error, D-07)
  └─ Plaintext mode: single-record text → stdout    (success)
                     error message → return error    (error, goes to stderr via root)
```

### Recommended File Layout (delta from Phase 1)

```
internal/rollouts/
├── client.go           # Add Start() method to Client interface + RolloutsClient impl
│                       # Add setStartHeaders() helper for semantic-patch Content-Type
├── instructions.go     # Flesh out StartInstruction with all Phase 2 fields + SemanticPatch
├── errors.go           # Extend mapAPIError: add message-matching for mutation errors (D-12)
├── envelope.go         # Add NewRolloutEnvelope(r *Rollout) Envelope
├── mock_client.go      # Add Start() to MockClient (hand-written, testify/mock)
├── models.go           # No changes needed
└── (idempotency.go)    # DELETE as cleanup per D-10

cmd/flags/rollouts/
├── start.go            # NEW: Cobra RunE + stages parser + flag init
├── flags.go            # Add new flag constants for start verb
├── rollouts.go         # Add: cmd.AddCommand(NewStartCmd(client))
└── start_test.go       # NEW: table-driven tests using MockClient

cmd/cliflags/flags.go   # Add new constants: StagesFlag, TargetVariationFlag,
                        # OriginalVariationFlag, RandomizationUnitFlag,
                        # PauseOnRegressionFlag, RevertOnRegressionFlag, RuleIDFlag
```

### Pattern 1: Semantic-Patch PATCH Request

The PATCH requires a different Content-Type than the standard GET headers. Phase 1's `setStandardHeaders` sets `Content-Type: application/json`. Phase 2 needs a variant:

```go
// Source: internal/rollouts/client.go (to be added)
// PAPERCUT: PC-012 — releaseKind in the instruction body, kind in the response
func (c RolloutsClient) setStartHeaders(req *retryablehttp.Request, accessToken string) {
    req.Header.Set("Authorization", accessToken)
    req.Header.Set("Content-Type", "application/json; domain-model=launchdarkly.semanticpatch")
    req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
    req.Header.Set("LD-API-Version", "beta")
}
```

The re-fetch GET uses the existing `setStandardHeaders` unchanged.

### Pattern 2: StartInstruction Canonical Shape

Confirmed from gonfalon source `instruction_start_automated_release.go`:

```go
// Source: internal/rollouts/instructions.go (to be fleshed out in Phase 2)
// PAPERCUT: PC-010 — metrics + metricMonitoringPreferences are parallel maps; CLI reconciles them
// PAPERCUT: PC-012 — field is releaseKind in request, kind in response
// PAPERCUT: PC-013 — these are the unified names (originalVariationId, not controlVariationId)
// PAPERCUT: PC-014 — durationMillis is int64 millis; CLI converts from Go duration string (D-03)
type StartInstruction struct {
    Kind                        string                               `json:"kind"` // "startAutomatedRelease"
    ReleaseKind                 string                               `json:"releaseKind"` // "guarded" | "progressive"
    OriginalVariationID         string                               `json:"originalVariationId"` // UUID _id, NOT key
    TargetVariationID           string                               `json:"targetVariationId"`   // UUID _id, NOT key
    RandomizationUnit           string                               `json:"randomizationUnit"`
    Stages                      []StageInput                         `json:"stages"`
    Metrics                     []MetricSource                       `json:"metrics,omitempty"` // guarded only
    MetricMonitoringPreferences map[string]MetricMonitoringPref      `json:"metricMonitoringPreferences,omitempty"`
    ExtensionDurationMillis     *int64                               `json:"extensionDurationMillis,omitempty"` // guarded only; deferred
    RuleID                      string                               `json:"ruleId,omitempty"` // D-07: --rule-id
    // Ref, Clauses, Description, BeforeRuleId: deferred per D-07; include fields but omitempty
}

type StageInput struct {
    Allocation     int   `json:"allocation"`     // basis points (100 * percent); D-02
    DurationMillis int64 `json:"durationMillis"` // from time.ParseDuration(s).Milliseconds(); D-03
}

type MetricSource struct {
    Key     string `json:"key"`
    IsGroup bool   `json:"isGroup,omitempty"` // always false in Phase 2 per D-06
}

type MetricMonitoringPref struct {
    AutoRollback bool `json:"autoRollback"`
}
```

### Pattern 3: Two-Step Start Implementation

```go
// Source: internal/rollouts/client.go (to be added)
func (c RolloutsClient) Start(
    ctx context.Context,
    accessToken, baseURI, projKey, flagKey, envKey string,
    instr StartInstruction,
) (*Rollout, error) {
    // Step 1: PATCH with semantic-patch envelope
    // PAPERCUT: PC-001 — response is FeatureFlag, not Rollout; we discard it
    patch := SemanticPatch{
        EnvironmentKey: envKey,      // NOTE: EnvironmentKey must be on SemanticPatch
        Instructions:   []interface{}{instr},
    }
    // ... build retryablehttp.NewRequestWithContext, setStartHeaders, do PATCH ...
    // 4xx immediately returned (no retry per DefaultRetryPolicy)
    // 5xx retried up to 4x; on exhaustion mapAPIError → upstream_unavailable

    // Step 2: Re-fetch — GET filtered list, limit=1
    // PAPERCUT: PC-011 — /internal/ URL prefix
    // See robustness notes below
    path := fmt.Sprintf("%s/internal/projects/%s/flags/%s/automated-releases", ...)
    q := url.Values{"filter": {"environmentKey:" + envKey}, "limit": {"1"}}
    // ... GET, decode rawRolloutList, return items[0].toRollout() ...
}
```

**IMPORTANT:** The SemanticPatch struct in `instructions.go` needs an `EnvironmentKey` field added (currently only has `Comment` and `Instructions`):

```go
type SemanticPatch struct {
    EnvironmentKey string        `json:"environmentKey"`  // ADD THIS — required for semantic-patch
    Comment        string        `json:"comment,omitempty"`
    Instructions   []interface{} `json:"instructions"`
}
```

[VERIFIED: gonfalon source `instruction_start_automated_release.go` — the Execute function resolves from `request.Body.EnvironmentKey`]

### Anti-Patterns to Avoid

- **Do NOT copy `cmd/flags/toggle.go` Content-Type.** Toggle uses `application/json` (JSON Patch). Start needs `application/json; domain-model=launchdarkly.semanticpatch`. The comment in 02-CONTEXT.md canonical_refs and ARCHITECTURE.md both call this out explicitly.
- **Do NOT add generic CLI-robustness features.** Per D-10 and user preference documented in project memory, idempotency, retry-contract headers, and numeric exit-code taxonomies stay out of the rollouts subtree.
- **Do NOT pass variation keys.** The API's `originalVariationId` / `targetVariationId` fields are matched against `variation.Id` (the `_id` UUID field in the LD API response) — not against variation name/key. Passing a key string is silently an invalid UUID and will result in a server-side "originalVariationId must be a valid variation id" error.
- **Do NOT pre-fetch flag state before PATCH.** D-12: server is authoritative; pre-fetching for client-side validation adds latency to the happy path and has a TOCTOU race. Surface the server's error via `mapAPIError`.
- **Do NOT use `go.uber.org/mock/mockgen` for rollouts package.** Phase 1 chose hand-written testify/mock. Consistent with `internal/flags/mock_client.go` pattern.

---

## Open Questions Resolved

### Q1: Variation key vs UUID — ANSWER: UUID (_id) only [VERIFIED: gonfalon source]

The gonfalon instruction execution calls `instructionShared.VariationIDToIndex(flag, e.Instruction.OriginalVariationId)` which loops over `flag.Variations` comparing against `variation.Id`. The `Variate.Id` field is the MongoDB `_id` field exposed as `"_id"` in the flag GET response's variation array (`VariateRep` has `json:"_id"`).

**Conclusion:** The API only accepts UUIDs. The CLI flag `--target-variation` and `--original-variation` MUST accept UUIDs. There is NO key-to-ID resolution built into the server.

**UX consequence:** Users and agents must supply UUIDs. This is a known friction point. The `--help` text for these flags MUST call it out clearly: e.g., `"The variation UUID (_id field from ldcli flags get or the LaunchDarkly UI)"`.

**Option A (accept UUIDs only) is the correct choice** given D-12's rationale — no pre-fetching. The planner should document in `--help` that UUIDs are required and how to obtain them (`ldcli flags get --flag <key> --output json | jq '.variations[]'` as an example).

**No pre-GET key-to-ID resolution in Phase 2.** If real-world demand surfaces (users consistently frustrated by UUID requirement), a v2 enhancement would add `--target-variation-key` that does the GET+resolve. Not in Phase 2 scope.

### Q2: Two-step re-fetch robustness [VERIFIED: Phase 1 two-step pattern + staging smoke test]

The Phase 1 codebase successfully used the two-step pattern for read operations. For the start mutation, the planner should apply:

**Robustness strategy (planner discretion per D-11):**

a) **Stale-detection:** The GET at `limit=1` returns the most recent rollout for the env. After a successful PATCH, the new rollout must be present. Detection approach: compare `items[0].createdAt` against a timestamp captured just before the PATCH call (`beforePatch := time.Now()`). The new rollout's `createdAt` must be `>= beforePatch - 2s` (2s buffer for clock skew). If not, treat as "stale result" and retry the GET.

b) **Retry on empty:** If `items` is empty after a successful PATCH, retry the GET up to 3 times with 100ms, 250ms, 500ms backoffs. After 3 retries with empty results, return a structured error with `code: "unknown_upstream"` and message `"Start succeeded but rollout could not be fetched; check rollouts list for the new rollout"`.

c) **Consistency window:** Based on Phase 1 smoke test observations (no eventual consistency gaps observed for GET-after-LIST), the rollout is typically immediately visible. The 3-retry/500ms envelope is defensive and should rarely trigger.

d) **Race with concurrent rollout:** The server already rejects a second `startAutomatedRelease` with "Flag must not have ongoing guarded/progressive rollout" — so the GET returning a pre-existing rollout from a concurrent start is not a real risk. The `createdAt >= beforePatch` check provides belt-and-suspenders.

e) **Auto-rejected rollout:** If the flag was turned off between PATCH and GET, the PATCH would have been rejected by the server (server-side check: `!flagConfig.On → "flag X is off"`). So a successful PATCH guarantees the rollout was created; the GET should find it.

**Acceptance criteria:**
- GET after PATCH returns `items[0]` with `createdAt` within a 2s window of the PATCH call.
- GET returns empty: retry 3x with backoff, then error.
- GET returns a rollout older than the PATCH window: retry 3x, then return that item with a warning in `meta.warnings`.

### Q3: Error-message to error.code mapping [VERIFIED: gonfalon source instruction_start_automated_release.go]

Phase 1's `mapAPIError` already handles 401/403/404/409/400/429/5xx. The PATCH for `startAutomatedRelease` returns semantic-patch validation errors as 4xx with a body `{"code":"...", "message":"..."}`. The server wraps instruction errors as `sempatch.NewInstructionError(...)` which typically surfaces as a 400 Bad Request.

**New message-based switch cases to add in `mapAPIError` (insert before the generic `ErrCodeBadRequest` branch for 400):**

| Upstream message substring (exact gonfalon source) | `error.code` | `nextAction` |
|-----------------------------------------------------|--------------|-------------|
| `"flag X is off"` (where X = flag key, dynamic) | `"flag_not_configured_for_rollout"` | `"Turn on the flag before starting a rollout"` |
| `"Flag must not have ongoing guarded rollout"` | `"rollout_already_running"` | `"Stop the current rollout before starting a new one, or check the list for the active rollout"` |
| `"Flag must not have ongoing progressive rollout"` | `"rollout_already_running"` | (same as above) |
| `"instruction kind 'startAutomatedRelease' unsupported"` | `"beta_gate_closed"` | `"Enable the release-guardian feature flag for this account in the LaunchDarkly UI"` |
| `"Automated releases cannot be created on the default rule"` | `"unknown_upstream"` | `"The fallthrough rule targeting mode is disabled for this account"` |
| `"cannot start an automated release on a disabled rule"` | `"unknown_upstream"` | `"Ensure the target rule is enabled before starting a rollout"` |
| `"originalVariationId must be a valid variation id"` | `"invalid_variation"` | `"Pass the variation UUID (_id) from the flag definition, not the variation key"` |
| `"instruction targetVariationId and originalVariationId must be different"` | `"invalid_variation"` | `"--target-variation and --original-variation must refer to different variations"` |
| `"stage allocation must be greater than 0"` | `"bad_request"` | (use existing bad_request handling) |
| `"stage allocation must not exceed 50%"` | `"bad_request"` | (use existing bad_request handling) |
| Other 400 messages (metric config, randomization unit, etc.) | `"bad_request"` | passthrough API message |

**Matching strategy:** Use `strings.Contains(apiBody.Message, <substring>)`. The dynamic part (flag key) in "flag X is off" can be matched with `strings.HasSuffix(apiBody.Message, " is off")`.

The extended `mapAPIError` should insert these checks in a new block before the existing `case statusCode == http.StatusBadRequest:` branch. Rationale: many of these land as 400 Bad Request but should get more specific codes.

**New error code constants to add to `errors.go`:**

```go
const (
    // ... existing constants ...
    ErrCodeRolloutAlreadyRunning         = "rollout_already_running"
    ErrCodeFlagNotConfiguredForRollout   = "flag_not_configured_for_rollout"
    ErrCodeInvalidVariation              = "invalid_variation"
    // ErrCodeBetaGateClosed already exists in Phase 1
)
```

[ASSUMED: the exact HTTP status codes for these messages; gonfalon source confirms the error messages but the transport layer (400 vs 409 vs other) may vary; planner should verify via staging and update if a different status code is used for "Flag must not have ongoing rollout"]

### Q4: Stage instruction field shape [VERIFIED: gonfalon source instruction_start_automated_release.go]

From `AutomatedReleaseStageInput` struct:

```go
type AutomatedReleaseStageInput struct {
    Allocation     int   `json:"allocation"`     // basis points: 25% → 25000
    DurationMillis int64 `json:"durationMillis"` // milliseconds: 60m → 3600000
}
```

- No `stageNumber` or `percentRollout` field — just `allocation` (basis-points int) and `durationMillis` (int64).
- No special first stage. All stages have the same shape.
- `controlVariationId` / `targetVariationId` are top-level on the instruction, NOT nested in stages.
- For guarded: `allocation` must be `> 0` and `<= 50000` (50%) per stage. [VERIFIED: gonfalon source line 144-147]
- For progressive: `allocation` must be `> 0`, no upper bound per stage. [VERIFIED: gonfalon source line 141-143]

**CLI conversion (D-02 + D-03):**
```go
// --stages 25:60m → StageInput{Allocation: 25000, DurationMillis: 3600000}
func parseStages(raw string) ([]StageInput, error) {
    // split on comma, for each pair:
    //   left of colon → strconv.Atoi → multiply by 1000 (basis points)
    //   right of colon → time.ParseDuration → .Milliseconds()
    // reject non-integer allocation (e.g. "12.5")
    // reject plain integer duration (no unit suffix)
}
```

**Canonical instruction JSON (source of truth for `instructions.go` tests):**

```json
{
  "kind": "startAutomatedRelease",
  "releaseKind": "guarded",
  "originalVariationId": "uuid-control",
  "targetVariationId": "uuid-treatment",
  "randomizationUnit": "user",
  "stages": [
    {"allocation": 25000, "durationMillis": 3600000},
    {"allocation": 50000, "durationMillis": 3600000}
  ],
  "metrics": [
    {"key": "my-error-rate"}
  ],
  "metricMonitoringPreferences": {
    "my-error-rate": {"autoRollback": true}
  }
}
```

Progressive example (no metrics, releaseKind: "progressive"):
```json
{
  "kind": "startAutomatedRelease",
  "releaseKind": "progressive",
  "originalVariationId": "uuid-control",
  "targetVariationId": "uuid-treatment",
  "randomizationUnit": "user",
  "stages": [
    {"allocation": 25000, "durationMillis": 3600000},
    {"allocation": 50000, "durationMillis": 3600000},
    {"allocation": 100000, "durationMillis": 3600000}
  ]
}
```

### Q5: `--extension-duration` recommendation [ASSUMED based on D-10 rationale]

**Recommendation: omit `--extension-duration` from Phase 2.**

Rationale: `extensionDurationMillis` is a guarded-only optional field. Including it adds validation complexity (only valid when guarded, error if passed with progressive), adds a CLI flag constant, and a test case — for a feature with no known user demand in Phase 2. The API supports it, and it can be added as a one-line change later. The planner may include it if desired, but researcher recommends omitting.

If included: validate in start.go that it's only set when `releaseKind == "guarded"` (i.e., when at least one pause/revert flag is present); reject with a usage error otherwise.

### Q6: `--comment` recommendation [ASSUMED]

**Recommendation: omit `--comment` from Phase 2.**

The `comment` field on the semantic-patch envelope is human-readable metadata captured in the flag's audit log. It has value for human operators but is of marginal use for agent-driven workflows and adds CLI surface. The `SemanticPatch.Comment` field is already declared in `instructions.go` with `omitempty` — the planner can wire it trivially if desired. Default: omit the `--comment` flag but leave `SemanticPatch.Comment` as `omitempty` so it's ready when needed.

### Q7: Mock regeneration [VERIFIED: codebase grep]

The `internal/rollouts/mock_client.go` uses the **hand-written testify/mock pattern** — NOT `go.uber.org/mock/mockgen`. The mock was written by hand in Phase 1 to match the `internal/flags/mock_client.go` precedent.

**To add `Start` to the mock, hand-edit `mock_client.go`:**

```go
// Add this method to MockClient in internal/rollouts/mock_client.go
func (c *MockClient) Start(
    _ context.Context,
    accessToken,
    baseURI,
    projKey,
    flagKey,
    envKey string,
    instr StartInstruction,
) (*Rollout, error) {
    args := c.Called(accessToken, baseURI, projKey, flagKey, envKey, instr)

    var r *Rollout
    if v := args.Get(0); v != nil {
        r = v.(*Rollout)
    }
    return r, args.Error(1)
}
```

Also add `Start` to the `Client` interface in `client.go`:

```go
type Client interface {
    List(ctx context.Context, accessToken, baseURI, projKey, flagKey string, opts ListOpts) (*RolloutList, error)
    Get(ctx context.Context, accessToken, baseURI, projKey, envKey, rolloutID string) (*Rollout, error)
    Start(ctx context.Context, accessToken, baseURI, projKey, flagKey, envKey string, instr StartInstruction) (*Rollout, error)
}
```

The compile-time assertion `var _ Client = RolloutsClient{}` will fail until `RolloutsClient.Start` is implemented — that's correct and deliberate.

**`go generate ./...` is NOT needed** for the rollouts mock. That command runs oapi-codegen for the dev server. Don't run it expecting rollouts mock regeneration.

### Q8: golangci-lint nits to anticipate [VERIFIED: Phase 1 pre-commit hook + go fmt]

Phase 1 did not surface specific golangci-lint issues in the rollouts package. Standard hygiene to pre-apply:

- `gofmt` will flag any un-formatted code. Run `go fmt ./internal/rollouts/ ./cmd/flags/rollouts/` before committing.
- `go vet` flags unused variables and unreachable code — easy to trigger in stub implementations.
- The `end-of-file-fixer` hook enforces a trailing newline on all files.
- `golangci-lint` with the repo's v1.63.4 config may flag:
  - `errcheck`: ignored return values from `viper.BindPFlag` (consistent with Phase 1 pattern — use `_ =` to suppress).
  - `staticcheck`: any use of `interface{}` vs `any` (codebase uses both — follow the pattern in `instructions.go` and `models.go`).
  - `revive`: exported function without godoc comment (match Phase 1's sparse-but-present doc style).

### Q9: Test strategy [VERIFIED: Phase 1 test files]

**`internal/rollouts/client_test.go` pattern (httptest.Server):**
- Use `makeServer` / `makeFlakyServer` helpers (already in the file) for PATCH + GET sequences.
- For the two-step test: the server needs to handle two sequential requests — a PATCH path and a GET path. Use the existing `recordedRequest.allPaths` to verify both calls fired.
- Load JSON fixtures from `testdata/` (e.g., `testdata/start_success.json` containing the GET list response).
- Test the error path by returning specific error messages in the PATCH response body and asserting the correct `RolloutError.Code`.
- Use `NewClientWithRetryWaitsForTest` (already exists) to keep retry tests fast.

**`cmd/flags/rollouts/start_test.go` pattern (MockClient + cmd.CallCmd):**
- Mirror `list_test.go` exactly: set `mockClient.On("Start", ...)`, call `cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, ...)`.
- Test JSON output: unmarshal the stdout into `rollouts.Envelope` and assert `schemaVersion`, `kind`, `data.id`.
- Test error output: assert the JSON error envelope on stdout (not stderr) per D-07.
- Test stages parsing: call `start` with `--stages 25:60m,50:60m` and verify the `StartInstruction` passed to mock matches expected `{Allocation:25000, DurationMillis:3600000}` per-stage.
- Test guarded inference: `--pause-on-regression metric1` → mock receives `StartInstruction{ReleaseKind:"guarded", Metrics:[{Key:"metric1"}], MetricMonitoringPreferences:{"metric1":{AutoRollback:false}}}`.
- Test revert inference: `--revert-on-regression metric2` → mock receives `{AutoRollback: true}`.
- Test same-metric error: passing a metric to both flags → usage error before client is called.

**Fixture for `testdata/start_success.json`:** Should be a single-item list response (the GET re-fetch response), matching the `rawRolloutList` shape with `int64` timestamps (matching the real-staging format confirmed in 01-SMOKE.md).

### Q10: Real-staging validation plan [VERIFIED: 01-SMOKE.md precedent]

Phase 1's four-bug gap proved that real-staging smoke is mandatory before claiming done. Phase 2 smoke contract:

**Pre-smoke prerequisites:**
- Flag `ldcli-blitz-phase2-start` created in `alex-engelberg-dev` project, `test` environment, ON, two boolean variations.
- Note down the variation UUIDs from `ldcli flags get --flag ldcli-blitz-phase2-start --output json | jq '.variations[]'`.

**Smoke A — progressive rollout (happy path):**
```bash
./ldcli flags rollouts-beta start \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-phase2-start \
  --environment test \
  --target-variation <uuid-true> \
  --original-variation <uuid-false> \
  --randomization-unit user \
  --stages 25:5m,50:5m,100:5m \
  --output json
```
Expected: exit 0; envelope with `schemaVersion: "rollouts.v1beta1"`, `kind: "Rollout"`, `data.id` non-empty, `data.status.kind: "active"`, stdout only (nothing on stderr in JSON mode).

**Smoke B — guarded rollout with pause-on-regression:**
```bash
./ldcli flags rollouts-beta start ... \
  --pause-on-regression rg-simulator-errors \
  --stages 10:5m,25:5m \
  --output json
```
Expected: `kind: "guarded"` in `data`, `metricConfigurations[0].autoRollback: false`.

**Smoke C — "already running" error path (run Smoke A twice without stopping):**
Expected: exit 1; stdout envelope `{error: {code: "rollout_already_running", ...}}` in JSON mode.

**Smoke D — flag-off error path:**
Turn off the flag, then run start. Expected: exit 1; stdout envelope `{error: {code: "flag_not_configured_for_rollout", ...}}`.

**Smoke E — invalid variation UUID:**
Pass a non-UUID string as `--target-variation`. Expected: exit 1; `{error: {code: "invalid_variation", ...}}`.

**Fields to spot-check in success envelope:**
- `data.id` non-empty UUID
- `data.kind` matches inferred releaseKind
- `data.status.status` matches a known raw status value (likely `"not_started"` or `"in_progress"` immediately after start)
- `data.stages` array length matches `--stages` count
- `data.createdAt` is RFC 3339 (the CLI converts from int64 millis in rawRollout.toRollout)
- `meta.fetchedAt` present and recent

**Any new API contract observation → Confluence page 4875452435 (fetch-first pattern per project memory).**

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTTP retry on PATCH | Custom backoff loop | `go-retryablehttp` already wired in Phase 1 | Request-body rewinding, 4xx-never-retry policy already configured |
| JSON marshaling of StartInstruction | Custom serializer | `encoding/json` with struct tags | No custom types needed; all fields are strings, ints, and slices |
| Error envelope on stdout | New fmt.Println calls | `emitError` from `list.go` as pattern | D-07 contract is tested; new start.go copies the exact pattern |
| Variation UUID lookup | Pre-GET flag variations endpoint | N/A: require UUID from user | D-12: no pre-fetch; server is authoritative; document in --help |

---

## Common Pitfalls

### Pitfall 1: Wrong Content-Type on PATCH

**What goes wrong:** Using `setStandardHeaders` (which sets `Content-Type: application/json`) for the PATCH call. The server's semantic-patch middleware gates on `domain-model=launchdarkly.semanticpatch` and returns 400 "unsupported content type".

**Prevention:** Implement `setStartHeaders` with `Content-Type: application/json; domain-model=launchdarkly.semanticpatch` and use it ONLY for the PATCH call. The subsequent GET re-fetch uses `setStandardHeaders`.

**Warning sign:** 400 response with message about content type in early staging test.

### Pitfall 2: SemanticPatch missing environmentKey

**What goes wrong:** The existing `SemanticPatch` struct in `instructions.go` only has `Comment` and `Instructions`. The server requires `environmentKey` in the body to route the semantic-patch to the right environment. Omitting it causes a 400 or targets the wrong env.

**Prevention:** Add `EnvironmentKey string json:"environmentKey"` to `SemanticPatch` before Phase 2 uses it. [VERIFIED: ARCHITECTURE.md Pattern 3 example shows `EnvironmentKey: envKey` in the struct; the existing stub omits it]

### Pitfall 3: Variation keys instead of UUIDs

**What goes wrong:** User passes `--target-variation true` (a key name) instead of the UUID. Server silently returns "originalVariationId must be a valid variation id" as a 400 error.

**Prevention:** `--help` text must explicitly state "UUID required". The error message from the server is already mapped to `invalid_variation` with a nextAction hint directing users to the UUID lookup workflow.

### Pitfall 4: Decimal allocation rejected incorrectly or silently passed

**What goes wrong (D-02):** `--stages 12.5:60m` — `strconv.Atoi` returns an error, but if the developer uses `strconv.ParseFloat` instead (more familiar), decimals pass through and multiply to `12500` basis points, violating D-02's "decimals rejected" contract.

**Prevention:** Use `strconv.Atoi` for allocation parsing. Return a usage error if Atoi fails. Include `"allocation must be a whole percent integer (e.g. 25, not 12.5)"` in the error message.

### Pitfall 5: Duration without unit suffix silently parsed

**What goes wrong (D-03):** `time.ParseDuration("3600")` returns an error ("missing unit in duration 3600"). But the developer might add a fallback `strconv.ParseInt` for millis passthrough, violating D-03.

**Prevention:** Use only `time.ParseDuration`. If it returns an error, surface it as a usage error. No millis passthrough. Help text: `"Duration must include a unit (e.g. 60m, 1h30m, 300s)"`.

### Pitfall 6: re-fetch GET returns stale rollout from concurrent start

**What goes wrong:** Very unlikely but possible: another client starts a rollout on the same flag/env between the PATCH and the GET. The GET returns a different rollout than the one just created.

**Prevention:** Compare `items[0].createdAt` against `beforePatch` timestamp (see Q2 robustness strategy). If `createdAt < beforePatch - 2s`, retry the GET. Document the workaround at the call site with `// PAPERCUT: PC-001`.

### Pitfall 7: idempotency.go deletion left as a stale TODO

**What goes wrong:** `internal/rollouts/idempotency.go` is never imported but still compiles. It doesn't block Phase 2, but it's dead code with a misleading comment ("Phase 2 calls this") that contradicts D-10.

**Prevention:** Include deletion of `idempotency.go` as an explicit task in the plan. The compile-time assertion `var _ Client = RolloutsClient{}` is not affected by deleting idempotency.go (it's not referenced by the interface).

---

## Code Examples

### Stages parser (CLI layer, `cmd/flags/rollouts/start.go`)

```go
// Source: per D-01/D-02/D-03 + gonfalon source StageInput shape
func parseStages(raw string) ([]rollouts.StageInput, error) {
    parts := strings.Split(raw, ",")
    if len(parts) == 0 {
        return nil, errors.NewError("--stages must specify at least one stage (e.g. 25:60m)")
    }
    stages := make([]rollouts.StageInput, 0, len(parts))
    for _, p := range parts {
        p = strings.TrimSpace(p)
        colonIdx := strings.Index(p, ":")
        if colonIdx < 0 {
            return nil, errors.NewError(fmt.Sprintf("invalid stage %q: expected <allocation>:<duration> (e.g. 25:60m)", p))
        }
        allocStr := p[:colonIdx]
        durStr := p[colonIdx+1:]
        alloc, err := strconv.Atoi(allocStr)
        if err != nil {
            return nil, errors.NewError(fmt.Sprintf("stage allocation %q must be a whole percent integer (e.g. 25, not 12.5)", allocStr))
        }
        if alloc <= 0 || alloc > 100 {
            return nil, errors.NewError(fmt.Sprintf("stage allocation %d must be in range [1, 100]", alloc))
        }
        dur, err := time.ParseDuration(durStr)
        if err != nil {
            return nil, errors.NewError(fmt.Sprintf("stage duration %q must include a unit (e.g. 60m, 1h30m, 300s)", durStr))
        }
        stages = append(stages, rollouts.StageInput{
            Allocation:     alloc * 1000, // percent → basis points (D-02)
            DurationMillis: dur.Milliseconds(), // D-03
        })
    }
    return stages, nil
}
```

### Guarded/progressive inference (CLI layer)

```go
// Source: per D-04/D-05
pauseMetrics  := viper.GetStringSlice(cliflags.PauseOnRegressionFlag)
revertMetrics := viper.GetStringSlice(cliflags.RevertOnRegressionFlag)

// Mutual exclusion check
for _, m := range pauseMetrics {
    for _, n := range revertMetrics {
        if m == n {
            return errors.NewError(fmt.Sprintf("metric %q cannot appear in both --pause-on-regression and --revert-on-regression", m))
        }
    }
}

releaseKind := "progressive"
if len(pauseMetrics)+len(revertMetrics) > 0 {
    releaseKind = "guarded"
}
```

### Metric source + preference reconciliation (CLI layer)

```go
// Source: per D-04, PC-010 workaround
// Builds metrics[]  and metricMonitoringPreferences{} from the two verb flags.
metrics := make([]rollouts.MetricSource, 0)
prefs   := make(map[string]rollouts.MetricMonitoringPref)
for _, m := range pauseMetrics {
    metrics = append(metrics, rollouts.MetricSource{Key: m})
    prefs[m] = rollouts.MetricMonitoringPref{AutoRollback: false}
}
for _, m := range revertMetrics {
    metrics = append(metrics, rollouts.MetricSource{Key: m})
    prefs[m] = rollouts.MetricMonitoringPref{AutoRollback: true}
}
// PAPERCUT: PC-010 — two parallel collections; this function is the single reconciliation point.
```

### NewRolloutEnvelope (to add to envelope.go)

```go
// Source: mirrors NewListEnvelope in internal/rollouts/envelope.go
func NewRolloutEnvelope(r *Rollout) Envelope {
    return Envelope{
        SchemaVersion: SchemaVersionV1Beta1,
        Kind:          "Rollout",
        Data:          r,
        Meta: &EnvelopeMeta{
            FetchedAt: time.Now().UTC(),
        },
    }
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `--release-kind guarded/progressive` explicit flag | Inferred from presence of pause/revert flags (D-05) | Phase 2 discussion | Smaller surface; progressive is the zero-configuration case |
| `--metric key autoRollback=true` syntax | `--pause-on-regression key` / `--revert-on-regression key` (D-04) | Phase 2 discussion | Flag names describe behavior; eliminates parallel-list sync trap (PC-010 in CLI form) |
| Idempotency-Key header on all mutations | No idempotency support (D-10) | Phase 2 discussion | Reduces surface; out of scope for project; server already guards via "already running" |
| Preflight before mutation | No preflight in Phase 2 (D-09) | Phase 2 discussion | Preflight deferred to dedicated future phase |

**Deprecated/outdated after this research:**
- `STACK.md` reference to `google/uuid` for `Idempotency-Key` — that use case is dropped per D-10; `google/uuid` remains vendored and used elsewhere.
- `idempotency.go:SetIdempotencyKey` — function is unreferenced and contradicts D-10; DELETE in Phase 2 cleanup.
- Phase 2 ROADMAP.md Success Criterion #3 (preflight) and #5 (idempotency clause) — no longer applicable after D-09 and D-10.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The PATCH returns 400 (not 409 or 422) for "Flag must not have ongoing rollout" errors | Q3 error mapping | If it's 409, the existing `ErrCodeConflict` branch catches it but with the wrong code; message-matching fix is still needed |
| A2 | The re-fetch GET immediately reflects the new rollout after a successful PATCH (no multi-second eventual consistency window) | Q2 re-fetch robustness | If there's a >500ms consistency delay, the 3-retry/500ms envelope may not be enough; would need a longer backoff |
| A3 | `--extension-duration` omit recommendation — no user demand in Phase 2 | Q5 | Low risk; the field is optional and can be added without breaking changes |
| A4 | Metric validation errors (metric not found, randomization unit incompatible) surface as 400 Bad Request | Q3 error mapping | Would need different branch if they surface as a different status code |

---

## Open Questions (RESOLVED)

1. **HTTP status code for "Flag must not have ongoing rollout" errors.**
   - What we know: gonfalon source confirms the message text; the transport wraps it as `sempatch.NewInstructionError` which typically becomes a 400.
   - What's unclear: exact status code on the wire — could be 400, 409, or 422 depending on semantic-patch error routing.
   - **RESOLVED:** Plan 02-02 Task 1's `mapAPIError` extension matches by message substring *before* checking status code, so the mapping fires regardless of which of 400/409/422 the server returns. Plan 02-02 Task 4's Smoke C/D/E entries validate the actual on-wire status against staging.

2. **`SemanticPatch.EnvironmentKey` field is missing from the Phase 1 stub.**
   - What we know: Architecture research Pattern 3 example includes it; the Phase 1 `instructions.go` stub only has `Comment` and `Instructions`.
   - **RESOLVED:** Fixed in Plan 02-01 Task 1 (Wave 0). Adds `EnvironmentKey string \`json:"environmentKey"\`` to `SemanticPatch` before any downstream code references it. [VERIFIED: ARCHITECTURE.md Pattern 3]

---

## Environment Availability

Step 2.6 SKIPPED (no new external dependencies for Phase 2 — all tools already verified in Phase 1).

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | `go test` + testify v1.11.1 |
| Config file | None (standard `go test ./...`) |
| Quick run command | `go test ./internal/rollouts/ ./cmd/flags/rollouts/` |
| Full suite command | `make test` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| START-01 | Progressive rollout started when no pause/revert flags | unit | `go test ./cmd/flags/rollouts/ -run TestStart` | ❌ Wave 0 |
| START-01 | Guarded rollout when `--pause-on-regression` present | unit | `go test ./cmd/flags/rollouts/ -run TestStart` | ❌ Wave 0 |
| START-02 | Stages parsed: percent×1000 basis-pts, duration→millis | unit | `go test ./cmd/flags/rollouts/ -run TestParseStages` | ❌ Wave 0 |
| START-02 | Same metric in both pause/revert flags → usage error | unit | `go test ./cmd/flags/rollouts/ -run TestStartValidation` | ❌ Wave 0 |
| START-02 | Decimal allocation rejected | unit | `go test ./cmd/flags/rollouts/ -run TestParseStages` | ❌ Wave 0 |
| START-02 | Duration without unit suffix rejected | unit | `go test ./cmd/flags/rollouts/ -run TestParseStages` | ❌ Wave 0 |
| START-03 | `--environment` flows into SemanticPatch.EnvironmentKey | unit (httptest) | `go test ./internal/rollouts/ -run TestStart` | ❌ Wave 0 |
| START-05 | Two-step: PATCH + GET called in sequence | unit (httptest) | `go test ./internal/rollouts/ -run TestStart_TwoStep` | ❌ Wave 0 |
| START-05 | GET empty → retry → error after 3 retries | unit (httptest) | `go test ./internal/rollouts/ -run TestStart_EmptyRefetch` | ❌ Wave 0 |
| START-07 | "flag X is off" → `flag_not_configured_for_rollout` code | unit (httptest) | `go test ./internal/rollouts/ -run TestMapAPIError` | ❌ Wave 0 |
| START-07 | "Flag must not have ongoing" → `rollout_already_running` | unit (httptest) | `go test ./internal/rollouts/ -run TestMapAPIError` | ❌ Wave 0 |
| START-07 | "originalVariationId must be a valid variation id" → `invalid_variation` | unit (httptest) | `go test ./internal/rollouts/ -run TestMapAPIError` | ❌ Wave 0 |
| START-07 | JSON-mode error → stdout envelope, not stderr (D-07) | integration | `go test ./cmd/flags/rollouts/ -run TestStartErrorOnStdout` | ❌ Wave 0 |
| AGENT-01 | `--output json` produces parseable envelope | unit | (covered by START-01 tests) | ❌ Wave 0 |
| AGENT-04 | `data.createdAt` is RFC 3339 in success envelope | unit | (covered by START-01 fixture decode) | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test ./internal/rollouts/ ./cmd/flags/rollouts/`
- **Per wave merge:** `make test`
- **Phase gate:** `make test` green + real-staging smoke (Smoke A through E) before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `cmd/flags/rollouts/start.go` — new command file
- [ ] `cmd/flags/rollouts/start_test.go` — command-layer tests
- [ ] `internal/rollouts/client_test.go` — extend with `TestStart_*` cases (file exists; add new test functions)
- [ ] `internal/rollouts/testdata/start_success.json` — GET re-fetch fixture (single-item list, int64 timestamps)
- [ ] Fix `SemanticPatch.EnvironmentKey` missing field in `instructions.go` — must land in Wave 0 or first task

---

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Existing `--access-token` / `LD_ACCESS_TOKEN` (no new auth surface per CLAUDE.md constraint) |
| V3 Session Management | no | CLI is stateless per invocation |
| V4 Access Control | yes | RBAC enforced server-side; CLI surfaces 403 via existing `ErrCodeForbidden` + `mapAPIError` |
| V5 Input Validation | yes | Stages parser (Atoi, ParseDuration), variation UUID format, metric key format |
| V6 Cryptography | no | No new crypto; HTTPS inherited from retryablehttp |

### Known Threat Patterns for This Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Sensitive upstream 5xx response body leaked in error output | Information Disclosure | Phase 1 already masks 5xx bodies: `"LaunchDarkly returned 5xx"` not echoed raw (T-02-03 in Phase 1 plan) |
| API token logged via retryablehttp debug output | Information Disclosure | Phase 1: `c.Logger = nil` on retryablehttp client — no request URLs or auth headers logged |
| Malformed stage string causing panic | Denial of Service (CLI) | `strconv.Atoi` + `time.ParseDuration` both return errors; no panics; parse errors surfaced as usage errors |
| Variation UUID injection (e.g., path traversal in UUID) | Tampering | `url.PathEscape` on projKey/flagKey; variationIDs go in the JSON body (not URL); JSON encoding handles escaping |

---

## Sources

### Primary (HIGH confidence)

- `/Users/alex/code/launchdarkly/gonfalon/internal/flags/instruction/instruction_start_automated_release.go` — authoritative field list, validation rules, exact error messages
- `/Users/alex/code/launchdarkly/gonfalon/internal/flags/instruction/shared/variation.go` — `VariationIDToIndex` confirms UUID-only matching
- `/Users/alex/code/launchdarkly/ldcli/internal/rollouts/client.go` — Phase 1 RolloutsClient patterns
- `/Users/alex/code/launchdarkly/ldcli/internal/rollouts/instructions.go` — Phase 1 stub to be fleshed out
- `/Users/alex/code/launchdarkly/ldcli/internal/rollouts/errors.go` — error taxonomy to extend
- `/Users/alex/code/launchdarkly/ldcli/internal/rollouts/mock_client.go` — hand-written testify/mock pattern
- `/Users/alex/code/launchdarkly/ldcli/cmd/flags/rollouts/list.go` — canonical pattern for start.go
- `/Users/alex/code/launchdarkly/ldcli/.planning/phases/01-list-foundation-first-end-to-end-slice/01-SMOKE.md` — real-staging contract (int64 millis, LD-API-Version: beta, error envelope on stdout)
- `/Users/alex/code/launchdarkly/ldcli/.planning/research/ARCHITECTURE.md` — instruction field table, Pattern 1, Pattern 3

### Secondary (MEDIUM confidence)

- `.planning/phases/02-start-a-rollout/02-CONTEXT.md` — locked decisions constrain research scope
- `.planning/API-PAPERCUTS.md` — PC-001, PC-010, PC-012, PC-013, PC-014 all affect Phase 2 implementation

### Tertiary (LOW confidence / Assumed)

- Error HTTP status code for "Flag must not have ongoing rollout" — assumed 400; not verified from gonfalon routing layer
- Eventual consistency window for re-fetch GET — assumed immediate based on Phase 1 observations; not formally measured

---

## Metadata

**Confidence breakdown:**
- Instruction field shape: HIGH — verified from gonfalon source
- Error messages: HIGH — verified from gonfalon source
- Variation ID requirement (UUID only): HIGH — verified from `VariationIDToIndex` source
- Error HTTP status codes for mutation errors: MEDIUM — messages verified, status codes assumed 400
- Re-fetch eventual consistency behavior: MEDIUM — Phase 1 smoke suggests immediate; not formally tested for POST
- Mock regeneration approach: HIGH — verified from Phase 1 mock file and lack of go:generate annotation

**Research date:** 2026-05-13
**Valid until:** 2026-06-13 (30 days; gonfalon `automated-releases` API is unstable and may change)
