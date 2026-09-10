package setup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestVerify_Active(t *testing.T) {
	client := &resources.MockClient{
		Response: []byte(`{"active": true}`),
	}
	verifier := &Verifier{
		Client:   client,
		Interval: 10 * time.Millisecond,
		Timeout:  1 * time.Second,
	}

	result, err := verifier.Verify("token", "https://app.launchdarkly.com", "proj", "env", "node-server")
	require.NoError(t, err)
	assert.True(t, result.Active)
	assert.Equal(t, 1, result.Attempts)
}

func TestVerify_InactiveTimesOut(t *testing.T) {
	client := &resources.MockClient{
		Response: []byte(`{"active": false}`),
	}
	verifier := &Verifier{
		Client:   client,
		Interval: 10 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}

	result, err := verifier.Verify("token", "https://app.launchdarkly.com", "proj", "env", "node-server")
	require.NoError(t, err)
	assert.False(t, result.Active)
	assert.Greater(t, result.Attempts, 1)
}

// Unfiltered, sdk-active answers for any SDK active in the environment in the past
// seven days, so it reports success for a project that already used LaunchDarkly.
func TestVerify_FiltersOnTheConfiguredSDK(t *testing.T) {
	client := &resources.MockClient{Response: []byte(`{"active": true}`)}
	verifier := &Verifier{Client: client, Interval: time.Millisecond, Timeout: time.Second}

	result, err := verifier.Verify("token", "https://app.launchdarkly.com", "proj", "env", "ruby-server-sdk")

	require.NoError(t, err)
	assert.Equal(t, "ruby-server-sdk", client.Query.Get("sdk_name"))
	assert.Equal(t, "ruby-server-sdk", result.SDKName)
}

// The setup id and the name the SDK reports itself as are not always the same.
func TestVerify_UsesTheReportedSDKName(t *testing.T) {
	client := &resources.MockClient{Response: []byte(`{"active": true}`)}
	verifier := &Verifier{Client: client, Interval: time.Millisecond, Timeout: time.Second}

	_, err := verifier.Verify("token", "https://app.launchdarkly.com", "proj", "env", "node-server")

	require.NoError(t, err)
	assert.Equal(t, "node-server-sdk", client.Query.Get("sdk_name"))
}

// An id with no known reported name must not send a filter that can never match.
func TestVerify_UnknownSDK_SendsNoFilter(t *testing.T) {
	client := &resources.MockClient{Response: []byte(`{"active": true}`)}
	verifier := &Verifier{Client: client, Interval: time.Millisecond, Timeout: time.Second}

	result, err := verifier.Verify("token", "https://app.launchdarkly.com", "proj", "env", "made-up-sdk")

	require.NoError(t, err)
	assert.Empty(t, client.Query.Get("sdk_name"))
	assert.Empty(t, result.SDKName, "an unnarrowed check must not claim it was narrowed")
}

// Every SDK that init writes runnable code for reaches verification, so each needs
// a reported name or its check silently falls back to the whole environment.
func TestReportedSDKName_CoversEverySDKThatVerifies(t *testing.T) {
	for _, sdk := range KnownSDKs {
		if !InjectsInPlace(sdk.ID) {
			continue
		}
		assert.NotEmpty(t, ReportedSDKName(sdk.ID), "%s reaches verify with no sdk_name", sdk.ID)
	}
}
