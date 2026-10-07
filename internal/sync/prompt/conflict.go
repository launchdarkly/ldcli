package prompt

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
)

// ConflictResolution identifies which side should win a sync conflict.
type ConflictResolution string

const (
	// ConflictUseLaunchDarkly applies the current LaunchDarkly resource locally.
	ConflictUseLaunchDarkly ConflictResolution = "launchdarkly"
	// ConflictUseLocal applies the current local resource to LaunchDarkly.
	ConflictUseLocal ConflictResolution = "local"
	// ConflictAbort stops before applying the conflicted plan.
	ConflictAbort ConflictResolution = "abort"
)

// ConflictPolicy supplies a default resolution and per-variation exceptions.
type ConflictPolicy struct {
	Default   ConflictResolution
	Overrides map[ResourceID]ConflictResolution
}

// ParseConflictResolution validates a CLI conflict choice.
func ParseConflictResolution(value string) (ConflictResolution, error) {
	resolution := ConflictResolution(value)
	switch resolution {
	case ConflictUseLaunchDarkly, ConflictUseLocal, ConflictAbort:
		return resolution, nil
	default:
		return "", fmt.Errorf("invalid conflict resolution %q; expected launchdarkly, local, or abort", value)
	}
}

type conflictResolutionResult struct {
	resolutions    map[ResourceID]ConflictResolution
	aborted        bool
	sourcesChanged bool
}

var errConflictAborted = errors.New("sync conflict left unresolved")

type conflictChoice struct {
	resolution     ConflictResolution
	aborted        bool
	sourcesChanged bool
}

type conflictGroup struct {
	resources  []PlannedResource
	attachment bool
}

// resolveConflicts shows each conflict and asks which side should win.
func resolveConflicts(
	options Options,
	plan Plan,
	reader io.Reader,
	interactive bool,
	watched *watchedSources,
) (conflictResolutionResult, error) {
	conflicts := groupConflicts(plan)
	if len(conflicts) == 0 {
		return conflictResolutionResult{}, nil
	}

	result := conflictResolutionResult{resolutions: make(map[ResourceID]ConflictResolution)}
	for _, group := range conflicts {
		visible := make([]PlannedResource, 0, len(group.resources))
		for _, resource := range group.resources {
			if len(resource.Diff) != 0 {
				visible = append(visible, resource)
			}
		}
		if len(visible) == 0 {
			visible = append(visible, group.resources[0])
		}
		conflict := Plan{Resources: visible}
		if err := writePlanReview(options.ErrorOutput, "plaintext", conflict, terminalWidth(options.ErrorOutput)); err != nil {
			return conflictResolutionResult{}, err
		}
		if group.attachment && len(group.resources) > 1 {
			console := syncconsole.New(options.ErrorOutput)
			_ = console.Printf("This attachment conflict affects %d variations:\n", len(group.resources))
			for _, resource := range group.resources {
				_ = console.Printf("- %s/%s\n", resource.ID.ProjectKey, resource.ID.LookupKey)
			}
		}

		resolution, explicit, err := options.ConflictPolicy.resolve(group)
		if err != nil {
			return conflictResolutionResult{}, err
		}
		var choice conflictChoice
		if explicit {
			choice = conflictChoice{
				resolution: resolution,
				aborted:    resolution == ConflictAbort,
			}
			writeConflictChoice(options.ErrorOutput, choice)
			if choice.aborted {
				return conflictResolutionResult{}, errConflictAborted
			}
		} else {
			if !interactive {
				return conflictResolutionResult{}, fmt.Errorf(
					"conflict resolution requires --conflict or --resolve without interactive input",
				)
			}
			choice, err = readConflictChoice(options, reader, watched)
			if err != nil {
				return conflictResolutionResult{}, err
			}
		}
		if choice.sourcesChanged {
			result.sourcesChanged = true
			return result, nil
		}
		if choice.aborted {
			result.aborted = true
			return result, nil
		}
		for _, resource := range group.resources {
			result.resolutions[resource.ID] = choice.resolution
		}
	}
	return result, nil
}

func (policy ConflictPolicy) resolve(group conflictGroup) (ConflictResolution, bool, error) {
	var selected ConflictResolution
	for _, resource := range group.resources {
		resolution, ok := policy.Overrides[resource.ID]
		if !ok {
			continue
		}
		if _, err := ParseConflictResolution(string(resolution)); err != nil {
			return "", false, err
		}
		if selected != "" && selected != resolution {
			return "", false, fmt.Errorf("conflicting --resolve choices affect the same shared attachment")
		}
		selected = resolution
	}
	if selected != "" {
		return selected, true, nil
	}
	if policy.Default == "" {
		return "", false, nil
	}
	resolution, err := ParseConflictResolution(string(policy.Default))
	return resolution, err == nil, err
}

