package bootstrap

import (
	"errors"
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// addAttachments writes the files of the selected tools or skills from
// LaunchDarkly and records their baseline. It checks and reads every
// selection before it writes, so an error writes nothing.
func addAttachments(options Options) error {
	selections := options.Selections
	if len(selections) == 0 {
		var canceled bool
		var err error
		if selections, canceled, err = promptForAttachment(options); err != nil || canceled {
			return err
		}
	}
	attachments, err := readNewAttachments(options, selections)
	if err != nil {
		return err
	}
	if options.DryRun {
		previews := make([]synclocal.RenderedFile, 0, len(attachments))
		for _, attachment := range attachments {
			preview, err := synclocal.RenderAttachment(attachment)
			if err != nil {
				return err
			}
			previews = append(previews, preview)
		}
		writePreviews(options.Output, previews, options.Diff)
		return nil
	}
	return writeAttachments(options, attachments)
}

// promptForAttachment asks the user for a project and for one tool or skill.
// ProjectKey skips the project question. The bool result is true when the
// user cancels.
func promptForAttachment(options Options) ([]syncdomain.ResourceID, bool, error) {
	kind := syncdomain.AttachmentKind(options.Kind)
	arguments := "--project and --key"
	if options.ProjectKey != "" {
		arguments = "--key values"
	}
	if err := syncinteractive.RequireTerminal(options.Input, options.Output, options.NoInput, arguments, string(kind)+" selection"); err != nil {
		return nil, false, err
	}

	projectKey := options.ProjectKey
	if projectKey == "" {
		project, canceled, err := syncinteractive.SelectProject(options.Input, options.Output, options.Catalog)
		if err != nil || canceled {
			return nil, canceled, err
		}
		projectKey = project.Key
	}
	attachment, canceled, err := syncinteractive.SelectAttachment(options.Input, options.Output, options.Attachments, projectKey, kind)
	if err != nil || canceled {
		return nil, canceled, err
	}
	return []syncdomain.ResourceID{attachment.ID(projectKey)}, false, nil
}

// readNewAttachments reads each selected tool or skill from LaunchDarkly. A
// selection that this repository already syncs, or that LaunchDarkly does not
// have, is an error. The local checks come first, so they need no read.
func readNewAttachments(options Options, selections []syncdomain.ResourceID) ([]syncdomain.SyncedAttachment, error) {
	if err := CheckNotSynced(options.Store, selections); err != nil {
		return nil, err
	}

	attachments := make([]syncdomain.SyncedAttachment, 0, len(selections))
	for _, id := range selections {
		attachment, err := options.Attachments.ReadAttachment(id.ProjectKey, syncdomain.AttachmentKind(id.Kind), id.LookupKey)
		if syncapi.IsNotFound(err) {
			return nil, fmt.Errorf("%s does not exist in LaunchDarkly. To create it, run %s with %s.",
				id.Label(), AddCommand(id), createFlags[id.Kind])
		}
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, syncdomain.SyncedAttachment{ProjectKey: id.ProjectKey, Attachment: attachment})
	}
	return attachments, nil
}

// createFlags names the flags of an add that creates a tool or a skill.
var createFlags = map[syncdomain.Kind]string{
	syncdomain.KindTool:  "--json-schema, and optionally --description",
	syncdomain.KindSkill: "--content, and optionally --name and --description",
}

// AddCommand is the command that starts syncing one resource.
func AddCommand(id syncdomain.ResourceID) string {
	if configKey, variationKey, err := id.VariationKeys(); err == nil {
		return fmt.Sprintf("ldcli sync prompts add --project %s --config-key %s --key %s", id.ProjectKey, configKey, variationKey)
	}
	return fmt.Sprintf("ldcli sync %ss add --project %s --key %s", id.Kind, id.ProjectKey, id.LookupKey)
}

// CheckNotSynced returns an error for a tool or skill that this repository
// already syncs, because it has a local file or a sync.lock entry. It reads
// only local files.
func CheckNotSynced(store synclocal.Store, ids []syncdomain.ResourceID) error {
	lock, err := syncmanifest.ReadLock(store)
	if err != nil {
		return err
	}
	for _, id := range ids {
		exists, err := store.AttachmentExists(id)
		switch {
		case err != nil:
			return err
		case exists:
			path, err := synclocal.AttachmentPath(id)
			if err != nil {
				return err
			}
			return fmt.Errorf("%s is already synced. Edit %s and run ldcli sync %ss or ldcli sync all.", id.Label(), path, id.Kind)
		case slices.ContainsFunc(lock.Resources, func(resource syncmanifest.Resource) bool { return resource.ID() == id }):
			return fmt.Errorf("%s has a sync.lock entry but no local file. Run ldcli sync %ss or ldcli sync all to restore it.", id.Label(), id.Kind)
		}
	}
	return nil
}

// writeAttachments writes the files and records their baseline. If the
// baseline save fails, it removes the files again.
func writeAttachments(options Options, attachments []syncdomain.SyncedAttachment) error {
	projectKeys := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		projectKeys = append(projectKeys, attachment.ProjectKey)
	}
	baseline, err := options.Baselines.Load(projectKeys)
	if err != nil {
		return err
	}

	paths, err := options.Store.ReplaceAttachments(attachments)
	if err != nil {
		return err
	}
	next := baseline.Lock.Clone()
	for _, attachment := range attachments {
		if err = next.SetAttachments(attachment.ProjectKey, []syncdomain.Attachment{attachment.Attachment}); err != nil {
			break
		}
	}
	if err == nil {
		_, err = options.Baselines.Save(baseline, next)
	}
	if err != nil {
		return errors.Join(err, options.Store.RollbackCreation(synclocal.Creation{CreatedPaths: paths}))
	}
	writeSummary(options.Output, options.Initial, len(paths), string(options.Kind))
	return nil
}
