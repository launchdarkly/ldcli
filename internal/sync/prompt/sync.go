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
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
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
	return runner.runWorkspaceSyncWithSource(options, workspace, watcher, nil, linkIntent{})
}

func (runner Runner) runWorkspaceSyncWithSource(
	options Options,
	workspace syncWorkspace,
	watcher *sourceWatcher,
	prepared *synclink.Prepared,
	intent linkIntent,
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
	var link *sourceLink
	if prepared != nil {
		link = &sourceLink{id: prepared.ID, proposal: &prepared.Definition, intent: intent}
	}
	reviewed, err := workspace.loadStateWithSource(client, options.Scope, target.Filter, link)
	if err != nil {
		return err
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
	// A conflict that LaunchDarkly wins becomes a pull now, so the source
	// publication checks run on it before the review, as they did on the
	// pulls of the unresolved plan.
	checkSourcePublications(workspace.root, resolved.Resources)
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
		_ = syncconsole.New(options.ErrorOutput).Printf("Linked %s.\n", prepared.Path)
	}

	// Read both sides again, so that no action uses state from before the
	// review. The descriptor exists now, so the link has no proposal.
	if link != nil {
		link = &sourceLink{id: link.id, intent: intent}
	}
	current, err := workspace.loadStateWithSource(client, options.Scope, target.Filter, link)
	if err == nil {
		err = checkUnchanged(options, reviewed, current)
	}
	if err != nil {
		// The lock does not record the new descriptor, so the next sync
		// would report a stale binding. Put the previous descriptor back.
		if prepared != nil {
			err = errors.Join(err, prepared.Rollback(workspace.local))
		}
		return err
	}
	outcomes, err := runner.applyPlan(options, workspace, client, current, conflicts.resolutions)
	if err != nil && prepared != nil && !lockRecordsLink(workspace, *prepared) && !outcomeSucceeded(outcomes, prepared.ID) {
		// The apply did not sync the linked resource and did not record the
		// new descriptor, so the next sync would report a stale binding. Put
		// the previous descriptor back. After a sync of the resource, the
		// descriptor stays, because LaunchDarkly or the source has the new
		// content, even when a later step such as the lock write failed.
		err = errors.Join(err, prepared.Rollback(workspace.local))
	}
	return err
}

// outcomeSucceeded reports whether the apply succeeded for a resource.
func outcomeSucceeded(outcomes []ResourceOutcome, id syncdomain.ResourceID) bool {
	return slices.ContainsFunc(outcomes, func(outcome ResourceOutcome) bool {
		return outcome.ID == id && outcome.Status == OutcomeSucceeded
	})
}

// lockRecordsLink reports whether the lock records the descriptor that a
// link wrote. An apply records it once the linked resource syncs, and then
// the descriptor stays even when another row of the plan failed.
func lockRecordsLink(workspace syncWorkspace, prepared synclink.Prepared) bool {
	lock, err := syncmanifest.ReadLock(workspace.local)
	if err != nil {
		return false
	}
	digest, err := syncdomain.ReferenceDigest(prepared.Reference)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(lock.Resources, func(resource syncmanifest.Resource) bool {
		return resource.ID() == prepared.ID && resource.ReferenceDigest == digest
	})
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
