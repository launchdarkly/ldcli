package prompt

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type planOutputResource struct {
	ResourceKind           string                 `json:"resourceKind"`
	LookupKey              string                 `json:"lookupKey"`
	Status                 syncapi.ResourceStatus `json:"status"`
	SyncDirection          syncapi.SyncDirection  `json:"syncDirection"`
	ManifestUpdateRequired bool                   `json:"manifestUpdateRequired"`
	LocalDeleted           bool                   `json:"localDeleted,omitempty"`
	ServerDeleted          bool                   `json:"serverDeleted,omitempty"`
	Diff                   json.RawMessage        `json:"diff,omitempty"`
	Error                  *syncapi.ResourceError `json:"error,omitempty"`
}

type projectPlanOutput struct {
	ProjectKey string               `json:"projectKey"`
	PlanID     string               `json:"planId,omitempty"`
	ExpiresAt  string               `json:"expiresAt,omitempty"`
	Resources  []planOutputResource `json:"resources"`
}

type applyOutputResource struct {
	ResourceKind string                       `json:"resourceKind"`
	LookupKey    string                       `json:"lookupKey"`
	Outcome      syncapi.ResourceApplyOutcome `json:"outcome"`
	Error        *syncapi.ResourceError       `json:"error,omitempty"`
}

type projectApplyOutput struct {
	ProjectKey string                 `json:"projectKey"`
	PlanID     string                 `json:"planId"`
	Status     syncapi.PlanStatus     `json:"status"`
	Error      *syncapi.ResourceError `json:"error,omitempty"`
	Resources  []applyOutputResource  `json:"resources"`
}

type pulledResourceOutput struct {
	ProjectKey string `json:"projectKey"`
	LookupKey  string `json:"lookupKey"`
	Path       string `json:"path"`
	Deleted    bool   `json:"deleted,omitempty"`
}

type syncOutput struct {
	Pulls   []pulledResourceOutput `json:"pulls,omitempty"`
	Plans   []projectPlanOutput    `json:"plans,omitempty"`
	Applies []projectApplyOutput   `json:"applies,omitempty"`
}

func writePlanOutput(
	out io.Writer,
	outputKind string,
	plans []syncapi.ProjectPlan,
) error {
	if outputKind == "" {
		outputKind = "plaintext"
	}
	outputPlans := newProjectPlanOutputs(plans)
	if outputKind == "json" {
		return writeJSON(out, outputPlans)
	}
	if outputKind != "plaintext" && outputKind != "markdown" {
		return fmt.Errorf("unsupported output kind %q", outputKind)
	}
	return writePlanReview(out, outputKind, plans, terminalWidth(out))
}

func writeSyncOutput(
	out io.Writer,
	outputKind string,
	pulls []serverPull,
	plans []syncapi.ProjectPlan,
	applies []syncapi.ProjectApply,
) error {
	if outputKind == "" {
		outputKind = "plaintext"
	}
	if outputKind == "json" {
		return writeJSON(out, syncOutput{
			Pulls:   newPulledResourceOutputs(pulls),
			Plans:   newProjectPlanOutputs(plans),
			Applies: newProjectApplyOutputs(applies),
		})
	}
	if outputKind != "plaintext" && outputKind != "markdown" {
		return fmt.Errorf("unsupported output kind %q", outputKind)
	}

	if len(pulls) != 0 {
		writePullResults(out, outputKind, pulls)
		if len(plans) != 0 || len(applies) != 0 {
			_, _ = fmt.Fprintln(out)
		}
	}
	if len(plans) != 0 {
		if err := writePlanReview(out, outputKind, plans, terminalWidth(out)); err != nil {
			return err
		}
		if len(applies) != 0 {
			_, _ = fmt.Fprintln(out)
		}
	}
	return writeApplyResults(out, outputKind, applies)
}

func newPulledResourceOutputs(pulls []serverPull) []pulledResourceOutput {
	result := make([]pulledResourceOutput, 0, len(pulls))
	for _, pull := range pulls {
		result = append(result, pulledResourceOutput{
			ProjectKey: pull.ProjectKey,
			LookupKey:  pull.LookupKey,
			Path:       pull.Path,
			Deleted:    pull.Action == deleteLocalFile,
		})
	}
	return result
}

func writePullResults(out io.Writer, outputKind string, pulls []serverPull) {
	if outputKind == "markdown" {
		_, _ = fmt.Fprintln(out, "## Pulled server changes")
	} else {
		_, _ = fmt.Fprintln(out, "Pulled server changes:")
	}
	for _, pull := range pulls {
		target := ".launchdarkly/" + pull.Path
		if pull.Action == deleteLocalFile {
			target = "removed " + target
		}
		_, _ = fmt.Fprintf(
			out,
			"- %s/%s -> %s\n",
			pull.ProjectKey,
			pull.LookupKey,
			target,
		)
	}
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
				ResourceKind:           string(resource.ResourceKind),
				LookupKey:              resource.LookupKey,
				Status:                 resource.Status,
				SyncDirection:          resource.SyncDirection,
				ManifestUpdateRequired: resource.ManifestUpdateRequired,
				LocalDeleted:           resource.LocalDeleted,
				ServerDeleted:          resource.ServerDeleted,
				Diff:                   resource.Diff,
				Error:                  resource.Error,
			})
		}
		outputPlans = append(outputPlans, outputPlan)
	}

	return outputPlans
}

