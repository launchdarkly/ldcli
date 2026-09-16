package symbols

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/analytics"
)

func TestNewGenerateCmd(t *testing.T) {
	cmd := NewGenerateCmd(func(accessToken, baseURI string, analyticsOptOut bool) analytics.Tracker {
		return &analytics.MockTracker{}
	})

	assert.Equal(t, "generate", cmd.Use)

	// Every flag the command reads has to be one it accepts. Both commands bind the
	// same viper keys, so a flag this one only reads still answers — with the value
	// of `upload`'s flag, which the caller here has no way to see or to set.
	for _, flag := range []string{
		typeFlag,
		pathFlag,
		outputFlag,
		appVersionFlag,
		symbolsIdFlag,
		basePathFlag,
		includeSourcesFlag,
		sourcePathFlag,
	} {
		assert.NotNil(t, cmd.Flags().Lookup(flag), "--%s", flag)
	}

	assert.Equal(t, []string{"true"}, cmd.Flags().Lookup(typeFlag).Annotations["required"])
}
