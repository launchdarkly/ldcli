# Phase 1: List (foundation + first end-to-end slice) - Pattern Map

**Mapped:** 2026-05-12
**Files analyzed:** 14 (12 new, 3 modified)
**Analogs found:** 12 / 14 (2 net-new infrastructure files have no direct analog)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/flags/rollouts/rollouts.go` | cmd (parent group) | request-response | `cmd/dev_server/dev_server.go` | exact (parent-cmd with PersistentPreRun + persistent flags) |
| `cmd/flags/rollouts/list.go` | cmd (verb) | request-response | `cmd/flags/toggle.go` | exact (sibling under `flags`; same DI pattern) |
| `cmd/flags/rollouts/flags.go` | cmd (flag registration helper) | n/a | `cmd/flags/toggle.go:94-111` (`initFlags`) | exact |
| `cmd/flags/rollouts/plaintext.go` | output renderer (rollouts-specific) | transform | `internal/output/plaintext_fns.go` (`SingularPlaintextOutputFn`, `MultiplePlaintextOutputFn`) | role-match (table output is novel; verbs match) |
| `cmd/flags/rollouts/list_test.go` | test (integration via `cmd.CallCmd`) | request-response | `cmd/flags/toggle_test.go` | exact |
| `cmd/flags/rollouts/rollouts_test.go` | test (banner suppression) | n/a | — | no analog (new concern: TTY-gated banner) |
| `internal/rollouts/client.go` | client (typed domain) | request-response | `internal/flags/client.go` | role-match (interface + struct + `var _` + `NewClient(cliVersion)`); diverges in HTTP path (uses `retryablehttp` directly, not `client.New`) |
| `internal/rollouts/models.go` | dto | n/a | `internal/dev_server/model/*.go` (typed structs with `json:` tags) | role-match |
| `internal/rollouts/mock_client.go` | mock (testify-based) | n/a | `internal/flags/mock_client.go` | exact |
| `internal/rollouts/client_test.go` | test (`httptest.NewServer` round-trip) | request-response | `internal/resources/client_test.go` | exact |
| `internal/rollouts/errors.go` | error mapper (`error.code` enum) | transform | `internal/resources/client.go:73-111` (HTTP→errMap), `internal/errors/errors.go` | role-match (error envelope shape exists; rollouts-specific enum is new) |
| `internal/rollouts/status_mapping.go` | transform (UI parity logic) | transform | — | no analog (new domain concern) |
| `internal/rollouts/idempotency.go` | infra (header helper) | n/a | — | no analog (new infra; one-liner using `google/uuid`) |
| `internal/rollouts/instructions.go` | dto (stubbed for Phase 2/4) | n/a | `internal/flags/client.go:14-18` (`UpdateInput` for JSON-patch ops) | role-match |
| `cmd/cliflags/flags.go` (MODIFY) | config (constants) | n/a | self (append constants) | self-extension |
| `cmd/root.go` (MODIFY) | wiring | n/a | self (existing `APIClients`, `NewRootCommand`, `Execute`) | self-extension |
| `.planning/API-PAPERCUTS.md` | doc | n/a | — | no analog (template defined in RESEARCH.md §"API-PAPERCUTS.md") |

## Pattern Assignments

### `cmd/flags/rollouts/rollouts.go` (cmd, parent group)

**Analog:** `cmd/dev_server/dev_server.go` (parent-cmd pattern: persistent flags + `PersistentPreRun` for analytics)

**Imports pattern** (lines 1-15):
```go
package dev_server

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/resources"
)
```

**Constructor pattern** (lines 17-36 — `NewDevServerCmd`):
```go
func NewDevServerCmd(client resources.Client, analyticsTrackerFn analytics.TrackerFn, ldClient dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev-server",
		Short: "Development server",
		Long:  "Start and use a local development server for overriding flag values.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {

			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"dev-server",
				map[string]interface{}{
					"action": cmd.Name(),
				}))
		},
	}
```

**Persistent-flag binding pattern** (lines 38-65):
```go
cmd.PersistentFlags().String(
    cliflags.DevStreamURIFlag,
    cliflags.DevStreamURIDefault,
    cliflags.DevStreamURIDescription,
)
_ = viper.BindPFlag(cliflags.DevStreamURIFlag, cmd.PersistentFlags().Lookup(cliflags.DevStreamURIFlag))
```

**Subcommand registration + usage template** (lines 67-88):
```go
// Add subcommands here
cmd.AddCommand(NewListProjectsCmd(client))
// ...
cmd.SetUsageTemplate(resourcecmd.SubcommandUsageTemplate())
return cmd
```

**Adaptations for rollouts.go:**
- Replace `"dev-server"` use with `"rollouts-beta"`.
- The `PersistentPreRun` should additionally emit the beta banner to **stderr** when `cliflags.GetOutputKind(cmd) != "json"` AND `isTerminal(stderr)`. Use `golang.org/x/term.IsTerminal(int(os.Stderr.Fd()))`.
- **Banner copy (authoritative, must match Plan 01 verbatim):**
  - Line 1: `⚠ flags rollouts-beta is unstable; the command surface and JSON schema may change.`
  - Line 2 (indented two spaces): `  Pin to ldcli vX.Y.Z for production use.` — `X.Y.Z` is interpolated from `cmd.Root().Version` at runtime.
  - This is the single source of truth for banner wording across SKELETON.md, PATTERNS.md, and Plan 01.
- Constructor signature: `NewRolloutsCmd(client rollouts.Client, analyticsTrackerFn analytics.TrackerFn) *cobra.Command`.
- Persistent flags here are the ones shared across all rollouts verbs: `--flag` (required), `--project` (required). `--environment` is per-verb (only `list`, `get`, `status` use it).

---

### `cmd/flags/rollouts/list.go` (cmd, verb)

**Analog:** `cmd/flags/toggle.go`

**Imports pattern** (lines 1-16):
```go
package flags

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)
```

**Cobra constructor pattern** (lines 18-31):
```go
func NewToggleOnCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Turn a feature flag on",
		RunE:  runE(client),
		Short: "Turn a feature flag on",
		Use:   "toggle-on",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initFlags(cmd)

	return cmd
}
```

**`runE` closure pattern — reads Viper at `RunE` time, NOT constructor time** (lines 47-92):
```go
func runE(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// ...
		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/flags",
			viper.GetString(cliflags.ProjectFlag),
			viper.GetString(cliflags.FlagFlag),
		)
		// ...
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"PATCH",
			path,
			// ...
		)
		if err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}

		output, err := output.CmdOutput("update", cliflags.GetOutputKind(cmd), res, output.CmdOutputOpts{
			Fields:       cliflags.GetFields(cmd),
			ResourceName: "flags",
		})
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprint(cmd.OutOrStdout(), output+"\n")
		return nil
	}
}
```

**Adaptations for list.go:**
- Replace `client.MakeRequest(...)` with `client.List(ctx, accessToken, baseURI, projKey, flagKey, opts)` (signature defined in RESEARCH.md "Client Interface").
- Build `ListOpts` from Viper: `Environment` (optional), `Limit` (default 20 — D-05), `All` (bool — D-05).
- Do NOT use `output.CmdOutput` directly for rollouts — the existing dispatcher works on flat `resource` maps. The rollouts JSON envelope (`schemaVersion`/`kind`/`data`/`meta` per FOUND-03) is a typed struct that should be marshaled directly with `json.MarshalIndent` for JSON output; plaintext should call the new `plaintext.go` table renderer. See "Shared Patterns: JSON envelope emission" below.
- `Args: validators.Validate()` — same as analog.

---

### `cmd/flags/rollouts/flags.go` (cmd, flag registration helper)

**Analog:** `cmd/flags/toggle.go:94-111` (`initFlags`)

**Per-flag registration pattern**:
```go
func initFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.EnvironmentFlag, "", "The environment key")
	_ = cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.EnvironmentFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))

	cmd.Flags().String(cliflags.FlagFlag, "", "The feature flag key")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().Bool(cliflags.DryRunFlag, false, cliflags.DryRunFlagDescription)
}
```

**Adaptations for list:**
- `--flag` is required (parent-cmd persistent flag).
- `--project` is required (parent-cmd persistent flag).
- `--environment` (optional, list-local — D-04 keeps env filter).
- `--limit` int, default 20 (D-05).
- `--all` bool, default false (D-05).
- `--detailed` bool, default false (D-06).

**Required vs optional flag pattern:** required flags receive the `MarkFlagRequired` + `SetAnnotation` triple; optional flags get just `cmd.Flags().X(...)` plus an optional `viper.BindPFlag`.

---

### `cmd/flags/rollouts/plaintext.go` (output renderer, rollouts-specific)

**Analog:** `internal/output/plaintext_fns.go` (existing per-resource renderer pattern)

**Renderer signature pattern** (`internal/output/plaintext_fns.go:30-49`):
```go
var ErrorPlaintextOutputFn = func(r resource) string {
	var parts []string
	switch {
	// ...
	}
	return strings.Join(parts, "")
}
```

**Existing table dispatch** (`internal/output/resource_output.go:22-49` — `CmdOutput`):
```go
func CmdOutput(action string, outputKind string, input []byte, opts ...CmdOutputOpts) (string, error) {
	// ...
	if outputKind == "json" {
		if len(fields) > 0 {
			filtered, err := filterFields(input, fields)
			if err != nil {
				return string(input), nil
			}
			return string(filtered), nil
		}
		return string(input), nil
	}
	if len(fields) > 0 {
		fmt.Fprintln(os.Stderr, "note: --fields is only supported with JSON output; ignoring")
	}
	// ...
}
```

**Existing table utility:** `internal/output/table.go` defines table-rendering helpers; reuse those instead of hand-rolling alignment logic.

**Adaptations for rollouts plaintext:**
- Write `RenderRolloutListPlaintext(list *rollouts.RolloutList, detailed bool) string` and `RenderRolloutPlaintext(r *rollouts.Rollout, detailed bool) string`.
- Default 5-column table: `ID | kind | environment | state/label | started` (D-06).
- `--detailed` adds: `variations`, `endedAt`, `latestStageIndex`, raw `status.status`.
- Reuse `internal/output/table.go` table helpers (verified present per `ls` of `internal/output/`).
- Timestamps emitted as **RFC 3339 UTC** (AGENT-04, per RESEARCH.md "Pitfall 2").
- Place in `cmd/flags/rollouts/plaintext.go` (per RESEARCH.md structure) rather than `internal/output/` — keeps the rollouts surface co-located. If linting or convention complains, alternative is `internal/output/rollouts.go`; both are equivalent — planner picks.

---

### `cmd/flags/rollouts/list_test.go` (test, integration)

**Analog:** `cmd/flags/toggle_test.go`

**Test harness invocation pattern** (lines 14-48):
```go
func TestToggleOn(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{...}`),
	}

	t.Run("succeeds with plaintext output", func(t *testing.T) {
		args := []string{
			"flags", "toggle-on",
			"--access-token", "abcd1234",
			"--environment", "test-env",
			"--flag", "test-flag",
			"--project", "test-proj",
		}
		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient: mockClient,
			},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Equal(t, `[...]`, string(mockClient.Input))
		assert.Contains(t, string(output), "Successfully updated\n\nKey:")
	})
```

**`cmd.CallCmd` signature** (`cmd/cmdtest.go:25-59`):
```go
func CallCmd(
	t *testing.T,
	clients APIClients,
	trackerFn analytics.TrackerFn,
	args []string,
) ([]byte, error) {
	rootCmd, err := NewRootCommand(
		config.NewService(&resources.MockClient{}),
		trackerFn,
		clients,
		"test",
		false,
		func() bool { return true },
		nil,
	)
	// ...
}
```

**Adaptations:**
- `APIClients{RolloutsClient: mockClient}` — requires extending `APIClients` struct in `cmd/root.go` AND extending `cmd/root.go:Execute()` to construct the real `rollouts.NewClient(version)`.
- Tests verify JSON envelope contains `"schemaVersion":"rollouts.v1beta1"` and `"kind":"RolloutList"` (D-07).
- Tests verify mockClient was called with `ListOpts{Limit: 20}` by default; `ListOpts{All: true}` with `--all`.
- Tests verify exit-code-1 path (any error per D-01) by checking `err != nil` from `CallCmd`.

---

### `cmd/flags/rollouts/rollouts_test.go` (test, banner suppression)

**Analog:** none — new concern.

**Pattern to apply (constructive):**
- Test 1: when `cmd.CallCmd` is invoked with TTY (the harness sets `isTerminal: func() bool { return true }`) AND `--output plaintext`, the banner is present in stderr.
- Test 2: with `--output json` (or `--json`), banner is NOT in stderr.
- Banner is written to `cmd.ErrOrStderr()`; test must call `cmd.SetErr(&bytes.Buffer{})` separately from the existing `cmd.SetOut`. **This requires extending `CallCmd` to also return stderr, OR adding a new `CallCmdWithStderr` helper.** Recommend a new helper in `cmd/cmdtest.go` so we don't break the existing `CallCmd` signature used by every other test in the codebase.

---

### `internal/rollouts/client.go` (client, typed domain)

**Analog:** `internal/flags/client.go`

**Imports pattern** (lines 1-12):
```go
package flags

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"github.com/launchdarkly/ldcli/internal/client"
	"github.com/launchdarkly/ldcli/internal/errors"
)
```

**Interface + struct + compile-time assertion pattern** (lines 20-43):
```go
type Client interface {
	Create(ctx context.Context, accessToken, baseURI, name, key, projKey string) ([]byte, error)
	Get(ctx context.Context, accessToken, baseURI, key, projKey, envKey string) ([]byte, error)
	Update(...) ([]byte, error)
}

type FlagsClient struct {
	cliVersion string
}

var _ Client = FlagsClient{}

func NewClient(cliVersion string) FlagsClient {
	return FlagsClient{
		cliVersion: cliVersion,
	}
}
```

**Method implementation pattern** (lines 45-66 — `Create`):
```go
func (c FlagsClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key,
	projectKey string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	post := ldapi.NewFeatureFlagBody(name, key)
	flag, _, err := client.FeatureFlagsApi.PostFeatureFlag(ctx, projectKey).FeatureFlagBody(*post).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
	}

	responseJSON, err := json.Marshal(flag)
	if err != nil {
		return nil, err
	}

	return responseJSON, nil
}
```

**Adaptations (significant divergence from analog):**
- `internal/flags/client.go` returns `[]byte` (raw JSON) and uses the generated `ldapi.APIClient`. Rollouts deliberately does NOT — it uses `retryablehttp.Client` directly and returns typed structs (`*RolloutList`, `*Rollout`) per CONTEXT.md "Integration Points" line 125 and RESEARCH.md "Architecture decision" §"Retry Layer Wiring".
- Mirror the `Client` interface shape exactly; mirror the `var _ Client = RolloutsClient{}` assertion.
- Constructor: `NewClient(cliVersion string) RolloutsClient` — same shape, but the struct ALSO holds `httpClient *retryablehttp.Client` constructed via a `newRetryableClient()` helper (RESEARCH.md lines 218-240).
- Path: `/internal/projects/{p}/flags/{flagKey}/automated-releases` (NOT `/api/v2/...`) — papercut PC-011.

**Secondary analog for HTTP-layer concerns:** `internal/resources/client.go:46-112` (`MakeRequest`)

**Auth header + User-Agent pattern** (lines 53-59):
```go
req.Header.Add("Authorization", accessToken)
req.Header.Add("Content-Type", contentType)
req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
if isBeta {
    req.Header.Set("LD-API-Version", "beta")
}
req.URL.RawQuery = query.Encode()
```

**HTTP error mapping pattern** (lines 73-111):
```go
if res.StatusCode < http.StatusBadRequest {
    return body, nil
}

if len(body) > 0 {
    var errMap map[string]interface{}
    if err := json.Unmarshal(body, &errMap); err != nil {
        errMap = map[string]interface{}{
            "code":       strings.ToLower(strings.ReplaceAll(http.StatusText(res.StatusCode), " ", "_")),
            "message":    string(body),
            "statusCode": res.StatusCode,
        }
    } else {
        if _, exists := errMap["statusCode"]; !exists {
            errMap["statusCode"] = res.StatusCode
        }
    }
    if suggestion := errors.SuggestionForStatus(res.StatusCode, baseURI); suggestion != "" {
        errMap["suggestion"] = suggestion
    }
    body, _ = json.Marshal(errMap)
    return body, errors.NewError(string(body))
}
```

**Adaptations for rollouts:**
- Use `retryablehttp.NewRequestWithContext` not `http.NewRequest` (so the retry wrapper can re-execute).
- Authorization header value is the raw access token (matches `resources.Client` precedent — no `Bearer ` prefix).
- Error mapping logic is shifted into `internal/rollouts/errors.go` (see below) and returns a typed `error.code` enum value, not a raw map.
- Call `errors.SuggestionForStatus(...)` for parity with existing error envelopes.

---

### `internal/rollouts/models.go` (dto)

**Analog:** `internal/dev_server/model/*.go` (typed structs with `json:` tags) — RESEARCH.md identifies this. The simpler nearby precedent for "DTO with json tags + converter":

`internal/flags/client.go:14-18`:
```go
type UpdateInput struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}
```

**Adaptations:** Hand-roll all types per RESEARCH.md lines 249-364 (`Rollout`, `Stage`, `Event`, `MetricConfiguration`, `Link`, `RolloutList`, `StatusBlock`). Include both `Rollout.Kind` ("guarded"/"progressive" — API name) AND nested `Rollout.Status.Kind` (5-bucket — D-02) to avoid the `kind` name collision flagged in RESEARCH.md A1.

**Critical:** Timestamps in the struct should be `time.Time` (Go) but emitted as **RFC 3339 UTC** in JSON (AGENT-04). The API returns int64 unixMillis; converter functions in `models.go` (`rawRolloutList.toRolloutList()`) handle the conversion. This pattern has no analog in the existing codebase but is well-specified in RESEARCH.md.

---

### `internal/rollouts/mock_client.go` (mock, testify-based)

**Analog:** `internal/flags/mock_client.go` — exact match.

**Full pattern to copy verbatim** (entire 53-line file):
```go
package flags

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type MockClient struct {
	mock.Mock
}

var _ Client = &MockClient{}

func (c *MockClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key,
	projKey string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, name, key, projKey)

	return args.Get(0).([]byte), args.Error(1)
}
```

**Adaptations:**
- Package `rollouts`.
- One method per `Client` interface method (`List`, `Get` for Phase 1 — D-08).
- Return type wrappers per RESEARCH.md Example 3 (lines 1421-1444):
```go
var list *RolloutList
if v := args.Get(0); v != nil {
    list = v.(*RolloutList)
}
return list, args.Error(1)
```
- Nil-safe extraction is required because rollouts methods return pointers (`*RolloutList`, `*Rollout`) — the flags analog returns slice/byte values which can't be nil-cast.

---

### `internal/rollouts/client_test.go` (test, httptest round-trip)

**Analog:** `internal/resources/client_test.go`

**httptest pattern** (lines 15-25):
```go
func TestMakeUnauthenticatedRequest(t *testing.T) {
	t.Run("with a successful response returns body and no error", func(t *testing.T) {
		server := makeServer(t, http.StatusOK, `{"message": "success"}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		response, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		require.NoError(t, err)
		assert.JSONEq(t, `{"message": "success"}`, string(response))
	})
```

**Adaptations:**
- Construct an `httptest.NewServer` that returns canned `testdata/list_*.json` fixtures (RESEARCH.md lines 170-173).
- Set `baseURI` to `server.URL`; client's `List` should target `server.URL/internal/projects/...`.
- Verify retry behavior: a `makeServer` variant that returns 500 on first N calls, then 200, asserts exactly N+1 requests received.
- Verify request headers (Authorization, Content-Type, User-Agent, Idempotency-Key absent for GET).
- Verify 4xx is **never** retried (return 400 once; assert exactly 1 request).

---

### `internal/rollouts/errors.go` (error mapper)

**Primary analog:** `internal/errors/errors.go` (existing `Error` type, `NewError`, `NewErrorWrapped`, `APIError`).

**Existing `Error` type** (`internal/errors/errors.go:15-46`):
```go
type Error struct {
	err     error
	message string
}

func (e Error) Error() string {
	return e.message
}

func (e Error) Unwrap() error {
	return e.err
}

func (e Error) Is(err error) bool {
	_, ok := err.(Error)
	return ok
}

func NewError(message string) error {
	return errors.WithStack(Error{
		err:     errors.New(message),
		message: message,
	})
}

func NewErrorWrapped(message string, underlying error) error {
	return errors.WithStack(Error{
		err:     underlying,
		message: message,
	})
}
```

**Secondary analog:** `internal/resources/client.go:82-111` (HTTP status → `errMap` with `code`, `message`, `statusCode`, `suggestion`).

**Adaptations:**
- Define `RolloutError` struct with fields `Code string`, `Message string`, `NextAction string`, `StatusCode int`, `RetryAfter time.Duration` (RESEARCH.md §"error.code Enum & Mapping").
- Define enum constants per RESEARCH.md "Enum (Phase 1)":
  - `ErrCodeUnauthorized`, `ErrCodeForbidden`, `ErrCodeNotFound`, `ErrCodeBadRequest`, `ErrCodeConflict`, `ErrCodeRateLimited`, `ErrCodeServerError`, `ErrCodeNetworkError`, `ErrCodeTimeout`, `ErrCodeUnsupportedResponse`.
- Implement `mapAPIError(body []byte, status int) error` — RESEARCH.md "Mapping function" §lines 762-819.
- Implement `mapTransportError(err error) error` for network errors.
- Reuse `errors.SuggestionForStatus` for the `nextAction` field where applicable.
- Embed `errors.NewErrorWrapped` so the existing root-cmd error printing (cmd/root.go:330) Just Works.

---

### `internal/rollouts/status_mapping.go` (transform, UI parity)

**Analog:** none — new domain concern.

**Pattern to construct:**
```go
package rollouts

// MapStatus returns the 5-bucket kind + human-readable label for a raw API status.
// Mirrors gonfalon's GuardedRolloutUIStates.tsx (UI parity per REQ-UX-01, D-02).
func MapStatus(r *Rollout) StatusBlock {
    rawStatus := r.RawStatus  // the original API status string on the wire
    rule := formatRule(r.RuleIDOrFallthrough) // "the default rule" | "rule <id>"
    metrics := formatMetrics(r.MetricConfigurations) // "metric-a, metric-b"
    alloc := currentAllocationPct(r) // basis points → "25"

    switch rawStatus {
    case "not_started", "waiting":
        return StatusBlock{
            Status: rawStatus,
            Kind:   "active",
            Label:  fmt.Sprintf("Monitoring %s", rule),
        }
    // ... 12 more cases per CONTEXT.md <specifics> lines 132-148
    }
}
```

The table is fully specified in CONTEXT.md `<specifics>` (lines 132-148) and RESEARCH.md §"Status Mapping: 13 UI States → 5 Kinds" (line 835).

**Companion test file:** `status_mapping_test.go` — table-driven, one entry per the 13 documented UI states. Mirror `internal/output/plaintext_fns_internal_test.go` pattern for table-driven tests.

---

### `internal/rollouts/idempotency.go` (infra, header helper)

**Analog:** none — net-new.

**Pattern (god-mode simple):**
```go
package rollouts

import "github.com/google/uuid"

// SetIdempotencyKey assigns a UUIDv4 to the request's Idempotency-Key header.
// Phase 1: wired but not exercised (no mutations). Phase 2+: exercised by Start/Stop.
func SetIdempotencyKey(req *retryablehttp.Request) {
    req.Header.Set("Idempotency-Key", uuid.NewString())
}

// SetIdempotencyKeyValue is the variant for user-supplied keys (--idempotency-key flag).
func SetIdempotencyKeyValue(req *retryablehttp.Request, key string) {
    if key == "" {
        key = uuid.NewString()
    }
    req.Header.Set("Idempotency-Key", key)
}
```

`google/uuid` is already vendored (`cmd/root.go:13` imports it for analytics client ID generation).

---

### `internal/rollouts/instructions.go` (dto, stubs for Phase 2/4)

**Analog:** `internal/flags/client.go:14-18` (`UpdateInput` for JSON-patch-style instructions).

**Adaptations:** Define `SemanticPatch` envelope, `StartInstruction`, `StopInstruction`, `DismissRegressionInstruction` types **as struct skeletons only** in Phase 1. The actual Phase 1 client interface (D-08 — `List` + `Get` only) doesn't call them. They land here so Phase 2 doesn't need to invent the file structure.

---

### `cmd/cliflags/flags.go` (MODIFY)

**Pattern:** Append to the existing `const ( ... )` block at lines 25-47.

**New constants to add:**
```go
// Inside the existing const block (alphabetize within):
AllFlag        = "all"
DetailedFlag   = "detailed"
LimitFlag      = "limit"
// IdempotencyKeyFlag = "idempotency-key"  // (optional per D-Discretion; planner picks)

// And matching descriptions outside the alphabetized cluster:
AllFlagDescription      = "Return all rollouts (ignores --limit)"
DetailedFlagDescription = "Include detailed fields (variations, ended-at, current stage index, raw API status)"
LimitFlagDescription    = "Maximum number of rollouts to return (default 20)"
```

**Existing pattern to mirror** (lines 30-47):
```go
AccessTokenFlag  = "access-token"
AnalyticsOptOut  = "analytics-opt-out"
BaseURIFlag      = "base-uri"
// ...
ProjectFlag      = "project"
```

Note: `FlagFlag` and `EnvironmentFlag` already exist (lines 39, 41) — reuse them.

---

### `cmd/root.go` (MODIFY)

**Three modification sites:**

**1. `APIClients` struct (lines 40-47)** — add `RolloutsClient` field:
```go
type APIClients struct {
	DevClient          dev_server.Client
	EnvironmentsClient environments.Client
	FlagsClient        flags.Client
	MembersClient      members.Client
	ProjectsClient     projects.Client
	ResourcesClient    resources.Client
	RolloutsClient     rollouts.Client  // NEW
}
```

**2. Subcommand registration (lines 263-275)** — add rollouts-beta as child of `flags`:
```go
for _, c := range cmd.Commands() {
    if c.Name() == "flags" {
        c.AddCommand(flagscmd.NewToggleOnCmd(clients.ResourcesClient))
        c.AddCommand(flagscmd.NewToggleOffCmd(clients.ResourcesClient))
        c.AddCommand(flagscmd.NewArchiveCmd(clients.ResourcesClient))
        c.AddCommand(rolloutscmd.NewRolloutsCmd(clients.RolloutsClient, analyticsTrackerFn))  // NEW
    }
    // ...
}
```

**3. `Execute()` client construction (lines 282-290)** — instantiate `RolloutsClient`:
```go
clients := APIClients{
    DevClient:          dev_server.NewClient(version),
    EnvironmentsClient: environments.NewClient(version),
    FlagsClient:        flags.NewClient(version),
    MembersClient:      members.NewClient(version),
    ProjectsClient:     projects.NewClient(version),
    ResourcesClient:    resources.NewClient(version),
    RolloutsClient:     rollouts.NewClient(version),  // NEW
}
```

**New import at top of `cmd/root.go`:**
```go
rolloutscmd "github.com/launchdarkly/ldcli/cmd/flags/rollouts"
"github.com/launchdarkly/ldcli/internal/rollouts"
```

Place alongside existing `flagscmd` and `internal/flags` imports. The short-alias convention is established by `flagscmd`, `configcmd`, etc.

---

### `.planning/API-PAPERCUTS.md` (new doc)

**Analog:** none — new doc.

**Pattern:** Use the template specified in RESEARCH.md §"API-PAPERCUTS.md: Seeded Content" (lines 1016-1090). Seed with all 16 papercuts (PC-001 through PC-016) from `.planning/research/ARCHITECTURE.md`.

---

## Shared Patterns

### Auth / Token Reading

**Source:** `cmd/flags/toggle.go:68` and `internal/resources/client.go:54`

**Apply to:** All new `*.RunE` closures and the `RolloutsClient.List`/`Get` implementations.

```go
// At RunE time (NOT constructor time):
accessToken := viper.GetString(cliflags.AccessTokenFlag)
baseURI     := viper.GetString(cliflags.BaseURIFlag)
projKey     := viper.GetString(cliflags.ProjectFlag)
flagKey     := viper.GetString(cliflags.FlagFlag)

// In HTTP request:
req.Header.Add("Authorization", accessToken)  // No "Bearer " prefix — matches resources.Client precedent
req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
```

### Error Handling (root command surface)

**Source:** `cmd/root.go:328-331`

```go
case err != nil:
    outcome = analytics.ERROR
    fmt.Fprintln(os.Stderr, err.Error())
    os.Exit(1)
```

**Apply to:** All new rollouts commands. Returning ANY error from `RunE` invokes this path — exit 1 (per D-01). Do NOT call `os.Exit` from inside the rollouts code. Errors flow up via `cobra.Command.RunE`'s error return.

**JSON-mode error rendering:** when `cliflags.GetOutputKind(cmd) == "json"`, the error returned from `RunE` should be a typed `RolloutError` whose `Error()` method returns a complete JSON envelope (`schemaVersion`/`kind: "Error"`/`error.code`/`error.message`/`error.nextAction`). The existing `cmd/root.go:330` `Fprintln(os.Stderr, err.Error())` will print that JSON unchanged.

**Plaintext-mode error rendering:** the error's `Error()` returns a human-readable string; `cmd/root.go:330` prints it. Match the existing `output.NewCmdOutputError` pattern shown in `cmd/flags/toggle.go:77` for plaintext errors.

### Analytics tracking

**Source:** `cmd/dev_server/dev_server.go:22-35`

**Apply to:** `cmd/flags/rollouts/rollouts.go`'s `PersistentPreRun` (so every rollout subcommand emits a tracked event).

```go
PersistentPreRun: func(cmd *cobra.Command, args []string) {
    tracker := analyticsTrackerFn(
        viper.GetString(cliflags.AccessTokenFlag),
        viper.GetString(cliflags.BaseURIFlag),
        viper.GetBool(cliflags.AnalyticsOptOut),
    )
    tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
        cmd,
        "flags-rollouts-beta",
        map[string]interface{}{
            "action": cmd.Name(),
        }))
},
```

Event names: `flags-rollouts-beta-list` (Phase 1), `-start` (P2), `-status` (P3), `-stop` / `-dismiss-regression` (P4).

### TTY Detection (banner gate)

**Source:** `cmd/root.go:303` — existing pattern.

```go
func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
```

**Apply to:** banner emission in `cmd/flags/rollouts/rollouts.go` `PersistentPreRun`. Use `os.Stderr.Fd()` for the banner check (banner goes to stderr, output goes to stdout).

```go
import "golang.org/x/term"

isStderrTTY := term.IsTerminal(int(os.Stderr.Fd()))
isJSON      := cliflags.GetOutputKind(cmd) == "json"
if isStderrTTY && !isJSON {
    fmt.Fprintln(cmd.ErrOrStderr(), "⚠ flags rollouts-beta is unstable; the command surface and JSON schema may change.")
    fmt.Fprintf(cmd.ErrOrStderr(), "  Pin to ldcli %s for production use.\n", cmd.Root().Version)
}
```

**Existing TTY default for `--output`** is already wired at `cmd/root.go:222-227` — do NOT re-implement.

### JSON Envelope Emission

**Source:** none (rollouts-only contract per FOUND-03 / D-07).

**Apply to:** All rollout commands' success path AND error path.

```go
// internal/rollouts/envelope.go (helper)
type Envelope struct {
    SchemaVersion string      `json:"schemaVersion"`
    Kind          string      `json:"kind"`
    Data          interface{} `json:"data,omitempty"`
    Meta          *Meta       `json:"meta,omitempty"`
    Error         *ErrInfo    `json:"error,omitempty"`
}

func NewListEnvelope(list *RolloutList) Envelope {
    return Envelope{
        SchemaVersion: "rollouts.v1beta1",
        Kind:          "RolloutList",
        Data:          list,
    }
}
```

Concrete shapes are specified in RESEARCH.md §"JSON Envelope: Concrete Go Types" (lines 639-727). The envelope is marshaled with `json.MarshalIndent(env, "", "  ")` for JSON output — do not route through `internal/output/output.go`'s existing `CmdOutput` which assumes flat-map resources.

### `validators.Validate()` on Cobra commands

**Source:** `cmd/flags/toggle.go:20`, `cmd/flags/archive.go:20`

```go
cmd := &cobra.Command{
    Args: validators.Validate(),
    // ...
}
```

**Apply to:** Every new Cobra command constructor (`NewRolloutsCmd`, `NewListCmd`).

### Subcommand Usage Template

**Source:** `cmd/flags/toggle.go:27`, `cmd/dev_server/dev_server.go:86`

```go
cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
```

**Apply to:** `NewRolloutsCmd` (parent) AND each verb (`NewListCmd`).

## No Analog Found

Files with no close match in the codebase (planner should use RESEARCH.md patterns instead):

| File | Role | Data Flow | Reason | Where to find spec |
|------|------|-----------|--------|---------|
| `internal/rollouts/status_mapping.go` | transform (UI parity) | transform | No precedent for UI-derived label tables in ldcli | CONTEXT.md `<specifics>` lines 132-148; RESEARCH.md §"Status Mapping" line 835 |
| `internal/rollouts/idempotency.go` | infra helper | n/a | New concern; one-liner | RESEARCH.md §"Idempotency-Key Plumbing" line 510 |
| `internal/rollouts/envelope.go` (helper for JSON envelope) | dto/infra | n/a | New cross-cutting concern (no existing CLI does versioned envelopes) | RESEARCH.md §"JSON Envelope: Concrete Go Types" line 639 |
| `cmd/flags/rollouts/rollouts_test.go` (banner suppression) | test | n/a | Banner concept is new | Build minimally per "Pattern Assignments" section above |
| `.planning/API-PAPERCUTS.md` | doc | n/a | New doc | RESEARCH.md §"API-PAPERCUTS.md: Seeded Content" line 1016 |
| Retry layer wiring inside `internal/rollouts/client.go` | infra | request-response | No existing ldcli command uses `retryablehttp` | RESEARCH.md §"Retry Layer Wiring" lines 416-509 (full code) |

## Metadata

**Analog search scope:**
- `cmd/flags/*.go` (full)
- `cmd/dev_server/dev_server.go` (full — for parent-cmd pattern)
- `cmd/cmdtest.go` (full — for `CallCmd` test harness)
- `cmd/root.go` (full — for `APIClients`, `Execute`, banner-relevant TTY pattern at lines 222-238)
- `cmd/cliflags/flags.go` (full)
- `internal/flags/*.go` (full)
- `internal/resources/client.go` + `client_test.go` (full — for HTTP error mapping and httptest pattern)
- `internal/errors/errors.go` + `suggestions.go` (relevant for `SuggestionForStatus`)
- `internal/output/output.go`, `outputters.go`, `plaintext_fns.go`, `resource_output.go` (full — for dispatch + renderer pattern)

**Files scanned:** 17

**Key cross-cutting findings:**
- The existing `output.CmdOutput` dispatcher is **not reusable as-is** for the rollouts JSON envelope — it operates on flat `resource` maps and would lose the typed envelope shape. Rollouts needs its own envelope-aware emitter.
- All required external deps for Phase 1 are present except `github.com/hashicorp/go-retryablehttp@v0.7.8` (the only `go get` step).
- `google/uuid` is already vendored (used for analytics client ID generation in `cmd/root.go:13`); the new `idempotency.go` reuses it.
- `golang.org/x/term` is already a direct dep (used in `cmd/root.go:303` for TTY check); reuse for the banner gate — do NOT introduce `mattn/go-isatty`.
- The `CallCmd` test harness in `cmd/cmdtest.go:25-59` only captures stdout; banner-suppression tests need either a new helper that also captures stderr, or a direct construction of `NewRootCommand` with explicit `cmd.SetErr(...)`.

**Pattern extraction date:** 2026-05-12