func newProjectApplyOutputs(
	applies []syncapi.ProjectApply,
) []projectApplyOutput {
	result := make([]projectApplyOutput, 0, len(applies))
	for _, apply := range applies {
		output := projectApplyOutput{
			ProjectKey: apply.ProjectKey,
			PlanID:     apply.PlanID,
			Status:     apply.Status,
			Error:      apply.Error,
			Resources:  make([]applyOutputResource, 0, len(apply.Resources)),
		}
		for _, resource := range apply.Resources {
			output.Resources = append(output.Resources, applyOutputResource{
				ResourceKind: string(resource.ResourceKind),
				LookupKey:    resource.LookupKey,
				Outcome:      resource.Outcome,
				Error:        resource.Error,
			})
		}
		result = append(result, output)
	}
	return result
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write sync output: %w", err)
	}
	return nil
}

func writePlanReview(
	out io.Writer,
	outputKind string,
	plans []syncapi.ProjectPlan,
	width int,
) error {
	for planIndex, plan := range plans {
		if planIndex != 0 {
			_, _ = fmt.Fprintln(out)
		}
		if outputKind == "markdown" {
			_, _ = fmt.Fprintf(out, "## Project `%s`\n", plan.ProjectKey)
		} else {
			_, _ = fmt.Fprintf(out, "Project: %s\n", plan.ProjectKey)
		}
		if plan.PlanID != "" {
			_, _ = fmt.Fprintf(
				out,
				"Plan: %s\nExpires: %s\n",
				plan.PlanID,
				formatExpiration(plan.ExpiresAt),
			)
		}
		for _, resource := range plan.Resources {
			presentation := presentPlanResource(resource)
			if outputKind == "markdown" {
				_, _ = fmt.Fprintf(
					out,
					"\n### `%s`\n\nStatus: **%s**\n",
					resource.LookupKey,
					presentation.status,
				)
			} else {
				_, _ = fmt.Fprintf(
					out,
					"\n%s\n  Status: %s\n",
					resource.LookupKey,
					presentation.status,
				)
			}
			if presentation.action != "" {
				if outputKind == "markdown" {
					_, _ = fmt.Fprintf(out, "Action: %s\n", presentation.action)
				} else {
					_, _ = fmt.Fprintf(out, "  Action: %s\n", presentation.action)
				}
			}
			if resource.Error != nil {
				_, _ = fmt.Fprintf(
					out,
					"  Error: %s: %s\n",
					resource.Error.Code,
					resource.Error.Message,
				)
			}
			if len(resource.Diff) != 0 {
				rendered, err := renderVariationDiff(
					resource.Diff,
					outputKind,
					width,
					presentation.diff,
				)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprint(out, rendered)
			}
		}
	}
	return nil
}

func formatExpiration(value string) string {
	return formatExpirationIn(value, time.Local)
}

func formatExpirationIn(value string, location *time.Location) string {
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return expiresAt.In(location).Format("January 2, 2006 at 3:04 PM MST")
}

func writeApplyResults(
	out io.Writer,
	outputKind string,
	applies []syncapi.ProjectApply,
) error {
	for index, apply := range applies {
		if index != 0 {
			_, _ = fmt.Fprintln(out)
		}
		if outputKind == "markdown" {
			_, _ = fmt.Fprintf(
				out,
				"## Apply `%s`\n\nProject: `%s`  Status: `%s`\n",
				apply.PlanID,
				apply.ProjectKey,
				apply.Status,
			)
		} else {
			_, _ = fmt.Fprintf(
				out,
				"Apply: %s  Project: %s  Status: %s\n",
				apply.PlanID,
				apply.ProjectKey,
				apply.Status,
			)
		}
		if apply.Error != nil {
			_, _ = fmt.Fprintf(
				out,
				"  Error: %s: %s\n",
				apply.Error.Code,
				apply.Error.Message,
			)
		}
		for _, resource := range apply.Resources {
			_, _ = fmt.Fprintf(
				out,
				"  %s  outcome=%s\n",
				resource.LookupKey,
				resource.Outcome,
			)
			if resource.Error != nil {
				_, _ = fmt.Fprintf(
					out,
					"    Error: %s: %s\n",
					resource.Error.Code,
					resource.Error.Message,
				)
			}
		}
	}
	return nil
}

type planResourcePresentation struct {
	status string
	action string
	diff   variationDiffPresentation
}

type variationDiffPresentation struct {
	beforeLabel string
	afterLabel  string
	reverse     bool
}

