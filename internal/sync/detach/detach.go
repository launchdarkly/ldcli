// Package detach stops syncing selected resources of any kind. It removes
// their local files and their baseline. LaunchDarkly keeps the resources,
// unless the user asks to archive the variations.
package detach

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

const detachQuestion = "\nDetach these resources? [y/N] "

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
	// Kind is the kind of resource that the user chooses from when
	// Selections is empty.
	Kind syncdomain.Kind
	// Selections are the resources to detach. If it is empty, Run asks the
	// user to choose from the synced resources that Filter keeps.
	Selections []syncdomain.ResourceID
	Filter     syncdomain.Filter
	NoInput    bool
	// Archive also archives each selected variation in LaunchDarkly.
	Archive  bool
	Archiver Archiver
	// Yes detaches, and archives, without a confirmation.
	Yes bool
	// Context stops the confirmation and the archive when it ends.
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

// Run detaches the selected resources after the user agrees.
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
		synced = slices.DeleteFunc(synced, func(id syncdomain.ResourceID) bool {
			return id.Kind != options.Kind || !options.Filter.Includes(id)
		})
		if len(synced) == 0 {
			_ = console.Line("No resources are currently synced.")
			return nil
		}
		var canceled bool
		if selected, canceled, err = promptForResources(options, synced); err != nil || canceled {
			return err
		}
	}

	// A workspace with an invalid variation file does not compile. A detach
	// of variations still works then, so the error only stops a check that
	// needs the variations.
	variations, compileErr := synclocal.CompileWorkspace(options.RepositoryRoot)
	if errors.Is(compileErr, synclocal.ErrNoDirectory) {
		compileErr = nil
	}
	if err := checkUnused(selected, variations, compileErr); err != nil {
		return err
	}
	confirmed, err := confirmDetach(options, baseline.Lock, variations, selected)
	if err != nil || !confirmed {
		return err
	}
	if options.Archive {
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
		_ = console.Printf("- %s\n", resource.Label())
	}
	return nil
}

