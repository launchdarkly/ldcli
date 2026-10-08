// Package detach stops syncing selected variations. It removes their local
// files and their baseline. LaunchDarkly keeps the variations, unless the
// user asks to archive them.
package detach

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

const archiveQuestion = "\nArchive these variations in LaunchDarkly? [y/N] "

// Archiver reads and archives a variation in LaunchDarkly.
type Archiver interface {
	ReadVariation(projectKey, configKey, variationKey string) (syncapi.VariationState, error)
	ArchiveVariation(projectKey, configKey, variationKey string) error
}

// Options are the dependencies and the input of one detach.
type Options struct {
	RepositoryRoot string
	Store          synclocal.Store
	Baselines      syncmanifest.Baselines
	// ProjectKeys are the projects that have local files.
	ProjectKeys []string
	Input       io.Reader
	Output      io.Writer
	// Selections are the variations to detach. If it is empty, Run asks the user.
	Selections []syncdomain.ResourceID
	NoInput    bool
	// Archive also archives each selected variation in LaunchDarkly. Without
	// Yes, the user must confirm the archive.
	Archive  bool
	Archiver Archiver
	Yes      bool
	// Context stops the archive confirmation and the archive when it ends.
	Context context.Context

	// isTerminal reports whether the streams are a terminal. Tests replace it.
	isTerminal func(io.Reader, io.Writer) bool
}

// context returns the context of the command, or a context that never ends.
func (options Options) context() context.Context {
	if options.Context == nil {
		return context.Background()
	}
	return options.Context
}

// Run detaches the selected variations.
func Run(options Options) error {
	projectKeys := slices.Clone(options.ProjectKeys)
	for _, selection := range options.Selections {
		projectKeys = append(projectKeys, selection.ProjectKey)
	}
	slices.Sort(projectKeys)
	projectKeys = slices.Compact(projectKeys)

	synced, baseline, err := loadResources(options.RepositoryRoot, options.Baselines, projectKeys)
	if err != nil {
		return err
	}
	// Check a named selector before the empty-workspace message, so that a
	// selector that is not synced always fails.
	console := syncconsole.New(options.Output)
	selected := options.Selections
	if len(selected) != 0 {
		if err := validateSelections(synced, selected); err != nil {
			return err
		}
	} else {
		if len(synced) == 0 {
			_ = console.Line("No resources are currently synced.")
			return nil
		}
		var canceled bool
		if selected, canceled, err = promptForResources(options, synced); err != nil || canceled {
			return err
		}
	}
	if options.Archive {
		confirmed, err := confirmArchive(options, selected)
		if err != nil || !confirmed {
			return err
		}
		// Archive first. If a later step fails, the same command can run again,
		// because it skips a variation that is already archived.
		if err := archiveVariations(options.context(), options.Archiver, selected); err != nil {
			return err
		}
	}
	if err := detachResources(options, baseline, selected); err != nil {
		return err
	}

	if options.Archive {
		_ = console.Line("Detached and archived resources:")
	} else {
		_ = console.Line("Detached resources:")
	}
	for _, resource := range selected {
		_ = console.Printf("- %s %s\n", resource.Kind, resource)
	}
	return nil
}

// promptForResources asks the user to choose one or more synced variations.
// The bool result is true when the user cancels.
func promptForResources(options Options, synced []syncdomain.ResourceID) ([]syncdomain.ResourceID, bool, error) {
	if err := syncinteractive.RequireTerminal(
		options.Input, options.Output, options.NoInput, "variation selectors", "resource selection",
	); err != nil {
		return nil, false, err
	}
	choices := make([]syncinteractive.Choice[syncdomain.ResourceID], 0, len(synced))
	for _, resource := range synced {
		choices = append(choices, syncinteractive.Choice[syncdomain.ResourceID]{
			Title: resource.String(), Description: string(resource.Kind), Value: resource,
		})
	}
	description := "Detached resources remain in LaunchDarkly."
	if options.Archive {
		description = "Detached resources are also archived in LaunchDarkly."
	}
	return syncinteractive.MultiSelect(options.Input, options.Output, "Select resources to detach", description, choices)
}

// confirmArchive asks the user to agree to the archive, unless Yes is set.
func confirmArchive(options Options, selected []syncdomain.ResourceID) (bool, error) {
	if options.Yes {
		return true, nil
	}
	console := syncconsole.New(options.Output)
	_ = console.Line("Variations to archive in LaunchDarkly:")
	for _, resource := range selected {
		_ = console.Printf("- %s\n", resource)
	}
	isTerminal := options.isTerminal
	if isTerminal == nil {
		isTerminal = syncinteractive.StreamsAreTerminal
	}
	interactive := !options.NoInput && isTerminal(options.Input, options.Output)
	confirmed, err := syncinteractive.Confirm(options.context(), options.Input, options.Output, interactive, archiveQuestion)
	if err == nil && !confirmed {
		_ = console.Line("Detach canceled.")
	}
	return confirmed, err
}

