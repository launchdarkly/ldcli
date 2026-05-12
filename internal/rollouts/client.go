package rollouts

import (
	"context"
	"time"

	"github.com/hashicorp/go-retryablehttp"
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
}

// RolloutsClient is the concrete Client implementation. It owns a *retryablehttp.Client so
// connection pooling and retry policy survive across calls. Plan 02 replaces the stub method
// bodies with real HTTP requests against `/internal/projects/{p}/flags/{flagKey}/automated-releases`.
type RolloutsClient struct {
	cliVersion string
	httpClient *retryablehttp.Client
}

// Compile-time assertion: RolloutsClient satisfies Client.
var _ Client = RolloutsClient{}

// NewClient constructs a RolloutsClient with a freshly-configured retry HTTP client. The
// `cliVersion` is captured at construction so the eventual User-Agent header (Plan 02) does
// not need to read Viper at request time (CONVENTIONS.md anti-pattern).
func NewClient(cliVersion string) RolloutsClient {
	return RolloutsClient{
		cliVersion: cliVersion,
		httpClient: newRetryableClient(),
	}
}

// newRetryableClient builds the shared retryablehttp.Client used by every RolloutsClient
// method. Retry envelope: 4 retries, 500ms..8s exponential backoff, retries 5xx + network
// errors only (DefaultRetryPolicy never retries 4xx). Logger=nil prevents the library from
// printing request URLs or Authorization headers (T-01-08 threat mitigation).
func newRetryableClient() *retryablehttp.Client {
	c := retryablehttp.NewClient()
	c.RetryMax = 4
	c.RetryWaitMin = 500 * time.Millisecond
	c.RetryWaitMax = 8 * time.Second
	c.CheckRetry = retryablehttp.DefaultRetryPolicy
	c.Backoff = retryablehttp.DefaultBackoff
	c.Logger = nil
	return c
}

// List returns the rollouts attached to a feature flag. Plan 01 returns a stub empty list so
// the end-to-end pipeline is provable without real upstream; Plan 02 swaps the body for a real
// HTTP GET + DTO conversion + status mapping.
func (c RolloutsClient) List(
	_ context.Context,
	_ string, // accessToken — used by Plan 02
	_ string, // baseURI — used by Plan 02
	_ string, // projKey — used by Plan 02
	_ string, // flagKey — used by Plan 02
	_ ListOpts,
) (*RolloutList, error) {
	return &RolloutList{Items: []Rollout{}}, nil
}

// Get returns a single rollout by ID. Plan 01 returns a stub zero-value Rollout so the type
// surface and method signature are locked in; Plan 02 swaps the body for a real HTTP GET.
func (c RolloutsClient) Get(
	_ context.Context,
	_ string, // accessToken
	_ string, // baseURI
	_ string, // projKey
	_ string, // envKey
	_ string, // rolloutID
) (*Rollout, error) {
	return &Rollout{}, nil
}
