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

type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Config struct {
	Key        string                   `json:"key"`
	Name       string                   `json:"name"`
	Mode       syncdomain.VariationMode `json:"mode"`
	Variations []syncdomain.Variation   `json:"variations"`
}

type CatalogClient struct {
	transport   resources.Client
	accessToken string
	baseURI     string
}

func NewCatalogClient(
	transport resources.Client,
	accessToken string,
	baseURI string,
) CatalogClient {
	return CatalogClient{
		transport:   transport,
		accessToken: accessToken,
		baseURI:     baseURI,
	}
}

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

func (client CatalogClient) Configs(projectKey string) ([]Config, error) {
	endpoint, err := url.JoinPath(
		client.baseURI,
		"api/v2/projects",
		projectKey,
		"ai-configs",
	)
	if err != nil {
		return nil, fmt.Errorf("build AI Configs endpoint: %w", err)
	}

	configs, err := listCatalog[Config](
		client,
		endpoint,
		"AI Configs",
		true,
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

func (client CatalogClient) Config(
	projectKey string,
	configKey string,
) (Config, error) {
	endpoint, err := url.JoinPath(
		client.baseURI,
		"api/v2/projects",
		projectKey,
		"ai-configs",
		configKey,
	)
	if err != nil {
		return Config{}, fmt.Errorf("build AI Config endpoint: %w", err)
	}

	response, err := client.transport.MakeRequest(
		client.accessToken,
		http.MethodGet,
		endpoint,
		"",
		nil,
		nil,
		true,
	)
	if err != nil {
		return Config{}, fmt.Errorf("get AI Config %q: %w", configKey, err)
	}

	var config Config
	if err := json.Unmarshal(response, &config); err != nil {
		return Config{}, fmt.Errorf("decode AI Config response: %w", err)
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
			"AI Config %q has unsupported mode %q",
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
