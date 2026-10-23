package prompt

import (
	"errors"
	"fmt"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	synccreate "github.com/launchdarkly/ldcli/internal/sync/create"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

// runCreate creates one tool or skill from the create flags of add. Add never
// changes a resource that LaunchDarkly has. It starts syncing one that has the
// content of the flags, and refuses one with other content.
func (runner Runner) runCreate(options Options, workspace syncWorkspace, action AddAction) error {
	client := runner.api(options)
	questions := synccreate.Questions{
		// The forms write to the output stream, so check that stream.
		Allowed:  !options.NoInput && runner.isTerminal(options.Input, options.Output),
		Input:    options.Input,
		Output:   options.Output,
		Projects: client,
	}
	var key string
	if len(action.Filter.Keys) == 1 {
		key = action.Filter.Keys[0]
	}
	id, canceled, err := synccreate.Target(options.Scope.KeyKind(), action.Filter.Project, key, questions)
	if err != nil || canceled {
		return err
	}

	// The values of the flags must pass the create rules before the first read.
	input := *action.Create
	created, err := input.Attachment(id)
	if err != nil {
		return err
	}
	if err := syncbootstrap.CheckNotSynced(workspace.local, []ResourceID{id}); err != nil {
		return err
	}
	existing, err := client.ReadAttachment(id.ProjectKey, created.Kind, id.LookupKey)
	switch {
	case err == nil:
		return runner.adoptAttachment(options, workspace, action, id, created, existing)
	case !syncapi.IsNotFound(err):
		return err
	}

	// Only a skill that add creates needs a name and a description.
	if input, canceled, err = input.Ask(id, questions); err != nil || canceled {
		return err
	}
	if created, err = input.Attachment(id); err != nil {
		return err
	}
	return runner.createAttachment(options, workspace, client, syncdomain.SyncedAttachment{
		ProjectKey: id.ProjectKey, Attachment: created,
	}, input.Name != nil)
}

// adoptAttachment starts syncing a tool or skill that LaunchDarkly already
// has, with the add of a resource that exists. It refuses when a flag that the
// user passed differs from LaunchDarkly.
func (runner Runner) adoptAttachment(
	options Options,
	workspace syncWorkspace,
	action AddAction,
	id ResourceID,
	created, existing syncdomain.Attachment,
) error {
	if !action.Create.Matches(created, existing) {
		return fmt.Errorf("%s already exists in LaunchDarkly with different content, and add does not change it. "+
			"To sync the LaunchDarkly version, run %s, then edit the file and run ldcli sync %ss or ldcli sync all.", id.Label(), syncbootstrap.AddCommand(id), id.Kind)
	}
	adopt := AddAction{Filter: syncdomain.Filter{Project: id.ProjectKey, Keys: []string{id.LookupKey}}, DryRun: action.DryRun}
	if err := runner.runAdd(options, workspace, adopt); err != nil {
		return err
	}

	notes := syncconsole.New(options.ErrorOutput)
	started := "started"
	if action.DryRun {
		started = "will start"
	}
	_ = notes.Printf("Note: %s already exists in LaunchDarkly with this content, so add %s syncing it.\n", id.Label(), started)
	if name := action.Create.Name; name != nil && existing.Skill != nil && *name != existing.Skill.Name {
		_ = notes.Printf("Note: %s keeps its LaunchDarkly name %q.\n", id.Label(), existing.Skill.Name)
	}
	return nil
}

// createAttachment creates a tool or skill that LaunchDarkly does not have. It
// plans the create in memory and asks before it writes the file, so that a
// cancel leaves nothing and a watch cannot create the resource before the
// answer. named is true when the user chose the name of a skill.
func (runner Runner) createAttachment(
	options Options,
	workspace syncWorkspace,
	client syncapi.Client,
	created syncdomain.SyncedAttachment,
	named bool,
) error {
	path, err := synclocal.AttachmentPath(created.ID())
	if err != nil {
		return err
	}
	reviewed := reviewedCreate(options.Scope, created, named)
	if options.dryRun() {
		if err := writePlanOutput(options.Output, options.OutputKind, reviewed.plan, options.Diff); err != nil {
			return err
		}
		return reviewed.plan.BlockingError()
	}
	proceed, err := reviewAndConfirmPlan(options, reviewed.plan, runner.interactive(options))
	if err != nil || !proceed {
		return err
	}

	// A file can appear while the question waits.
	if err := syncbootstrap.CheckNotSynced(workspace.local, []ResourceID{created.ID()}); err != nil {
		return err
	}
	written, err := workspace.local.ReplaceAttachments([]syncdomain.SyncedAttachment{created})
	if err != nil {
		return err
	}
	return runner.applyCreate(options, workspace, client, reviewed, path, synclocal.Creation{CreatedPaths: written})
}

// reviewedCreate is the state that the user reviews before a create: the
// create row of the new resource, with no file yet.
func reviewedCreate(scope syncdomain.Scope, created syncdomain.SyncedAttachment, named bool) workspaceState {
	id := created.ID()
	row := planAttachment(id, &created.Attachment, nil, "", false)
	if id.Kind == syncdomain.KindSkill {
		row.nameSource = nameFromKey
		if named {
			row.nameSource = nameFromFlag
		}
	}
	return workspaceState{projectKeys: []string{id.ProjectKey}, plan: Plan{Scope: scope, Resources: []PlannedResource{row}}}
}

// applyCreate applies the reviewed create after it wrote the new file at path.
// The apply is the normal sync of the new resource only, so that other pending
// changes stay as they are. If the state changed after the review, or if
// LaunchDarkly rejects the create, applyCreate removes the file. After a write
// with no response, it keeps the file, so that the next sync creates the
// resource or finds it.
func (runner Runner) applyCreate(
	options Options,
	workspace syncWorkspace,
	client syncapi.Client,
	reviewed workspaceState,
	path string,
	creation synclocal.Creation,
) error {
	notes := syncconsole.New(options.ErrorOutput)
	remove := func(cause error) error {
		if err := workspace.local.RollbackCreation(creation); err != nil {
			return errors.Join(cause, err)
		}
		_ = notes.Printf("Note: Removed %s.\n", path)
		return cause
	}

	id := reviewed.plan.Resources[0].ID
	current, err := workspace.loadState(client, options.Scope, syncdomain.Filter{Project: id.ProjectKey, Keys: []string{id.LookupKey}})
	if err == nil {
		err = checkUnchanged(options, reviewed, current)
	}
	if err != nil {
		return remove(err)
	}
	// The new file has the name of the skill, but only the reviewed row knows
	// where the name came from.
	current.plan.Resources[0].nameSource = reviewed.plan.Resources[0].nameSource

	outcomes, err := runner.applyPlan(options, workspace, client, current, nil)
	switch {
	case err == nil:
		_ = notes.Printf("Note: Created %s.\n", path)
	case len(outcomes) == 1 && outcomes[0].Status == OutcomeFailed && !syncapi.MutationMayHaveSucceeded(err):
		return remove(err)
	}
	return err
}