func presentPlanResource(
	resource syncapi.PlannedResource,
) planResourcePresentation {
	switch resource.Status {
	case syncapi.ResourceStatusInSync:
		return presentInSyncResource(resource)
	case syncapi.ResourceStatusLocalChanged:
		return presentLocalChange(resource)
	case syncapi.ResourceStatusServerChanged:
		return presentServerChange(resource)
	case syncapi.ResourceStatusConflict:
		return presentConflict(resource)
	default:
		return planResourcePresentation{
			status: string(resource.Status),
			diff:   currentSourcesDiff(),
		}
	}
}

func presentInSyncResource(
	resource syncapi.PlannedResource,
) planResourcePresentation {
	status := "In sync"
	if resource.LocalDeleted && resource.ServerDeleted {
		status = "Removed locally and from LaunchDarkly"
	}
	return planResourcePresentation{
		status: status,
		diff:   currentSourcesDiff(),
	}
}

func presentLocalChange(
	resource syncapi.PlannedResource,
) planResourcePresentation {
	if resource.SyncDirection == syncapi.SyncDirectionServerCanonical {
		if resource.LocalDeleted {
			return planResourcePresentation{
				status: "Local changes detected",
				action: "Update the local file from LaunchDarkly.",
				diff:   localFileFromServerDiff(),
			}
		}
		return planResourcePresentation{
			status: "Local changes detected",
			action: "No automatic change; LaunchDarkly is authoritative.",
			diff:   currentSourcesDiff(),
		}
	}

	if resource.LocalDeleted {
		return planResourcePresentation{
			status: "Local file removed",
			action: "Delete the variation from LaunchDarkly.",
			diff:   deleteServerVariationDiff(),
		}
	}
	return planResourcePresentation{
		status: "Local changes detected",
		action: "Update LaunchDarkly from the local file.",
		diff:   serverFromLocalFileDiff(),
	}
}

func presentServerChange(
	resource syncapi.PlannedResource,
) planResourcePresentation {
	if resource.SyncDirection == syncapi.SyncDirectionCodeCanonical {
		if resource.ServerDeleted {
			return planResourcePresentation{
				status: "LaunchDarkly changes detected",
				action: "Update LaunchDarkly from the local file.",
				diff:   serverFromLocalFileDiff(),
			}
		}
		return planResourcePresentation{
			status: "LaunchDarkly changes detected",
			action: "No automatic change; the local file is authoritative.",
			diff:   currentSourcesDiff(),
		}
	}

	if resource.ServerDeleted {
		return planResourcePresentation{
			status: "LaunchDarkly variation removed",
			action: "Delete the local file.",
			diff:   deleteLocalFileDiff(),
		}
	}
	return planResourcePresentation{
		status: "LaunchDarkly changes detected",
		action: "Update the local file from LaunchDarkly.",
		diff:   localFileFromServerDiff(),
	}
}

func presentConflict(
	resource syncapi.PlannedResource,
) planResourcePresentation {
	isDeletion := resource.LocalDeleted || resource.ServerDeleted
	status := "Conflict"
	if isDeletion {
		status = "Deletion conflict"
	}

	switch {
	case isDeletion &&
		resource.SyncDirection == syncapi.SyncDirectionCodeCanonical:
		return planResourcePresentation{
			status: status,
			action: "Resolve using the local file because it is authoritative.",
			diff:   serverFromLocalFileDiff(),
		}
	case isDeletion &&
		resource.SyncDirection == syncapi.SyncDirectionServerCanonical:
		return planResourcePresentation{
			status: status,
			action: "Resolve using LaunchDarkly because it is authoritative.",
			diff:   localFileFromServerDiff(),
		}
	default:
		return planResourcePresentation{
			status: status,
			action: "No automatic change; resolve the conflict first.",
			diff:   currentSourcesDiff(),
		}
	}
}

func currentSourcesDiff() variationDiffPresentation {
	return variationDiffPresentation{
		beforeLabel: "LaunchDarkly now",
		afterLabel:  "Local file now",
	}
}

func serverFromLocalFileDiff() variationDiffPresentation {
	return variationDiffPresentation{
		beforeLabel: "LaunchDarkly now",
		afterLabel:  "LaunchDarkly after sync (from local file)",
	}
}

func localFileFromServerDiff() variationDiffPresentation {
	return variationDiffPresentation{
		beforeLabel: "Local file now",
		afterLabel:  "Local file after sync (from LaunchDarkly)",
		reverse:     true,
	}
}

func deleteServerVariationDiff() variationDiffPresentation {
	return variationDiffPresentation{
		beforeLabel: "LaunchDarkly now",
		afterLabel:  "LaunchDarkly after sync",
	}
}

func deleteLocalFileDiff() variationDiffPresentation {
	return variationDiffPresentation{
		beforeLabel: "Local file now",
		afterLabel:  "Local file after sync",
		reverse:     true,
	}
}
