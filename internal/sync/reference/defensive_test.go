package reference

import (
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestStructuredAdaptersRejectMalformedSources(t *testing.T) {
	tests := []struct {
		name, format, source string
	}{
		{name: "JSON duplicate", format: "json", source: `{"key":1,"key":2}`},
		{name: "JSON trailing", format: "json", source: `{"key":1} trailing`},
		{name: "YAML duplicate", format: "yaml", source: "key: one\nkey: two\n"},
		{name: "TOML duplicate", format: "toml", source: "key = 1\nkey = 2\n"},
	}
	id := syncdomain.VariationID("project", "config", "default")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseSource(
				sourceBundle("source", []byte(test.source)),
				id,
				syncdomain.VariationModeAgent,
				syncdomain.Reference{
					File: "source", Format: test.format, Select: "$",
					Map: map[string]string{"prompt": "key"},
				},
			)
			require.Error(t, err)
		})
	}
}

func TestMarkdownProfilesRejectUnsafeMarkers(t *testing.T) {
	_, err := parseMarkdownProfile(
		Document{Path: "instructions.md", Data: []byte("<!-- launchdarkly:begin one -->\n")},
		false,
	)
	require.Error(t, err)
}

func TestPlaceholderTranslationRejectsAmbiguousForms(t *testing.T) {
	for _, value := range []string{"{{name}}", "{name", "{1name}", "literal }"} {
		_, err := placeholdersToLaunchDarkly(value, bracePlaceholder)
		require.Error(t, err)
	}
	for _, value := range []string{"{{name", "{{1name}}", "literal {"} {
		_, err := placeholdersFromLaunchDarkly(value, bracePlaceholder)
		require.Error(t, err)
	}
}

func TestPythonAdapterRejectsDynamicPrompt(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "agent.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	projection, err := ParseSource(
		sourceBundle("agent.py", []byte("from agents import Agent\nsupport = Agent(instructions=load_prompt())\n")),
		id,
		syncdomain.VariationModeAgent,
		reference,
	)
	require.NoError(t, err)
	state, ok := syncdomain.ProjectionFieldState(projection, syncdomain.FieldPrompt)
	require.True(t, ok)
	require.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
}
