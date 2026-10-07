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

type ManifestStore interface {
	Load(projectKeys []string) (syncmanifest.Manifest, error)
	Update(previous, next syncmanifest.Manifest) (syncmanifest.Manifest, error)
}

// Options contains the local stores and streams used by detach.
type Options struct {
	RepositoryRoot string
	Store          synclocal.Store
	Manifest       ManifestStore
	ProjectKeys    []string
	Input          io.Reader
	Output         io.Writer
	Selections     []Resource
	NoInput        bool
}

// Run resolves selected resources and removes their local sync state.
func Run(options Options) error {
	projectKeys := append([]string(nil), options.ProjectKeys...)
	for _, selection := range options.Selections {
		projectKeys = append(projectKeys, selection.ProjectKey)
	}
	slices.Sort(projectKeys)
	projectKeys = slices.Compact(projectKeys)

	resources, manifest, err := loadResources(options.RepositoryRoot, options.Manifest, projectKeys)
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		_ = syncconsole.New(options.Output).Line("No resources are currently synced.")
		return nil
	}

	selected := options.Selections
	if len(selected) == 0 {
		if options.NoInput {
			return fmt.Errorf("variation selectors are required with --no-input")
		}
		if !syncinteractive.StreamsAreTerminal(options.Input, options.Output) {
			return fmt.Errorf("interactive resource selection requires a terminal; use selectors with --no-input")
		}
		choices := make([]syncinteractive.Choice[Resource], 0, len(resources))
		for _, resource := range resources {
			choices = append(choices, syncinteractive.Choice[Resource]{
				Title:       resource.ProjectKey + "/" + resource.LookupKey,
				Description: string(resource.Kind),
				Value:       resource,
			})
		}
		var canceled bool
		selected, canceled, err = syncinteractive.MultiSelect(
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
	} else if err := validateSelections(resources, selected); err != nil {
		return err
	}
	if err := detachResources(options, manifest, selected); err != nil {
		return err
	}

	console := syncconsole.New(options.Output)
	_ = console.Line("Detached resources:")
	for _, resource := range selected {
		_ = console.Printf("- %s %s/%s\n", resource.Kind, resource.ProjectKey, resource.LookupKey)
	}
	return nil
}

func validateSelections(available, selected []Resource) error {
	seen := make(map[Resource]struct{}, len(selected))
	for _, selection := range selected {
		if _, duplicate := seen[selection]; duplicate {
			return fmt.Errorf("variation %s/%s was selected more than once", selection.ProjectKey, selection.LookupKey)
		}
		seen[selection] = struct{}{}
		if !slices.Contains(available, selection) {
			return fmt.Errorf("variation %s/%s is not synced", selection.ProjectKey, selection.LookupKey)
		}
	}
	return nil
}

// loadResources returns the union of local wrappers and manifested resources.
func loadResources(
	repositoryRoot string,
	manifestStore ManifestStore,
	projectKeys []string,
) ([]Resource, syncmanifest.Manifest, error) {
	manifest, err := manifestStore.Load(projectKeys)
	if err != nil {
		return nil, syncmanifest.Manifest{}, err
	}

	resources := make(map[Resource]struct{}, len(manifest.Resources))
	for _, resource := range manifest.Resources {
		if resource.ResourceKind == syncdomain.KindVariation {
			resources[resource.ID()] = struct{}{}
		}
	}

	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return nil, syncmanifest.Manifest{}, err
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
	return result, manifest, nil
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
func detachResources(options Options, original syncmanifest.Manifest, selected []Resource) error {
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
	if resources, err := synclocal.CompileWorkspace(options.RepositoryRoot); err == nil {
		referenced := make(map[syncdomain.ResourceID]struct{})
		for _, resource := range resources {
			if _, detach := selectedSet[syncdomain.ResourceID{
				Kind: resource.Kind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey,
			}]; detach {
				continue
			}
			for _, attachment := range resource.Attachments {
				referenced[syncdomain.ResourceID{
					Kind: syncdomain.Kind(attachment.Kind), ProjectKey: resource.ProjectKey, LookupKey: attachment.Key(),
				}] = struct{}{}
			}
		}
		updated.RemoveUnreferencedAttachments(referenced)
	}
	persisted, err := options.Manifest.Update(original, updated)
	if err != nil {
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
			return errors.Join(err, restoreManifest(options.Manifest, persisted, original))
		}
		if exists {
			deletions = append(deletions, synclocal.VariationDeletion{
				ProjectKey: resource.ProjectKey, ConfigKey: configKey, VariationKey: variationKey,
			})
		}
	}

	if _, err := options.Store.DeleteVariations(deletions); err != nil {
		return errors.Join(err, restoreManifest(options.Manifest, persisted, original))
	}
	if err := options.Store.RemoveEmptyDirectories(); err != nil {
		return err
	}
	return nil
}

// restoreManifest restores the manifest when local wrapper deletion fails.
func restoreManifest(store ManifestStore, current, original syncmanifest.Manifest) error {
	_, err := store.Update(current, original)
	return err
}
