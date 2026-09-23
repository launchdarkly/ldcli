package prompt

import (
	"encoding/json"
	"fmt"
	"io"
)

// OutcomeStatus describes whether a reviewed resource action completed.
type OutcomeStatus string

const (
	OutcomeSucceeded OutcomeStatus = "succeeded"
	OutcomeFailed    OutcomeStatus = "failed"
	OutcomeSkipped   OutcomeStatus = "skipped"
)

// ResourceOutcome records the result of executing one planned resource action.
type ResourceOutcome struct {
	ID     ResourceID    `json:"-"`
	Action Action        `json:"action"`
	Status OutcomeStatus `json:"status"`
	Error  string        `json:"error,omitempty"`
}

type planResourceOutput struct {
	ResourceKind string              `json:"resourceKind"`
	ProjectKey   string              `json:"projectKey"`
	LookupKey    string              `json:"lookupKey"`
	Action       Action              `json:"action"`
	Error        string              `json:"error,omitempty"`
	Diff         variationDiffFields `json:"diff,omitempty"`
}

type outcomeOutput struct {
	ResourceKind string        `json:"resourceKind"`
	ProjectKey   string        `json:"projectKey"`
	LookupKey    string        `json:"lookupKey"`
	Action       Action        `json:"action"`
	Status       OutcomeStatus `json:"status"`
	Error        string        `json:"error,omitempty"`
}

// writePlanOutput renders the local synchronization plan.
func writePlanOutput(out io.Writer, outputKind string, plan Plan) error {
	if outputKind == "" {
		outputKind = "plaintext"
	}
	if outputKind == "json" {
		resources := make([]planResourceOutput, 0, len(plan.Resources))
		for _, resource := range plan.Resources {
			resources = append(resources, planResourceOutput{
				ResourceKind: string(resource.ID.Kind),
				ProjectKey:   resource.ID.ProjectKey,
				LookupKey:    resource.ID.LookupKey,
				Action:       resource.Action,
				Error:        resource.Error,
				Diff:         resource.Diff,
			})
		}
		return writeJSON(out, map[string]any{"resources": resources})
	}
	if outputKind != "plaintext" && outputKind != "markdown" {
		return fmt.Errorf("unsupported output kind %q", outputKind)
	}
	return writePlanReview(out, outputKind, plan, terminalWidth(out))
}

// writeOutcomeOutput renders execution results, including partial failures.
func writeOutcomeOutput(out io.Writer, outputKind string, outcomes []ResourceOutcome) error {
	if outputKind == "" {
		outputKind = "plaintext"
	}
	if outputKind == "json" {
		resources := make([]outcomeOutput, 0, len(outcomes))
		for _, outcome := range outcomes {
			resources = append(resources, outcomeOutput{
				ResourceKind: string(outcome.ID.Kind),
				ProjectKey:   outcome.ID.ProjectKey,
				LookupKey:    outcome.ID.LookupKey,
				Action:       outcome.Action,
				Status:       outcome.Status,
				Error:        outcome.Error,
			})
		}
		return writeJSON(out, map[string]any{"resources": resources})
	}
	if outputKind != "plaintext" && outputKind != "markdown" {
		return fmt.Errorf("unsupported output kind %q", outputKind)
	}

	if outputKind == "markdown" {
		_, _ = fmt.Fprintln(out, "## Sync results")
	} else {
		_, _ = fmt.Fprintln(out, "Sync results:")
	}
	for _, outcome := range outcomes {
		_, _ = fmt.Fprintf(out, "- %s/%s  action=%s  status=%s\n", outcome.ID.ProjectKey, outcome.ID.LookupKey, outcome.Action, outcome.Status)
		if outcome.Error != "" {
			_, _ = fmt.Fprintf(out, "  Error: %s\n", outcome.Error)
		}
	}
	return nil
}

func writePlanReview(out io.Writer, outputKind string, plan Plan, width int) error {
	if len(plan.Resources) == 0 {
		_, _ = fmt.Fprintln(out, "No prompt variations are tracked.")
		return nil
	}

	currentProject := ""
	for _, resource := range plan.Resources {
		if resource.ID.ProjectKey != currentProject {
			if currentProject != "" {
				_, _ = fmt.Fprintln(out)
			}
			currentProject = resource.ID.ProjectKey
			if outputKind == "markdown" {
				_, _ = fmt.Fprintf(out, "## Project `%s`\n", currentProject)
			} else {
				_, _ = fmt.Fprintf(out, "Project: %s\n", currentProject)
			}
		}

		if outputKind == "markdown" {
			_, _ = fmt.Fprintf(out, "\n### `%s`\n\nAction: **%s**\n", resource.ID.LookupKey, actionDescription(resource.Action))
		} else {
			_, _ = fmt.Fprintf(out, "\n%s\n  Action: %s\n", resource.ID.LookupKey, actionDescription(resource.Action))
		}
		if resource.Error != "" {
			_, _ = fmt.Fprintf(out, "  Error: %s\n", resource.Error)
		}
		if len(resource.Diff) != 0 {
			rendered, err := renderVariationDiff(resource.Diff, outputKind, width, diffPresentation(resource.Action))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(out, rendered)
		}
	}
	return nil
}

func actionDescription(action Action) string {
	switch action {
	case ActionInSync:
		return "No change (in sync)"
	case ActionCreateServer:
		return "Create the variation in LaunchDarkly"
	case ActionUpdateServer:
		return "Update LaunchDarkly from the local file"
	case ActionArchiveServer:
		return "Archive the variation in LaunchDarkly"
	case ActionUpdateLocal:
		return "Update the local file from LaunchDarkly"
	case ActionDeleteLocal:
		return "Delete the local file"
	case ActionUpdateManifest:
		return "Record the matching state in the manifest"
	case ActionRemoveManifest:
		return "Remove the deleted resource from the manifest"
	case ActionConflict:
		return "Resolve the conflict before syncing"
	case ActionError:
		return "Fix the resource before syncing"
	default:
		return string(action)
	}
}

type variationDiffPresentation struct {
	beforeLabel  string
	afterLabel   string
	missingAfter string
	reverse      bool
}

func diffPresentation(action Action) variationDiffPresentation {
	switch action {
	case ActionUpdateLocal, ActionDeleteLocal:
		return variationDiffPresentation{beforeLabel: "Local file now", afterLabel: "Local file after sync", reverse: true}
	case ActionCreateServer, ActionUpdateServer:
		return variationDiffPresentation{beforeLabel: "LaunchDarkly now", afterLabel: "LaunchDarkly after sync"}
	case ActionArchiveServer:
		return variationDiffPresentation{
			beforeLabel: "LaunchDarkly now", afterLabel: "LaunchDarkly after sync", missingAfter: "(archived in LaunchDarkly)",
		}
	default:
		return variationDiffPresentation{beforeLabel: "LaunchDarkly now", afterLabel: "Local file now"}
	}
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write sync output: %w", err)
	}
	return nil
}
