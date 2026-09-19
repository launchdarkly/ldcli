package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

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

type applyOutputResource struct {
	ResourceKind  string                      `json:"resourceKind"`
	LookupKey     string                      `json:"lookupKey"`
	Status        syncapi.ResourceStatus      `json:"status"`
	SyncDirection syncapi.SyncDirection       `json:"syncDirection"`
	ApplyStatus   syncapi.ResourceApplyStatus `json:"applyStatus"`
	Diff          json.RawMessage             `json:"diff,omitempty"`
	Error         *syncapi.ResourceError      `json:"error,omitempty"`
	ApplyError    *syncapi.ResourceError      `json:"applyError,omitempty"`
}

type projectApplyOutput struct {
	ProjectKey string                 `json:"projectKey"`
	PlanID     string                 `json:"planId"`
	Status     syncapi.PlanStatus     `json:"status"`
	AppliedAt  string                 `json:"appliedAt,omitempty"`
	Error      *syncapi.ResourceError `json:"error,omitempty"`
	Resources  []applyOutputResource  `json:"resources"`
}

type syncOutput struct {
	Plans   []projectPlanOutput  `json:"plans,omitempty"`
	Applies []projectApplyOutput `json:"applies,omitempty"`
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
	plans []syncapi.ProjectPlan,
	applies []syncapi.ProjectApply,
) error {
	if outputKind == "" {
		outputKind = "plaintext"
	}
	if outputKind == "json" {
		return writeJSON(out, syncOutput{
			Plans:   newProjectPlanOutputs(plans),
			Applies: newProjectApplyOutputs(applies),
		})
	}
	if outputKind != "plaintext" && outputKind != "markdown" {
		return fmt.Errorf("unsupported output kind %q", outputKind)
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

func newProjectApplyOutputs(
	applies []syncapi.ProjectApply,
) []projectApplyOutput {
	result := make([]projectApplyOutput, 0, len(applies))
	for _, apply := range applies {
		output := projectApplyOutput{
			ProjectKey: apply.ProjectKey,
			PlanID:     apply.PlanID,
			Status:     apply.Status,
			AppliedAt:  apply.AppliedAt,
			Error:      apply.Error,
			Resources:  make([]applyOutputResource, 0, len(apply.Resources)),
		}
		for _, resource := range apply.Resources {
			output.Resources = append(output.Resources, applyOutputResource{
				ResourceKind:  string(resource.ResourceKind),
				LookupKey:     resource.LookupKey,
				Status:        resource.Status,
				SyncDirection: resource.SyncDirection,
				ApplyStatus:   resource.ApplyStatus,
				Diff:          resource.Diff,
				Error:         resource.Error,
				ApplyError:    resource.ApplyError,
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
				"Plan: %s  Expires: %s\n",
				plan.PlanID,
				plan.ExpiresAt,
			)
		}
		for _, resource := range plan.Resources {
			if outputKind == "markdown" {
				_, _ = fmt.Fprintf(
					out,
					"\n### `%s`\n\nStatus: `%s`  Direction: `%s`\n",
					resource.LookupKey,
					resource.Status,
					resource.SyncDirection,
				)
			} else {
				_, _ = fmt.Fprintf(
					out,
					"\n%s\n  Status: %s  Direction: %s\n",
					resource.LookupKey,
					resource.Status,
					resource.SyncDirection,
				)
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
				"  %s  apply=%s\n",
				resource.LookupKey,
				resource.ApplyStatus,
			)
			if resource.ApplyError != nil {
				_, _ = fmt.Fprintf(
					out,
					"    Error: %s: %s\n",
					resource.ApplyError.Code,
					resource.ApplyError.Message,
				)
			}
		}
	}
	return nil
}

type variationFieldDiff struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func renderVariationDiff(
	payload json.RawMessage,
	outputKind string,
	width int,
) (string, error) {
	fields := make(map[string]variationFieldDiff)
	if err := json.Unmarshal(payload, &fields); err != nil {
		return "", fmt.Errorf("decode variation diff: %w", err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var rendered strings.Builder
	for _, key := range keys {
		diff := fields[key]
		before, err := formatDiffValue(diff.Before)
		if err != nil {
			return "", err
		}
		after, err := formatDiffValue(diff.After)
		if err != nil {
			return "", err
		}
		change := "changed"
		if len(diff.Before) == 0 {
			change = "added"
		}
		if len(diff.After) == 0 {
			change = "removed"
		}

		if outputKind == "plaintext" && width >= 100 {
			rendered.WriteString(renderSideBySideDiff(
				key,
				change,
				before,
				after,
				width,
			))
			continue
		}

		if outputKind == "markdown" {
			_, _ = fmt.Fprintf(&rendered, "\n#### %s (%s)\n\n", key, change)
			_, _ = fmt.Fprintf(
				&rendered,
				"**Before**\n\n```json\n%s\n```\n\n",
				before,
			)
			_, _ = fmt.Fprintf(
				&rendered,
				"**After**\n\n```json\n%s\n```\n",
				after,
			)
			continue
		}
		_, _ = fmt.Fprintf(&rendered, "  %s (%s)\n", key, change)
		_, _ = fmt.Fprintf(
			&rendered,
			"    Before:\n%s\n",
			indent(before, 6),
		)
		_, _ = fmt.Fprintf(
			&rendered,
			"    After:\n%s\n",
			indent(after, 6),
		)
	}
	return rendered.String(), nil
}

func renderSideBySideDiff(
	field string,
	change string,
	before string,
	after string,
	width int,
) string {
	gap := 2
	panelWidth := (width - gap) / 2
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(panelWidth - 2)
	beforePanel := panelStyle.Render("Before\n" + before)
	afterPanel := panelStyle.Render("After\n" + after)
	return fmt.Sprintf(
		"  %s (%s)\n%s\n",
		field,
		change,
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			beforePanel,
			strings.Repeat(" ", gap),
			afterPanel,
		),
	)
}

func formatDiffValue(value json.RawMessage) (string, error) {
	if len(value) == 0 {
		return "(not present)", nil
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, value, "", "  "); err != nil {
		return "", fmt.Errorf("format variation diff: %w", err)
	}
	return formatted.String(), nil
}

func indent(value string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	return prefix + strings.ReplaceAll(value, "\n", "\n"+prefix)
}

func terminalWidth(out io.Writer) int {
	file, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0
	}
	return width
}
