package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

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
	resources  []PlannedResource
	attachment bool
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

// writeConflict shows the diff of each resource in a group that has one.
func writeConflict(output io.Writer, group conflictGroup) error {
	visible := slices.DeleteFunc(slices.Clone(group.resources), func(resource PlannedResource) bool {
		return len(resource.Diff) == 0
	})
	if len(visible) == 0 {
		visible = group.resources[:1]
	}
	if err := writePlanReview(output, "plaintext", Plan{Resources: visible}, terminalWidth(output)); err != nil {
		return err
	}
	if group.attachment && len(group.resources) > 1 {
		console := syncconsole.New(output)
		_ = console.Printf("This attachment conflict affects %d variations:\n", len(group.resources))
		for _, resource := range group.resources {
			_ = console.Printf("- %s\n", resource.ID)
		}
	}
	return nil
}

// groupConflicts returns one group for each conflict. A group also has every
// other variation that shares a changed attachment with it, directly or
// through another variation in the group.
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
				if visited[candidate] ||
					plan.Resources[candidate].Action == ActionError ||
					!sharesChangedAttachment(plan.Resources[members[next]], plan.Resources[candidate]) {
					continue
				}
				visited[candidate] = true
				members = append(members, candidate)
			}
		}

		group := conflictGroup{attachment: len(members) > 1}
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
	resolved := Plan{Resources: slices.Clone(plan.Resources)}
	for index := range resolved.Resources {
		resource := &resolved.Resources[index]
		if resolution, ok := resolutions[resource.ID]; ok {
			resource.Action = resolvedConflictAction(*resource, resolution)
		}
	}
	return resolved
}

// resolvedConflictAction returns the action that makes the chosen side win.
func resolvedConflictAction(resource PlannedResource, resolution ConflictResolution) Action {
	switch resolution {
	case ConflictUseLaunchDarkly:
		if resource.Server == nil {
			return ActionDeleteLocal
		}
		return ActionUpdateLocal
	case ConflictUseLocal:
		// A conflict always has a local file, because a missing file is restored.
		if resource.Server == nil {
			return ActionCreateServer
		}
		return ActionUpdateServer
	default:
		return ActionConflict
	}
}
