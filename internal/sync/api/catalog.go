package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

const catalogPageLimit = 25

// Project is the project metadata needed by interactive sync flows.
type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ModelConfig contains the model settings assigned to a new variation.
type ModelConfig struct {
	Key          string         `json:"key"`
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
		"modelName":  config.Key,
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

// CatalogClient lists resources used by bootstrap and link flows.
type CatalogClient struct {
	transport   resources.Client
	accessToken string
	baseURI     string
}

// NewCatalogClient creates a client for sync catalog reads.
func NewCatalogClient(transport resources.Client, accessToken, baseURI string) CatalogClient {
	return CatalogClient{
		transport:   transport,
		accessToken: accessToken,
		baseURI:     baseURI,
	}
}

// Projects returns projects ordered by name.
func (client CatalogClient) Projects() ([]Project, error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects")
	if err != nil {
		return nil, fmt.Errorf("build projects endpoint: %w", err)
	}

	return listCatalog[Project](
		client,
		endpoint,
		"projects",
		false,
		url.Values{"sort": {"name"}},
	)
}

// ModelConfigs returns model configs available to one project.
func (client CatalogClient) ModelConfigs(projectKey string) ([]ModelConfig, error) {
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

// Configs returns the agent and completion configs in one project.
func (client CatalogClient) Configs(projectKey string) ([]Config, error) {
	endpoint, err := url.JoinPath(client.baseURI, "api/v2/projects", projectKey, "ai-configs")
	if err != nil {
		return nil, fmt.Errorf("build configs endpoint: %w", err)
	}

	configs, err := listCatalog[Config](
		client,
		endpoint,
		"configs",
		false,
		url.Values{
			"sort":   {"name"},
			"filter": {`mode anyOf ["agent","completion"]`},
		},
	)
	if err != nil {
		return nil, err
	}

	for index := range configs {
		if err := configs[index].applyMode(); err != nil {
			return nil, err
		}
	}

	return configs, nil
}

// Config returns one config with its variation modes normalized.
func (client CatalogClient) Config(projectKey, configKey string) (Config, error) {
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

type catalogPage[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
}

func listCatalog[T any](
	client CatalogClient,
	endpoint string,
	resourceName string,
	beta bool,
	baseQuery url.Values,
) ([]T, error) {
	var items []T

	for offset := 0; ; offset += catalogPageLimit {
		query := maps.Clone(baseQuery)
		query.Set("limit", fmt.Sprintf("%d", catalogPageLimit))
		query.Set("offset", fmt.Sprintf("%d", offset))

		response, err := client.transport.MakeRequest(
			client.accessToken,
			http.MethodGet,
			endpoint,
			"",
			query,
			nil,
			beta,
		)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", resourceName, err)
		}

		var page catalogPage[T]
		if err := json.Unmarshal(response, &page); err != nil {
			return nil, fmt.Errorf("decode %s response: %w", resourceName, err)
		}

		items = append(items, page.Items...)
		if len(page.Items) < catalogPageLimit ||
			(page.TotalCount > 0 && len(items) >= page.TotalCount) {
			return items, nil
		}
	}
}
