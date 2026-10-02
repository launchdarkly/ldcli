package prompt

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

func TestResolveVariationModelConfigs(t *testing.T) {
	versioned := testVariation("versioned")
	versioned.ModelConfigKey = "custom-model"
	versioned.Model = map[string]any{
		"parameters": map[string]any{"temperature": 0.8},
		"custom":     map[string]any{"region": "us-east"},
	}
	unversioned := testVariation("unversioned")
	unversioned.Key = "global"
	unversioned.ModelConfigKey = "global-model"
	unversioned.Model = map[string]any{"modelName": "global", "custom": map[string]any{"region": "us-east"}}
	pinned := testVariation("pinned")
	pinned.ModelConfigKey = "custom-model"
	pinned.ModelConfigVersion = 2
	pinned.Model = map[string]any{"modelName": "claude-2", "parameters": map[string]any{"temperature": 0.4}}

	resources := []syncdomain.SyncedResource{
		syncedVariation(t, "production", "support/versioned", versioned),
		syncedVariation(t, "production", "support/global", unversioned),
		syncedVariation(t, "production", "support/pinned", pinned),
	}
	var requested []string

	followLatest, err := resolveVariationModelConfigs(resources, func(projectKey, modelConfigKey string) (syncapi.ModelConfig, error) {
		assert.Equal(t, "production", projectKey)
		requested = append(requested, modelConfigKey)
		return map[string]syncapi.ModelConfig{
			"custom-model": {Key: "custom-model", ID: "claude-4", Version: 4, Params: map[string]any{"temperature": 0.2}},
			"global-model": {Key: "global-model", ID: "global"},
		}[modelConfigKey], nil
	})

	require.NoError(t, err)
	require.Equal(t, []string{"custom-model", "global-model"}, requested)
	require.Equal(t, map[ResourceID]struct{}{
		{Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/versioned"}: {},
		{Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/global"}:    {},
	}, followLatest)

	var resolvedVersioned syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[0].Payload, &resolvedVersioned))
	assert.Equal(t, 4, resolvedVersioned.ModelConfigVersion)
	assert.Equal(t, map[string]any{
		"modelName":  "claude-4",
		"parameters": map[string]any{"temperature": 0.8},
		"custom":     map[string]any{"region": "us-east"},
	}, resolvedVersioned.Model)

	var resolvedUnversioned syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[1].Payload, &resolvedUnversioned))
	assert.Equal(t, unversioned, resolvedUnversioned)

	var resolvedPinned syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[2].Payload, &resolvedPinned))
	assert.Equal(t, pinned, resolvedPinned)
}

func TestResolveVariationModelConfigsRejectsUnknownConfig(t *testing.T) {
	variation := testVariation("local")
	variation.ModelConfigKey = "missing"

	_, err := resolveVariationModelConfigs(
		[]syncdomain.SyncedResource{syncedVariation(t, "production", "support/default", variation)},
		func(string, string) (syncapi.ModelConfig, error) {
			return syncapi.ModelConfig{}, errors.New("model config not found")
		},
	)

	require.ErrorContains(t, err, "model config not found")
}

func syncedVariation(t *testing.T, projectKey, lookupKey string, variation syncdomain.Variation) syncdomain.SyncedResource {
	t.Helper()
	payload, err := json.Marshal(variation)
	require.NoError(t, err)
	return syncdomain.SyncedResource{
		Kind: syncdomain.KindVariation, ProjectKey: projectKey, LookupKey: lookupKey, Payload: payload,
	}
}
