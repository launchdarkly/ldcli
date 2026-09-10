package sync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/resources"
)

type APIClient struct {
	client resources.Client
}

type ResourceStatus struct {
	ProjectKey        string      `json:"projectKey"`
	ResourceKind      Kind        `json:"resourceKind"`
	LookupKey         string      `json:"lookupKey"`
	Status            string      `json:"status"`
	SyncDirection     string      `json:"syncDirection"`
	ServerFingerprint Fingerprint `json:"serverFingerprint,omitempty"`
	Error             any         `json:"error,omitempty"`
}

type statusRequest struct {
	RepoIdentifier string                  `json:"repoIdentifier"`
	Resources      []statusRequestResource `json:"resources"`
}

type statusRequestResource struct {
	Fingerprint  Fingerprint `json:"fingerprint"`
	ResourceKind Kind        `json:"resourceKind"`
	LookupKey    string      `json:"lookupKey"`
	Upsert       bool        `json:"upsert"`
}

type projectResources struct {
	ProjectKey string
	Resources  []SyncedResource
}

func NewAPIClient(client resources.Client) APIClient {
	return APIClient{client: client}
}

func (c APIClient) Status(
	accessToken string,
	baseURI string,
	repoIdentifier string,
	synced []SyncedResource,
) ([]ResourceStatus, error) {
	statuses := make([]ResourceStatus, 0, len(synced))

	for _, project := range groupResourcesByProject(synced) {
		projectStatuses, err := c.projectStatus(accessToken, baseURI, repoIdentifier, project)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, projectStatuses...)
	}

	return statuses, nil
}

func (c APIClient) projectStatus(
	accessToken string,
	baseURI string,
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
		baseURI,
		"api/v2/projects",
		project.ProjectKey,
		"ai-configs/sync/status",
	)
	if err != nil {
		return nil, fmt.Errorf("build status endpoint: %w", err)
	}

	response, err := c.client.MakeRequest(
		accessToken,
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

func groupResourcesByProject(synced []SyncedResource) []projectResources {
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
