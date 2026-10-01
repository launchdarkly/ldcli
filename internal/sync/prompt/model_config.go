package prompt

import (
	"encoding/json"
	"fmt"
	"maps"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type modelConfigGetter func(projectKey, modelConfigKey string) (syncapi.ModelConfig, error)

type modelConfigID struct {
	projectKey string
	configKey  string
}

// resolveVariationModelConfigs prepares compiled variations for planning.
// An omitted version follows the latest versioned model config.
func resolveVariationModelConfigs(
	resources []syncdomain.SyncedResource,
	getModelConfig modelConfigGetter,
) (map[ResourceID]struct{}, error) {
	modelConfigs := make(map[modelConfigID]syncapi.ModelConfig)
	followLatest := make(map[ResourceID]struct{})

	for index := range resources {
		resource := &resources[index]
		if resource.Kind != syncdomain.KindVariation {
			continue
		}

		var variation syncdomain.Variation
		if err := json.Unmarshal(resource.Payload, &variation); err != nil {
			return nil, fmt.Errorf("decode local variation %q: %w", resource.LookupKey, err)
		}
		if variation.ModelConfigKey == "" || variation.ModelConfigVersion != 0 {
			continue
		}
		followLatest[ResourceID{
			Kind: resource.Kind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey,
		}] = struct{}{}

		id := modelConfigID{projectKey: resource.ProjectKey, configKey: variation.ModelConfigKey}
		modelConfig, ok := modelConfigs[id]
		if !ok {
			var err error
			modelConfig, err = getModelConfig(id.projectKey, id.configKey)
			if err != nil {
				return nil, err
			}
			modelConfigs[id] = modelConfig
		}
		if modelConfig.Version == 0 {
			continue
		}

		variation.ModelConfigVersion = modelConfig.Version
		resolvedModel := modelConfig.VariationModel()
		maps.Copy(resolvedModel, variation.Model)
		variation.Model = resolvedModel
		payload, err := json.Marshal(variation)
		if err != nil {
			return nil, fmt.Errorf("encode resolved variation %q: %w", resource.LookupKey, err)
		}
		resource.Payload = payload
	}

	return followLatest, nil
}
