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

func TestValidateDirectAPIVariationSupportsModelConfigVersion(t *testing.T) {
	base := Variation{Mode: VariationModeAgent, Key: "default", Name: "Default"}

	withVersion := base
	withVersion.ModelConfigVersion = 3
	require.NoError(t, ValidateDirectAPIVariation(withVersion))

	withOutput := base
	withOutput.OutputFormat = map[string]any{"type": "json"}
	require.ErrorContains(t, ValidateDirectAPIVariation(withOutput), "outputFormat")
}
