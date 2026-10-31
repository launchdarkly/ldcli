package reference

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestImportedLiteralReadAndWrite(t *testing.T) {
	tests := []struct {
		name, format, primaryPath, dependencyPath string
	}{
		{name: "Python relative import", format: "python", primaryPath: "app/agent.py", dependencyPath: "app/prompts.py"},
		{name: "TypeScript relative import", format: "typescript", primaryPath: "src/agent.ts", dependencyPath: "src/prompts.ts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixtureRoot := "imports/python/"
			if test.format == "typescript" {
				fixtureRoot = "imports/typescript/"
			}
			primary := fixtureFile(t, fixtureRoot+test.primaryPath)
			dependency := fixtureFile(t, fixtureRoot+test.dependencyPath)
			bundle := SourceBundle{
				Primary: Document{Path: test.primaryPath, Data: primary},
				Dependencies: map[string]Document{
					test.dependencyPath: {Path: test.dependencyPath, Data: dependency},
				},
			}
			paths, err := DependencyPaths(test.format, bundle.Primary)
			require.NoError(t, err)
			assert.Contains(t, paths, test.dependencyPath)
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{
				File: test.primaryPath, Format: test.format, Select: `agents[id="support"]`,
				Map: map[string]string{"prompt": "instructions"},
			}
			projection, err := ParseSource(bundle, id, syncdomain.VariationModeAgent, reference)
			require.NoError(t, err)
			assert.Equal(t,
				"Ask for an order ID before discussing an order.",
				*projection.(syncdomain.VariationProjection).Instructions,
			)

			prompt := "Ask for an order ID and region."
			result, err := UpdateSource(
				bundle, id, syncdomain.VariationModeAgent, reference,
				syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &prompt},
			)
			require.NoError(t, err)
			edits := directEdits(t, result)
			require.Len(t, edits, 1)
			assert.Equal(t, test.dependencyPath, edits[0].File)
			updated := applyConformanceEdits(t, dependency, edits)
			bundle.Dependencies[test.dependencyPath] = Document{Path: test.dependencyPath, Data: updated}
			reparsed, err := ParseSource(bundle, id, syncdomain.VariationModeAgent, reference)
			require.NoError(t, err)
			assert.Equal(t, prompt, *reparsed.(syncdomain.VariationProjection).Instructions)
			assert.Equal(t, primary, bundle.Primary.Data)
		})
	}
}

func TestUnresolvedImportIsReadOnlyWithSourceHint(t *testing.T) {
	source := []byte(`from agents import Agent
from third_party.prompts import SUPPORT_PROMPT
support = Agent(instructions=SUPPORT_PROMPT, model="gpt-5")
`)
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "agent.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}

	projection, err := ParseSource(
		SourceBundle{Primary: Document{Path: "agent.py", Data: source}},
		id, syncdomain.VariationModeAgent, reference,
	)

	require.NoError(t, err)
	state, ok := syncdomain.ProjectionFieldState(projection, syncdomain.FieldPrompt)
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
	assert.Equal(t, syncdomain.ErrorImportUnresolved, state.Code)
	assert.NotEmpty(t, state.File)
}
