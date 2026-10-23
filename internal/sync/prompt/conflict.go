package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
)

// ConflictResolution is the side that wins a conflict.
type ConflictResolution string

const (
	// ConflictUseLaunchDarkly writes the LaunchDarkly state to the local file.
	ConflictUseLaunchDarkly ConflictResolution = "launchdarkly"
	// ConflictUseLocal writes the local state to LaunchDarkly.
	ConflictUseLocal ConflictResolution = "local"
	// ConflictAbort stops the sync before it writes anything.
	ConflictAbort ConflictResolution = "abort"
)

// Valid reports whether the resolution is one of the supported choices.
func (resolution ConflictResolution) Valid() bool {
	return resolution == ConflictUseLaunchDarkly || resolution == ConflictUseLocal || resolution == ConflictAbort
}

// ParseConflictResolution parses a conflict choice from the command line.
func ParseConflictResolution(value string) (ConflictResolution, error) {
	if resolution := ConflictResolution(value); resolution.Valid() {
		return resolution, nil
	}
	return "", fmt.Errorf("invalid conflict resolution %q; expected launchdarkly, local, or abort", value)
}

// ConflictPolicy is a default choice and a choice for each named variation.
// A command without a policy asks the user.
type ConflictPolicy struct {
	Default   ConflictResolution
	Overrides map[ResourceID]ConflictResolution
}

// resolve returns the policy choice for a group. The bool result is false when
// the policy has no choice for the group.
func (policy ConflictPolicy) resolve(group conflictGroup) (ConflictResolution, bool, error) {
	var selected ConflictResolution
	for _, resource := range group.resources {
		resolution, ok := policy.Overrides[resource.ID]
		if !ok {
			continue
		}
		if !resolution.Valid() {
			return "", false, fmt.Errorf("invalid conflict resolution %q", resolution)
		}
		if selected != "" && selected != resolution {
			return "", false, errors.New("conflicting --resolve choices affect the same shared attachment")
		}
		selected = resolution
	}
	if selected == "" {
		selected = policy.Default
	}
	if selected == "" {
		return "", false, nil
	}
	if !selected.Valid() {
		return "", false, fmt.Errorf("invalid conflict resolution %q", selected)
	}
	return selected, true, nil
}

var errConflictAborted = errors.New("sync conflict left unresolved")

// conflictGroup is one conflict choice. Variations that share a changed
// attachment are in one group, because the attachment can have only one state.
type conflictGroup struct {
	resources []PlannedResource
}

type conflictResolutionResult struct {
	resolutions    map[ResourceID]ConflictResolution
	aborted        bool
	sourcesChanged bool
}

type conflictChoice struct {
	resolution     ConflictResolution
	aborted        bool
	sourcesChanged bool
}

// resolveConflicts shows each conflict and gets a choice from the policy or
// from the user. In watch mode, a file change during the choice stops it.
func resolveConflicts(
	options Options,
	plan Plan,
	reader io.Reader,
	interactive bool,
	watched *watchedSources,
) (conflictResolutionResult, error) {
	groups := groupConflicts(plan)
	if len(groups) == 0 {
		return conflictResolutionResult{}, nil
	}

	_ = syncconsole.New(options.ErrorOutput).Line(planSummary(plan))
	result := conflictResolutionResult{resolutions: make(map[ResourceID]ConflictResolution)}
	for _, group := range groups {
		if err := writeConflict(options.ErrorOutput, group); err != nil {
			return conflictResolutionResult{}, err
		}

		resolution, fromPolicy, err := options.ConflictPolicy.resolve(group)
		if err != nil {
			return conflictResolutionResult{}, err
		}
		var choice conflictChoice
		switch {
		case fromPolicy:
			choice = conflictChoice{resolution: resolution, aborted: resolution == ConflictAbort}
			writeConflictChoice(options.ErrorOutput, choice)
			if choice.aborted {
				return conflictResolutionResult{}, errConflictAborted
			}
		case !interactive:
			return conflictResolutionResult{}, errors.New(
				"conflict resolution requires --conflict or --resolve without interactive input",
			)
		case watched != nil:
			choice, err = promptWatchedConflictResolution(options.Context, options.Input, options.ErrorOutput, *watched)
		default:
			choice, err = promptConflictResolution(options.Context, reader, options.ErrorOutput)
		}
		if err != nil {
			return conflictResolutionResult{}, err
		}

		if choice.sourcesChanged || choice.aborted {
			result.sourcesChanged, result.aborted = choice.sourcesChanged, choice.aborted
			return result, nil
		}
		for _, resource := range group.resources {
			result.resolutions[resource.ID] = choice.resolution
		}
	}
	return result, nil
}

