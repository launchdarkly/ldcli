package setup

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/launchdarkly/ldcli/internal/resources"
)

// VerifyResult describes the outcome of verifying SDK connectivity.
type VerifyResult struct {
	Active   bool   `json:"active"`
	Attempts int    `json:"attempts"`
	Elapsed  string `json:"elapsed"`
	// SDKName is the sdk_name the check was filtered on, empty when the SDK id had
	// no known reported name. Empty means Active reports any SDK in the
	// environment, not necessarily the one setup just configured.
	SDKName string `json:"sdk_name,omitempty"`
}

// reportedSDKNames maps a setup SDK id to the sdk_name that SDK identifies itself
// as in the events the sdk-active endpoint aggregates. Only the SDKs that reach
// verification need an entry: verify runs after init injected runnable code, which
// only happens for the append-safe SDKs.
var reportedSDKNames = map[string]string{
	"node-server":       "node-server-sdk",
	"python-server-sdk": "python-server-sdk",
	"ruby-server-sdk":   "ruby-server-sdk",
}

// ReportedSDKName returns the sdk_name to filter sdk-active on for sdkID, or an
// empty string when it is unknown and the check cannot be narrowed.
func ReportedSDKName(sdkID string) string {
	return reportedSDKNames[sdkID]
}

// Verifier polls the sdk-active endpoint until the SDK reports as active or a timeout is reached.
type Verifier struct {
	Client   resources.Client
	Interval time.Duration
	Timeout  time.Duration
}

// DefaultVerifier returns a Verifier with sensible defaults.
func DefaultVerifier(client resources.Client) *Verifier {
	return &Verifier{
		Client:   client,
		Interval: 5 * time.Second,
		Timeout:  120 * time.Second,
	}
}

// Verify polls GET /api/v2/projects/{project}/environments/{env}/sdk-active until
// active=true, narrowed to the SDK sdkID reports itself as. Without the filter the
// endpoint answers for any SDK active in the environment in the past seven days,
// which reports success for a project that was already using LaunchDarkly.
func (v *Verifier) Verify(accessToken, baseURI, projectKey, envKey, sdkID string) (*VerifyResult, error) {
	start := time.Now()
	deadline := start.Add(v.Timeout)
	attempts := 0
	sdkName := ReportedSDKName(sdkID)

	for {
		attempts++
		active, err := v.checkOnce(accessToken, baseURI, projectKey, envKey, sdkName)
		if err != nil {
			return nil, err
		}
		if active {
			return &VerifyResult{
				Active:   true,
				Attempts: attempts,
				Elapsed:  time.Since(start).Round(time.Millisecond).String(),
				SDKName:  sdkName,
			}, nil
		}

		if time.Now().After(deadline) {
			return &VerifyResult{
				Active:   false,
				Attempts: attempts,
				Elapsed:  time.Since(start).Round(time.Millisecond).String(),
				SDKName:  sdkName,
			}, nil
		}

		time.Sleep(v.Interval)
	}
}

func (v *Verifier) checkOnce(accessToken, baseURI, projectKey, envKey, sdkName string) (bool, error) {
	path, _ := url.JoinPath(baseURI, "api/v2/projects", projectKey, "environments", envKey, "sdk-active")

	var query url.Values
	if sdkName != "" {
		query = url.Values{"sdk_name": []string{sdkName}}
	}

	res, err := v.Client.MakeRequest(accessToken, "GET", path, "application/json", query, nil, false)
	if err != nil {
		return false, fmt.Errorf("checking sdk-active: %w", err)
	}

	var resp struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(res, &resp); err != nil {
		return false, fmt.Errorf("parsing sdk-active response: %w", err)
	}

	return resp.Active, nil
}
