package bootstrap

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// Catalog lists projects and configs available for bootstrap.
type Catalog interface {
	Projects() ([]syncapi.Project, error)
	Configs(projectKey string) ([]syncapi.Config, error)
}

// ManifestStore persists the synchronization baseline after local files are written.
type ManifestStore interface {
	Load() (syncmanifest.Manifest, bool, error)
	Write(syncmanifest.Manifest) error
}

// Options contains the dependencies and streams for one bootstrap flow.
type Options struct {
	Catalog  Catalog
	Store    synclocal.Store
	Manifest ManifestStore
	Input    io.Reader
	Output   io.Writer
	Initial  bool
	DryRun   bool
}

// Run interactively selects prompt variations and writes their local wrappers.
func Run(options Options) error {
	if !syncinteractive.StreamsAreTerminal(options.Input, options.Output) {
		return fmt.Errorf(
			"interactive prompt selection requires a terminal; run this command in a terminal",
		)
	}

	files, canceled, err := selectVariationFiles(options)
	if err != nil {
		return err
	}
	if canceled {
		return nil
	}
	return finishSelection(options, files)
}

// selectVariationFiles guides the user from project to config to variations
// and converts the selections into local wrapper definitions.
func selectVariationFiles(options Options) ([]synclocal.VariationFile, bool, error) {
	console := syncconsole.New(options.Output)
	_ = console.Line("Loading LaunchDarkly projects...")
	projects, err := options.Catalog.Projects()
	if err != nil {
		return nil, false, err
	}
	project, canceled, err := syncinteractive.Select(
		options.Input,
		options.Output,
		"Choose a LaunchDarkly project",
		projectChoices(projects),
	)
	if err != nil || canceled {
		return nil, canceled, err
	}

	_ = console.Line("Loading configs...")
	configs, err := options.Catalog.Configs(project.Key)
	if err != nil {
		return nil, false, err
	}
	config, canceled, err := syncinteractive.Select(
		options.Input,
		options.Output,
		"Choose a config",
		configChoices(configs),
	)
	if err != nil || canceled {
		return nil, canceled, err
	}
	if len(config.Variations) == 0 {
		return nil, false, fmt.Errorf("config %q has no prompt variations", config.Key)
	}

	choices, existingCount, err := variationChoices(
		options.Store,
		project.Key,
		config,
	)
	if err != nil {
		return nil, false, err
	}
	if len(choices) == 0 {
		return nil, false, nil
	}

	action := "write"
	if options.DryRun {
		action = "preview"
	}
	description := fmt.Sprintf("Choose one or more variations to %s.", action)
	if existingCount != 0 {
		description += fmt.Sprintf(" %d already synced variations are omitted.", existingCount)
	}
	variations, canceled, err := syncinteractive.MultiSelect(
		options.Input,
		options.Output,
		"Select prompt variations",
		description,
		choices,
	)
	if err != nil || canceled {
		return nil, canceled, err
	}

	files := make([]synclocal.VariationFile, 0, len(variations))
	for _, variation := range variations {
		files = append(files, synclocal.VariationFile{
			ProjectKey: project.Key,
			ConfigKey:  config.Key,
			Upsert:     true,
			Variation:  variation,
		})
	}
	return files, false, nil
}

// variationChoices returns unsynchronized variations in stable display order
// and separately counts variations that already have local wrappers.
func variationChoices(
	store synclocal.Store,
	projectKey string,
	config syncapi.Config,
) ([]syncinteractive.Choice[syncdomain.Variation], int, error) {
	slices.SortFunc(config.Variations, func(left, right syncdomain.Variation) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)),
			cmp.Compare(left.Key, right.Key),
		)
	})

	choices := make([]syncinteractive.Choice[syncdomain.Variation], 0, len(config.Variations))
	existingCount := 0
	for _, variation := range config.Variations {
		exists, err := store.VariationExists(projectKey, config.Key, variation.Key)
		if err != nil {
			return nil, 0, err
		}
		if exists {
			existingCount++
			continue
		}
		choices = append(choices, syncinteractive.Choice[syncdomain.Variation]{
			Title: variation.Name, Description: variation.Key, Value: variation,
		})
	}
	return choices, existingCount, nil
}

// projectChoices adapts API projects to the shared interactive choice model.
func projectChoices(projects []syncapi.Project) []syncinteractive.Choice[syncapi.Project] {
	choices := make([]syncinteractive.Choice[syncapi.Project], 0, len(projects))
	for _, project := range projects {
		choices = append(choices, syncinteractive.Choice[syncapi.Project]{
			Title: project.Name, Description: project.Key, Value: project,
		})
	}
	return choices
}