// promptForResources asks the user to choose one or more synced resources.
// The bool result is true when the user cancels.
func promptForResources(options Options, synced []syncdomain.ResourceID) ([]syncdomain.ResourceID, bool, error) {
	arguments := "--project and --key"
	if options.Filter.Project != "" {
		arguments = "--key values"
	}
	if err := syncinteractive.RequireTerminal(options.Input, options.Output, options.NoInput, arguments, "resource selection"); err != nil {
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

// checkUnused refuses to detach a tool or a skill that a local variation
// still uses, unless the same detach removes that variation.
func checkUnused(selected []syncdomain.ResourceID, variations []syncdomain.SyncedResource, compileErr error) error {
	for _, id := range selected {
		if id.Kind == syncdomain.KindVariation {
			continue
		}
		if compileErr != nil {
			return fmt.Errorf("find the variations that use %s: %w", id.Label(), compileErr)
		}
		for _, variation := range variations {
			_, uses := variation.Variation.Attachment(syncdomain.AttachmentKind(id.Kind), id.LookupKey)
			if uses && variation.ProjectKey == id.ProjectKey && !slices.Contains(selected, variation.ID()) {
				return fmt.Errorf("%s is used by variation %s. Remove it from that variation first.", id.Label(), variation.ID())
			}
		}
	}
	return nil
}

// confirmDetach lists the selected resources and asks once, unless Yes is
// set. Without a terminal, it returns the error that suggests --yes.
func confirmDetach(
	options Options,
	lock syncmanifest.Manifest,
	variations []syncdomain.SyncedResource,
	selected []syncdomain.ResourceID,
) (bool, error) {
	if options.Yes {
		return true, nil
	}
	isTerminal := options.isTerminal
	if isTerminal == nil {
		isTerminal = syncinteractive.StreamsAreTerminal
	}
	interactive := !options.NoInput && isTerminal(options.Input, options.Output)
	if interactive {
		console := syncconsole.New(options.Output)
		if options.Archive {
			_ = console.Line("Resources to detach and archive in LaunchDarkly:")
		} else {
			_ = console.Line("Resources to detach:")
		}
		notes := localChanges(options.RepositoryRoot, lock, variations, selected)
		for _, id := range selected {
			if note := notes[id]; note != "" {
				_ = console.Printf("  %s (%s)\n", id.Label(), note)
			} else {
				_ = console.Printf("  %s\n", id.Label())
			}
		}
	}

	confirmed, err := syncinteractive.Confirm(options.context(), options.Input, options.Output, interactive, detachQuestion)
	if err == nil && !confirmed {
		_ = syncconsole.New(options.Output).Line("Detach canceled.")
	}
	return confirmed, err
}

// The notes that the detach review shows next to a resource.
const (
	// noteLocalChangesLost marks a managed file whose content differs from
	// sync.lock. The detach deletes the file.
	noteLocalChangesLost = "local changes will be lost"
	// noteUnsyncedChanges marks a linked resource whose mapping, mapped
	// source values, or relationships differ from sync.lock. The detach
	// deletes the descriptor with its relationships, and keeps the source.
	noteUnsyncedChanges = "unsynced changes"
)

// localChanges returns a note for each selected resource whose local state
// differs from its sync.lock entry. A file that sync cannot read counts as
// changed. A resource without a local file loses nothing.
func localChanges(
	repositoryRoot string,
	lock syncmanifest.Manifest,
	variations []syncdomain.SyncedResource,
	selected []syncdomain.ResourceID,
) map[syncdomain.ResourceID]string {
	baseline := make(map[syncdomain.ResourceID]syncmanifest.Resource, len(lock.Resources))
	for _, resource := range lock.Resources {
		baseline[resource.ID()] = resource
	}
	// note returns the note of a resource, or an empty string when its lock
	// entry still describes it. A linked resource compares its mapping and
	// the fingerprint of its projection, which has the relationships.
	note := func(id syncdomain.ResourceID, source *syncdomain.Reference, fingerprint func() (string, error)) string {
		actual, err := fingerprint()
		if source == nil {
			if err == nil && actual == baseline[id].Fingerprint {
				return ""
			}
			return noteLocalChangesLost
		}
		digest, digestErr := syncdomain.ReferenceDigest(*source)
		if err == nil && digestErr == nil && digest == baseline[id].ReferenceDigest && actual == baseline[id].Fingerprint {
			return ""
		}
		return noteUnsyncedChanges
	}

	notes := make(map[syncdomain.ResourceID]string)
	for _, variation := range variations {
		notes[variation.ID()] = note(variation.ID(), variation.Source, func() (string, error) {
			if variation.Source == nil {
				return syncdomain.FingerprintVariation(variation.ProjectKey, variation.LookupKey, variation.Variation)
			}
			projection, err := syncdomain.ProjectVariation(variation.Variation, *variation.Source)
			if err != nil {
				return "", err
			}
			return syncdomain.FingerprintProjection(variation.ID(), projection)
		})
	}
	isSelected := func(id syncdomain.ResourceID) bool { return slices.Contains(selected, id) }
	attachments, _ := synclocal.CompileAttachments(repositoryRoot, isSelected)
	for _, attachment := range attachments {
		notes[attachment.ID()] = note(attachment.ID(), attachment.Source, attachment.Fingerprint)
	}

	files, _ := synclocal.SourceFiles(repositoryRoot)
	result := make(map[syncdomain.ResourceID]string)
	for _, file := range files {
		id, ok := synclocal.ParseManagedPath(file)
		if !ok {
			id, ok = synclocal.ParseReferencePath(file)
		}
		if !ok || !isSelected(id) {
			continue
		}
		if link, err := leftoverLink(repositoryRoot, file); err != nil || link {
			// A symbolic link at a managed path is not a definition, so a
			// detach loses no content there.
			continue
		}
		result[id] = noteLocalChangesLost
		if _, readable := notes[id]; readable {
			result[id] = notes[id]
		} else if strings.HasSuffix(file, ".ref.yaml") {
			result[id] = noteUnsyncedChanges
		}
	}
	return result
}

// archiveVariations archives each variation that LaunchDarkly still has. It
// reads each variation first, because the API rejects the archive of an
// archived variation. A variation that is already archived or gone does not
// fail, so the same command can run again after a later step fails. It stops
// before the next archive when ctx ends.
func archiveVariations(ctx context.Context, archiver Archiver, selected []syncdomain.ResourceID) error {
	for _, resource := range selected {
		// LaunchDarkly keeps a detached tool or skill.
		if resource.Kind != syncdomain.KindVariation {
			continue
		}
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
			return fmt.Errorf("%s was selected more than once", selection.Label())
		}
		seen[selection] = struct{}{}
		if !slices.Contains(synced, selection) {
			return fmt.Errorf("%s is not synced", selection.Label())
		}
	}
	return nil
}

// loadResources returns each resource that the lock tracks or that has a
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
		synced = append(synced, resource.ID())
	}
	for _, file := range files {
		id, ok := synclocal.ParseManagedPath(file)
		if !ok {
			id, ok = synclocal.ParseReferencePath(file)
		}
		if !ok {
			continue
		}
		// A symbolic link at a managed path is not a definition, so it is
		// not a synced resource to detach.
		link, err := leftoverLink(repositoryRoot, file)
		if err != nil {
			return nil, syncmanifest.Baseline{}, err
		}
		if !link {
			synced = append(synced, id)
		}
	}
	slices.SortFunc(synced, syncdomain.CompareResourceIDs)
	return slices.Compact(synced), baseline, nil
}

