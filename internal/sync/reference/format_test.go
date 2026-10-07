package reference

import (
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/reference/adapters"
)

func TestPlainMarkdownAdapterReturnsCommonPromptDomain(t *testing.T) {
	prompt, err := Parse(PlainMarkdown, []byte("Be helpful."))

	require.NoError(t, err)
	require.Empty(t, prompt.Mode)
	require.Empty(t, prompt.Key)
	require.Empty(t, prompt.Name)
	require.Equal(t, []adapters.Message{{Role: adapters.RoleSystem, Content: "Be helpful."}}, prompt.Messages)
}

func TestPlainMarkdownRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		variation syncdomain.Variation
		assert    func(*testing.T, syncdomain.Variation)
	}{
		{
			name:      "agent instructions",
			variation: syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: "prompt"},
			assert: func(t *testing.T, variation syncdomain.Variation) {
				require.Equal(t, "Be helpful.", variation.Instructions)
				require.Empty(t, variation.Messages)
			},
		},
		{
			name:      "completion system message",
			variation: syncdomain.Variation{Mode: syncdomain.VariationModeCompletion, Key: "prompt"},
			assert: func(t *testing.T, variation syncdomain.Variation) {
				require.Empty(t, variation.Instructions)
				require.Equal(t, []syncdomain.Message{{Role: "system", Content: "Be helpful."}}, variation.Messages)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ApplyToVariation(PlainMarkdown, []byte("\nBe helpful.\n"), &test.variation)
			require.NoError(t, err)
			test.assert(t, test.variation)

			rendered, err := Render(PlainMarkdown, test.variation)
			require.NoError(t, err)
			require.Equal(t, "Be helpful.\n", string(rendered))
		})
	}
}

func TestPlainMarkdownRejectsUnrepresentableCompletion(t *testing.T) {
	_, err := Render(PlainMarkdown, syncdomain.Variation{
		Mode: syncdomain.VariationModeCompletion,
		Key:  "prompt",
		Messages: []syncdomain.Message{
			{Role: "system", Content: "System"},
			{Role: "user", Content: "User"},
		},
	})

	require.ErrorContains(t, err, "at most one system message")
}

func TestReferenceFormatRejectsUnknownFormat(t *testing.T) {
	var variation syncdomain.Variation
	err := ValidateFormat("anthropic-prompt")
	require.ErrorContains(t, err, "unsupported referenced prompt format")
	_, err = ApplyToVariation("anthropic-prompt", nil, &variation)
	require.ErrorContains(t, err, "unsupported referenced prompt format")
	_, err = Render("anthropic-prompt", variation)
	require.ErrorContains(t, err, "unsupported referenced prompt format")
}
