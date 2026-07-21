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

	result, err := verifier.Verify("token", "https://app.launchdarkly.com", "proj", "env")
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

	result, err := verifier.Verify("token", "https://app.launchdarkly.com", "proj", "env")
	require.NoError(t, err)
	assert.False(t, result.Active)
	assert.Greater(t, result.Attempts, 1)
}
