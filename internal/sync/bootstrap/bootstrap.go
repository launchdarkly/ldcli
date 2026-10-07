package bootstrap

import (
	"cmp"
	"encoding/json"
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
	SearchProjects(query string, limit, offset int) (syncapi.Page[syncapi.Project], error)
	SearchConfigs(projectKey, query string, modes []syncdomain.VariationMode, limit, offset int) (syncapi.Page[syncapi.Config], error)
	Config(projectKey, configKey string) (syncapi.Config, error)
}

type AttachmentReader interface {
	ReadAttachment(projectKey string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error)
}

// ManifestStore persists the synchronization baseline after local files are written.
type ManifestStore interface {
	Load(projectKeys []string) (syncmanifest.Manifest, error)
	Update(previous, next syncmanifest.Manifest) (syncmanifest.Manifest, error)
}

// Options contains the dependencies and streams for one bootstrap flow.
type Options struct {
	Catalog     Catalog
	Attachments AttachmentReader
	Store       synclocal.Store
	Manifest    ManifestStore
	Input       io.Reader
	Output      io.Writer
	Initial     bool
	DryRun      bool
	Selections  []syncdomain.ResourceID
	NoInput     bool
}

// Run resolves selected prompt variations and writes their local wrappers.
func Run(options Options) error {
	if len(options.Selections) != 0 {
		files, err := selectVariationFilesByID(options)
		if err != nil {
			return err
		}
		return finishSelection(options, files)
	}
	if options.NoInput {
		return fmt.Errorf("variation selectors are required with --no-input")
	}
	if !syncinteractive.StreamsAreTerminal(options.Input, options.Output) {
		return fmt.Errorf("interactive prompt selection requires a terminal; use selectors with --no-input")
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

func selectVariationFilesByID(options Options) ([]synclocal.VariationFile, error) {
	configs := make(map[string]syncapi.Config)
	seen := make(map[syncdomain.ResourceID]struct{}, len(options.Selections))
	files := make([]synclocal.VariationFile, 0, len(options.Selections))

	for _, selection := range options.Selections {
		if selection.Kind != syncdomain.KindVariation {
			return nil, fmt.Errorf("cannot add %s resource", selection.Kind)
		}
		if _, duplicate := seen[selection]; duplicate {
			return nil, fmt.Errorf("variation %s/%s was selected more than once", selection.ProjectKey, selection.LookupKey)
		}
		seen[selection] = struct{}{}

		configKey, variationKey, ok := strings.Cut(selection.LookupKey, "/")
		if !ok {
			return nil, fmt.Errorf("invalid variation %q", selection.LookupKey)
		}
		cacheKey := selection.ProjectKey + "/" + configKey
		config, ok := configs[cacheKey]
		if !ok {
			var err error
			config, err = options.Catalog.Config(selection.ProjectKey, configKey)
			if err != nil {
				return nil, err
			}
			configs[cacheKey] = config
		}

		variationIndex := slices.IndexFunc(config.Variations, func(variation syncdomain.Variation) bool {
			return variation.Key == variationKey
		})
		if variationIndex < 0 {
			return nil, fmt.Errorf(
				"variation %q does not exist in config %q in project %q",
				variationKey,
				configKey,
				selection.ProjectKey,
			)
		}
		exists, err := options.Store.VariationExists(selection.ProjectKey, configKey, variationKey)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, fmt.Errorf("variation %s/%s is already synced", selection.ProjectKey, selection.LookupKey)
		}

		variation := config.Variations[variationIndex]
		if err := hydrateAttachments(options.Attachments, selection.ProjectKey, &variation); err != nil {
			return nil, err
		}
		files = append(files, synclocal.VariationFile{
			ProjectKey: selection.ProjectKey,
			ConfigKey:  configKey,
			Upsert:     true,
			Variation:  variation,
		})
	}
	return files, nil
}

// selectVariationFiles guides the user from project to config to variations
// and converts the selections into local wrapper definitions.
func selectVariationFiles(options Options) ([]synclocal.VariationFile, bool, error) {
	project, canceled, err := syncinteractive.SearchSelect(syncinteractive.SearchOptions[syncapi.Project]{
		Input:             options.Input,
		Output:            options.Output,
		SearchTitle:       "Search LaunchDarkly projects",
		SearchPlaceholder: "Project name or key",
		SelectTitle:       "Choose a LaunchDarkly project",
		ItemName:          "projects",
		Fetch: func(query string, limit, offset int) ([]syncapi.Project, int, error) {
			page, err := options.Catalog.SearchProjects(query, limit, offset)
			return page.Items, page.TotalCount, err
		},
		Choice: projectChoice,
	})
	if err != nil || canceled {
		return nil, canceled, err
	}

	config, canceled, err := syncinteractive.SearchSelect(syncinteractive.SearchOptions[syncapi.Config]{
		Input:             options.Input,
		Output:            options.Output,
		SearchTitle:       "Search LaunchDarkly configs",
		SearchPlaceholder: "Config name or key",
		SelectTitle:       "Choose a config",
		ItemName:          "configs",
		Fetch: func(query string, limit, offset int) ([]syncapi.Config, int, error) {
			page, err := options.Catalog.SearchConfigs(project.Key, query, nil, limit, offset)
			return page.Items, page.TotalCount, err
		},
		Choice: configChoice,
	})
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
		if err := hydrateAttachments(options.Attachments, project.Key, &variation); err != nil {
			return nil, false, err
		}
		files = append(files, synclocal.VariationFile{
			ProjectKey: project.Key,
			ConfigKey:  config.Key,
			Upsert:     true,
			Variation:  variation,
		})
	}
	return files, false, nil
}

