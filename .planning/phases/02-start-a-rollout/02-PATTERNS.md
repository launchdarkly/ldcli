# Phase 2: Start a Rollout - Pattern Map

**Mapped:** 2026-05-13
**Files analyzed:** 11 (new/modified/deleted)
**Analogs found:** 10 / 10 (idempotency.go is a delete with no analog needed)

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/rollouts/client.go` | service client | request-response (PATCH + re-fetch GET) | `internal/rollouts/client.go:List+Get` | exact — same file, extends Client interface |
| `internal/rollouts/instructions.go` | domain model / instruction body | transform | `internal/rollouts/instructions.go` (Phase 1 stub) | exact — same file, fleshes out stub |
| `internal/rollouts/errors.go` | error taxonomy | transform | `internal/rollouts/errors.go` | exact — same file, extends switch |
| `internal/rollouts/envelope.go` | output adapter | transform | `internal/rollouts/envelope.go:NewListEnvelope` | exact — same file, adds sibling helper |
| `internal/rollouts/mock_client.go` | test double | N/A | `internal/rollouts/mock_client.go` | exact — same file, hand-adds Start |
| `internal/rollouts/start.go` | service client method | request-response | `internal/rollouts/client.go:Get` | role-match (same client pattern, new PATCH verb) |
| `internal/rollouts/start_test.go` | test (client layer) | request-response | `internal/rollouts/client_test.go` | exact — same httptest pattern |
| `cmd/flags/rollouts/start.go` | command handler | CLI flags → PATCH → envelope | `cmd/flags/rollouts/list.go` | exact — same RunE / emitSuccess / emitError shape |
| `cmd/flags/rollouts/start_test.go` | test (command layer) | N/A | `cmd/flags/rollouts/list_test.go` | exact — same MockClient + CallCmd pattern |
| `cmd/flags/rollouts/rollouts.go` | command wiring | N/A | `cmd/flags/rollouts/rollouts.go` | exact — same AddCommand pattern |
| `cmd/cliflags/flags.go` | config constants | N/A | `cmd/cliflags/flags.go` existing entries | exact — add sibling constants |
| `internal/rollouts/idempotency.go` | **DELETE** (cleanup per D-10) | N/A | N/A | N/A |

---

## Pattern Assignments

### `internal/rollouts/client.go` — extend Client interface + add setStartHeaders

**Analog:** `internal/rollouts/client.go` (Phase 1, same file)

**Interface extension** (lines 31–34 — add `Start` after `Get`):
```go
type Client interface {
    List(ctx context.Context, accessToken, baseURI, projKey, flagKey string, opts ListOpts) (*RolloutList, error)
    Get(ctx context.Context, accessToken, baseURI, projKey, envKey, rolloutID string) (*Rollout, error)
    Start(ctx context.Context, accessToken, baseURI, projKey, flagKey, envKey string, instr StartInstruction) (*Rollout, error)
}
```
The compile-time assertion `var _ Client = RolloutsClient{}` (line 44) will fail until `Start` is implemented on the concrete struct — this is deliberate and correct.

**setStandardHeaders pattern** (lines 223–228) — reuse unchanged for the re-fetch GET:
```go
func (c RolloutsClient) setStandardHeaders(req *retryablehttp.Request, accessToken string) {
    req.Header.Set("Authorization", accessToken)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
    req.Header.Set("LD-API-Version", "beta")
}
```

**New setStartHeaders** — copy setStandardHeaders but override Content-Type for the PATCH only:
```go
// setStartHeaders is identical to setStandardHeaders except Content-Type carries the
// semantic-patch domain-model parameter required by the flag PATCH endpoint.
// DO NOT use setStandardHeaders for the PATCH call — the server gates on the domain-model
// parameter and returns 400 "unsupported content type" without it (Pitfall 1, RESEARCH.md).
func (c RolloutsClient) setStartHeaders(req *retryablehttp.Request, accessToken string) {
    req.Header.Set("Authorization", accessToken)
    req.Header.Set("Content-Type", "application/json; domain-model=launchdarkly.semanticpatch")
    req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
    req.Header.Set("LD-API-Version", "beta")
}
```

**newRetryableClient** (lines 76–86) — reuse as-is for both the PATCH and the re-fetch GET. `DefaultRetryPolicy` never retries 4xx, so "rollout already running" (4xx) returns immediately — correct behavior.

**Request-execute-decode skeleton** from `Get` (lines 186–213) — copy structure for each of the two steps in `Start`:
```go
req, err := retryablehttp.NewRequestWithContext(ctx, "PATCH", path, bytes.NewReader(bodyBytes))
if err != nil {
    return nil, errors.NewErrorWrapped("failed to build request", err)
}
c.setStartHeaders(req, accessToken)   // NOTE: setStartHeaders, not setStandardHeaders

