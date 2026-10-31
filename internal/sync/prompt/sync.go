package prompt

import (
	"bytes"
	"errors"
	"io"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
)

// runWorkspaceSync runs one sync in five steps:
//
//  1. Read the baseline, the local files, and LaunchDarkly, and build a plan.
//  2. Ask the user to resolve each conflict.
//  3. Show the plan and ask the user to apply it.
//  4. Read the state again, and stop if it changed after the review.
//  5. Apply the plan, record the new baseline, and report the results.
//
// In watch mode, watcher is not nil. A file change during the review returns
// errRefreshWatchPlan, so that the watch loop builds a new plan.
func (runner Runner) runWorkspaceSync(options Options, workspace syncWorkspace, watcher *sourceWatcher) error {
	return runner.runWorkspaceSyncWithSource(options, workspace, watcher, nil)
}

func (runner Runner) runWorkspaceSyncWithSource(
	options Options,
	workspace syncWorkspace,
	watcher *sourceWatcher,
	prepared *synclink.Prepared,
) error {
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

	target := options.syncAction()
	var reviewed workspaceState
	var err error
	if prepared == nil {
		reviewed, err = workspace.loadState(client, options.Scope, target.Filter)
	} else {
		reviewed, err = workspace.loadStateWithSource(
			client, options.Scope, target.Filter, &prepared.Definition,
		)
	}
	if err != nil {
		return err
	}
	if link, ok := options.Action.(SourceLinkAction); ok {
		allowReviewedSourceChange(workspace.root, &reviewed.plan, link.ID)
	}
	if prepared != nil {
		reviewed.plan.ForceConfirmation = true
		writeDescriptorPreview(options.ErrorOutput, *prepared)
	}
	if options.dryRun() {
		writePlanNotes(options.ErrorOutput, reviewed.plan)
		if err := writePlanOutput(options.Output, options.OutputKind, reviewed.plan, options.Diff); err != nil {
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
		return nil
	}
	if prepared != nil {
		if _, err := prepared.Write(workspace.local); err != nil {
			return err
		}
		_ = syncconsole.New(options.Output).Printf("Linked %s.\n", prepared.Path)
	}

	// Read both sides again, so that no action uses state from before the review.
	current, err := workspace.loadState(client, options.Scope, target.Filter)
	if err != nil {
		return err
	}
	if link, ok := options.Action.(SourceLinkAction); ok {
		allowReviewedSourceChange(workspace.root, &current.plan, link.ID)
	}
	if err := checkUnchanged(options, reviewed, current); err != nil {
		return err
	}
	_, err = runner.applyPlan(options, workspace, client, current, conflicts.resolutions)
	return err
}

func writeDescriptorPreview(output io.Writer, prepared synclink.Prepared) {
	console := syncconsole.New(output)
	if len(prepared.Previous) != 0 && !bytes.Equal(prepared.Previous, prepared.Content) {
		_ = console.Printf("Current descriptor %s:\n%s\n", prepared.Path, prepared.Previous)
	}
	_ = console.Printf("Proposed descriptor %s:\n%s\n", prepared.Path, prepared.Content)
	var dependencies []string
	if prepared.Definition.Variation != nil {
		dependencies = prepared.Definition.Variation.Dependencies
	}
	if prepared.Definition.Attachment != nil {
		dependencies = prepared.Definition.Attachment.Dependencies
	}
	for _, dependency := range dependencies {
		_ = console.Printf("Mapped dependency: %s\n", dependency)
	}
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
// records the new baseline, and returns the outcome of each row.
func (runner Runner) applyPlan(
	options Options,
	workspace syncWorkspace,
	client syncapi.Client,
	current workspaceState,
	resolutions map[syncdomain.ResourceID]ConflictResolution,
) ([]ResourceOutcome, error) {
	plan := applyConflictResolutions(current.plan, resolutions)
	outcomes, next, err := executePlan(workspace.root, workspace.local, client, current.baseline.Lock, plan, current.localFiles)

	failures := []error{err}
	if _, err := workspace.baselines.Save(current.baseline, next); err != nil {
		failures = append(failures, err)
	}
	if err := workspace.local.RemoveEmptyDirectories(); err != nil {
		failures = append(failures, err)
	}
	if err := writeOutcomeOutput(options.Output, options.OutputKind, plan, outcomes); err != nil {
		failures = append(failures, err)
	}
	writeRepinNotes(options.ErrorOutput, plan, outcomes)
	return outcomes, errors.Join(failures...)
}
