package prompt

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"

	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
)

// The output kinds that the --output flag selects.
const (
	outputPlaintext = "plaintext"
	outputMarkdown  = "markdown"
	outputJSON      = "json"
)

// OutcomeStatus is the result of one planned action.
type OutcomeStatus string

const (
	OutcomeSucceeded OutcomeStatus = "succeeded"
	OutcomeFailed    OutcomeStatus = "failed"
	OutcomeSkipped   OutcomeStatus = "skipped"
)

// ResourceOutcome is the result of the action of one planned resource.
type ResourceOutcome struct {
	ID     ResourceID    `json:"-"`
	Action Action        `json:"action"`
	Status OutcomeStatus `json:"status"`
	Error  string        `json:"error,omitempty"`
}

// resourceOutput is the identity of a resource in JSON output.
type resourceOutput struct {
	ResourceKind string `json:"resourceKind"`
	ProjectKey   string `json:"projectKey"`
	LookupKey    string `json:"lookupKey"`
}

func newResourceOutput(id ResourceID) resourceOutput {
	return resourceOutput{ResourceKind: string(id.Kind), ProjectKey: id.ProjectKey, LookupKey: id.LookupKey}
}

// writePlanOutput writes the plan in the selected output kind.
func writePlanOutput(out io.Writer, outputKind string, plan Plan) error {
	outputKind, err := checkOutputKind(outputKind)
	if err != nil {
		return err
	}
	if outputKind != outputJSON {
		return writePlanReview(out, outputKind, plan, terminalWidth(out))
	}

	type planResourceOutput struct {
		resourceOutput
		Action          Action              `json:"action"`
		SyncedElsewhere bool                `json:"syncedElsewhere,omitempty"`
		Suggestion      string              `json:"suggestion,omitempty"`
		Error           string              `json:"error,omitempty"`
		Diff            variationDiffFields `json:"diff,omitempty"`
	}
	resources := make([]planResourceOutput, 0, len(plan.Resources))
	for _, resource := range plan.Resources {
		resources = append(resources, planResourceOutput{
			resourceOutput:  newResourceOutput(resource.ID),
			Action:          resource.Action,
			SyncedElsewhere: resource.SyncedElsewhere,
			Suggestion:      staleSuggestion(resource),
			Error:           resource.Error,
			Diff:            resource.Diff,
		})
	}
	return writeJSON(out, map[string]any{"resources": resources})
}

// writeOutcomeOutput writes the result of each action, which includes failures.
func writeOutcomeOutput(out io.Writer, outputKind string, outcomes []ResourceOutcome) error {
	outputKind, err := checkOutputKind(outputKind)
	if err != nil {
		return err
	}
	if outputKind == outputJSON {
		type outcomeOutput struct {
			resourceOutput
			ResourceOutcome
		}
		resources := make([]outcomeOutput, 0, len(outcomes))
		for _, outcome := range outcomes {
			resources = append(resources, outcomeOutput{resourceOutput: newResourceOutput(outcome.ID), ResourceOutcome: outcome})
		}
		return writeJSON(out, map[string]any{"resources": resources})
	}

	console := syncconsole.New(out)
	if outputKind == outputMarkdown {
		_ = console.Line("## Sync results")
	} else {
		_ = console.Line("Sync results:")
	}
	for _, outcome := range outcomes {
		_ = console.Printf("- %s  action=%s  status=%s\n", outcome.ID, outcome.Action, outcome.Status)
		if outcome.Error != "" {
			_ = console.Printf("  Error: %s\n", outcome.Error)
		}
	}
	return nil
}

// checkOutputKind returns the output kind. An empty kind is plain text.
func checkOutputKind(outputKind string) (string, error) {
	switch outputKind {
	case "":
		return outputPlaintext, nil
	case outputPlaintext, outputMarkdown, outputJSON:
		return outputKind, nil
	default:
		return "", fmt.Errorf("unsupported output kind %q", outputKind)
	}
}

// writePlanReview writes the plan for a person to read. It groups the
// resources by project and config, and shows each action, error, and diff.
func writePlanReview(out io.Writer, outputKind string, plan Plan, width int) error {
	console := syncconsole.New(out)
	if len(plan.Resources) == 0 {
		_ = console.Line("No prompt variations are tracked.")
		return nil
	}
	markdown := outputKind == outputMarkdown
	detail := func(label, text string) {
		if markdown {
			_ = console.Printf("%s: %s\n", label, text)
		} else {
			_ = console.Printf("      %s: %s\n", label, text)
		}
	}

	currentProject, currentConfig := "", ""
	for _, resource := range plan.Resources {
		if resource.ID.ProjectKey != currentProject {
			if currentProject != "" {
				_ = console.Line("")
			}
			currentProject, currentConfig = resource.ID.ProjectKey, ""
			if markdown {
				_ = console.Printf("## Project `%s`\n", currentProject)
			} else {
				_ = console.Printf("%s\n", reviewHeading("Project: "+currentProject, width))
			}
		}

		action := actionDescription(resource.Action)
		configKey, variationKey, err := resource.ID.VariationKeys()
		switch {
		case markdown:
			_ = console.Printf("\n### Variation `%s`\n\nAction: **%s**\n", resource.ID.LookupKey, action)
		case err != nil:
			_ = console.Printf("\n  %s\n    Action: %s\n", reviewHeading("Variation: "+resource.ID.LookupKey, width), action)
		default:
			if configKey != currentConfig {
				currentConfig = configKey
				_ = console.Printf("\n  %s\n", reviewHeading("Config: "+configKey, width))
			}
			_ = console.Printf("\n    %s\n      Action: %s\n", reviewHeading("Variation: "+variationKey, width), action)
		}

		if resource.SyncedElsewhere {
			detail("Note", "Another working copy synced this variation after your sync.lock.")
			detail("Suggestion", staleSuggestion(resource))
		}
		if resource.Error != "" {
			detail("Error", resource.Error)
		}
		if len(resource.Diff) != 0 {
			rendered, err := renderVariationDiff(resource.Diff, outputKind, width, diffPresentation(resource.Action))
			if err != nil {
				return err
			}
			_ = console.Write(rendered)
		}
	}
	return nil
}

// staleSuggestion tells the user what to do when another working copy synced
// the resource. It returns an empty string for a resource that is not stale.
func staleSuggestion(resource PlannedResource) string {
	switch {
	case !resource.SyncedElsewhere:
		return ""
	case resource.Action == ActionUpdateLocal:
		return "If the other working copy pushed its change to Git, run git pull before you sync. " +
			"Then your sync.lock does not get a merge conflict."
	case resource.Action == ActionConflict:
		return "Run git pull to get the other change, and then run sync again. " +
			"If the conflict remains, choose a side with --conflict or --resolve."
	default:
		return "Run git pull to get the latest .launchdarkly files and sync.lock, and then run sync again."
	}
}

// reviewHeading colors a heading when the output is a terminal.
func reviewHeading(value string, width int) string {
	if width <= 0 {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("67")).Bold(true).Render(value)
}

// actionDescription describes an action for the user.
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

// variationDiffPresentation is the labels and the direction of a diff.
type variationDiffPresentation struct {
	beforeLabel  string
	afterLabel   string
	missingAfter string
	// reverse shows the local file as "before", because the local file changes.
	reverse bool
}

// diffPresentation returns the labels that describe the change of an action.
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

// writeJSON writes indented JSON with a final newline.
func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("write sync output: %w", err)
	}
	return nil
}