resp, err := c.httpClient.Do(req)
if err != nil {
    return nil, mapTransportError(err)
}
defer resp.Body.Close()

body, err := io.ReadAll(resp.Body)
if err != nil {
    return nil, errors.NewErrorWrapped("failed to read response body", err)
}

if resp.StatusCode >= 400 {
    return nil, mapAPIError(body, resp.StatusCode)
}
// PATCH response is FeatureFlag — discard per PC-001; proceed to re-fetch.
```

**Re-fetch GET** — copy path construction from `List` (lines 100–104), substituting `?filter=environmentKey:{envKey}&limit=1`:
```go
// PAPERCUT: PC-001 — PATCH returns FeatureFlag not Rollout; re-fetch via list+filter.
path := fmt.Sprintf("%s/internal/projects/%s/flags/%s/automated-releases",
    strings.TrimRight(baseURI, "/"),
    url.PathEscape(projKey),
    url.PathEscape(flagKey),
)
q := url.Values{}
q.Set("filter", "environmentKey:"+envKey)
q.Set("limit", "1")
req, err := retryablehttp.NewRequestWithContext(ctx, "GET", path+"?"+q.Encode(), nil)
// ... setStandardHeaders (not setStartHeaders) for the GET ...
```

---

### `internal/rollouts/instructions.go` — flesh out StartInstruction + fix SemanticPatch

**Analog:** `internal/rollouts/instructions.go` (Phase 1 stub, lines 1–29, same file)

**PREREQUISITE FIX — SemanticPatch missing EnvironmentKey** (lines 9–12 in Phase 1):

The Phase 1 stub:
```go
type SemanticPatch struct {
    Comment      string        `json:"comment,omitempty"`
    Instructions []interface{} `json:"instructions"`
}
```

Must become (add `EnvironmentKey` first — it is required by the server per RESEARCH Pitfall 2):
```go
type SemanticPatch struct {
    EnvironmentKey string        `json:"environmentKey"`           // ADD — required by server
    Comment        string        `json:"comment,omitempty"`
    Instructions   []interface{} `json:"instructions"`
}
```

**Full StartInstruction shape** (replaces the Phase 1 single-field stub at lines 15–18):
```go
// StartInstruction kicks off an automated rollout. All field names match the gonfalon
// instruction shape exactly (PC-012: releaseKind in request vs kind in response;
// PC-013: originalVariationId is the unified name; PC-014: durationMillis from D-03).
type StartInstruction struct {
    Kind                        string                          `json:"kind"` // always "startAutomatedRelease"
    ReleaseKind                 string                          `json:"releaseKind"` // "guarded" | "progressive" (inferred per D-05)
    OriginalVariationID         string                          `json:"originalVariationId"` // UUID _id only — NOT variation key (Q1, RESEARCH.md)
    TargetVariationID           string                          `json:"targetVariationId"`   // UUID _id only
    RandomizationUnit           string                          `json:"randomizationUnit"`
    Stages                      []StageInput                    `json:"stages"`
    Metrics                     []MetricSource                  `json:"metrics,omitempty"`                      // guarded only
    MetricMonitoringPreferences map[string]MetricMonitoringPref `json:"metricMonitoringPreferences,omitempty"`  // guarded only; PC-010
    RuleID                      string                          `json:"ruleId,omitempty"`    // D-07: --rule-id; empty = fallthrough
}

type StageInput struct {
    Allocation     int   `json:"allocation"`     // basis points: 25% → 25000 (D-02: multiply percent × 1000)
    DurationMillis int64 `json:"durationMillis"` // D-03: time.ParseDuration(s).Milliseconds()
}

type MetricSource struct {
    Key string `json:"key"`
    // IsGroup omitted per D-06 — metric groups deferred to v1.1
}

