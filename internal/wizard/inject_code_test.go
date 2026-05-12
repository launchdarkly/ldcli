package wizard_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/wizard"
)

func TestBuildSnippet(t *testing.T) {
	tests := []struct {
		sdkID   string
		flagKey string
	}{
		{"go-server-sdk", "my-go-flag"},
		{"python-server-sdk", "my-python-flag"},
		{"node-server", "my-node-flag"},
		{"react-client-sdk", "my-react-flag"},
		{"java-server-sdk", "my-java-flag"},
	}

	for _, tt := range tests {
		t.Run("inserts flag key for "+tt.sdkID, func(t *testing.T) {
			snippet := wizard.BuildSnippet(tt.sdkID, tt.flagKey)

			assert.Contains(t, snippet, tt.flagKey,
				"snippet should contain the real flag key")
			assert.NotContains(t, snippet, "MY_FLAG_KEY",
				"snippet should not contain the placeholder")
		})
	}

	t.Run("unknown SDK returns fallback message", func(t *testing.T) {
		snippet := wizard.BuildSnippet("unknown-sdk", "some-flag")

		assert.NotContains(t, snippet, "MY_FLAG_KEY")
	})
}

func TestInitFilename(t *testing.T) {
	tests := map[string]struct {
		sdkID    string
		wantSuffix string
	}{
		"go":     {"go-server-sdk", "launchdarkly.go"},
		"python": {"python-server-sdk", "launchdarkly_client.py"},
		"node":   {"node-server", "launchdarkly_client.js"},
		"react":  {"react-client-sdk", "launchdarkly.tsx"},
		"java":   {"java-server-sdk", "LaunchDarklyConfig.java"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			filename := wizard.InitFilename(tt.sdkID, "/some/project")

			assert.True(t, strings.HasSuffix(filename, tt.wantSuffix),
				"expected filename to end with %s, got %s", tt.wantSuffix, filename)
		})
	}
}
