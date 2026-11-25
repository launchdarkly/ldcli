package prompt

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type modelConfigGetter func(projectKey, modelConfigKey string) (syncapi.ModelConfig, error)

// modelConfigVersionsGetter returns every version of one model config.
type modelConfigVersionsGetter func(projectKey, modelConfigKey string) ([]syncapi.ModelConfig, error)

// canonicalizeLocalVariationModels returns a copy of the local variations in
// the form that LaunchDarkly stores. A variation that names a model config
// without a version uses the latest version, and its local model settings
// replace the settings of that model config. The input does not change.
//
// server is what LaunchDarkly holds for each variation. A local modelName
// that LaunchDarkly already holds for the variation is not checked. Any other
// modelName must name the model config: its key, or the id of the version
// that the variation uses. A variation that pins an older version reads the
// versions of its model config, only when its modelName needs the check.
func canonicalizeLocalVariationModels(
	localFiles []syncdomain.SyncedResource,
	getModelConfig modelConfigGetter,
	getModelConfigVersions modelConfigVersionsGetter,
	server map[ResourceID]ServerResource,
) ([]syncdomain.SyncedResource, error) {
	type modelConfigID struct{ projectKey, key string }
	modelConfigs := make(map[modelConfigID]syncapi.ModelConfig)
	modelConfigVersions := make(map[modelConfigID][]syncapi.ModelConfig)
	canonical := slices.Clone(localFiles)

	for index := range canonical {
		variation := &canonical[index].Variation
		if variation.ModelConfigKey == "" {
			continue
		}

		resourceID := canonical[index].ID()
		id := modelConfigID{projectKey: canonical[index].ProjectKey, key: variation.ModelConfigKey}
		modelConfig, ok := modelConfigs[id]
		if !ok {
			var err error
			if modelConfig, err = getModelConfig(id.projectKey, id.key); err != nil {
				if syncapi.IsNotFound(err) {
					return nil, syncdomain.WrapProblem(syncdomain.NewProblem(
						syncdomain.ErrorInvalidReference,
						fmt.Sprintf("model config %q was not found in project %q", id.key, id.projectKey),
						"Fix modelConfigKey, or create the model config in LaunchDarkly.",
					), err).WithResource(resourceID)
				}
				return nil, err
			}
			modelConfigs[id] = modelConfig
		}
		pinned := variation.ModelConfigVersion
		if pinned != 0 && modelConfig.Version > 0 && pinned > modelConfig.Version {
			return nil, noModelConfigVersionProblem(variation.ModelConfigKey, pinned, modelConfig.Version, resourceID)
		}

		modelName, _ := variation.Model["modelName"].(string)
		if modelName != "" && modelName != variation.ModelConfigKey &&
			!serverHoldsModelName(server[resourceID], variation.ModelConfigKey, modelName) {
			// The version the variation uses names its model.
			used := modelConfig
			if pinned != 0 && modelConfig.Version > 0 && pinned != modelConfig.Version {
				versions, ok := modelConfigVersions[id]
				if !ok {
					var err error
					if versions, err = getModelConfigVersions(id.projectKey, id.key); err != nil {
						return nil, err
					}
					modelConfigVersions[id] = versions
				}
				at := slices.IndexFunc(versions, func(version syncapi.ModelConfig) bool { return version.Version == pinned })
				if at < 0 {
					return nil, noModelConfigVersionProblem(variation.ModelConfigKey, pinned, modelConfig.Version, resourceID)
				}
				used = versions[at]
			}
			if modelName != used.ID {
				return nil, syncdomain.NewProblem(
					syncdomain.ErrorInvalidReference,
					fmt.Sprintf("model %q does not match model config %q, whose model is %q", modelName, variation.ModelConfigKey, used.ID),
					"Use that name, or remove modelName.",
				).WithResource(resourceID)
			}
		}
		if pinned != 0 || modelConfig.Version == 0 {
			continue
		}

		model := modelConfig.VariationModel()
		maps.Copy(model, variation.Model)
		variation.ModelConfigVersion = modelConfig.Version
		variation.Model = model
	}
	return canonical, nil
}

// noModelConfigVersionProblem refuses a variation that pins a version that
// its model config doesn't have.
func noModelConfigVersionProblem(modelConfigKey string, pinned, latest int, resource ResourceID) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorInvalidReference,
		fmt.Sprintf("model config %q has no version %d", modelConfigKey, pinned),
		fmt.Sprintf("Its latest version is %d. Pin that version, or remove modelConfigVersion.", latest),
	).WithResource(resource)
}

// serverHoldsModelName reports whether LaunchDarkly already holds modelName
// for the variation under the same model config. Some LaunchDarkly UI paths
// store a name that is not the model config's id, and an unchanged name is not
// refused. A name held under another model config is checked, because the file
// switched model configs and kept the old name.
func serverHoldsModelName(server ServerResource, modelConfigKey, modelName string) bool {
	if server.Variation == nil || server.Variation.ModelConfigKey != modelConfigKey {
		return false
	}
	held, _ := server.Variation.Model["modelName"].(string)
	return held == modelName
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
