package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestIndividualModelParameterPlansPushPullAndConflict(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	target := `model.parameters["temperature"]`
	reference := &syncdomain.Reference{
		File: "agent.json", Format: "json",
		Map: map[string]string{"prompt": "prompt", target: "temperature"},
	}
	baselineVariation := parameterVariation(0.2)
	baselineProjection, err := syncdomain.ProjectVariation(baselineVariation, *reference)
	require.NoError(t, err)
	baseline, err := syncdomain.FingerprintProjection(id, baselineProjection)
	require.NoError(t, err)

	tests := []struct {
		name              string
		localTemperature  float64
		serverTemperature float64
		action            Action
	}{
		{name: "push", localTemperature: 0.3, serverTemperature: 0.2, action: ActionUpdateServer},
		{name: "pull", localTemperature: 0.2, serverTemperature: 0.3, action: ActionUpdateLocal},
		{name: "conflict", localTemperature: 0.3, serverTemperature: 0.4, action: ActionConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			localVariation := parameterVariation(test.localTemperature)
			localProjection, err := syncdomain.ProjectVariation(localVariation, *reference)
			require.NoError(t, err)
			local := syncdomain.SyncedResource{
				Kind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey,
				Source: reference, Projection: localProjection, Variation: localVariation,
			}
			server := parameterVariation(test.serverTemperature)

			resource := planResource(
				id,
				&local,
				ServerResource{Variation: &server, ConfigMode: syncdomain.VariationModeAgent},
				baseline,
				true,
			)

			assert.Equal(t, test.action, resource.Action)
		})
	}
}

func parameterVariation(temperature float64) syncdomain.Variation {
	return syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default", Instructions: "Help",
		Model: map[string]any{"parameters": map[string]any{
			"temperature": temperature,
			"top_p":       0.9,
		}},
	}
}