// detachResources removes the selected resources from the baseline, and then
// deletes their local files in one transaction. Every other entry stays, also
// the tools and skills that only the detached variations used, because they
// sync on their own. If the delete fails, it keeps every file and restores the
// baseline.
func detachResources(options Options, original syncmanifest.Baseline, selected []syncdomain.ResourceID) error {
	next := original.Lock.Clone()
	for _, id := range selected {
		next.Remove(id)
	}
	saved, err := options.Baselines.Save(original, next)
	if err != nil {
		return err
	}
	restoreBaseline := func(cause error) error {
		_, restoreErr := options.Baselines.Save(saved, original.Lock)
		return errors.Join(cause, restoreErr)
	}

	deletions, err := localDefinitions(options.RepositoryRoot, selected)
	if err != nil {
		return restoreBaseline(err)
	}
	for _, id := range selected {
		if slices.Contains(deletions, id) {
			continue
		}
		exists, err := options.Store.ResourceExists(id)
		if err != nil {
			return restoreBaseline(err)
		}
		if exists {
			deletions = append(deletions, id)
		}
	}
	if _, err := options.Store.DeleteDefinitions(deletions); err != nil {
		return restoreBaseline(err)
	}
	return nil
}

// leftoverLink reports whether a repository file is a symbolic link.
func leftoverLink(repositoryRoot, file string) (bool, error) {
	info, err := os.Lstat(filepath.Join(repositoryRoot, filepath.FromSlash(file)))
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", file, err)
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// localDefinitions returns each selected resource that has a managed file,
// a descriptor, or a leftover link at its managed path.
func localDefinitions(repositoryRoot string, selected []syncdomain.ResourceID) ([]syncdomain.ResourceID, error) {
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return nil, err
	}
	var definitions []syncdomain.ResourceID
	for _, file := range files {
		id, ok := synclocal.ParseManagedPath(file)
		if !ok {
			id, ok = synclocal.ParseReferencePath(file)
		}
		if !ok || !slices.Contains(selected, id) {
			continue
		}
		// A symbolic link at a managed path is not a definition, but sync
		// owns the managed directory and compile rejects the link, so the
		// detach deletes it with the resource. The delete removes the link
		// itself, not its target.
		definitions = append(definitions, id)
	}
	slices.SortFunc(definitions, syncdomain.CompareResourceIDs)
	return slices.Compact(definitions), nil
}
