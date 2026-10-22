package create

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/huh"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
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

// VariationTarget returns the identity of a new variation. The filter gives
// the project, the config, and the key, and a question asks for each one that
// it does not give. Without questions, the error names every missing flag,
// --model-config-key too, before any read. The bool result is true when the
// user cancels.
func (input Input) VariationTarget(catalog Catalog, filter syncdomain.Filter, questions Questions) (syncdomain.ResourceID, bool, error) {
	var key string
	if len(filter.Keys) == 1 {
		key = filter.Keys[0]
	}
	var missing []string
	for _, flag := range []struct {
		name string
		set  bool
	}{{"--project", filter.Project != ""}, {"--config-key", filter.ConfigKey != ""}, {"--key", key != ""}, {"--model-config-key", input.ModelConfigKey != nil}} {
		if !flag.set {
			missing = append(missing, flag.name)
		}
	}
	if len(missing) != 0 && !questions.Allowed {
		return syncdomain.ResourceID{}, false, fmt.Errorf("variation creation requires %s without interactive input", joinFlags(missing))
	}
	if key != "" {
		if err := keyValidator(syncdomain.KindVariation)(key); err != nil {
			return syncdomain.ResourceID{}, false, err
		}
	}

	projectKey, configKey := filter.Project, filter.ConfigKey
	if configKey == "" {
		project, config, canceled, err := SelectConfig(questions, catalog, projectKey, nil)
		if err != nil || canceled {
			return syncdomain.ResourceID{}, canceled, err
		}
		projectKey, configKey = project.Key, config.Key
	}
	if key == "" {
		field := huh.NewInput().Title("Variation key").Value(&key).Validate(keyValidator(syncdomain.KindVariation))
		if canceled, err := syncinteractive.RunForm(questions.Input, questions.Output, field); err != nil || canceled {
			return syncdomain.ResourceID{}, canceled, err
		}
	}
	return syncdomain.VariationID(projectKey, configKey, key), false, nil
}

// Variation returns the variation that the input creates with id in config.
// It uses the latest version of its model config. A question asks for the
// model config and the name that the flags do not give. Without questions,
// the name comes from the key. The bool result is true when the user cancels.
func (input Input) Variation(
	catalog Catalog,
	id syncdomain.ResourceID,
	config syncapi.Config,
	questions Questions,
) (syncdomain.Variation, bool, error) {
	prompt, err := input.withContent(syncdomain.Variation{Mode: config.Mode})
	if err != nil {
		return syncdomain.Variation{}, false, err
	}

	var modelConfig syncapi.ModelConfig
	canceled := false
	if input.ModelConfigKey != nil {
		modelConfig, err = FindModelConfig(catalog, id.ProjectKey, *input.ModelConfigKey)
	} else {
		modelConfig, canceled, err = SelectModelConfig(questions, catalog, id.ProjectKey)
	}
	if err != nil || canceled {
		return syncdomain.Variation{}, canceled, err
	}

	_, key, err := id.VariationKeys()
	if err != nil {
		return syncdomain.Variation{}, false, err
	}
	name := syncdomain.DisplayName(key)
	if input.Name != nil {
		name = *input.Name
	} else if questions.Allowed {
		field := huh.NewInput().Title("Variation name").Value(&name).Validate(RequiredValue("variation name"))
		if canceled, err := syncinteractive.RunForm(questions.Input, questions.Output, field); err != nil || canceled {
			return syncdomain.Variation{}, canceled, err
		}
	}
	if err := syncdomain.ValidateCreate(syncdomain.KindVariation, key, name, ""); err != nil {
		return syncdomain.Variation{}, false, err
	}

	variation := NewVariation(config, key, name, modelConfig)
	variation.Instructions, variation.Messages = prompt.Instructions, prompt.Messages
	if err := variation.Validate(); err != nil {
		return syncdomain.Variation{}, false, fmt.Errorf("variation %q: %w", key, err)
	}
	return variation, false, nil
}

// NewVariation returns a variation in config with no prompt. It uses the
// latest version of modelConfig.
func NewVariation(config syncapi.Config, key, name string, modelConfig syncapi.ModelConfig) syncdomain.Variation {
	return syncdomain.Variation{
		Mode:               config.Mode,
		Key:                key,
		Name:               name,
		ModelConfigKey:     modelConfig.Key,
		ModelConfigVersion: modelConfig.Version,
		Model:              modelConfig.VariationModel(),
	}
}

// MatchesVariation reports whether LaunchDarkly already has the value of each
// flag that the user passed: the prompt, and the name and the model config key
// when they are set. id is the identity of existing. The fields without a
// flag, such as the model config version and the tools, come from
// LaunchDarkly.
func (input Input) MatchesVariation(id syncdomain.ResourceID, existing syncdomain.Variation) (bool, error) {
	candidate, err := input.withContent(existing)
	if err != nil {
		return false, err
	}
	if input.Name != nil {
		candidate.Name = *input.Name
	}
	if input.ModelConfigKey != nil {
		candidate.ModelConfigKey = *input.ModelConfigKey
	}
	want, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, candidate)
	if err != nil {
		return false, err
	}
	have, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, existing)
	return want == have, err
}

// withContent returns variation with the prompt of --content. It reads the
// content with the body rules of a variation file: the instructions of an
// agent, or the role blocks of a completion, in which plain text is one system
// message.
func (input Input) withContent(variation syncdomain.Variation) (syncdomain.Variation, error) {
	if err := synclocal.ParsePromptBody(*input.Content, &variation); err != nil {
		return syncdomain.Variation{}, fmt.Errorf("--content: %w", err)
	}
	return variation, nil
}
