package prompt

import (
	"errors"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

// runWorkspaceSync runs one sync in five steps:
//
//  1. Read the manifest, the local files, and LaunchDarkly, and build a plan.
//  2. Ask the user to resolve each conflict.
//  3. Show the plan and ask the user to apply it.
//  4. Read the state again, and stop if it changed after the review.
//  5. Apply the plan, record the new baseline, and report the results.
//
// In watch mode, watcher is not nil. A file change during the review returns
// errRefreshWatchPlan, so that the watch loop builds a new plan.
func (runner Runner) runWorkspaceSync(options Options, workspace syncWorkspace, watcher *sourceWatcher) error {
	options = options.withDefaults()
	client := runner.api(options)

	var watched *watchedSources
	if watcher != nil {
		snapshot, err := sourceSnapshot(workspace.root)
		if err != nil {
			return err
		}
		watched = &watchedSources{watcher: watcher, snapshot: snapshot, debounce: watchDebounce}
	}

	reviewed, err := workspace.loadState(client)
	if err != nil {
		return err
	}
	if options.dryRun() {
		if err := writePlanOutput(options.Output, options.OutputKind, reviewed.plan); err != nil {
			return err
		}
		return reviewed.plan.BlockingError()
	}

	interactive := runner.interactive(options)
	conflicts, err := resolveConflicts(options, reviewed.plan, options.Input, interactive, watched)
	switch {
	case err != nil:
		return err
	case conflicts.sourcesChanged:
		return errRefreshWatchPlan
	case conflicts.aborted:
		return nil
	}

	resolved := applyConflictResolutions(reviewed.plan, conflicts.resolutions)
	proceed, err := reviewAndConfirmPlan(options, resolved, interactive)
	if err != nil {
		return err
	}
	if !proceed {
		if !resolved.HasChanges() {
			return cleanupOrphanedAttachments(options, workspace.local, interactive)
		}
		return nil
	}

	// Read both sides again, so that no action uses state from before the review.
	current, err := workspace.loadState(client)
	if err != nil {
		return err
	}
	if err := checkUnchanged(options, reviewed, current); err != nil {
		return err
	}
	return runner.applyPlan(options, workspace, client, current, conflicts.resolutions, interactive)
}

// checkUnchanged returns an error if the state changed after the review.
func checkUnchanged(options Options, reviewed, current workspaceState) error {
	if slices.Equal(reviewed.projectKeys, current.projectKeys) && samePlanState(reviewed.plan, current.plan) {
		return nil
	}
	if options.watching() {
		return errRefreshWatchPlan
	}
	if !slices.Equal(reviewed.projectKeys, current.projectKeys) {
		return errors.New("sync projects changed after review; run sync again")
	}
	return errors.New("sync state changed after review; run sync again")
}

// applyPlan applies the conflict choices to the current plan, executes it,
// and records the new baseline. It removes unused attachment files only when
// every step succeeds.
func (runner Runner) applyPlan(
	options Options,
	workspace syncWorkspace,
	client syncapi.Client,
	current workspaceState,
	resolutions map[syncdomain.ResourceID]ConflictResolution,
	interactive bool,
) error {
	plan := applyConflictResolutions(current.plan, resolutions)
	outcomes, next, err := executePlan(workspace.root, workspace.local, client, current.manifest, plan, current.localFiles)

	failures := []error{err}
	if _, err := workspace.manifest.Update(current.manifest, next); err != nil {
		failures = append(failures, err)
	}
	if err := workspace.local.RemoveEmptyDirectories(); err != nil {
		failures = append(failures, err)
	}
	if err := writeOutcomeOutput(options.Output, options.OutputKind, outcomes); err != nil {
		failures = append(failures, err)
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	return cleanupOrphanedAttachments(options, workspace.local, interactive)
}
