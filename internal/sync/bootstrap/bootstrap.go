// Package bootstrap adds LaunchDarkly variations to the local workspace. The
// first add creates the .launchdarkly directory.
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

// Catalog finds the projects, configs, and variations to add.
type Catalog interface {
	syncinteractive.ProjectSearcher
	syncinteractive.ConfigSearcher
	Config(projectKey, configKey string) (syncapi.Config, error)
}

// AttachmentReader reads the tools and skills that a variation uses.
type AttachmentReader interface {
	ReadAttachment(projectKey string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error)
}

// Options are the dependencies and the input of one add.
type Options struct {
	Catalog     Catalog
	Attachments AttachmentReader
	Store       synclocal.Store
	Baselines   syncmanifest.Baselines
	Input       io.Reader
	Output      io.Writer
	// Initial is true when the workspace has no .launchdarkly directory.
	Initial bool
	DryRun  bool
	// Selections are the variations to add. If it is empty, Run asks the user.
	Selections []syncdomain.ResourceID
	NoInput    bool
}

// Run writes the files of the selected variations and records their baseline.
func Run(options Options) error {
	var files []synclocal.VariationFile
	var err error
	if len(options.Selections) != 0 {
		files, err = selectedVariations(options)
	} else {
		if err := syncinteractive.RequireTerminal(
			options.Input, options.Output, options.NoInput, "variation selectors", "prompt selection",
		); err != nil {
			return err
		}
		var canceled bool
		files, canceled, err = promptForVariations(options)
		if canceled {
			return nil
		}
	}
	if err != nil {
		return err
	}
	return finishSelection(options, files)
}

// selectedVariations reads each variation that the user named.
func selectedVariations(options Options) ([]synclocal.VariationFile, error) {
	configs := make(map[string]syncapi.Config)
	seen := make(map[syncdomain.ResourceID]struct{}, len(options.Selections))
	files := make([]synclocal.VariationFile, 0, len(options.Selections))

	for _, selection := range options.Selections {
		configKey, variationKey, err := selection.VariationKeys()
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[selection]; duplicate {
			return nil, fmt.Errorf("variation %s was selected more than once", selection)
		}
		seen[selection] = struct{}{}

		configID := selection.ProjectKey + "/" + configKey
		config, ok := configs[configID]
		if !ok {
			if config, err = options.Catalog.Config(selection.ProjectKey, configKey); err != nil {
				return nil, err
			}
			configs[configID] = config
		}
		index := slices.IndexFunc(config.Variations, func(variation syncdomain.Variation) bool {
			return variation.Key == variationKey
		})
		if index < 0 {
			return nil, fmt.Errorf(
				"variation %q does not exist in config %q in project %q", variationKey, configKey, selection.ProjectKey,
			)
		}

		exists, err := options.Store.VariationExists(selection.ProjectKey, configKey, variationKey)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, fmt.Errorf("variation %s is already synced", selection)
		}

		file, err := newVariationFile(options.Attachments, selection.ProjectKey, configKey, config.Variations[index])
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// promptForVariations asks the user for a project, a config, and one or more
// variations. The bool result is true when the user cancels.
func promptForVariations(options Options) ([]synclocal.VariationFile, bool, error) {
	project, canceled, err := syncinteractive.SelectProject(options.Input, options.Output, options.Catalog)
	if err != nil || canceled {
		return nil, canceled, err
	}
	config, canceled, err := syncinteractive.SelectConfig(options.Input, options.Output, options.Catalog, project.Key, nil)
	if err != nil || canceled {
		return nil, canceled, err
	}
	if len(config.Variations) == 0 {
		return nil, false, fmt.Errorf("config %q has no prompt variations", config.Key)
	}

	choices, existingCount, err := variationChoices(options.Store, project.Key, config)
	if err != nil || len(choices) == 0 {
		return nil, false, err
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
		options.Input, options.Output, "Select prompt variations", description, choices,
	)
	if err != nil || canceled {
		return nil, canceled, err
	}

	files := make([]synclocal.VariationFile, 0, len(variations))
	for _, variation := range variations {
		file, err := newVariationFile(options.Attachments, project.Key, config.Key, variation)
		if err != nil {
			return nil, false, err
		}
		files = append(files, file)
	}
	return files, false, nil
}

// variationChoices returns the variations that are not synced yet, by name.
// It also returns the number of variations that are already synced.
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
			Title: variation.Name, Description: "Key: " + variation.Key, Value: variation,
		})
	}
	return choices, existingCount, nil
}

