package signup

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/analytics"
)

func TestSignupCmd(t *testing.T) {
	t.Run("creates signup command with correct attributes", func(t *testing.T) {
		mockTracker := &analytics.MockTracker{}
		analyticsTrackerFn := func(accessToken string, baseURI string, optOut bool) analytics.Tracker {
			return mockTracker
		}

		cmd := NewSignupCmd(analyticsTrackerFn)

		assert.Equal(t, "signup", cmd.Use)
		assert.Equal(t, "Create a new LaunchDarkly account", cmd.Short)
		assert.Equal(t, "Open your browser to create a new LaunchDarkly account", cmd.Long)
		assert.NotNil(t, cmd.RunE)
		assert.NotNil(t, cmd.PreRun)
	})
}
