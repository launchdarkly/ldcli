package prompt

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type modelConfigGetter func(projectKey, modelConfigKey string) (syncapi.ModelConfig, error)

type modelConfigID struct {
	projectKey string
	configKey  string
}

// canonicalizeLocalVariationModels returns local resources in the
// server-comparable shape used by planning. An omitted version uses the latest
// versioned model config.
func canonicalizeLocalVariationModels(
	localFileResources []syncdomain.SyncedResource,
	getModelConfig modelConfigGetter,
) ([]syncdomain.SyncedResource, error) {
	// Clone the slice so canonical payloads cannot replace local file payloads.
	canonicalResources := slices.Clone(localFileResources)
	modelConfigs := make(map[modelConfigID]syncapi.ModelConfig)

	for index := range canonicalResources {
		resource := &canonicalResources[index]
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
		canonicalModel := modelConfig.VariationModel()
		maps.Copy(canonicalModel, variation.Model)
		variation.Model = canonicalModel
		payload, err := json.Marshal(variation)
		if err != nil {
			return nil, fmt.Errorf("encode canonical variation %q: %w", resource.LookupKey, err)
		}
		resource.Payload = payload
	}

	return canonicalResources, nil
}

// variationForLocalFile converts server state into local file form. It retains
// model keys from the file and new server values as overrides.
func variationForLocalFile(
	serverVariation syncdomain.Variation,
	canonicalLocalVariation *syncdomain.Variation,
	localFileResource syncdomain.SyncedResource,
) (syncdomain.Variation, error) {
	if len(localFileResource.Payload) == 0 || canonicalLocalVariation == nil {
		return serverVariation, nil
	}

	var localFileVariation syncdomain.Variation
	if err := json.Unmarshal(localFileResource.Payload, &localFileVariation); err != nil {
		return syncdomain.Variation{}, fmt.Errorf("decode local file variation %q: %w", localFileResource.LookupKey, err)
	}
	if localFileVariation.ModelConfigKey == "" || localFileVariation.ModelConfigVersion != 0 {
		return serverVariation, nil
	}
	// Preserve an explicit server reference when it differs from the canonical local reference.
	if serverVariation.ModelConfigKey != canonicalLocalVariation.ModelConfigKey ||
		serverVariation.ModelConfigVersion != canonicalLocalVariation.ModelConfigVersion {
		return serverVariation, nil
	}

	serverVariation.ModelConfigVersion = 0
	// Clone the server model before removing canonical fields from the local form.
	serverVariation.Model = maps.Clone(serverVariation.Model)
	for key, value := range serverVariation.Model {
		_, definedInLocalFile := localFileVariation.Model[key]
		canonicalValue, presentInCanonical := canonicalLocalVariation.Model[key]
		if !definedInLocalFile && presentInCanonical && reflect.DeepEqual(value, canonicalValue) {
			delete(serverVariation.Model, key)
		}
	}
	if len(serverVariation.Model) == 0 {
		serverVariation.Model = nil
	}
	return serverVariation, nil
}
