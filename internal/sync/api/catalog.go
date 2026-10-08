package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Project is the project metadata that the interactive flows show.
type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Config is one agent or completion config and its active variations.
type Config struct {
	Key        string                   `json:"key"`
	Name       string                   `json:"name"`
	Mode       syncdomain.VariationMode `json:"mode"`
	Variations []syncdomain.Variation   `json:"variations"`
}

// ModelConfig is a versioned set of model settings that a variation can use.
type ModelConfig struct {
	Key          string         `json:"key"`
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Version      int            `json:"version"`
	Params       map[string]any `json:"params"`
	CustomParams map[string]any `json:"customParams"`
}

// VariationModel returns the model settings in the form that a variation stores.
func (config ModelConfig) VariationModel() map[string]any {
	return map[string]any{
		"modelName":  config.ID,
		"parameters": emptyIfNil(config.Params),
		"custom":     emptyIfNil(config.CustomParams),
	}
}

// SearchProjects returns one page of projects whose name or key matches query.
func (client Client) SearchProjects(query string, limit, offset int) (Page[Project], error) {
	values := pageQuery(limit, offset)
	values.Set("sort", "name")
	if query != "" {
		values.Set("filter", "query:"+query)
	}
	response, err := client.read("search projects", "", values, "api/v2/projects")
	if err != nil {
		return Page[Project]{}, err
	}
	return decodeJSON[Page[Project]](response, "projects response")
}

// SearchConfigs returns one page of configs that match query and use one of
// modes. If modes is empty, the page includes agent and completion configs.
func (client Client) SearchConfigs(projectKey, query string, modes []syncdomain.VariationMode, limit, offset int) (Page[Config], error) {
	if len(modes) == 0 {
		modes = []syncdomain.VariationMode{syncdomain.VariationModeAgent, syncdomain.VariationModeCompletion}
	}
	quotedModes := make([]string, len(modes))
	for index, mode := range modes {
		quotedModes[index] = strconv.Quote(string(mode))
	}
	filter := "mode anyOf [" + strings.Join(quotedModes, ",") + "]"
	if query != "" {
		filter = "query equals " + strconv.Quote(query) + ", " + filter
	}

	values := pageQuery(limit, offset)
	values.Set("sort", "name")
	values.Set("filter", filter)
	response, err := client.read(
		fmt.Sprintf("search configs in project %q", projectKey), projectKey, values,
		projectPath(projectKey, "ai-configs")...,
	)
	if err != nil {
		return Page[Config]{}, err
	}
	page, err := decodeJSON[Page[Config]](response, "configs response")
	if err != nil {
		return Page[Config]{}, err
	}
	for index := range page.Items {
		if err := page.Items[index].applyMode(); err != nil {
			return Page[Config]{}, err
		}
	}
	return page, nil
}

// Config returns one config. Each variation has the mode of the config.
func (client Client) Config(projectKey, configKey string) (Config, error) {
	response, err := client.read(
		fmt.Sprintf("get config %q in project %q", configKey, projectKey), projectKey, nil,
		projectPath(projectKey, "ai-configs", configKey)...,
	)
	if err != nil {
		return Config{}, err
	}
	config, err := decodeJSON[Config](response, "config response")
	if err != nil {
		return Config{}, err
	}
	if err := config.applyMode(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// ModelConfig returns the latest version of one model config.
func (client Client) ModelConfig(projectKey, modelConfigKey string) (ModelConfig, error) {
	response, err := client.read(
		fmt.Sprintf("get model config %q in project %q", modelConfigKey, projectKey), projectKey, nil,
		projectPath(projectKey, "ai-configs/model-configs", modelConfigKey)...,
	)
	if err != nil {
		return ModelConfig{}, err
	}
	return decodeJSON[ModelConfig](response, "model config response")
}

// ModelConfigs returns every model config that a project can use.
func (client Client) ModelConfigs(projectKey string) ([]ModelConfig, error) {
	response, err := client.read(
		fmt.Sprintf("list model configs in project %q", projectKey), projectKey, nil,
		projectPath(projectKey, "ai-configs/model-configs")...,
	)
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]ModelConfig](response, "model configs response")
}

// UnmarshalJSON drops archived variations. LaunchDarkly returns them in a
// config, but sync treats an archived variation as absent.
func (config *Config) UnmarshalJSON(data []byte) error {
	var response struct {
		Key        string                   `json:"key"`
		Name       string                   `json:"name"`
		Mode       syncdomain.VariationMode `json:"mode"`
		Variations []struct {
			syncdomain.Variation
			State string `json:"state"`
		} `json:"variations"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	*config = Config{Key: response.Key, Name: response.Name, Mode: response.Mode}
	config.Variations = make([]syncdomain.Variation, 0, len(response.Variations))
	for _, variation := range response.Variations {
		if variation.State != "archived" {
			config.Variations = append(config.Variations, variation.Variation)
		}
	}
	return nil
}

// applyMode copies the config mode to each variation, because the API does
// not include the mode in a variation. A config without a mode is a
// completion config.
func (config *Config) applyMode() error {
	if config.Mode == "" {
		config.Mode = syncdomain.VariationModeCompletion
	}
	if !config.Mode.Valid() {
		return fmt.Errorf("config %q has unsupported mode %q", config.Key, config.Mode)
	}
	for index := range config.Variations {
		config.Variations[index].Mode = config.Mode
	}
	return nil
}
