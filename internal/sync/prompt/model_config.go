package prompt

import (
	"maps"
	"reflect"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type modelConfigGetter func(projectKey, modelConfigKey string) (syncapi.ModelConfig, error)

// canonicalizeLocalVariationModels returns a copy of the local variations in
// the form that LaunchDarkly stores. A variation that names a model config
// without a version uses the latest version, and its local model settings
// replace the settings of that model config. The input does not change.
func canonicalizeLocalVariationModels(
	localFiles []syncdomain.SyncedResource,
	getModelConfig modelConfigGetter,
) ([]syncdomain.SyncedResource, error) {
	type modelConfigID struct{ projectKey, key string }
	modelConfigs := make(map[modelConfigID]syncapi.ModelConfig)
	canonical := slices.Clone(localFiles)

	for index := range canonical {
		variation := &canonical[index].Variation
		if variation.ModelConfigKey == "" || variation.ModelConfigVersion != 0 {
			continue
		}

		id := modelConfigID{projectKey: canonical[index].ProjectKey, key: variation.ModelConfigKey}
		modelConfig, ok := modelConfigs[id]
		if !ok {
			var err error
			if modelConfig, err = getModelConfig(id.projectKey, id.key); err != nil {
				return nil, err
			}
			modelConfigs[id] = modelConfig
		}
		if modelConfig.Version == 0 {
			continue
		}

		model := modelConfig.VariationModel()
		maps.Copy(model, variation.Model)
		variation.ModelConfigVersion = modelConfig.Version
		variation.Model = model
	}
	return canonical, nil
}

// variationForLocalFile converts a LaunchDarkly variation to the form of the
// local file. If the local file names a model config without a version, the
// result keeps that form. It removes each model value that the model config
// supplies and that the local file does not set. A new server value stays as
// a local override.
//
// localFile is the variation as the file stores it. canonicalLocal is the same
// variation after canonicalizeLocalVariationModels. Both are nil when the
// variation has no local file.
func variationForLocalFile(server syncdomain.Variation, localFile, canonicalLocal *syncdomain.Variation) syncdomain.Variation {
	if localFile == nil || canonicalLocal == nil {
		return server
	}
	if localFile.ModelConfigKey == "" || localFile.ModelConfigVersion != 0 {
		return server
	}
	// A server variation that names a different model config, or a different
	// version, is an explicit change. Keep it as LaunchDarkly stores it.
	if server.ModelConfigKey != canonicalLocal.ModelConfigKey ||
		server.ModelConfigVersion != canonicalLocal.ModelConfigVersion {
		return server
	}

	server.ModelConfigVersion = 0
	server.Model = maps.Clone(server.Model)
	for key, value := range server.Model {
		_, setInFile := localFile.Model[key]
		inherited, fromModelConfig := canonicalLocal.Model[key]
		if !setInFile && fromModelConfig && reflect.DeepEqual(value, inherited) {
			delete(server.Model, key)
		}
	}
	if len(server.Model) == 0 {
		server.Model = nil
	}
	return server
}
