package detach

import (
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// Resource identifies one local or manifested resource that can be detached.
type Resource = syncdomain.ResourceID

// Options contains the local stores and streams used by detach.
type Options struct {
	RepositoryRoot string
	Store          synclocal.Store
	Manifest       syncmanifest.Store
	Input          io.Reader
	Output         io.Writer
}

// Run lets the user select resources and removes their local sync state.
func Run(options Options) error {
	resources, manifest, manifestExists, err := loadResources(options.RepositoryRoot, options.Manifest)
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		_ = syncconsole.New(options.Output).Line("No resources are currently synced.")
		return nil
	}
	if !syncinteractive.StreamsAreTerminal(options.Input, options.Output) {
		return fmt.Errorf("interactive resource selection requires a terminal; run this command in a terminal")
	}

	choices := make([]syncinteractive.Choice[Resource], 0, len(resources))
	for _, resource := range resources {
		choices = append(choices, syncinteractive.Choice[Resource]{
			Title:       resource.ProjectKey + "/" + resource.LookupKey,
			Description: string(resource.Kind),
			Value:       resource,
		})
	}
	selected, canceled, err := syncinteractive.MultiSelect(
		options.Input,
		options.Output,
		"Select resources to detach",
		"Detached resources remain in LaunchDarkly.",
		choices,
	)
	if err != nil {
		return err
	}
	if canceled {
		return nil
	}
	if err := detachResources(options, manifest, manifestExists, selected); err != nil {
		return err
	}

	console := syncconsole.New(options.Output)
	_ = console.Line("Detached resources:")
	for _, resource := range selected {
		_ = console.Printf("- %s %s/%s\n", resource.Kind, resource.ProjectKey, resource.LookupKey)
	}
	return nil
}

// loadResources returns the union of local wrappers and manifested resources.
func loadResources(repositoryRoot string, manifestStore syncmanifest.Store) ([]Resource, syncmanifest.Manifest, bool, error) {
	manifest, manifestExists, err := manifestStore.Load()
	if err != nil {
		return nil, syncmanifest.Manifest{}, false, err
	}

	resources := make(map[Resource]struct{}, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		resources[resource.ID()] = struct{}{}
	}

	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return nil, syncmanifest.Manifest{}, false, err
	}
	for _, file := range files {
		resource, ok := resourceFromWrapperPath(file)
		if ok {
			resources[resource] = struct{}{}
		}
	}

	result := make([]Resource, 0, len(resources))
	for resource := range resources {
		result = append(result, resource)
	}
	slices.SortFunc(result, syncdomain.CompareResourceIDs)
	return result, manifest, manifestExists, nil
}

// resourceFromWrapperPath derives a variation identity without parsing its contents.
func resourceFromWrapperPath(file string) (Resource, bool) {
	parts := strings.Split(file, "/")
	if len(parts) != 5 || parts[0] != syncdomain.RootDir || parts[2] != "configs" || !strings.HasSuffix(parts[4], ".prompt.md") {
		return Resource{}, false
	}
	variationKey := strings.TrimSuffix(parts[4], ".prompt.md")
	if parts[1] == "" || parts[3] == "" || variationKey == "" {
		return Resource{}, false
	}
	return Resource{Kind: syncdomain.KindVariation, ProjectKey: parts[1], LookupKey: path.Join(parts[3], variationKey)}, true
}

// detachResources removes selected resources from the manifest before deleting local wrappers.
func detachResources(options Options, original syncmanifest.Manifest, manifestExists bool, selected []Resource) error {
	selectedSet := make(map[Resource]struct{}, len(selected))
	for _, resource := range selected {
		selectedSet[resource] = struct{}{}
	}

	updated := syncmanifest.New()
	for _, resource := range original.Resources {
		if _, detach := selectedSet[resource.ID()]; !detach {
			updated.Resources = append(updated.Resources, resource)
		}
	}
	if err := options.Manifest.Write(updated); err != nil {
		return err
	}

	var deletions []synclocal.VariationDeletion
	for _, resource := range selected {
		if resource.Kind != syncdomain.KindVariation {
			continue
		}
		configKey, variationKey, ok := strings.Cut(resource.LookupKey, "/")
		if !ok || strings.Contains(variationKey, "/") {
			continue
		}
		exists, err := options.Store.VariationExists(resource.ProjectKey, configKey, variationKey)
		if err != nil {
			return errors.Join(err, restoreManifest(options.Manifest, original, manifestExists))
		}
		if exists {
			deletions = append(deletions, synclocal.VariationDeletion{
				ProjectKey: resource.ProjectKey, ConfigKey: configKey, VariationKey: variationKey,
			})
		}
	}

	if _, err := options.Store.DeleteVariations(deletions); err != nil {
		return errors.Join(err, restoreManifest(options.Manifest, original, manifestExists))
	}
	if err := options.Store.RemoveEmptyDirectories(); err != nil {
		return err
	}
	return nil
}

// restoreManifest restores the manifest when local wrapper deletion fails.
func restoreManifest(store syncmanifest.Store, manifest syncmanifest.Manifest, existed bool) error {
	if existed {
		return store.Write(manifest)
	}
	return store.Remove()
}