// configChoices adapts configs to labels that include both identity and mode.
func configChoices(configs []syncapi.Config) []syncinteractive.Choice[syncapi.Config] {
	choices := make([]syncinteractive.Choice[syncapi.Config], 0, len(configs))
	for _, config := range configs {
		choices = append(choices, syncinteractive.Choice[syncapi.Config]{
			Title: config.Name, Description: fmt.Sprintf("%s · %s", config.Key, config.Mode), Value: config,
		})
	}
	return choices
}

// finishSelection validates the selected variations, renders dry-run previews,
// or commits the wrappers and their manifest fingerprints together.
func finishSelection(options Options, files []synclocal.VariationFile) error {
	if len(files) == 0 {
		writeNoChangeSummary(options.Output, options.DryRun)
		return nil
	}
	for _, file := range files {
		if err := syncdomain.ValidateDirectAPIVariation(file.Variation); err != nil {
			return err
		}
	}
	if options.DryRun {
		previews, err := options.Store.RenderVariations(files)
		if err != nil {
			return err
		}
		writePreviews(options.Output, previews)
		return nil
	}

	manifest, _, err := options.Manifest.Load()
	if err != nil {
		return err
	}
	for _, file := range files {
		lookupKey := file.ConfigKey + "/" + file.Variation.Key
		fingerprint, err := syncdomain.FingerprintVariation(file.ProjectKey, lookupKey, file.Variation)
		if err != nil {
			return err
		}
		manifest.SetFingerprint(syncdomain.ResourceID{
			Kind: syncdomain.KindVariation, ProjectKey: file.ProjectKey, LookupKey: lookupKey,
		}, fingerprint)
	}

	var paths []string
	if options.Initial {
		paths, err = options.Store.Bootstrap(files)
	} else {
		paths, err = options.Store.Add(files)
	}
	if err != nil {
		return err
	}
	// The manifest is written last so it never claims a wrapper exists before
	// that wrapper reaches disk. Roll back the wrappers if persistence fails.
	if err := options.Manifest.Write(manifest); err != nil {
		return errors.Join(err, rollbackVariationFiles(options.Store, files))
	}

	writeSummary(options.Output, options.Initial, len(paths))

	return nil
}

// rollbackVariationFiles removes wrappers created by a failed bootstrap or add.
func rollbackVariationFiles(store synclocal.Store, files []synclocal.VariationFile) error {
	deletions := make([]synclocal.VariationDeletion, 0, len(files))
	for _, file := range files {
		deletions = append(deletions, synclocal.VariationDeletion{
			ProjectKey:   file.ProjectKey,
			ConfigKey:    file.ConfigKey,
			VariationKey: file.Variation.Key,
		})
	}
	_, err := store.DeleteVariations(deletions)
	return err
}

// writeNoChangeSummary explains that every available variation is already local.
func writeNoChangeSummary(output io.Writer, dryRun bool) {
	message := "No variations added; every variation in that config is already synced."
	if dryRun {
		message = "No variations would be added; every variation in that config is already synced."
	}
	_ = syncconsole.New(output).Line(message)
}

// writeSummary reports how many wrapper files were created.
func writeSummary(output io.Writer, initial bool, count int) {
	verb := "Added"
	if initial {
		verb = "Bootstrapped"
	}

	resource := "variation file"
	if count != 1 {
		resource += "s"
	}

	_ = syncconsole.New(output).Printf(
		"%s %d %s in %s.\n",
		verb,
		count,
		resource,
		syncdomain.RootDir,
	)
}

// writePreviews prints each dry-run file with clear boundaries and its target path.
func writePreviews(output io.Writer, previews []synclocal.RenderedVariationFile) {
	console := syncconsole.New(output)
	for index, preview := range previews {
		if index != 0 {
			_ = console.Line("")
		}
		_ = console.Line("============================================================")
		_ = console.Printf(
			"File %d of %d\nWould create: %s\n",
			index+1,
			len(previews),
			path.Join(syncdomain.RootDir, preview.Path),
		)
		_ = console.Line("------------------------------------------------------------")
		_ = console.WriteBytes(preview.Content)
		if len(preview.Content) == 0 || preview.Content[len(preview.Content)-1] != '\n' {
			_ = console.Line("")
		}
		_ = console.Line("============================================================")
	}
}
