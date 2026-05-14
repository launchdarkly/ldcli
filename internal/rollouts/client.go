package rollouts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/launchdarkly/ldcli/internal/errors"
)

// ListOpts carries optional knobs for Client.List. Plan 01 leaves all fields zero-valued by
// the command layer; Plan 03 wires the --environment / --limit / --all flags.
type ListOpts struct {
	Environment string // optional environment-key filter
	Limit       int    // default 20 (D-05) when not All
	All         bool   // fetch the full history
}

// Client is the typed domain interface for the rollouts subtree. Phase 1 ships only `List` and
// `Get` per D-08; Phase 2 adds `Start`; Phase 4 adds `Stop` + `DismissRegression`. Keeping the
// interface narrow lets test doubles stay simple and forces planners to make each new method
// explicit.
type Client interface {
	List(ctx context.Context, accessToken, baseURI, projKey, flagKey string, opts ListOpts) (*RolloutList, error)
	Get(ctx context.Context, accessToken, baseURI, projKey, envKey, rolloutID string) (*Rollout, error)
	Start(ctx context.Context, accessToken, baseURI, projKey, flagKey, envKey string, instr StartInstruction) (*Rollout, error)
	Stop(ctx context.Context, accessToken, baseURI, projKey, flagKey, envKey string, instr StopInstruction) (*Rollout, error)
	GetMetricResult(ctx context.Context, accessToken, baseURI, projKey, flagKey, envKey, rolloutID, metricKey string) (*MetricResult, *float64, error)
}

// RolloutsClient is the concrete Client implementation. It owns a *retryablehttp.Client so
// connection pooling and retry policy survive across calls.
type RolloutsClient struct {
	cliVersion string
	httpClient *retryablehttp.Client
}

// Compile-time assertion: RolloutsClient satisfies Client.
var _ Client = RolloutsClient{}

// NewClient constructs a RolloutsClient with a freshly-configured retry HTTP client. The
// `cliVersion` is captured at construction so the User-Agent header does not need to read
// Viper at request time (CONVENTIONS.md anti-pattern).
func NewClient(cliVersion string) RolloutsClient {
	return RolloutsClient{
		cliVersion: cliVersion,
		httpClient: newRetryableClient(500*time.Millisecond, 8*time.Second),
	}
}

// NewClientWithRetryWaitsForTest is a test-only constructor that lets tests override the
// retry wait bounds so retry-envelope tests can run in milliseconds instead of seconds.
// Production callers should use NewClient.
func NewClientWithRetryWaitsForTest(cliVersion string, retryWaitMin, retryWaitMax time.Duration) RolloutsClient {
	return RolloutsClient{
		cliVersion: cliVersion,
		httpClient: newRetryableClient(retryWaitMin, retryWaitMax),
	}
}

// newRetryableClient builds the shared retryablehttp.Client used by every RolloutsClient
// method. Retry envelope: 4 retries, exponential backoff capped by retryWaitMax, retries 5xx
// + network errors only (DefaultRetryPolicy never retries 4xx except 429). Logger=nil prevents
// the library from printing request URLs or Authorization headers (T-02-01 threat mitigation).
//
// ErrorHandler is set to PassthroughErrorHandler so retry-exhaustion on a final 5xx/429
// response returns the response itself (rather than nil + a "giving up after N attempts"
// wrapped error). This lets the caller branch on resp.StatusCode and route through
// mapAPIError (→ ErrCodeUpstreamUnavailable for 5xx, ErrCodeRateLimited for 429) instead
// of falling through to mapTransportError.
func newRetryableClient(retryWaitMin, retryWaitMax time.Duration) *retryablehttp.Client {
	c := retryablehttp.NewClient()
	c.RetryMax = 4
	c.RetryWaitMin = retryWaitMin
	c.RetryWaitMax = retryWaitMax
	c.CheckRetry = retryablehttp.DefaultRetryPolicy
	c.Backoff = retryablehttp.DefaultBackoff
	c.Logger = nil
	c.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return c
}

// List returns the rollouts attached to a feature flag. Issues a GET against
// `/internal/projects/{projKey}/flags/{flagKey}/automated-releases`, retries 5xx up to
// RetryMax times, and converts the raw API DTOs to the CLI shape (RFC 3339 timestamps,
// nested Status block per D-02). 4xx responses are NEVER retried and map to one of the
// documented error.code values via mapAPIError (D-01 / FOUND-08).
func (c RolloutsClient) List(
	ctx context.Context,
	accessToken, baseURI, projKey, flagKey string,
	opts ListOpts,
) (*RolloutList, error) {
	// PAPERCUT: PC-011 — the `/internal/` URL prefix is access-control-irrelevant; the API
	// team has signaled the prefix may be dropped, but for now it's required.
	path := fmt.Sprintf("%s/internal/projects/%s/flags/%s/automated-releases",
		strings.TrimRight(baseURI, "/"),
		url.PathEscape(projKey),
		url.PathEscape(flagKey),
	)

	q := url.Values{}
	if opts.Environment != "" {
		// PAPERCUT: PC-002 — `filter` accepts array but only honors element [0]; we send
		// exactly one filter element here, which is the supported subset.
		q.Set("filter", "environmentKey:"+opts.Environment)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20 // D-05 default
	}
	// PAPERCUT: PC-003 — the list endpoint has no pagination (no offset/cursor); --all
	// best-effort asks for a large limit. Plan 03 will surface a meta.warning if response
	// length equals the requested limit (likely truncated upstream).
	if opts.All {
		limit = 1000
	}
	q.Set("limit", strconv.Itoa(limit))

	req, err := retryablehttp.NewRequestWithContext(ctx, "GET", path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, errors.NewErrorWrapped("failed to build request", err)
	}
	c.setStandardHeaders(req, accessToken)
	// No Idempotency-Key on GETs — it's a mutation-only header (Phase 2 wires it on PATCH/POST).

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

	var raw rawRolloutList
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.NewErrorWrapped("failed to parse response", err)
	}
	list := raw.toRolloutList()

	// Client-side deterministic sort: CreatedAt DESC, then ID ASC as tiebreaker (AGENT-05).
	// The upstream API does not guarantee an order on list responses; the CLI normalizes here
	// so consumers (humans, agents, CI) see stable output regardless of upstream behavior.
	//
	// Saturation hint (meta.warnings — list returned exactly `limit` items, likely truncated
	// upstream per PC-003) lives at the envelope-construction site (cmd/flags/rollouts/list.go)
	// rather than here, so internal/rollouts/ stays free of UI/envelope concerns.
	sort.Slice(list.Items, func(i, j int) bool {
		ti, tj := list.Items[i].CreatedAt, list.Items[j].CreatedAt
		if ti.Equal(tj) {
			return list.Items[i].ID < list.Items[j].ID
		}
		return ti.After(tj) // DESC
	})
	return list, nil
}