// writeConflict shows one line that names the resources of the group, and
// then the diff of each resource that has one.
func writeConflict(output io.Writer, group conflictGroup) error {
	console := syncconsole.New(output)
	header := fmt.Sprintf("\nConflict: %s changed on both sides.", group.resources[0].ID.Label())
	if others := group.resources[1:]; len(others) != 0 {
		labels := make([]string, 0, len(others))
		for _, resource := range others {
			labels = append(labels, resource.ID.Label())
		}
		header += fmt.Sprintf(" It shares a changed tool or skill with %s, so one choice covers them all.", strings.Join(labels, ", "))
	}
	_ = console.Line(header)

	width := terminalWidth(output)
	for _, resource := range group.resources {
		if len(resource.Diff) == 0 {
			continue
		}
		if len(group.resources) > 1 {
			_ = console.Printf("\n  %s\n", resource.ID.Label())
		}
		rendered, err := renderVariationDiff(resource.Diff, outputPlaintext, width, diffPresentation(resource.Action))
		if err != nil {
			return err
		}
		_ = console.Write(rendered)
	}
	return nil
}

// groupConflicts returns one group for each conflict. A group also has every
// other variation that shares a changed attachment with it, directly or
// through another variation in the group. An error row does not join a group.
// A tool or skill that LaunchDarkly deleted does not join either, because a
// local choice would create it again. checkResolvedAttachmentDeletes decides
// that row after the choice.
func groupConflicts(plan Plan) []conflictGroup {
	var groups []conflictGroup
	visited := make([]bool, len(plan.Resources))
	for start, resource := range plan.Resources {
		if visited[start] || resource.Action != ActionConflict {
			continue
		}
		visited[start] = true
		members := []int{start}
		for next := 0; next < len(members); next++ {
			for candidate := range plan.Resources {
				row := plan.Resources[candidate]
				// A row that only conflicts need keeps its own action, unless
				// it is a conflict itself. applyConflictResolutions decides it.
				if visited[candidate] ||
					row.Action == ActionError ||
					(len(row.conflictUses) != 0 && row.Action != ActionConflict) ||
					(row.ID.Kind != syncdomain.KindVariation && row.Action == ActionDeleteLocal) ||
					!sharesChangedAttachment(plan.Resources[members[next]], row) {
					continue
				}
				visited[candidate] = true
				members = append(members, candidate)
			}
		}

		var group conflictGroup
		for _, index := range members {
			group.resources = append(group.resources, plan.Resources[index])
		}
		groups = append(groups, group)
	}
	return groups
}

// sharesChangedAttachment reports whether two variations share a changed
// attachment, so that both must use the same side.
func sharesChangedAttachment(left, right PlannedResource) bool {
	return slices.ContainsFunc(left.changedAttachments, func(id ResourceID) bool {
		return slices.Contains(right.changedAttachments, id)
	})
}

// promptConflictResolution asks the user which side wins one conflict.
func promptConflictResolution(ctx context.Context, input io.Reader, output io.Writer) (conflictChoice, error) {
	resolution, canceled, err := syncinteractive.SelectInline(
		ctx,
		input,
		output,
		"Choose how to resolve this conflict",
		[]syncinteractive.Choice[ConflictResolution]{
			{Title: "Use LaunchDarkly", Value: ConflictUseLaunchDarkly},
			{Title: "Use local", Value: ConflictUseLocal},
			{Title: "Abort and resolve manually", Value: ConflictAbort},
		},
	)
	if err != nil {
		return conflictChoice{}, err
	}
	choice := conflictChoice{resolution: resolution, aborted: canceled || resolution == ConflictAbort}
	writeConflictChoice(output, choice)
	return choice, nil
}

