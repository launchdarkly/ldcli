package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type ResourceStatus string

const (
	ResourceStatusInSync        ResourceStatus = "in_sync"
	ResourceStatusLocalChanged  ResourceStatus = "local_changed"
	ResourceStatusServerChanged ResourceStatus = "server_changed"
	ResourceStatusConflict      ResourceStatus = "conflict"
)

type SyncDirection string

const (
	SyncDirectionCodeCanonical   SyncDirection = "code_canonical"
	SyncDirectionServerCanonical SyncDirection = "server_canonical"
	SyncDirectionBoth            SyncDirection = "both"
)

type ResourceAction string

const (
	ResourceActionNoChange        ResourceAction = "no_change"
	ResourceActionCreate          ResourceAction = "create"
	ResourceActionUpdate          ResourceAction = "update"
	ResourceActionPull            ResourceAction = "pull"
	ResourceActionBlocked         ResourceAction = "blocked"
	ResourceActionResolveConflict ResourceAction = "resolve_conflict"
)

type ResourceError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PlannedResource struct {
	ResourceKind  syncdomain.Kind `json:"resourceKind"`
	LookupKey     string          `json:"lookupKey"`
	Status        ResourceStatus  `json:"status"`
	SyncDirection SyncDirection   `json:"syncDirection"`
	Action        ResourceAction  `json:"action"`
	Diff          json.RawMessage `json:"diff,omitempty"`
	Error         *ResourceError  `json:"error,omitempty"`
}

type ProjectPlan struct {
	ProjectKey string            `json:"-"`
	PlanID     string            `json:"planId,omitempty"`
	ExpiresAt  string            `json:"expiresAt,omitempty"`
	Resources  []PlannedResource `json:"resources"`
}

type planRequest struct {
	Source    sourceRequest   `json:"source"`
	DryRun    bool            `json:"dryRun"`
	Resources []resourceInput `json:"resources"`
}

type sourceRequest struct {
	Type       syncdomain.SourceType `json:"type"`
	Identifier string                `json:"identifier"`
}

type resourceInput struct {
	ResourceKind syncdomain.Kind `json:"resourceKind"`
	LookupKey    string          `json:"lookupKey"`
	Upsert       bool            `json:"upsert"`
	Payload      json.RawMessage `json:"payload"`
}

type projectResources struct {
	ProjectKey string
	Resources  []syncdomain.SyncedResource
}

type Client struct {
	transport resources.Client
}

func NewClient(transport resources.Client) Client {
	return Client{transport: transport}
}

func (client Client) Plan(
	accessToken string,
	baseURI string,
	source syncdomain.Source,
	dryRun bool,
	synced []syncdomain.SyncedResource,
) ([]ProjectPlan, error) {
	plans := make([]ProjectPlan, 0)

	for _, project := range groupResourcesByProject(synced) {
		plan, err := client.planProject(accessToken, baseURI, source, dryRun, project)
		if err != nil {
			return nil, err
		}

		plans = append(plans, plan)
	}

	return plans, nil
}

func (client Client) planProject(
	accessToken string,
	baseURI string,
	source syncdomain.Source,
	dryRun bool,
	project projectResources,
) (ProjectPlan, error) {
	request := planRequest{
		Source: sourceRequest{
			Type:       source.Type(),
			Identifier: source.Identifier(),
		},
		DryRun:    dryRun,
		Resources: make([]resourceInput, 0, len(project.Resources)),
	}

	for _, resource := range project.Resources {
		if resource.Kind != syncdomain.KindVariation {
			return ProjectPlan{}, fmt.Errorf("unsupported sync resource kind %q", resource.Kind)
		}

		request.Resources = append(request.Resources, resourceInput{
			ResourceKind: resource.Kind,
			LookupKey:    resource.LookupKey,
			Upsert:       resource.Upsert,
			Payload:      resource.Payload,
		})
	}

	body, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return ProjectPlan{}, fmt.Errorf("marshal plan request: %w", err)
	}

	endpoint, err := url.JoinPath(
		baseURI,
		"api/v2/projects",
		project.ProjectKey,
		"ai-configs/sync/plan",
	)
	if err != nil {
		return ProjectPlan{}, fmt.Errorf("build plan endpoint: %w", err)
	}

	response, err := client.transport.MakeRequest(
		accessToken,
		http.MethodPost,
		endpoint,
		"application/json",
		nil,
		body,
		false,
	)
	if err != nil {
		return ProjectPlan{}, err
	}

	var plan ProjectPlan
	if err := json.Unmarshal(response, &plan); err != nil {
		return ProjectPlan{}, fmt.Errorf("decode plan response: %w", err)
	}

	plan.ProjectKey = project.ProjectKey

	return plan, nil
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