type MetricMonitoringPref struct {
    AutoRollback bool `json:"autoRollback"` // false → pause; true → revert (D-04)
}
```

---

### `internal/rollouts/errors.go` — extend error code enum and mapAPIError

**Analog:** `internal/rollouts/errors.go` (same file, lines 14–25 for constants, lines 82–158 for mapAPIError)

**New constants** — add after `ErrCodeUnknownUpstream` (line 25):
```go
const (
    // ... existing constants ...
    ErrCodeRolloutAlreadyRunning       = "rollout_already_running"          // D-12
    ErrCodeFlagNotConfiguredForRollout = "flag_not_configured_for_rollout"  // D-12
    ErrCodeInvalidVariation            = "invalid_variation"                // D-12
    // ErrCodeBetaGateClosed already exists in Phase 1
)
```

**mapAPIError extension** — insert a new block BEFORE `case statusCode == http.StatusBadRequest` (line 128). Use `strings.Contains` / `strings.HasSuffix` on `apiBody.Message`. Message substrings are from gonfalon `instruction_start_automated_release.go` (HIGH confidence per RESEARCH Q3):

```go
// --- Phase 2 mutation-specific message matching (insert before the generic StatusBadRequest branch) ---
// Match message strings before branching on status code — the server wraps instruction errors
// as sempatch.NewInstructionError, and the exact HTTP status (400 vs 409) is unconfirmed for
// some messages (see RESEARCH.md Assumptions A1 and Open Question 1).
case strings.HasSuffix(apiBody.Message, " is off"):
    // "flag X is off" — server rejects startAutomatedRelease on a disabled flag.
    e.Code = ErrCodeFlagNotConfiguredForRollout
    e.Message = apiBody.Message
    e.NextAction = "Turn on the flag before starting a rollout"

case strings.Contains(apiBody.Message, "Flag must not have ongoing guarded rollout"),
    strings.Contains(apiBody.Message, "Flag must not have ongoing progressive rollout"):
    e.Code = ErrCodeRolloutAlreadyRunning
    e.Message = apiBody.Message
    e.NextAction = "Stop the current rollout before starting a new one, or check the rollouts list for the active rollout"

case strings.Contains(apiBody.Message, "instruction kind 'startAutomatedRelease' unsupported"):
    e.Code = ErrCodeBetaGateClosed
    e.Message = apiBody.Message
    e.NextAction = "Enable the release-guardian feature flag for this account in the LaunchDarkly UI"

case strings.Contains(apiBody.Message, "originalVariationId must be a valid variation id"),
    strings.Contains(apiBody.Message, "instruction targetVariationId and originalVariationId must be different"):
    e.Code = ErrCodeInvalidVariation
    e.Message = apiBody.Message
    e.NextAction = "Pass the variation UUID (_id) from the flag definition, not the variation key; run: ldcli flags get --flag <key> --output json | jq '.variations[]'"
// --- end Phase 2 block; existing StatusBadRequest branch follows ---
```

The message-matching block goes before (not inside) the `case statusCode == http.StatusBadRequest:` branch so it fires regardless of which 4xx code the server returns.

---

### `internal/rollouts/envelope.go` — add NewRolloutEnvelope

**Analog:** `internal/rollouts/envelope.go:NewListEnvelope` (lines 7–16, same file)

```go
// NewListEnvelope pattern (lines 7-16) to copy exactly, substituting Kind and type:
func NewListEnvelope(list *RolloutList) Envelope {
    return Envelope{
        SchemaVersion: SchemaVersionV1Beta1,
        Kind:          "RolloutList",
        Data:          list,
        Meta: &EnvelopeMeta{
            FetchedAt: time.Now().UTC(),
        },
    }
}
```

New helper to add:
```go
// NewRolloutEnvelope wraps a single *Rollout into the v1beta1 envelope with `kind: "Rollout"`.
// Used by the start and (future) status commands. Kind "Rollout" matches Phase 3's status
// command so consumers do not need to special-case envelope kinds across verbs.
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

### `internal/rollouts/mock_client.go` — hand-add Start method

**Analog:** `internal/rollouts/mock_client.go` (same file — copy the Get method shape, lines 36–50)

DO NOT use `go.uber.org/mock/mockgen`. Phase 1 chose the hand-written testify/mock pattern to match `internal/flags/mock_client.go` (RESEARCH Q7 — HIGH confidence). Running `go generate ./...` will NOT regenerate rollouts mocks; it drives oapi-codegen for the dev server.

