package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type conflictResolution string

const (
	useLaunchDarkly          conflictResolution = "launchdarkly"
	useLocal                 conflictResolution = "local"
	conflictResolutionPrompt                    = `
Choose how to resolve this conflict:
  1. Use LaunchDarkly
  2. Use local
  3. Abort and resolve manually
Choice [1-3]: `
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
	reader *bufio.Reader,
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
func readConflictChoice(options Options, reader *bufio.Reader, watched *watchedSources) (conflictChoice, error) {
	if watched != nil {
		return promptWatchedConflictResolution(options.Context, options.Input, options.ErrorOutput, *watched)
	}
	resolution, aborted, err := promptConflictResolution(reader, options.ErrorOutput)
	return conflictChoice{resolution: resolution, aborted: aborted}, err
}

// promptConflictResolution reads one LaunchDarkly, local, or abort choice.
func promptConflictResolution(reader *bufio.Reader, output io.Writer) (conflictResolution, bool, error) {
	if _, err := fmt.Fprint(output, conflictResolutionPrompt); err != nil {
		return "", false, err
	}

	for {
		answer, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", false, fmt.Errorf("read conflict resolution: %w", err)
		}
		if err == io.EOF && strings.TrimSpace(answer) == "" {
			return "", false, fmt.Errorf("read conflict resolution: input closed")
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "" {
			continue
		}

		switch answer {
		case "1", "launchdarkly", "server":
			_, _ = fmt.Fprintln(output, "Using LaunchDarkly.")
			return useLaunchDarkly, false, nil
		case "2", "local":
			_, _ = fmt.Fprintln(output, "Using local.")
			return useLocal, false, nil
		case "3", "abort":
			_, _ = fmt.Fprintln(output, "Sync canceled; conflict left unresolved.")
			return "", true, nil
		default:
			_, _ = fmt.Fprint(output, "Enter 1, 2, or 3.\nChoice [1-3]: ")
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
