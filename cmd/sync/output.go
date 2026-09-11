package sync

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/launchdarkly/ldcli/internal/output"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type planOutputResource struct {
	ProjectKey    string                 `json:"projectKey"`
	ResourceKind  string                 `json:"resourceKind"`
	LookupKey     string                 `json:"lookupKey"`
	Status        syncapi.ResourceStatus `json:"status"`
	SyncDirection syncapi.SyncDirection  `json:"syncDirection"`
	Action        syncapi.ResourceAction `json:"action"`
	Diff          json.RawMessage        `json:"diff,omitempty"`
	Error         *syncapi.ResourceError `json:"error,omitempty"`
}

type planOutputEnvelope struct {
	Items []planOutputItem `json:"items"`
}

type planOutputItem struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func writePlanOutput(
	out io.Writer,
	outputKind string,
	plans []syncapi.ProjectPlan,
) error {
	resources := flattenPlanResources(plans)

	var outputValue any = planOutputEnvelope{Items: planOutputItems(resources)}
	if outputKind == "json" {
		outputValue = resources
	}

	data, err := json.Marshal(outputValue)
	if err != nil {
		return fmt.Errorf("marshal plan output: %w", err)
	}

	formatted, err := output.CmdOutput("list", outputKind, data)
	if err != nil {
		return err
	}
	if formatted == "" {
		return nil
	}

	if _, err := fmt.Fprintln(out, formatted); err != nil {
		return fmt.Errorf("write plan output: %w", err)
	}

	return nil
}

func flattenPlanResources(plans []syncapi.ProjectPlan) []planOutputResource {
	resources := make([]planOutputResource, 0)

	for _, plan := range plans {
		for _, resource := range plan.Resources {
			resources = append(resources, planOutputResource{
				ProjectKey:    plan.ProjectKey,
				ResourceKind:  string(resource.ResourceKind),
				LookupKey:     resource.LookupKey,
				Status:        resource.Status,
				SyncDirection: resource.SyncDirection,
				Action:        resource.Action,
				Diff:          resource.Diff,
				Error:         resource.Error,
			})
		}
	}

	return resources
}

func planOutputItems(resources []planOutputResource) []planOutputItem {
	items := make([]planOutputItem, 0, len(resources))

	for _, resource := range resources {
		details := fmt.Sprintf(
			"status=%s direction=%s action=%s",
			resource.Status,
			resource.SyncDirection,
			resource.Action,
		)
		if len(resource.Diff) > 0 {
			details += " diff=" + string(resource.Diff)
		}
		if resource.Error != nil {
			details += fmt.Sprintf(
				" error=%s: %s",
				resource.Error.Code,
				resource.Error.Message,
			)
		}

		items = append(items, planOutputItem{
			Key:  resource.ProjectKey + "/" + resource.LookupKey,
			Name: details,
		})
	}

	return items
}