**Get method shape to clone** (lines 36–50):
```go
func (c *MockClient) Get(
    _ context.Context,
    accessToken,
    baseURI,
    projKey,
    envKey,
    rolloutID string,
) (*Rollout, error) {
    args := c.Called(accessToken, baseURI, projKey, envKey, rolloutID)

    var r *Rollout
    if v := args.Get(0); v != nil {
        r = v.(*Rollout)
    }
    return r, args.Error(1)
}
```

New `Start` method to add (mirroring Get's nil-safe pointer pattern):
```go
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

The compile-time assertion `var _ Client = &MockClient{}` (line 17) will also fail until Start is added — deliberate.

---

### `internal/rollouts/start_test.go` — new httptest-based client tests

**Analog:** `internal/rollouts/client_test.go` (full file)

**Key helpers to reuse** (lines 38–85):
- `makeServer(t, statusCode, body)` — for happy-path tests and error-mapping tests
- `makeFlakyServer(t, failureStatus, successStatus, failuresBeforeSuccess, successBody)` — for 5xx retry tests
- `loadFixture(t, name)` — for loading `testdata/start_success.json`
- `recordedRequest.allPaths` — assert both PATCH path and GET path fired in sequence for two-step test

**Two-step test structure** (new pattern for Phase 2 — no existing analog; route via makeServer with path-conditional handler):
```go
// For the two-step test, the server must respond differently to PATCH vs GET.
// Pattern: use a custom handler that dispatches by method+path.
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method == "PATCH" {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte(`{}`)) // FeatureFlag body (discarded per PC-001)
        return
    }
    // GET re-fetch — return the list fixture
    w.WriteHeader(http.StatusOK)
    _, _ = w.Write([]byte(loadFixture(t, "start_success.json")))
}))
```

**testdata/start_success.json** fixture — must be a single-item list response matching the `rawRolloutList` shape. Use int64 unix-millis timestamps (NOT RFC 3339 strings) to match the real-API wire format confirmed in 01-SMOKE.md:
```json
{
  "items": [
    {
      "_id": "01JVTEST000000000000000001",
      "kind": "guarded",
      "environmentKey": "test",
      "originalVariationId": "uuid-false",
      "targetVariationId": "uuid-true",
      "randomizationUnit": "user",
      "createdAt": 1715353200000,
      "status": "in_progress",
      "stages": [
        {"allocation": 25000, "durationMillis": 300000},
        {"allocation": 50000, "durationMillis": 300000}
      ],
      "metricConfigurations": []
    }
  ]
}
```

**Error-mapping test pattern** (lines 282–294 in client_test.go):
```go
t.Run("Start maps 'flag X is off' message to flag_not_configured_for_rollout", func(t *testing.T) {
    srv, _ := makeServer(t, http.StatusBadRequest, `{"code":"bad_request","message":"flag my-flag is off"}`)
    defer srv.Close()

    c := rollouts.NewClient("test-version")
    _, err := c.Start(context.Background(), "tok", srv.URL, "p", "f", "env", rollouts.StartInstruction{})
    require.Error(t, err)

    var rerr *rollouts.RolloutError
    require.True(t, errors.As(err, &rerr))
    assert.Equal(t, rollouts.ErrCodeFlagNotConfiguredForRollout, rerr.Code)
})
```

**Header assertion pattern** (lines 152–168 in client_test.go) — for the PATCH, assert `Content-Type: application/json; domain-model=launchdarkly.semanticpatch`; for the GET re-fetch, assert `Content-Type: application/json`.

---

### `cmd/flags/rollouts/start.go` — new Cobra command handler

**Analog:** `cmd/flags/rollouts/list.go` (full file — exact structural match)

**Imports pattern** (lines 1–17 in list.go — copy and adjust):
```go
package rollouts

