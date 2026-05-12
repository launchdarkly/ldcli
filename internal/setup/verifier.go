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

// Verify polls GET /api/v2/projects/{project}/environments/{env}/sdk-active until active=true.
func (v *Verifier) Verify(accessToken, baseURI, projectKey, envKey string) (*VerifyResult, error) {
	start := time.Now()
	deadline := start.Add(v.Timeout)
	attempts := 0

	for {
		attempts++
		active, err := v.checkOnce(accessToken, baseURI, projectKey, envKey)
		if err != nil {
			return nil, err
		}
		if active {
			return &VerifyResult{
				Active:   true,
				Attempts: attempts,
				Elapsed:  time.Since(start).Round(time.Millisecond).String(),
			}, nil
		}

		if time.Now().After(deadline) {
			return &VerifyResult{
				Active:   false,
				Attempts: attempts,
				Elapsed:  time.Since(start).Round(time.Millisecond).String(),
			}, nil
		}

		time.Sleep(v.Interval)
	}
}

func (v *Verifier) checkOnce(accessToken, baseURI, projectKey, envKey string) (bool, error) {
	path, _ := url.JoinPath(baseURI, "api/v2/projects", projectKey, "environments", envKey, "sdk-active")

	res, err := v.Client.MakeRequest(accessToken, "GET", path, "application/json", nil, nil, false)
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
