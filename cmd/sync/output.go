package sync

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/launchdarkly/ldcli/internal/output"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type planOutputResource struct {
	ResourceKind  string                 `json:"resourceKind"`
	LookupKey     string                 `json:"lookupKey"`
	Status        syncapi.ResourceStatus `json:"status"`
	SyncDirection syncapi.SyncDirection  `json:"syncDirection"`
	Diff          json.RawMessage        `json:"diff,omitempty"`
	Error         *syncapi.ResourceError `json:"error,omitempty"`
}

type projectPlanOutput struct {
	ProjectKey string               `json:"projectKey"`
	PlanID     string               `json:"planId,omitempty"`
	ExpiresAt  string               `json:"expiresAt,omitempty"`
	Resources  []planOutputResource `json:"resources"`
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
	outputPlans := newProjectPlanOutputs(plans)

	var outputValue any = planOutputEnvelope{Items: planOutputItems(outputPlans)}
	if outputKind == "json" {
		outputValue = outputPlans
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

func newProjectPlanOutputs(plans []syncapi.ProjectPlan) []projectPlanOutput {
	outputPlans := make([]projectPlanOutput, 0, len(plans))
	for _, plan := range plans {
		outputPlan := projectPlanOutput{
			ProjectKey: plan.ProjectKey,
			PlanID:     plan.PlanID,
			ExpiresAt:  plan.ExpiresAt,
			Resources:  make([]planOutputResource, 0, len(plan.Resources)),
		}
		for _, resource := range plan.Resources {
			outputPlan.Resources = append(outputPlan.Resources, planOutputResource{
				ResourceKind:  string(resource.ResourceKind),
				LookupKey:     resource.LookupKey,
				Status:        resource.Status,
				SyncDirection: resource.SyncDirection,
				Diff:          resource.Diff,
				Error:         resource.Error,
			})
		}
		outputPlans = append(outputPlans, outputPlan)
	}

	return outputPlans
}

func planOutputItems(plans []projectPlanOutput) []planOutputItem {
	var items []planOutputItem

	for _, plan := range plans {
		if plan.PlanID != "" {
			items = append(items, planOutputItem{
				Key: plan.ProjectKey,
				Name: fmt.Sprintf(
					"planId=%s expiresAt=%s",
					plan.PlanID,
					plan.ExpiresAt,
				),
			})
		}

		for _, resource := range plan.Resources {
			details := fmt.Sprintf(
				"status=%s direction=%s",
				resource.Status,
				resource.SyncDirection,
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
				Key:  plan.ProjectKey + "/" + resource.LookupKey,
				Name: details,
			})
		}
	}

	return items
}