import (
    "encoding/json"
    stderrors "errors"
    "fmt"
    "strings"
    "strconv"
    "time"

    "github.com/spf13/cobra"
    "github.com/spf13/viper"

    "github.com/launchdarkly/ldcli/cmd/cliflags"
    resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
    "github.com/launchdarkly/ldcli/cmd/validators"
    "github.com/launchdarkly/ldcli/internal/errors"
    "github.com/launchdarkly/ldcli/internal/rollouts"
)
```

**NewStartCmd constructor** (mirrors NewListCmd, lines 36–48):
```go
func NewStartCmd(client rollouts.Client) *cobra.Command {
    cmd := &cobra.Command{
        Args:  validators.Validate(),
        Long:  startLongDescription,
        RunE:  startRunE(client),
        Short: "Start an automated rollout for a feature flag (beta)",
        Use:   "start",
    }
    cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
    initStartFlags(cmd)
    return cmd
}
```

**runE / startRunE closure** (mirrors runE in list.go, lines 60–103):
```go
func startRunE(client rollouts.Client) func(*cobra.Command, []string) error {
    return func(cmd *cobra.Command, _ []string) error {
        accessToken := viper.GetString(cliflags.AccessTokenFlag)
        baseURI     := viper.GetString(cliflags.BaseURIFlag)
        projKey     := viper.GetString(cliflags.ProjectFlag)
        flagKey     := viper.GetString(cliflags.FlagFlag)
        envKey      := viper.GetString(cliflags.EnvironmentFlag)

        // Parse + validate -- <build StartInstruction from viper values> --

        rollout, err := client.Start(cmd.Context(), accessToken, baseURI, projKey, flagKey, envKey, instr)
        if err != nil {
            return emitStartError(cmd, err)
        }

        env := rollouts.NewRolloutEnvelope(rollout)
        return emitStartSuccess(cmd, env, rollout)
    }
}
```

**emitSuccess pattern** (lines 108–119 in list.go — copy for start, replace `RolloutList` type with `*Rollout`):
```go
func emitStartSuccess(cmd *cobra.Command, env rollouts.Envelope, rollout *rollouts.Rollout) error {
    if cliflags.GetOutputKind(cmd) == "json" {
        body, err := json.MarshalIndent(env, "", "  ")
        if err != nil {
            return errors.NewErrorWrapped("failed to marshal rollouts envelope", err)
        }
        fmt.Fprintln(cmd.OutOrStdout(), string(body))
        return nil
    }
    fmt.Fprint(cmd.OutOrStdout(), RenderRolloutPlaintext(rollout))
    return nil
}
```

**emitError pattern** (lines 153–180 in list.go — copy exactly, change sentinel string):
```go
func emitStartError(cmd *cobra.Command, err error) error {
    code    := rollouts.ErrCodeUnknownUpstream
    message := err.Error()
    nextAction := ""

    var rerr *rollouts.RolloutError
    if stderrors.As(err, &rerr) && rerr != nil {
        code       = rerr.Code
        message    = rerr.Message
        nextAction = rerr.NextAction
    }

    if cliflags.GetOutputKind(cmd) == "json" {
        env := rollouts.NewErrorEnvelope(code, message, nextAction)
        body, mErr := json.MarshalIndent(env, "", "  ")
        if mErr != nil {
            return errors.NewErrorWrapped(message, mErr)
        }
        fmt.Fprintln(cmd.OutOrStdout(), string(body))
        return errors.NewError("rollouts start failed")  // short sentinel; root prints to stderr
    }

    return errors.NewError(message)
}
```

**CRITICAL — do NOT copy `cmd/flags/toggle.go`**: toggle.go uses `application/json` (RFC 6902 JSON Patch). `start.go` needs `application/json; domain-model=launchdarkly.semanticpatch`. The Content-Type override is in `setStartHeaders` in the client, not in the command layer — but the executor must not confuse the two command files as analogs.

---

### `cmd/flags/rollouts/start_test.go` — new command-layer tests

**Analog:** `cmd/flags/rollouts/list_test.go` (full file — exact structural match)

**Test package and imports** (lines 1–16 in list_test.go):
```go
package rollouts_test

import (
    "encoding/json"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/require"

    "github.com/launchdarkly/ldcli/cmd"
    "github.com/launchdarkly/ldcli/internal/analytics"
    "github.com/launchdarkly/ldcli/internal/rollouts"
)
```

**MockClient.On("Start", ...) setup pattern** (mirror List mock setup, lines 41–43):
```go
mockClient := &rollouts.MockClient{}
mockClient.On("Start", "abcd1234", mock.Anything, "test-proj", "test-flag", "test-env",
    mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
        return instr.ReleaseKind == "progressive" && len(instr.Stages) == 2
    })).Return(&rollouts.Rollout{ID: "new-rollout-id", Kind: "progressive"}, nil)
