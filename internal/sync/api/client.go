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

const listPageLimit = 25

type APIClient struct {
	client      resources.Client
	accessToken string
	baseURI     string
}

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

type ResourceStatus struct {
	ProjectKey        string                 `json:"projectKey"`
	ResourceKind      syncdomain.Kind        `json:"resourceKind"`
	LookupKey         string                 `json:"lookupKey"`
	Status            string                 `json:"status"`
	SyncDirection     string                 `json:"syncDirection"`
	ServerFingerprint syncdomain.Fingerprint `json:"serverFingerprint,omitempty"`
	Error             any                    `json:"error,omitempty"`
}

type statusRequest struct {
	RepoIdentifier string                  `json:"repoIdentifier"`
	Resources      []statusRequestResource `json:"resources"`
}

type statusRequestResource struct {
	Fingerprint  syncdomain.Fingerprint `json:"fingerprint"`
	ResourceKind syncdomain.Kind        `json:"resourceKind"`
	LookupKey    string                 `json:"lookupKey"`
	Upsert       bool                   `json:"upsert"`
}

type projectResources struct {
	ProjectKey string
	Resources  []syncdomain.SyncedResource
}

type listResponse[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
}

func NewAPIClient(client resources.Client, accessToken, baseURI string) APIClient {
	return APIClient{
		client:      client,
		accessToken: accessToken,
		baseURI:     baseURI,
	}
}

func (c APIClient) Projects(search string) ([]Project, error) {
	endpoint, err := url.JoinPath(c.baseURI, "api/v2/projects")
	if err != nil {
		return nil, fmt.Errorf("build projects endpoint: %w", err)
	}

	query := url.Values{"sort": {"name"}}
	if search != "" {
		query.Set("filter", "query:"+search)
	}
	return listAll[Project](c, endpoint, "projects", false, query)
}

func (c APIClient) Configs(projectKey, search string) ([]Config, error) {
	endpoint, err := url.JoinPath(c.baseURI, "api/v2/projects", projectKey, "ai-configs")
	if err != nil {
		return nil, fmt.Errorf("build configs endpoint: %w", err)
	}

	query := url.Values{
		"sort":   {"name"},
		"filter": {configFilter(search)},
	}
	configs, err := listAll[Config](c, endpoint, "configs", true, query)
	if err != nil {
		return nil, err
	}
	for i := range configs {
		if err := configs[i].applyMode(); err != nil {
			return nil, err
		}
	}

	return configs, nil
}

func configFilter(search string) string {
	const modes = `mode anyOf ["agent","completion"]`
	if search == "" {
		return modes
	}

	encoded, _ := json.Marshal(search)
	return "query equals " + string(encoded) + "," + modes
}

func (c APIClient) Config(
	projectKey,
	configKey string,
) (Config, error) {
	endpoint, err := url.JoinPath(c.baseURI, "api/v2/projects", projectKey, "ai-configs", configKey)
	if err != nil {
		return Config{}, fmt.Errorf("build config endpoint: %w", err)
	}

	response, err := c.client.MakeRequest(
		c.accessToken,
		http.MethodGet,
		endpoint,
		"",
		nil,
		nil,
		true,
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

func (c *Config) applyMode() error {
	if c.Mode == "" {
		c.Mode = syncdomain.VariationModeCompletion
	}
	if !c.Mode.Valid() {
		return fmt.Errorf("config %q has unsupported mode %q", c.Key, c.Mode)
	}
	for i := range c.Variations {
		c.Variations[i].Mode = c.Mode
	}

	return nil
}

func (c APIClient) Status(
	repoIdentifier string,
	synced []syncdomain.SyncedResource,
) ([]ResourceStatus, error) {
	statuses := make([]ResourceStatus, 0, len(synced))

	for _, project := range groupResourcesByProject(synced) {
		projectStatuses, err := c.projectStatus(repoIdentifier, project)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, projectStatuses...)
	}

	return statuses, nil
}

func listAll[T any](
	c APIClient,
	endpoint,
	resourceName string,
	isBeta bool,
	baseQuery url.Values,
) ([]T, error) {
	var items []T

	for offset := 0; ; offset += listPageLimit {
		query := maps.Clone(baseQuery)
		query.Set("limit", fmt.Sprintf("%d", listPageLimit))
		query.Set("offset", fmt.Sprintf("%d", offset))

		response, err := c.client.MakeRequest(
			c.accessToken,
			http.MethodGet,
			endpoint,
			"",
			query,
			nil,
			isBeta,
		)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", resourceName, err)
		}

		var page listResponse[T]
		if err := json.Unmarshal(response, &page); err != nil {
			return nil, fmt.Errorf("decode %s response: %w", resourceName, err)
		}

		items = append(items, page.Items...)
		if len(page.Items) < listPageLimit ||
			(page.TotalCount > 0 && len(items) >= page.TotalCount) {
			return items, nil
		}
	}
}

func (c APIClient) projectStatus(
	repoIdentifier string,
	project projectResources,
) ([]ResourceStatus, error) {
	request := statusRequest{
		RepoIdentifier: repoIdentifier,
		Resources:      make([]statusRequestResource, 0, len(project.Resources)),
	}
	for _, resource := range project.Resources {
		request.Resources = append(request.Resources, statusRequestResource{
			Fingerprint:  resource.Fingerprint,
			ResourceKind: resource.Kind,
			LookupKey:    resource.LookupKey,
			Upsert:       resource.Upsert,
		})
	}

	body, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal status request: %w", err)
	}

	endpoint, err := url.JoinPath(
		c.baseURI,
		"api/v2/projects",
		project.ProjectKey,
		"ai-configs/sync/status",
	)
	if err != nil {
		return nil, fmt.Errorf("build status endpoint: %w", err)
	}

	response, err := c.client.MakeRequest(
		c.accessToken,
		http.MethodPost,
		endpoint,
		"application/json",
		nil,
		body,
		false,
	)
	if err != nil {
		return nil, err
	}

	var statuses []ResourceStatus
	if err := json.Unmarshal(response, &statuses); err != nil {
		return nil, fmt.Errorf("decode status response: %w", err)
	}

	for i := range statuses {
		statuses[i].ProjectKey = project.ProjectKey
	}

	return statuses, nil
}

func groupResourcesByProject(synced []syncdomain.SyncedResource) []projectResources {
	var projects []projectResources
	byProject := make(map[string]int)

	for _, resource := range synced {
		index, ok := byProject[resource.ProjectKey]
		if !ok {
			index = len(projects)
			byProject[resource.ProjectKey] = index
			projects = append(projects, projectResources{ProjectKey: resource.ProjectKey})
		}
		projects[index].Resources = append(projects[index].Resources, resource)
	}

	return projects
}
