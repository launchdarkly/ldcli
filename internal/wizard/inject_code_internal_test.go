package wizard

import (
	"strings"
	"testing"

	"github.com/launchdarkly/sdk-meta/api/sdkmeta"
	"github.com/stretchr/testify/assert"
)

func TestEnvVarName(t *testing.T) {
	tests := map[string]struct {
		sdkType sdkmeta.Type
		want    string
	}{
		"client-side SDK uses React env var": {
			sdkType: sdkmeta.ClientSideType,
			want:    "REACT_APP_LD_CLIENT_SIDE_ID",
		},
		"server-side SDK uses generic SDK key var": {
			sdkType: sdkmeta.ServerSideType,
			want:    "LAUNCHDARKLY_SDK_KEY",
		},
		"edge SDK uses generic SDK key var": {
			sdkType: sdkmeta.EdgeType,
			want:    "LAUNCHDARKLY_SDK_KEY",
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			sdk := sdkDetail{sdkType: tt.sdkType}

			got := envVarName(sdk)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnvVarValue(t *testing.T) {
	t.Run("returns placeholder when env is nil", func(t *testing.T) {
		sdk := sdkDetail{sdkType: sdkmeta.ServerSideType}

		got := envVarValue(sdk, nil)

		assert.Equal(t, "<your-sdk-key>", got)
	})

	t.Run("returns SDK key for server-side SDK", func(t *testing.T) {
		sdk := sdkDetail{sdkType: sdkmeta.ServerSideType}
		env := &envData{sdkKey: "sdk-abc123", mobileKey: "mob-xyz", clientSideId: "csi-456"}

		got := envVarValue(sdk, env)

		assert.Equal(t, "sdk-abc123", got)
	})

	t.Run("returns client-side ID for client-side SDK", func(t *testing.T) {
		sdk := sdkDetail{sdkType: sdkmeta.ClientSideType}
		env := &envData{sdkKey: "sdk-abc123", mobileKey: "mob-xyz", clientSideId: "csi-456"}

		got := envVarValue(sdk, env)

		assert.Equal(t, "csi-456", got)
	})

	t.Run("returns SDK key for edge SDK", func(t *testing.T) {
		sdk := sdkDetail{sdkType: sdkmeta.EdgeType}
		env := &envData{sdkKey: "sdk-edge", clientSideId: "csi-456"}

		got := envVarValue(sdk, env)

		assert.Equal(t, "sdk-edge", got)
	})
}

func TestSnippetLang(t *testing.T) {
	tests := map[string]struct {
		sdkID string
		want  string
	}{
		"go":      {"go-server-sdk", "go"},
		"python":  {"python-server-sdk", "python"},
		"node":    {"node-server", "javascript"},
		"react":   {"react-client-sdk", "tsx"},
		"java":    {"java-server-sdk", "java"},
		"unknown": {"unknown-sdk", ""},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			got := snippetLang(tt.sdkID)

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFenceSnippet(t *testing.T) {
	t.Run("wraps content in markdown code fence with correct language", func(t *testing.T) {
		result := fenceSnippet("go-server-sdk", "package main")

		assert.True(t, strings.HasPrefix(result, "```go\n"), "should start with ```go")
		assert.Contains(t, result, "package main")
		assert.True(t, strings.HasSuffix(result, "```\n"), "should end with ```")
	})

	t.Run("uses empty fence for unknown SDK", func(t *testing.T) {
		result := fenceSnippet("unknown-sdk", "some content")

		assert.True(t, strings.HasPrefix(result, "```\n"), "should start with plain ```")
	})
}