// newVariationFile reads the tools and skills of a LaunchDarkly variation, so
// that the add also writes their files.
func newVariationFile(
	reader AttachmentReader,
	projectKey, configKey string,
	variation syncdomain.Variation,
) (synclocal.VariationFile, error) {
	err := variation.HydrateAttachments(func(kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
		return reader.ReadAttachment(projectKey, kind, key)
	})
	if err != nil {
		return synclocal.VariationFile{}, err
	}
	return synclocal.VariationFile{ProjectKey: projectKey, ConfigKey: configKey, Upsert: true, Variation: variation}, nil
}

// finishSelection previews the files, or writes them and records their
// baseline. The manifest update comes last, so that the manifest never
// tracks a file that is not on disk. If the baseline or the update fails,
// the files that this add created are removed.
func finishSelection(options Options, files []synclocal.VariationFile) error {
	if len(files) == 0 {
		writeNoChangeSummary(options.Output, options.DryRun)
		return nil
	}
	if options.DryRun {
		previews, err := options.Store.Render(files)
		if err != nil {
			return err
		}
		writePreviews(options.Output, previews)
		return nil
	}

	projectKeys := make([]string, 0, len(files))
	for _, file := range files {
		projectKeys = append(projectKeys, file.ProjectKey)
	}
	baseline, err := options.Baselines.Load(projectKeys)
	if err != nil {
		return err
	}

	var creation synclocal.Creation
	if options.Initial {
		creation, err = options.Store.Bootstrap(files)
	} else {
		creation, err = options.Store.Add(files)
	}
	if err != nil {
		return err
	}
	next, err := recordCreatedVariations(baseline.Lock, options.Store, files)
	if err == nil {
		_, err = options.Baselines.Save(baseline, next)
	}
	if err != nil {
		return errors.Join(err, options.Store.RollbackCreation(creation))
	}

	writeSummary(options.Output, options.Initial, len(creation.VariationPaths))
	return nil
}

// recordCreatedVariations returns a copy of the manifest with a baseline for
// each new variation. It reads the variations from disk, because Add keeps an
// existing tool or skill file, and the baseline must match that file.
func recordCreatedVariations(
	manifest syncmanifest.Manifest,
	store synclocal.Store,
	files []synclocal.VariationFile,
) (syncmanifest.Manifest, error) {
	created := make(map[syncdomain.ResourceID]struct{}, len(files))
	for _, file := range files {
		created[syncdomain.VariationID(file.ProjectKey, file.ConfigKey, file.Variation.Key)] = struct{}{}
	}
	variations, err := store.Compile()
	if err != nil {
		return syncmanifest.Manifest{}, err
	}

	next := manifest.Clone()
	for _, variation := range variations {
		id := variation.ID()
		if _, ok := created[id]; !ok {
			continue
		}
		fingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation.Variation)
		if err != nil {
			return syncmanifest.Manifest{}, err
		}
		next.SetFingerprint(id, fingerprint)
		if err := next.SetAttachmentsIfMissing(id.ProjectKey, variation.Variation.Attachments); err != nil {
			return syncmanifest.Manifest{}, err
		}
		delete(created, id)
	}
	for id := range created {
		return syncmanifest.Manifest{}, fmt.Errorf("created variation %s was not found", id)
	}
	return next, nil
}

// writeNoChangeSummary reports that the config has no variation to add.
func writeNoChangeSummary(output io.Writer, dryRun bool) {
	message := "No variations added; every variation in that config is already synced."
	if dryRun {
		message = "No variations would be added; every variation in that config is already synced."
	}
	_ = syncconsole.New(output).Line(message)
}

// writeSummary reports how many variation files the add created.
func writeSummary(output io.Writer, initial bool, count int) {
	verb := "Added"
	if initial {
		verb = "Bootstrapped"
	}
	noun := "variation file"
	if count != 1 {
		noun += "s"
	}
	_ = syncconsole.New(output).Printf("%s %d %s in %s.\n", verb, count, noun, syncdomain.RootDir)
}

// writePreviews prints each file that a dry run would create.
func writePreviews(output io.Writer, previews []synclocal.RenderedFile) {
	console := syncconsole.New(output)
	const border = "============================================================"
	for index, preview := range previews {
		if index != 0 {
			_ = console.Line("")
		}
		_ = console.Line(border)
		_ = console.Printf(
			"File %d of %d\nWould create: %s\n", index+1, len(previews), path.Join(syncdomain.RootDir, preview.Path),
		)
		_ = console.Line("------------------------------------------------------------")
		_ = console.WriteBytes(preview.Content)
		if len(preview.Content) == 0 || preview.Content[len(preview.Content)-1] != '\n' {
			_ = console.Line("")
		}
		_ = console.Line(border)
	}
}
