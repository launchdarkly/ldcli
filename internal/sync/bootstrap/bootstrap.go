// Package bootstrap adds LaunchDarkly variations, tools, and skills to the
// local workspace. The first add creates the .launchdarkly directory.
package bootstrap

import (
	"bytes"
	"cmp"
	"context"
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

// AttachmentReader reads the tools and skills that a variation uses, and
// searches the tools or skills that the user adds.
type AttachmentReader interface {
	syncinteractive.AttachmentSearcher
	ReadAttachment(projectKey string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error)
}

// Settings reads and writes the LaunchDarkly sync settings that decide which
// side owns a variation.
type Settings interface {
	ResolveOwnership(projectKey string, ids []syncdomain.ResourceID) (map[syncdomain.ResourceID]syncdomain.Ownership, error)
	ClaimForCode(projectKey string, ids []syncdomain.ResourceID) error
}

// Options are the dependencies and the input of one add.
type Options struct {
	// Kind is the kind of resource to add. The empty kind is a variation.
	Kind        syncdomain.Kind
	Catalog     Catalog
	Attachments AttachmentReader
	Store       synclocal.Store
	Baselines   syncmanifest.Baselines
	Input       io.Reader
	Output      io.Writer
	// Initial is true when the workspace has no .launchdarkly directory.
	Initial bool
	DryRun  bool
	// Diff shows the content of each file that a dry run lists.
	Diff bool
	// ProjectKey and ConfigKey skip the project and the config questions.
	ProjectKey string
	ConfigKey  string
	// Selections are the resources to add. If it is empty, Run asks the user.
	Selections []syncdomain.ResourceID
	NoInput    bool
	// Settings makes code the owner of each variation that the add brings
	// into the repo for the first time.
	Settings Settings
	// ErrorOutput receives warnings and ownership notices.
	ErrorOutput io.Writer
	// Yes continues without a confirmation prompt when the sync settings are
	// unavailable.
	Yes bool
	// Context stops the settings-unavailable question when it ends. A nil
	// Context never ends.
	Context context.Context
	// isTerminal reports whether the streams are a terminal. Tests replace it.
	isTerminal func(io.Reader, io.Writer) bool
}

// Run writes the files of the selected resources and records their baseline.
func Run(options Options) error {
	switch options.Kind {
	case syncdomain.KindTool, syncdomain.KindSkill:
		return addAttachments(options)
	}

	var files []synclocal.VariationFile
	var err error
	if len(options.Selections) != 0 {
		files, err = selectedVariations(options)
	} else {
		if err := syncinteractive.RequireTerminal(
			options.Input, options.Output, options.NoInput, missingTargetFlags(options), "prompt selection",
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
			if slices.Contains(config.ArchivedVariationKeys, variationKey) {
				return nil, fmt.Errorf(
					"variation %q in config %q in project %q is archived in LaunchDarkly. "+
						"Restore it in LaunchDarkly, or pass another --key.",
					variationKey, configKey, selection.ProjectKey,
				)
			}
			return nil, fmt.Errorf(
				"variation %q does not exist in config %q in project %q. To create it, run %s with --model-config-key and --content.",
				variationKey, configKey, selection.ProjectKey, AddCommand(selection),
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

// missingTargetFlags names the flags that answer the questions that Run still
// has to ask.
func missingTargetFlags(options Options) string {
	switch {
	case options.ProjectKey == "":
		return "--project, --config-key, and --key"
	case options.ConfigKey == "":
		return "--config-key and --key"
	default:
		return "--key values"
	}
}

// promptForVariations asks the user for a project, a config, and one or more
// variations. ProjectKey and ConfigKey skip their questions. The bool result
// is true when the user cancels.
func promptForVariations(options Options) ([]synclocal.VariationFile, bool, error) {
	project := syncapi.Project{Key: options.ProjectKey}
	var canceled bool
	var err error
	if project.Key == "" {
		if project, canceled, err = syncinteractive.SelectProject(options.Input, options.Output, options.Catalog); err != nil || canceled {
			return nil, canceled, err
		}
	}
	config := syncapi.Config{Key: options.ConfigKey}
	if config.Key == "" {
		config, canceled, err = syncinteractive.SelectConfig(options.Input, options.Output, options.Catalog, project.Key, nil)
	} else {
		config, err = options.Catalog.Config(project.Key, config.Key)
	}
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
		writePreviews(options.Output, previews, options.Diff)
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
	claimed, proceed, err := claimNewVariations(options, baseline.Lock, files)
	if err != nil || !proceed {
		return err
	}
	// The claim is already written, so a later failure says so. The add can
	// run again, because the claim sets the same rules.
	claimedNote := func(err error) error {
		if len(claimed) == 0 {
			return err
		}
		return fmt.Errorf("%w; the new variations in project %s were already claimed for code, and running add again is safe",
			err, strings.Join(claimed, ", "))
	}

	var creation synclocal.Creation
	if options.Initial {
		creation, err = options.Store.Bootstrap(files)
	} else {
		creation, err = options.Store.Add(files)
	}
	if err != nil {
		return claimedNote(err)
	}
	next, err := recordCreatedVariations(baseline.Lock, options.Store, files)
	if err == nil {
		_, err = options.Baselines.Save(baseline, next)
	}
	if err != nil {
		return claimedNote(errors.Join(err, options.Store.RollbackCreation(creation)))
	}

	writeSummary(options.Output, options.Initial, len(creation.VariationPaths), "variation")
	return nil
}

// claimNewVariations makes code the owner of each variation that enters the
// repo for the first time, before the add writes any file. It reads the
// ownership of every project first, and claims nothing until every read
// succeeded, so that a failed read or a canceled add leaves no claim behind.
// Then it makes at most one claim per project. A variation that an explicit
// LaunchDarkly rule owns is pulled but not claimed. When a project has no
// sync settings, it warns, and asks whether to add anyway unless Yes is set.
// It returns the projects that it claimed in. The bool result is false when
// the user declines.
func claimNewVariations(options Options, lock syncmanifest.Manifest, files []synclocal.VariationFile) ([]string, bool, error) {
	if options.Settings == nil {
		return nil, true, nil
	}
	byProject := make(map[string][]syncdomain.ResourceID)
	var projectKeys []string
	for _, file := range files {
		id := syncdomain.VariationID(file.ProjectKey, file.ConfigKey, file.Variation.Key)
		if lock.Has(id) {
			continue
		}
		if _, seen := byProject[id.ProjectKey]; !seen {
			projectKeys = append(projectKeys, id.ProjectKey)
		}
		byProject[id.ProjectKey] = append(byProject[id.ProjectKey], id)
	}
	slices.Sort(projectKeys)

	errorOutput := options.ErrorOutput
	if errorOutput == nil {
		errorOutput = io.Discard
	}
	decisions := make(map[string]ownershipDecision, len(projectKeys))
	var unavailable []string
	for _, projectKey := range projectKeys {
		decision, err := decideProject(options.Settings, projectKey, byProject[projectKey])
		switch {
		case syncapi.IsNotFound(err):
			unavailable = append(unavailable, projectKey)
			continue
		case errors.Is(err, syncapi.ErrSyncSettingsForbidden):
			return nil, false, syncdomain.SyncSettingsForbiddenProblem(projectKey, "record code ownership", "nothing was added", err)
		case err != nil:
			return nil, false, fmt.Errorf("make code the owner of the new variations in project %s: %w", projectKey, err)
		}
		decisions[projectKey] = decision
	}
	if len(unavailable) != 0 {
		ctx := options.Context
		if ctx == nil {
			ctx = context.Background()
		}
		proceed, err := syncinteractive.ConfirmWithoutSettings(syncinteractive.SettingsUnavailable{
			Context:     ctx,
			Input:       options.Input,
			Prompt:      options.Output,
			Output:      options.Output,
			ErrorOutput: errorOutput,
			NoInput:     options.NoInput,
			Yes:         options.Yes,
			IsTerminal:  options.isTerminal,
			ProjectKeys: unavailable,
			Unrecorded:  "code ownership of the new variations",
			Question:    withoutOwnershipQuestion,
			Verb:        "Add",
		})
		if err != nil || !proceed {
			return nil, false, err
		}
	}

	console := syncconsole.New(errorOutput)
	var claimed []string
	for _, projectKey := range projectKeys {
		decision, available := decisions[projectKey]
		if !available {
			continue
		}
		if len(decision.claims) != 0 {
			err := options.Settings.ClaimForCode(projectKey, decision.claims)
			switch {
			case errors.Is(err, syncapi.ErrSyncSettingsForbidden):
				return nil, false, syncdomain.SyncSettingsForbiddenProblem(projectKey, "record code ownership", addConsequence(claimed), err)
			case err != nil:
				return nil, false, fmt.Errorf("make code the owner of the new variations in project %s, so %s: %w", projectKey, addConsequence(claimed), err)
			}
			claimed = append(claimed, projectKey)
		}
		for _, id := range decision.managed {
			_ = console.Line(syncdomain.ManagedInLaunchDarklyMessage(id))
		}
	}
	return claimed, true, nil
}

// addConsequence says what a failed claim leaves behind: no file, and the
// claims of the projects that were already claimed.
func addConsequence(claimed []string) string {
	if len(claimed) == 0 {
		return "nothing was added"
	}
	return fmt.Sprintf("nothing was added, but variations in project %s were already claimed for code", strings.Join(claimed, ", "))
}

// withoutOwnershipQuestion asks whether to add variations whose ownership
// LaunchDarkly cannot record.
const withoutOwnershipQuestion = "Add without recording ownership? [y/N] "

// ownershipDecision splits the new variations of one project into those to
// claim for code and those that LaunchDarkly manages.
type ownershipDecision struct {
	claims, managed []syncdomain.ResourceID
}

// decideProject reads the ownership of the new variations of one project
// and decides which ones to claim. It claims nothing. A variation that the
// read answers without an entry for fails closed.
func decideProject(settings Settings, projectKey string, ids []syncdomain.ResourceID) (ownershipDecision, error) {
	ownership, err := settings.ResolveOwnership(projectKey, ids)
	if err != nil {
		return ownershipDecision{}, err
	}
	var decision ownershipDecision
	for _, id := range ids {
		owner, ok := ownership[id]
		if !ok {
			return ownershipDecision{}, syncdomain.NewProblem(
				syncdomain.ErrorSyncSettingUnavailable,
				"LaunchDarkly returned no sync setting",
				"Configure the resource sync setting in LaunchDarkly, then add it again.",
			).WithResource(id)
		}
		switch syncdomain.DecideOwnership(id, owner) {
		case syncdomain.OwnershipClaim:
			decision.claims = append(decision.claims, id)
		case syncdomain.OwnershipManagedInLaunchDarkly:
			decision.managed = append(decision.managed, id)
		}
	}
	return decision, nil
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

// writeSummary reports how many files of kind the add created.
func writeSummary(output io.Writer, initial bool, count int, kind string) {
	verb := "Added"
	if initial {
		verb = "Bootstrapped"
	}
	noun := kind + " file"
	if count != 1 {
		noun += "s"
	}
	_ = syncconsole.New(output).Printf("%s %d %s in %s.\n", verb, count, noun, syncdomain.RootDir)
}

// writePreviews lists each file that the add creates, with its number of
// lines. With showContent, the content of each file follows the list.
func writePreviews(output io.Writer, previews []synclocal.RenderedFile, showContent bool) {
	console := syncconsole.New(output)
	noun := "file"
	if len(previews) != 1 {
		noun += "s"
	}
	_ = console.Printf("%d %s to create:\n", len(previews), noun)

	paths := make([]string, len(previews))
	width := 0
	for index, preview := range previews {
		paths[index] = path.Join(syncdomain.RootDir, preview.Path)
		width = max(width, len(paths[index]))
	}
	for index, preview := range previews {
		_ = console.Printf("  %-*s   +%d\n", width, paths[index], lineCount(preview.Content))
	}
	if !showContent {
		return
	}
	for index, preview := range previews {
		_ = console.Printf("\n%s\n", paths[index])
		for _, line := range strings.Split(strings.TrimSuffix(string(preview.Content), "\n"), "\n") {
			if line != "" {
				line = "    " + line
			}
			_ = console.Line(line)
		}
	}
}

// lineCount returns the number of lines in a file. A last line without a
// final newline counts.
func lineCount(content []byte) int {
	count := bytes.Count(content, []byte("\n"))
	if len(content) != 0 && content[len(content)-1] != '\n' {
		count++
	}
	return count
}