// promptWatchedConflictResolution asks the user which side wins, and stops
// when a watched file changes first. The plan is then out of date.
func promptWatchedConflictResolution(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	watched watchedSources,
) (conflictChoice, error) {
	promptContext, cancel := context.WithCancel(ctx)
	defer cancel()

	// The prompt and the watcher race. The first to finish cancels the
	// shared context, so that the other stops.
	changeResult := make(chan error, 1)
	go func() {
		changeResult <- watched.WaitForChange(promptContext)
		cancel()
	}()
	choice, promptErr := promptConflictResolution(promptContext, input, output)
	cancel()
	changeErr := <-changeResult

	switch {
	case changeErr == nil:
		_ = syncconsole.New(output).Line("\nA watched file changed; refreshing...")
		return conflictChoice{sourcesChanged: true}, nil
	case !errors.Is(changeErr, context.Canceled):
		return conflictChoice{}, changeErr
	case promptErr == nil && choice.aborted:
		return conflictChoice{}, context.Canceled
	case promptErr != nil && ctx.Err() != nil:
		return conflictChoice{}, context.Canceled
	case promptErr != nil:
		return conflictChoice{}, promptErr
	default:
		return choice, nil
	}
}

// writeConflictChoice confirms the choice after the form closes.
func writeConflictChoice(output io.Writer, choice conflictChoice) {
	console := syncconsole.New(output)
	switch {
	case choice.aborted:
		_ = console.Line("Sync canceled; conflict left unresolved.")
	case choice.resolution == ConflictUseLaunchDarkly:
		_ = console.Line("Using LaunchDarkly.")
	case choice.resolution == ConflictUseLocal:
		_ = console.Line("Using local.")
	}
}

// applyConflictResolutions returns a copy of the plan in which each resolved
// resource has the action for its chosen side. The original plan stays as
// the reviewed record.
func applyConflictResolutions(plan Plan, resolutions map[ResourceID]ConflictResolution) Plan {
	resolved := Plan{Resources: make([]PlannedResource, 0, len(plan.Resources))}
	for _, resource := range plan.Resources {
		if !chosenSideUses(resource, resolutions) {
			continue
		}
		if resolution, ok := resolutions[resource.ID]; ok {
			resource.Action = resolvedConflictAction(resource, resolution)
		}
		resolved.Resources = append(resolved.Resources, resource)
	}
	checkResolvedAttachmentDeletes(resolved.Resources)
	return resolved
}

// chosenSideUses reports whether a row stays after the conflict choices. A
// row that only conflicts need stays when the side that the user chose for
// one of them uses its tool or skill. Then the row keeps its own action, so
// that the variation does not write its own copy over the other side.
func chosenSideUses(resource PlannedResource, resolutions map[ResourceID]ConflictResolution) bool {
	if len(resource.conflictUses) == 0 || resource.Action == ActionConflict {
		return true
	}
	return slices.ContainsFunc(resource.conflictUses, func(use conflictUse) bool {
		resolution := resolutions[use.variation]
		return use.local && resolution == ConflictUseLocal || !use.local && resolution == ConflictUseLaunchDarkly
	})
}

// resolvedConflictAction returns the action that makes the chosen side win.
func resolvedConflictAction(resource PlannedResource, resolution ConflictResolution) Action {
	switch resolution {
	case ConflictUseLaunchDarkly:
		if !resource.hasServer() {
			return ActionDeleteLocal
		}
		return ActionUpdateLocal
	case ConflictUseLocal:
		// A conflict always has a local file, because a missing file is restored.
		if !resource.hasServer() {
			return ActionCreateServer
		}
		return ActionUpdateServer
	default:
		return ActionConflict
	}
}
