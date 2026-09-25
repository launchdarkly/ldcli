package prompt

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"time"

	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
)

type conflictResolution string

const (
	useLaunchDarkly conflictResolution = "launchdarkly"
	useLocal        conflictResolution = "local"
	abortConflict   conflictResolution = "abort"
)

type conflictResolutionResult struct {
	resolutions    map[ResourceID]conflictResolution
	aborted        bool
	sourcesChanged bool
}

type conflictChoice struct {
	resolution     conflictResolution
	aborted        bool
	sourcesChanged bool
}

// resolveConflicts shows each conflict and asks which side should win.
func resolveConflicts(
	options Options,
	plan Plan,
	reader io.Reader,
	interactive bool,
	watched *watchedSources,
) (conflictResolutionResult, error) {
	var conflicts []PlannedResource
	for _, resource := range plan.Resources {
		if resource.Action == ActionConflict {
			conflicts = append(conflicts, resource)
		}
	}
	if len(conflicts) == 0 {
		return conflictResolutionResult{}, nil
	}
	if !interactive {
		return conflictResolutionResult{}, fmt.Errorf(
			"interactive conflict resolution requires a terminal; rerun in a terminal or resolve the conflict manually",
		)
	}

	result := conflictResolutionResult{resolutions: make(map[ResourceID]conflictResolution, len(conflicts))}
	for _, resource := range conflicts {
		conflict := Plan{Resources: []PlannedResource{resource}}
		if err := writePlanReview(options.ErrorOutput, "plaintext", conflict, terminalWidth(options.ErrorOutput)); err != nil {
			return conflictResolutionResult{}, err
		}

		choice, err := readConflictChoice(options, reader, watched)
		if err != nil {
			return conflictResolutionResult{}, err
		}
		if choice.sourcesChanged {
			result.sourcesChanged = true
			return result, nil
		}
		if choice.aborted {
			result.aborted = true
			return result, nil
		}
		result.resolutions[resource.ID] = choice.resolution
	}
	return result, nil
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
func promptConflictResolution(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
) (conflictChoice, error) {
	resolution, canceled, err := syncinteractive.SelectContext(
		ctx,
		input,
		output,
		"Choose how to resolve this conflict",
		[]syncinteractive.Choice[conflictResolution]{
			{Title: "Use LaunchDarkly", Value: useLaunchDarkly},
			{Title: "Use local", Value: useLocal},
			{Title: "Abort and resolve manually", Value: abortConflict},
		},
	)
	if err != nil {
		return conflictChoice{}, err
	}
	choice := conflictChoice{
		resolution: resolution,
		aborted:    canceled || resolution == abortConflict,
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
	case choice.resolution == useLaunchDarkly:
		_ = console.Line("Using LaunchDarkly.")
	case choice.resolution == useLocal:
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

// applyConflictResolutions replaces conflict actions with the selected direction.
func applyConflictResolutions(plan Plan, resolutions map[ResourceID]conflictResolution) Plan {
	resolved := Plan{Resources: append([]PlannedResource(nil), plan.Resources...)}
	for index := range resolved.Resources {
		resource := &resolved.Resources[index]
		resolution, ok := resolutions[resource.ID]
		if !ok || resource.Action != ActionConflict {
			continue
		}
		resource.Action = resolvedConflictAction(*resource, resolution)
	}
	return resolved
}

// resolvedConflictAction returns the operation needed for the chosen side to win.
func resolvedConflictAction(resource PlannedResource, resolution conflictResolution) Action {
	switch resolution {
	case useLaunchDarkly:
		if resource.Server == nil {
			return ActionDeleteLocal
		}
		return ActionUpdateLocal
	case useLocal:
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