```

**CallCmd invocation pattern** (lines 51–52 in list_test.go):
```go
output, err := cmd.CallCmd(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
```

**JSON envelope assertion pattern** (lines 86–102 in list_test.go — adapt for single Rollout):
```go
var env rollouts.Envelope
require.NoError(t, json.Unmarshal(output, &env))
assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
assert.Equal(t, "Rollout", env.Kind)  // "Rollout" not "RolloutList"
rawData, err := json.Marshal(env.Data)
require.NoError(t, err)
var r rollouts.Rollout
require.NoError(t, json.Unmarshal(rawData, &r))
assert.Equal(t, "new-rollout-id", r.ID)
```

**Error envelope on stdout pattern** (lines 297–341 in list_test.go — use `cmd.CallCmdWithStderr`):
```go
stdout, stderr, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
require.Error(t, err)
var env rollouts.Envelope
require.NoError(t, json.Unmarshal(stdout, &env))
assert.Equal(t, "Error", env.Kind)
assert.NotContains(t, string(stderr), `"kind": "Error"`) // envelope must not leak to stderr
```

**Stages parsing test** — unique to Phase 2 (no list_test.go analog; new test):
```go
t.Run("--stages 25:60m,50:60m produces correct basis-points + millis", func(t *testing.T) {
    mockClient.On("Start", ..., mock.MatchedBy(func(instr rollouts.StartInstruction) bool {
        return len(instr.Stages) == 2 &&
            instr.Stages[0].Allocation == 25000 &&
            instr.Stages[0].DurationMillis == 3600000 &&
            instr.Stages[1].Allocation == 50000
    })).Return(...)
    // pass --stages "25:60m,50:60m" in args
})
```

---

### `cmd/flags/rollouts/rollouts.go` — add NewStartCmd

**Analog:** `cmd/flags/rollouts/rollouts.go` (same file — add one line)

**Existing AddCommand line** (line 52):
```go
cmd.AddCommand(NewListCmd(client))
```

Add immediately after:
```go
cmd.AddCommand(NewStartCmd(client))
```

No other changes to rollouts.go needed — the PersistentPreRun analytics and beta banner are inherited by start automatically.

---

### `cmd/cliflags/flags.go` — new flag constants for start verb

**Analog:** `cmd/cliflags/flags.go` existing constants (lines 31–70 — follow the exact `SCREAMING_SNAKE_CASE` constant + paired `*Description` constant pattern)

**Existing constant pattern to match** (lines 44–46 and 64–65):
```go
EnvironmentFlag            = "environment"
EnvironmentFlagDescription = "Default environment key"
```

**New constants to add** (insert in alphabetical order within the const block):
```go
OriginalVariationFlag            = "original-variation"
PauseOnRegressionFlag            = "pause-on-regression"
RandomizationUnitFlag            = "randomization-unit"
RevertOnRegressionFlag           = "revert-on-regression"
RuleIDFlag                       = "rule-id"
StagesFlag                       = "stages"
TargetVariationFlag              = "target-variation"

OriginalVariationFlagDescription  = "The variation UUID (_id) that represents the original/control variation. Obtain via: ldcli flags get --flag <key> --output json | jq '.variations[]'"
PauseOnRegressionFlagDescription  = "Metric key to monitor; pauses the rollout at current stage on regression (repeatable). Use --revert-on-regression to auto-rollback instead. A metric cannot appear in both flags."
RandomizationUnitFlagDescription  = "Randomization unit for the experiment (e.g. user, organization)"
RevertOnRegressionFlagDescription = "Metric key to monitor; automatically reverts the rollout on regression (repeatable). Use --pause-on-regression to pause instead. A metric cannot appear in both flags."
RuleIDFlagDescription             = "Existing rule UUID to roll out on. Omit for fallthrough (default rule). Obtain rule IDs from the LaunchDarkly UI or API."
StagesFlagDescription             = "Comma-separated list of stages as <allocation%>:<duration> (e.g. 25:60m,50:60m,100:60m). Allocation must be a whole percent integer [1-100]; duration must include a unit (60m, 1h30m, 300s). The CLI converts allocation to basis-points and duration to milliseconds for the API."
TargetVariationFlagDescription    = "The variation UUID (_id) that traffic will be shifted to. Obtain via: ldcli flags get --flag <key> --output json | jq '.variations[]'"
```

**Flag registration pattern** (from `cmd/flags/rollouts/flags.go:initListFlags`, lines 17–38 — copy for new `initStartFlags`):
```go
// Required string flag:
cmd.Flags().String(cliflags.StagesFlag, "", cliflags.StagesFlagDescription)
_ = cmd.MarkFlagRequired(cliflags.StagesFlag)
_ = cmd.Flags().SetAnnotation(cliflags.StagesFlag, "required", []string{"true"})
_ = viper.BindPFlag(cliflags.StagesFlag, cmd.Flags().Lookup(cliflags.StagesFlag))

// Optional repeatable string flag (--pause-on-regression / --revert-on-regression):
cmd.Flags().StringArray(cliflags.PauseOnRegressionFlag, nil, cliflags.PauseOnRegressionFlagDescription)
_ = viper.BindPFlag(cliflags.PauseOnRegressionFlag, cmd.Flags().Lookup(cliflags.PauseOnRegressionFlag))
```

Note: use `StringArray` (not `StringSlice`) for `--pause-on-regression` and `--revert-on-regression` to preserve values exactly as passed (no comma-splitting side effects).

---

## Shared Patterns

### Error emission to stdout in JSON mode (D-07 / AGENT-04)

**Source:** `cmd/flags/rollouts/list.go:emitError` (lines 153–180)
**Apply to:** `cmd/flags/rollouts/start.go:emitStartError` — exact copy, change sentinel string from `"rollouts list failed"` to `"rollouts start failed"`

The pattern is load-bearing: JSON-mode errors go to `cmd.OutOrStdout()`, not `cmd.ErrOrStderr()`. The returned error is a short sentinel so the root command's `Fprintln(os.Stderr, err)` does not re-emit the full envelope to stderr. This is tested explicitly in list_test.go `TestListErrorEnvelope` — replicate the same test in `start_test.go`.

### retryablehttp client reuse

**Source:** `internal/rollouts/client.go:newRetryableClient` (lines 76–86)
**Apply to:** Both the PATCH and the re-fetch GET in `Start`. Use `c.httpClient` (the shared instance, not a new one per call). `DefaultRetryPolicy` + `PassthroughErrorHandler` are already configured — do not change them.

### Viper read at RunE time

**Source:** `cmd/flags/rollouts/list.go:runE` (lines 62–66) and CONVENTIONS.md
**Apply to:** `cmd/flags/rollouts/start.go:startRunE` — all `viper.GetString/GetStringSlice/GetBool` calls must be inside the returned closure, not in `NewStartCmd`.

### mapAPIError as the single source of truth

**Source:** `internal/rollouts/errors.go:mapAPIError` (lines 82–158)
**Apply to:** All `resp.StatusCode >= 400` branches in `Start` (both PATCH and GET). Do not inline error code logic in `start.go` — always route through `mapAPIError`.

### RolloutError type assertion in command layer

**Source:** `cmd/flags/rollouts/list.go:emitError` (lines 158–162)
**Apply to:** `start.go:emitStartError` — use `stderrors.As(err, &rerr)` (standard library `errors.As`, not the `internal/errors` package) to extract `*rollouts.RolloutError` fields.
```go
var rerr *rollouts.RolloutError
if stderrors.As(err, &rerr) && rerr != nil {
    code       = rerr.Code
    message    = rerr.Message
    nextAction = rerr.NextAction
}
```

---

## Anti-Patterns — Do NOT Copy

### `cmd/flags/toggle.go` — WRONG Content-Type

`toggle.go` uses `PATCH` with `Content-Type: application/json` (RFC 6902 JSON Patch format). This is the **wrong** pattern for `start.go`. The rollouts PATCH requires `application/json; domain-model=launchdarkly.semanticpatch`. The two PATCH calls are entirely different protocol shapes. The executor must NOT treat `toggle.go` as an analog for the rollouts PATCH.

### `go.uber.org/mock/mockgen`

Do not run `mockgen` or `go generate ./...` expecting rollouts mock regeneration. The rollouts mock is intentionally hand-written (RESEARCH Q7). Running `go generate ./...` runs oapi-codegen and regenerates the dev server API — unrelated and potentially destructive if run in the wrong context.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `internal/rollouts/idempotency.go` (DELETE) | cleanup | N/A | Deleted per D-10; no analog needed |
| `internal/rollouts/testdata/start_success.json` | test fixture | N/A | New fixture; use list fixtures as shape reference |
| Stages parser (`parseStages` function inside start.go) | CLI-to-API transform | transform | No comparable multi-field string parser exists in ldcli; implement from scratch per RESEARCH Code Examples section |

---

## Metadata

**Analog search scope:** `internal/rollouts/`, `cmd/flags/rollouts/`, `cmd/cliflags/`
**Files read:** 10 source files + 3 planning documents
**Pattern extraction date:** 2026-05-13
