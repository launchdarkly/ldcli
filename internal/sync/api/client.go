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

type ResourceError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PlannedResource struct {
	ResourceKind           syncdomain.Kind `json:"resourceKind"`
	LookupKey              string          `json:"lookupKey"`
	Status                 ResourceStatus  `json:"status"`
	SyncDirection          SyncDirection   `json:"syncDirection"`
	ManifestUpdateRequired bool            `json:"manifestUpdateRequired"`
	LocalDeleted           bool            `json:"localDeleted"`
	ServerDeleted          bool            `json:"serverDeleted"`
	Diff                   json.RawMessage `json:"diff,omitempty"`
	Error                  *ResourceError  `json:"error,omitempty"`
}

type ProjectPlan struct {
	ProjectKey string            `json:"-"`
	PlanID     string            `json:"planId,omitempty"`
	ExpiresAt  string            `json:"expiresAt,omitempty"`
	Resources  []PlannedResource `json:"resources"`
}

type PlanStatus string

const (
	PlanStatusApplied PlanStatus = "applied"
	PlanStatusFailed  PlanStatus = "failed"
)

type ResourceApplyOutcome string

const (
	ResourceApplyOutcomeApplied      ResourceApplyOutcome = "applied"
	ResourceApplyOutcomeFailed       ResourceApplyOutcome = "failed"
	ResourceApplyOutcomeNotAttempted ResourceApplyOutcome = "not_attempted"
)

type AppliedResource struct {
	ResourceKind syncdomain.Kind      `json:"resourceKind"`
	LookupKey    string               `json:"lookupKey"`
	Outcome      ResourceApplyOutcome `json:"outcome"`
	Error        *ResourceError       `json:"error,omitempty"`
}

type ProjectApply struct {
	ProjectKey string            `json:"-"`
	PlanID     string            `json:"planId"`
	Status     PlanStatus        `json:"status"`
	Error      *ResourceError    `json:"error,omitempty"`
	Resources  []AppliedResource `json:"resources"`
}

type applyRequest struct {
	PlanID string `json:"planId"`
}

type planRequest struct {
	Source        sourceRequest   `json:"source"`
	DryRun        bool            `json:"dryRun"`
	FullInventory bool            `json:"fullInventory"`
	Resources     []resourceInput `json:"resources"`
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
	inventoryProjectKeys []string,
	synced []syncdomain.SyncedResource,
) ([]ProjectPlan, error) {
	plans := make([]ProjectPlan, 0)

	// Inventory projects must be planned even when they contain no local
	// resources, because an empty inventory can represent local deletions.
	for _, project := range groupResourcesByProject(synced, inventoryProjectKeys) {
		plan, err := client.planProject(accessToken, baseURI, source, dryRun, project)
		if err != nil {
			return nil, err
		}

		plans = append(plans, plan)
	}

	return plans, nil
}

func (client Client) Apply(
	accessToken string,
	baseURI string,
	projectKey string,
	planID string,
) (ProjectApply, error) {
	body, err := json.MarshalIndent(applyRequest{
		PlanID: planID,
	}, "", "  ")
	if err != nil {
		return ProjectApply{}, fmt.Errorf("marshal apply request: %w", err)
	}

	endpoint, err := url.JoinPath(
		baseURI,
		"api/v2/projects",
		projectKey,
		"ai-configs/sync/apply",
	)
	if err != nil {
		return ProjectApply{}, fmt.Errorf("build apply endpoint: %w", err)
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
		return ProjectApply{}, err
	}

	var result ProjectApply
	if err := json.Unmarshal(response, &result); err != nil {
		return ProjectApply{}, fmt.Errorf("decode apply response: %w", err)
	}
	if result.PlanID == "" || result.Status == "" {
		return ProjectApply{}, fmt.Errorf(
			"decode apply response: planId and status are required",
		)
	}
	result.ProjectKey = projectKey
	return result, nil
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
		DryRun:        dryRun,
		FullInventory: true,
		Resources:     make([]resourceInput, 0, len(project.Resources)),
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
	if !dryRun {
		hasPlanID := plan.PlanID != ""
		hasExpiration := plan.ExpiresAt != ""
		if hasPlanID != hasExpiration || (!hasPlanID && !hasConflict(plan)) {
			return ProjectPlan{}, fmt.Errorf(
				"decode plan response: durable plan requires planId and expiresAt",
			)
		}
	}

	plan.ProjectKey = project.ProjectKey

	return plan, nil
}

func hasConflict(plan ProjectPlan) bool {
	for _, resource := range plan.Resources {
		if resource.Status == ResourceStatusConflict {
			return true
		}
	}
	return false
}

func groupResourcesByProject(
	synced []syncdomain.SyncedResource,
	inventoryProjectKeys []string,
) []projectResources {
	var projects []projectResources
	byProject := make(map[string]int)

	for _, projectKey := range inventoryProjectKeys {
		if _, exists := byProject[projectKey]; exists {
			continue
		}
		byProject[projectKey] = len(projects)
		projects = append(projects, projectResources{ProjectKey: projectKey})
	}

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