// groupConflicts presents one choice for each connected set of variations that
// share a changed dependency, including conflicts with other local edits.
func groupConflicts(plan Plan) []conflictGroup {
	var groups []conflictGroup
	visited := make([]bool, len(plan.Resources))
	for start := range plan.Resources {
		if visited[start] || plan.Resources[start].Action != ActionConflict {
			continue
		}
		visited[start] = true
		indices := []int{start}
		for next := 0; next < len(indices); next++ {
			for candidate := range plan.Resources {
				if visited[candidate] ||
					plan.Resources[candidate].Action == ActionError ||
					!sharesChangedAttachment(plan.Resources[indices[next]], plan.Resources[candidate]) {
					continue
				}
				visited[candidate] = true
				indices = append(indices, candidate)
			}
		}

		group := conflictGroup{attachment: len(indices) > 1}
		for _, index := range indices {
			group.resources = append(group.resources, plan.Resources[index])
		}
		groups = append(groups, group)
	}
	return groups
}

// sharesChangedAttachment identifies variations that must resolve their shared
// dependency in the same direction.
func sharesChangedAttachment(left, right PlannedResource) bool {
	for _, leftID := range left.changedAttachments {
		if slices.Contains(right.changedAttachments, leftID) {
			return true
		}
	}
	return false
}

// readConflictChoice waits for a regular terminal choice or a watch-aware
// choice that can be interrupted by another source change.
func readConflictChoice(options Options, reader io.Reader, watched *watchedSources) (conflictChoice, error) {
	if watched != nil {
		return promptWatchedConflictResolution(options.Context, options.Input, options.ErrorOutput, *watched)
	}
	return promptConflictResolution(context.Background(), reader, options.ErrorOutput)
}

// promptConflictResolution asks which side should win one conflict.
func promptConflictResolution(ctx context.Context, input io.Reader, output io.Writer) (conflictChoice, error) {
	resolution, canceled, err := syncinteractive.SelectContext(
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
	choice := conflictChoice{
		resolution: resolution,
		aborted:    canceled || resolution == ConflictAbort,
	}
	writeConflictChoice(output, choice)
	return choice, nil
}

// promptWatchedConflictResolution refreshes the plan if a source changes
// while the conflict selector is open.
func promptWatchedConflictResolution(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	watched watchedSources,
) (conflictChoice, error) {
	promptContext, cancel := context.WithCancel(ctx)
	defer cancel()

	changeResult := make(chan error, 1)
	// Race the form against filesystem changes. Canceling the shared child
	// context guarantees exactly one path wins and the other exits promptly.
	go func() {
		changeResult <- watched.WaitForChange(promptContext)
		cancel()
	}()

	choice, promptErr := promptConflictResolution(promptContext, input, output)
	cancel()
	changeErr := <-changeResult
	if changeErr == nil {
		_ = syncconsole.New(output).Line("\nA watched file changed; refreshing...")
		return conflictChoice{sourcesChanged: true}, nil
	}
	if !errors.Is(changeErr, context.Canceled) {
		return conflictChoice{}, changeErr
	}
	if promptErr == nil && choice.aborted {
		return conflictChoice{}, context.Canceled
	}
	if promptErr != nil {
		if ctx.Err() != nil {
			return conflictChoice{}, context.Canceled
		}
		return conflictChoice{}, promptErr
	}
	return choice, nil
}

// writeConflictChoice confirms the selected resolution after the form exits.
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

type watchedSources struct {
	watcher  *sourceWatcher
	snapshot [sha256.Size]byte
	debounce time.Duration
}

// WaitForChange waits until the watched source content differs from the state
// used to build the current plan.
func (watched watchedSources) WaitForChange(ctx context.Context) error {
	for {
		if err := watched.watcher.WaitForChange(ctx, watched.debounce); err != nil {
			return err
		}
		current, err := sourceSnapshot(watched.watcher.root)
		if err != nil {
			return err
		}
		if current != watched.snapshot {
			return nil
		}
	}
}

// applyConflictResolutions applies one direction to every resource in each
// conflict group, including non-conflicted consumers of a shared attachment.
func applyConflictResolutions(plan Plan, resolutions map[ResourceID]ConflictResolution) Plan {
	// Clone the resource slice before changing actions so the reviewed plan
	// remains an immutable record for post-review revalidation.
	resolved := Plan{Resources: append([]PlannedResource(nil), plan.Resources...)}
	for index := range resolved.Resources {
		resource := &resolved.Resources[index]
		resolution, ok := resolutions[resource.ID]
		if !ok {
			continue
		}
		resource.Action = resolvedConflictAction(*resource, resolution)
	}
	return resolved
}

// resolvedConflictAction returns the operation needed for the chosen side to win.
func resolvedConflictAction(resource PlannedResource, resolution ConflictResolution) Action {
	switch resolution {
	case ConflictUseLaunchDarkly:
		if resource.Server == nil {
			return ActionDeleteLocal
		}
		return ActionUpdateLocal
	case ConflictUseLocal:
		if resource.Local == nil {
			return ActionArchiveServer
		}
		if resource.Server == nil {
			return ActionCreateServer
		}
		return ActionUpdateServer
	default:
		return ActionConflict
	}
}
