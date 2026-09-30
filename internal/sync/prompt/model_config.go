package prompt

import (
	"encoding/json"
	"fmt"

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
func resolveVariationModelConfigs(resources []syncdomain.SyncedResource, getModelConfig modelConfigGetter) error {
	modelConfigs := make(map[modelConfigID]syncapi.ModelConfig)

	for index := range resources {
		resource := &resources[index]
		if resource.Kind != syncdomain.KindVariation {
			continue
		}

		var variation syncdomain.Variation
		if err := json.Unmarshal(resource.Payload, &variation); err != nil {
			return fmt.Errorf("decode local variation %q: %w", resource.LookupKey, err)
		}
		if variation.ModelConfigKey == "" || variation.ModelConfigVersion != 0 {
			continue
		}

		id := modelConfigID{projectKey: resource.ProjectKey, configKey: variation.ModelConfigKey}
		modelConfig, ok := modelConfigs[id]
		if !ok {
			var err error
			modelConfig, err = getModelConfig(id.projectKey, id.configKey)
			if err != nil {
				return err
			}
			modelConfigs[id] = modelConfig
		}
		if modelConfig.Version == 0 {
			continue
		}

		variation.ModelConfigVersion = modelConfig.Version
		variation.Model = modelConfig.VariationModel()
		payload, err := json.Marshal(variation)
		if err != nil {
			return fmt.Errorf("encode resolved variation %q: %w", resource.LookupKey, err)
		}
		resource.Payload = payload
	}

	return nil
}
