// Package detach stops syncing selected variations. It removes their local
// files and their baseline. LaunchDarkly keeps the variations.
package detach

import (
	"errors"
	"fmt"
	"io"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// ManifestStore reads and writes the sync baseline.
type ManifestStore interface {
	Load(projectKeys []string) (syncmanifest.Manifest, error)
	Update(previous, next syncmanifest.Manifest) (syncmanifest.Manifest, error)
}

// Options are the dependencies and the input of one detach.
type Options struct {
	RepositoryRoot string
	Store          synclocal.Store
	Manifest       ManifestStore
	// ProjectKeys are the projects that have local files.
	ProjectKeys []string
	Input       io.Reader
	Output      io.Writer
	// Selections are the variations to detach. If it is empty, Run asks the user.
	Selections []syncdomain.ResourceID
	NoInput    bool
}

// Run detaches the selected variations.
func Run(options Options) error {
	projectKeys := slices.Clone(options.ProjectKeys)
	for _, selection := range options.Selections {
		projectKeys = append(projectKeys, selection.ProjectKey)
	}
	slices.Sort(projectKeys)
	projectKeys = slices.Compact(projectKeys)

	synced, manifest, err := loadResources(options.RepositoryRoot, options.Manifest, projectKeys)
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
	if err := detachResources(options, manifest, selected); err != nil {
		return err
	}

	_ = console.Line("Detached resources:")
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
	return syncinteractive.MultiSelect(
		options.Input, options.Output, "Select resources to detach", "Detached resources remain in LaunchDarkly.", choices,
	)
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

// loadResources returns each variation that the manifest tracks or that has
// a local file, in identity order.
func loadResources(
	repositoryRoot string,
	manifestStore ManifestStore,
	projectKeys []string,
) ([]syncdomain.ResourceID, syncmanifest.Manifest, error) {
	manifest, err := manifestStore.Load(projectKeys)
	if err != nil {
		return nil, syncmanifest.Manifest{}, err
	}
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return nil, syncmanifest.Manifest{}, err
	}

	var synced []syncdomain.ResourceID
	for _, resource := range manifest.Resources {
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
	return slices.Compact(synced), manifest, nil
}

// detachResources removes the selected variations from the manifest, and then
// deletes their local files. If the delete fails, it restores the manifest.
func detachResources(options Options, original syncmanifest.Manifest, selected []syncdomain.ResourceID) error {
	isSelected := func(id syncdomain.ResourceID) bool { return slices.Contains(selected, id) }

	next := syncmanifest.New()
	for _, resource := range original.Resources {
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
	persisted, err := options.Manifest.Update(original, next)
	if err != nil {
		return err
	}
	restoreManifest := func(cause error) error {
		_, restoreErr := options.Manifest.Update(persisted, original)
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
			return restoreManifest(err)
		}
		if exists {
			deletions = append(deletions, synclocal.VariationDeletion{
				ProjectKey: resource.ProjectKey, ConfigKey: configKey, VariationKey: variationKey,
			})
		}
	}
	if _, err := options.Store.DeleteVariations(deletions); err != nil {
		return restoreManifest(err)
	}
	return nil
}
