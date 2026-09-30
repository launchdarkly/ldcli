package sync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFingerprintVariationIsStable(t *testing.T) {
	variation := Variation{
		Mode: VariationModeCompletion,
		Key:  "default",
		Name: "Default",
		Model: map[string]any{
			"temperature": 0.5,
			"provider":    "openai",
		},
		Messages: []Message{{Role: "system", Content: "Be concise."}},
	}

	first, err := FingerprintVariation("production", "support/default", variation)
	require.NoError(t, err)

	variation.Model = map[string]any{"provider": "openai", "temperature": 0.5}
	variation.Instructions = "ignored for completion mode"
	second, err := FingerprintVariation("production", "support/default", variation)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, first)
}

func TestFingerprintVariationIncludesResourceIdentity(t *testing.T) {
	variation := Variation{Mode: VariationModeAgent, Key: "default", Name: "Default", Instructions: "Help"}
	one, err := FingerprintVariation("one", "config/default", variation)
	require.NoError(t, err)
	two, err := FingerprintVariation("two", "config/default", variation)
	require.NoError(t, err)

	require.NotEqual(t, one, two)
}

func TestFingerprintVariationNormalizesRenderedPromptWhitespace(t *testing.T) {
	agent := Variation{Mode: VariationModeAgent, Key: "agent", Name: "Agent", Instructions: "\n  Help the user.  \n"}
	trimmedAgent := agent
	trimmedAgent.Instructions = "Help the user."

	agentFingerprint, err := FingerprintVariation("project", "config/agent", agent)
	require.NoError(t, err)
	trimmedAgentFingerprint, err := FingerprintVariation("project", "config/agent", trimmedAgent)
	require.NoError(t, err)
	require.Equal(t, agentFingerprint, trimmedAgentFingerprint)

	completion := Variation{
		Mode:     VariationModeCompletion,
		Key:      "completion",
		Name:     "Completion",
		Messages: []Message{{Role: "system", Content: "\n  Be concise.  \n"}},
	}
	trimmedCompletion := completion
	trimmedCompletion.Messages = []Message{{Role: "system", Content: "Be concise."}}

	completionFingerprint, err := FingerprintVariation("project", "config/completion", completion)
	require.NoError(t, err)
	trimmedCompletionFingerprint, err := FingerprintVariation("project", "config/completion", trimmedCompletion)
	require.NoError(t, err)
	require.Equal(t, completionFingerprint, trimmedCompletionFingerprint)
	require.Equal(t, "\n  Be concise.  \n", completion.Messages[0].Content)
}

func TestFingerprintVariationNormalizesPromptLineEndings(t *testing.T) {
	tests := map[string]struct {
		windows Variation
		unix    Variation
	}{
		"agent": {
			windows: Variation{
				Mode: VariationModeAgent, Key: "agent", Name: "Agent",
				Instructions: "First line.\r\nSecond line.\rThird line.",
			},
			unix: Variation{
				Mode: VariationModeAgent, Key: "agent", Name: "Agent",
				Instructions: "First line.\nSecond line.\nThird line.",
			},
		},
		"completion": {
			windows: Variation{
				Mode: VariationModeCompletion, Key: "completion", Name: "Completion",
				Messages: []Message{{Role: "system", Content: "First line.\r\nSecond line.\rThird line."}},
			},
			unix: Variation{
				Mode: VariationModeCompletion, Key: "completion", Name: "Completion",
				Messages: []Message{{Role: "system", Content: "First line.\nSecond line.\nThird line."}},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			windowsFingerprint, err := FingerprintVariation("project", "config/"+name, test.windows)
			require.NoError(t, err)
			unixFingerprint, err := FingerprintVariation("project", "config/"+name, test.unix)
			require.NoError(t, err)

			require.Equal(t, unixFingerprint, windowsFingerprint)
		})
	}
}

func TestFingerprintVariationNormalizesModelDefaults(t *testing.T) {
	withoutDefaults := Variation{
		Mode: VariationModeAgent,
		Key:  "agent",
		Name: "Agent",
		Model: map[string]any{
			"modelName": "claude",
			"metadata":  map[string]any{},
			"retries":   1,
		},
	}
	withDefaults := withoutDefaults
	withDefaults.Model = map[string]any{
		"modelName":  "claude",
		"parameters": map[string]any{},
		"custom":     map[string]any{},
		"metadata":   map[string]any{},
		"retries":    1.0,
	}

	withoutDefaultsFingerprint, err := FingerprintVariation("project", "config/agent", withoutDefaults)
	require.NoError(t, err)
	withDefaultsFingerprint, err := FingerprintVariation("project", "config/agent", withDefaults)
	require.NoError(t, err)

	require.Equal(t, withoutDefaultsFingerprint, withDefaultsFingerprint)
	require.Contains(t, withDefaults.Model, "parameters")
	require.Contains(t, withDefaults.Model, "custom")
}

func TestFingerprintVariationPreservesMeaningfulModelChanges(t *testing.T) {
	base := Variation{
		Mode: VariationModeAgent,
		Key:  "agent",
		Name: "Agent",
		Model: map[string]any{
			"modelName": "claude",
		},
	}
	withEmptyMetadata := base
	withEmptyMetadata.Model = map[string]any{
		"modelName": "claude",
		"metadata":  map[string]any{},
	}
	withMetadata := base
	withMetadata.Model = map[string]any{
		"modelName": "claude",
		"metadata":  map[string]any{"region": "us-east"},
	}

	baseFingerprint, err := FingerprintVariation("project", "config/agent", base)
	require.NoError(t, err)
	emptyMetadataFingerprint, err := FingerprintVariation("project", "config/agent", withEmptyMetadata)
	require.NoError(t, err)
	metadataFingerprint, err := FingerprintVariation("project", "config/agent", withMetadata)
	require.NoError(t, err)

	require.NotEqual(t, baseFingerprint, emptyMetadataFingerprint)
	require.NotEqual(t, emptyMetadataFingerprint, metadataFingerprint)
}

func TestValidateDirectAPIVariationSupportsModelConfigVersion(t *testing.T) {
	base := Variation{Mode: VariationModeAgent, Key: "default", Name: "Default"}

	withVersion := base
	withVersion.ModelConfigVersion = 3
	require.NoError(t, ValidateDirectAPIVariation(withVersion))

	withOutput := base
	withOutput.OutputFormat = map[string]any{"type": "json"}
	require.ErrorContains(t, ValidateDirectAPIVariation(withOutput), "outputFormat")
}
