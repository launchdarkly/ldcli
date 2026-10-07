package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// Project is the project metadata needed by interactive sync flows.
type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Page is one server-filtered page of catalog resources.
type Page[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
}

// ModelConfig contains the model settings assigned to a new variation.
type ModelConfig struct {
	Key          string         `json:"key"`
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Version      int            `json:"version"`
	Params       map[string]any `json:"params"`
	CustomParams map[string]any `json:"customParams"`
}

// VariationModel returns the model representation used by variation APIs.
func (config ModelConfig) VariationModel() map[string]any {
	params := config.Params
	if params == nil {
		params = map[string]any{}
	}
	custom := config.CustomParams
	if custom == nil {
		custom = map[string]any{}
	}
	return map[string]any{
		"modelName":  config.ID,
		"parameters": params,
		"custom":     custom,
	}
}

// Config contains a supported config and its prompt variations.
type Config struct {
	Key        string                   `json:"key"`
	Name       string                   `json:"name"`
	Mode       syncdomain.VariationMode `json:"mode"`
	Variations []syncdomain.Variation   `json:"variations"`
}

type configVariationResponse struct {
	syncdomain.Variation
	State string `json:"state"`
}

// UnmarshalJSON excludes archived variations at the API boundary. LaunchDarkly
// retains archived variations in a config response, but sync treats them as
// absent and keeps lifecycle state out of the canonical variation model.
func (config *Config) UnmarshalJSON(data []byte) error {
	var response struct {
		Key        string                    `json:"key"`
		Name       string                    `json:"name"`
		Mode       syncdomain.VariationMode  `json:"mode"`
		Variations []configVariationResponse `json:"variations"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	config.Key = response.Key
	config.Name = response.Name
	config.Mode = response.Mode
	config.Variations = make([]syncdomain.Variation, 0, len(response.Variations))
	for _, variation := range response.Variations {
		if variation.State != "archived" {
			config.Variations = append(config.Variations, variation.Variation)
		}
	}
	return nil
}

// SearchProjects returns one name-sorted API page filtered by project name or key.
func (client Client) SearchProjects(query string, limit, offset int) (Page[Project], error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects")
	if err != nil {
		return Page[Project]{}, fmt.Errorf("build projects endpoint: %w", err)
	}

	values := url.Values{
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
		"sort":   {"name"},
	}
	if query != "" {
		values.Set("filter", "query:"+query)
	}
	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", values, nil, false)
	if err != nil {
		return Page[Project]{}, fmt.Errorf("search projects: %w", err)
	}

	var page Page[Project]
	if err := json.Unmarshal(response, &page); err != nil {
		return Page[Project]{}, fmt.Errorf("decode projects response: %w", err)
	}
	return page, nil
}

// ModelConfigs returns model configs available to one project.
func (client Client) ModelConfigs(projectKey string) ([]ModelConfig, error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects", projectKey, "ai-configs/model-configs")
	if err != nil {
		return nil, fmt.Errorf("build model configs endpoint: %w", err)
	}

	response, err := client.transport.MakeRequest(
		client.accessToken,
		http.MethodGet,
		endpoint,
		"",
		nil,
		nil,
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("list model configs: %w", err)
	}

	var modelConfigs []ModelConfig
	if err := json.Unmarshal(response, &modelConfigs); err != nil {
		return nil, fmt.Errorf("decode model configs response: %w", err)
	}

	return modelConfigs, nil
}

// SearchConfigs returns one name-sorted page of agent and completion configs.
func (client Client) SearchConfigs(projectKey, query string, modes []syncdomain.VariationMode, limit, offset int) (Page[Config], error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects", projectKey, "ai-configs")
	if err != nil {
		return Page[Config]{}, fmt.Errorf("build configs endpoint: %w", err)
	}

	if len(modes) == 0 {
		modes = []syncdomain.VariationMode{syncdomain.VariationModeAgent, syncdomain.VariationModeCompletion}
	}
	modeValues := make([]string, len(modes))
	for index, mode := range modes {
		modeValues[index] = strconv.Quote(string(mode))
	}

	filter := "mode anyOf [" + strings.Join(modeValues, ",") + "]"
	if query != "" {
		filter = "query equals " + strconv.Quote(query) + ", " + filter
	}

	values := url.Values{
		"filter": {filter},
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa(offset)},
		"sort":   {"name"},
	}
	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", values, nil, false)
	if err != nil {
		return Page[Config]{}, fmt.Errorf("search configs: %w", err)
	}

	var page Page[Config]
	if err := json.Unmarshal(response, &page); err != nil {
		return Page[Config]{}, fmt.Errorf("decode configs response: %w", err)
	}
	for index := range page.Items {
		if err := page.Items[index].applyMode(); err != nil {
			return Page[Config]{}, err
		}
	}
	return page, nil
}

// Config returns one config with its variation modes normalized.
func (client Client) Config(projectKey, configKey string) (Config, error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects", projectKey, "ai-configs", configKey)
	if err != nil {
		return Config{}, fmt.Errorf("build config endpoint: %w", err)
	}

	response, err := client.transport.MakeRequest(
		client.accessToken,
		http.MethodGet,
		endpoint,
		"",
		nil,
		nil,
		false,
	)
	if err != nil {
		return Config{}, fmt.Errorf("get config %q: %w", configKey, err)
	}

	var config Config
	if err := json.Unmarshal(response, &config); err != nil {
		return Config{}, fmt.Errorf("decode config response: %w", err)
	}
	if err := config.applyMode(); err != nil {
		return Config{}, err
	}

	return config, nil
}

// applyMode validates the parent config mode and copies it onto every
// variation, whose API representation does not contain its own mode.
func (config *Config) applyMode() error {
	if config.Mode == "" {
		config.Mode = syncdomain.VariationModeCompletion
	}
	if !config.Mode.Valid() {
		return fmt.Errorf(
			"config %q has unsupported mode %q",
			config.Key,
			config.Mode,
		)
	}

	for index := range config.Variations {
		config.Variations[index].Mode = config.Mode
	}

	return nil
}