// archiveVariations archives each variation that LaunchDarkly still has. It
// reads each variation first, because the API rejects the archive of an
// archived variation. A variation that is already archived or gone does not
// fail, so the same command can run again after a later step fails. It stops
// before the next archive when ctx ends.
func archiveVariations(ctx context.Context, archiver Archiver, selected []syncdomain.ResourceID) error {
	for _, resource := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		configKey, variationKey, err := resource.VariationKeys()
		if err != nil {
			return err
		}
		// ReadVariation treats an archived variation as absent.
		state, err := archiver.ReadVariation(resource.ProjectKey, configKey, variationKey)
		switch {
		case syncapi.IsNotFound(err) || err == nil && !state.Exists:
			continue
		case err != nil:
			return fmt.Errorf("read variation %s: %w", resource, err)
		}
		if err := archiver.ArchiveVariation(resource.ProjectKey, configKey, variationKey); err != nil && !syncapi.IsNotFound(err) {
			return fmt.Errorf("archive variation %s: %w", resource, err)
		}
	}
	return nil
}

func validateSelections(synced, selected []syncdomain.ResourceID) error {
	seen := make(map[syncdomain.ResourceID]struct{}, len(selected))
	for _, selection := range selected {
		if _, duplicate := seen[selection]; duplicate {
			return fmt.Errorf("variation %s was selected more than once", selection)
		}
		seen[selection] = struct{}{}
		if !slices.Contains(synced, selection) {
			return fmt.Errorf("variation %s is not synced", selection)
		}
	}
	return nil
}

// loadResources returns each variation that the lock tracks or that has a
// local file, in identity order.
func loadResources(
	repositoryRoot string,
	baselines syncmanifest.Baselines,
	projectKeys []string,
) ([]syncdomain.ResourceID, syncmanifest.Baseline, error) {
	baseline, err := baselines.Load(projectKeys)
	if err != nil {
		return nil, syncmanifest.Baseline{}, err
	}
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return nil, syncmanifest.Baseline{}, err
	}

	var synced []syncdomain.ResourceID
	for _, resource := range baseline.Lock.Resources {
		if resource.ResourceKind == syncdomain.KindVariation {
			synced = append(synced, resource.ID())
		}
	}
	for _, file := range files {
		if id, ok := synclocal.ParseManagedPath(file); ok && id.Kind == syncdomain.KindVariation {
			synced = append(synced, id)
		}
	}
	slices.SortFunc(synced, syncdomain.CompareResourceIDs)
	return slices.Compact(synced), baseline, nil
}

// detachResources removes the selected variations from the baseline, and then
// deletes their local files. If the delete fails, it restores the baseline.
func detachResources(options Options, original syncmanifest.Baseline, selected []syncdomain.ResourceID) error {
	isSelected := func(id syncdomain.ResourceID) bool { return slices.Contains(selected, id) }

	next := syncmanifest.New()
	for _, resource := range original.Lock.Resources {
		if !isSelected(resource.ID()) {
			next.Resources = append(next.Resources, resource)
		}
	}
	// Stop tracking each tool and skill that only the detached variations use.
	// If the workspace does not compile, keep every attachment baseline.
	if variations, err := synclocal.CompileWorkspace(options.RepositoryRoot); err == nil {
		remaining := slices.DeleteFunc(variations, func(variation syncdomain.SyncedResource) bool {
			return isSelected(variation.ID())
		})
		next.RemoveUnusedAttachments(remaining)
	}
	saved, err := options.Baselines.Save(original, next)
	if err != nil {
		return err
	}
	restoreBaseline := func(cause error) error {
		_, restoreErr := options.Baselines.Save(saved, original.Lock)
		return errors.Join(cause, restoreErr)
	}

	var deletions []synclocal.VariationDeletion
	for _, resource := range selected {
		configKey, variationKey, err := resource.VariationKeys()
		if err != nil {
			continue
		}
		exists, err := options.Store.VariationExists(resource.ProjectKey, configKey, variationKey)
		if err != nil {
			return restoreBaseline(err)
		}
		if exists {
			deletions = append(deletions, synclocal.VariationDeletion{
				ProjectKey: resource.ProjectKey, ConfigKey: configKey, VariationKey: variationKey,
			})
		}
	}
	if _, err := options.Store.DeleteVariations(deletions); err != nil {
		return restoreBaseline(err)
	}
	return nil
}