// Get returns a single rollout by ID. Issues a GET against
// `/internal/projects/{projKey}/environments/{envKey}/automated-releases/{rolloutID}`. Per
// RESEARCH.md ARCHITECTURE inventory the GET-by-ID requires the environment key in the
// path even though the rollout ID is globally unique (PAPERCUT PC-004 — Phase 1 annotates
// only at the call site since Get is not yet user-facing).
func (c RolloutsClient) Get(
	ctx context.Context,
	accessToken, baseURI, projKey, envKey, rolloutID string,
) (*Rollout, error) {
	// PAPERCUT: PC-011 — `/internal/` URL prefix; see List for the same anchor.
	path := fmt.Sprintf("%s/internal/projects/%s/environments/%s/automated-releases/%s",
		strings.TrimRight(baseURI, "/"),
		url.PathEscape(projKey),
		url.PathEscape(envKey),
		url.PathEscape(rolloutID),
	)

	req, err := retryablehttp.NewRequestWithContext(ctx, "GET", path, nil)
	if err != nil {
		return nil, errors.NewErrorWrapped("failed to build request", err)
	}
	c.setStandardHeaders(req, accessToken)

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

	var raw rawRollout
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.NewErrorWrapped("failed to parse response", err)
	}
	r := raw.toRollout()
	return &r, nil
}

// GetMetricResult fetches the latest snapshot for a single guarded-rollout metric.
// The metric-results endpoint sits under `/internal/projects/{p}/flags/{f}/environments/{e}/automated-releases/{id}/metric-results/{metricKey}`
// — note the flag key is part of the path, unlike the rollout Get path. Per the
// architecture research, status callers should parallelize one of these per metric.
// Time-series / chart data is intentionally not requested; only the latest snapshot.
//
// The second return value is the rollout-level probabilityOfMismatch, lifted out of the
// per-metric response per PC-020 (the upstream returns it as a per-metric field but the
// value is identical for every metric on the same rollout — so it belongs on the rollout,
// not on each metric). nil when the response did not include the field.
func (c RolloutsClient) GetMetricResult(
	ctx context.Context,
	accessToken, baseURI, projKey, flagKey, envKey, rolloutID, metricKey string,
) (*MetricResult, *float64, error) {
	path := fmt.Sprintf("%s/internal/projects/%s/flags/%s/environments/%s/automated-releases/%s/metric-results/%s",
		strings.TrimRight(baseURI, "/"),
		url.PathEscape(projKey),
		url.PathEscape(flagKey),
		url.PathEscape(envKey),
		url.PathEscape(rolloutID),
		url.PathEscape(metricKey),
	)

	req, err := retryablehttp.NewRequestWithContext(ctx, "GET", path, nil)
	if err != nil {
		return nil, nil, errors.NewErrorWrapped("failed to build request", err)
	}
	c.setStandardHeaders(req, accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, mapTransportError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, errors.NewErrorWrapped("failed to read response body", err)
	}

	if resp.StatusCode >= 400 {
		return nil, nil, mapAPIError(body, resp.StatusCode)
	}

	// Decode into a raw envelope that includes the per-metric ProbabilityOfMismatch field;
	// the public MetricResult type intentionally omits it so it can't leak per-metric.
	var raw struct {
		MetricResult
		ProbabilityOfMismatch *float64 `json:"probabilityOfMismatch,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, errors.NewErrorWrapped("failed to parse response", err)
	}
	mr := raw.MetricResult
	mr.MetricKey = metricKey
	return &mr, raw.ProbabilityOfMismatch, nil
}

// setStandardHeaders applies the four common headers (Authorization, Content-Type,
// User-Agent, LD-API-Version) to a retryablehttp.Request. The User-Agent matches the
// `internal/resources/Client` convention so analytics and observability stay consistent.
//
// LD-API-Version: the internal automated-releases API is gated behind the `beta` API version;
// without this header the server returns 403. Confirmed against real staging — synthetic
// httptest servers used in unit tests do not enforce this header, which is how the gap was
// missed during the Phase 1 unit-test pass (see quick task 260513-i1u).
func (c RolloutsClient) setStandardHeaders(req *retryablehttp.Request, accessToken string) {
	req.Header.Set("Authorization", accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
	req.Header.Set("LD-API-Version", "beta")
}

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
