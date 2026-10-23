package create

import (
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
)

// Catalog finds the project, the config, and the model config of a new
// variation.
type Catalog interface {
	syncinteractive.ProjectSearcher
	syncinteractive.ConfigSearcher
	Config(projectKey, configKey string) (syncapi.Config, error)
	ModelConfigs(projectKey string) ([]syncapi.ModelConfig, error)
}

// SelectConfig asks for the project of a new variation, unless projectKey is
// set, and for its config. modes keeps only the configs of those modes. The
// bool result is true when the user cancels.
func SelectConfig(
	questions Questions,
	catalog Catalog,
	projectKey string,
	modes []syncdomain.VariationMode,
) (syncapi.Project, syncapi.Config, bool, error) {
	project := syncapi.Project{Key: projectKey}
	if project.Key == "" {
		var canceled bool
		var err error
		if project, canceled, err = syncinteractive.SelectProject(questions.Input, questions.Output, catalog); err != nil || canceled {
			return syncapi.Project{}, syncapi.Config{}, canceled, err
		}
	}
	config, canceled, err := syncinteractive.SelectConfig(questions.Input, questions.Output, catalog, project.Key, modes)
	if syncapi.IsNotFound(err) && projectKey != "" {
		// The config search is the first read in the project, so a 404 means
		// that the project is missing.
		return syncapi.Project{}, syncapi.Config{}, false, fmt.Errorf(
			"project %q was not found in LaunchDarkly, or the access token cannot read it", projectKey,
		)
	}
	if err != nil || canceled {
		return syncapi.Project{}, syncapi.Config{}, canceled, err
	}
	return project, config, false, nil
}

// SelectModelConfig asks for the model config of a new variation. The bool
// result is true when the user cancels.
func SelectModelConfig(questions Questions, catalog Catalog, projectKey string) (syncapi.ModelConfig, bool, error) {
	_ = syncconsole.New(questions.Output).Line("Loading model configs...")
	modelConfigs, err := catalog.ModelConfigs(projectKey)
	if err != nil {
		return syncapi.ModelConfig{}, false, err
	}
	choices := make([]syncinteractive.Choice[syncapi.ModelConfig], 0, len(modelConfigs))
	for _, modelConfig := range modelConfigs {
		choices = append(choices, syncinteractive.Choice[syncapi.ModelConfig]{
			Title: modelConfig.Name, Description: "Key: " + modelConfig.Key, Value: modelConfig,
		})
	}
	return syncinteractive.Select(questions.Input, questions.Output, "Choose a model config", choices)
}

// FindModelConfig returns the model config with key in a project. The list
// has the latest version of each model config.
func FindModelConfig(catalog Catalog, projectKey, key string) (syncapi.ModelConfig, error) {
	modelConfigs, err := catalog.ModelConfigs(projectKey)
	if err != nil {
		return syncapi.ModelConfig{}, err
	}
	index := slices.IndexFunc(modelConfigs, func(candidate syncapi.ModelConfig) bool { return candidate.Key == key })
	if index < 0 {
		return syncapi.ModelConfig{}, fmt.Errorf("model config %q was not found", key)
	}
	return modelConfigs[index], nil
}