func hydrateAttachments(reader AttachmentReader, projectKey string, variation *syncdomain.Variation) error {
	for index := range variation.Tools {
		attachment, err := reader.ReadAttachment(projectKey, syncdomain.AttachmentTool, variation.Tools[index].Key)
		if err != nil {
			return err
		}
		variation.Tools[index].Version = attachment.Version
		variation.SetAttachment(attachment)
	}
	for index := range variation.Skills {
		attachment, err := reader.ReadAttachment(projectKey, syncdomain.AttachmentSkill, variation.Skills[index].Key)
		if err != nil {
			return err
		}
		variation.Skills[index].Version = attachment.Version
		variation.SetAttachment(attachment)
	}
	return variation.NormalizeAttachments()
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
			Title: variation.Name, Description: "Key: " + variation.Key, Value: variation,
		})
	}
	return choices, existingCount, nil
}

// projectChoice keeps the readable project name above its stable key.
func projectChoice(project syncapi.Project) syncinteractive.Choice[syncapi.Project] {
	return syncinteractive.Choice[syncapi.Project]{
		Title: project.Name, Description: "Key: " + project.Key, Value: project,
	}
}

// configChoice includes both the stable config identity and its mode.
func configChoice(config syncapi.Config) syncinteractive.Choice[syncapi.Config] {
	return syncinteractive.Choice[syncapi.Config]{
		Title: config.Name, Description: fmt.Sprintf("Key: %s · Mode: %s", config.Key, config.Mode), Value: config,
	}
}

// finishSelection validates the selected variations, renders dry-run previews,
// or commits the wrappers and their manifest fingerprints together.
func finishSelection(options Options, files []synclocal.VariationFile) error {
	if len(files) == 0 {
		writeNoChangeSummary(options.Output, options.DryRun)
		return nil
	}
	if options.DryRun {
		for _, file := range files {
			if err := syncdomain.ValidateDirectAPIVariation(file.Variation); err != nil {
				return err
			}
		}
		previews, err := options.Store.RenderResources(files)
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
	manifest, err := options.Manifest.Load(projectKeys)
	if err != nil {
		return err
	}
	// Keep the loaded versions unchanged because Update uses them for
	// optimistic concurrency.
	previousManifest := manifest
	manifest.Resources = slices.Clone(manifest.Resources)

	var creation synclocal.Creation
	if options.Initial {
		creation, err = options.Store.BootstrapResources(files)
	} else {
		creation, err = options.Store.AddResources(files)
	}
	if err != nil {
		return err
	}
	if err := setManifestFromLocalResources(&manifest, options.Store, files); err != nil {
		return errors.Join(err, options.Store.RollbackCreation(creation))
	}
	// The manifest is written last so it never claims a wrapper exists before
	// that wrapper reaches disk. Roll back every file this operation created if
	// persistence fails, including shared dependencies that did not exist before.
	if _, err := options.Manifest.Update(previousManifest, manifest); err != nil {
		return errors.Join(err, options.Store.RollbackCreation(creation))
	}

	writeSummary(options.Output, options.Initial, len(creation.VariationPaths))

	return nil
}

// setManifestFromLocalResources records the files that reached disk, including
// existing attachment files that creation preserved.
func setManifestFromLocalResources(
	manifest *syncmanifest.Manifest,
	store synclocal.Store,
	files []synclocal.VariationFile,
) error {
	selected := make(map[syncdomain.ResourceID]struct{}, len(files))
	for _, file := range files {
		selected[syncdomain.ResourceID{
			Kind:       syncdomain.KindVariation,
			ProjectKey: file.ProjectKey,
			LookupKey:  file.ConfigKey + "/" + file.Variation.Key,
		}] = struct{}{}
	}

	resources, err := store.Compile()
	if err != nil {
		return err
	}
	for _, resource := range resources {
		id := syncdomain.ResourceID{
			Kind: resource.Kind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey,
		}
		if _, ok := selected[id]; !ok {
			continue
		}
		var variation syncdomain.Variation
		if err := json.Unmarshal(resource.Payload, &variation); err != nil {
			return fmt.Errorf("decode local variation %q: %w", resource.LookupKey, err)
		}
		variation.Attachments = resource.Attachments
		fingerprint, err := syncdomain.FingerprintVariation(resource.ProjectKey, resource.LookupKey, variation)
		if err != nil {
			return err
		}
		manifest.SetFingerprint(id, fingerprint)
		if err := manifest.SetAttachmentsIfMissing(resource.ProjectKey, resource.Attachments); err != nil {
			return err
		}
		delete(selected, id)
	}
	for id := range selected {
		return fmt.Errorf("created variation %s/%s was not found", id.ProjectKey, id.LookupKey)
	}
	return nil
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
