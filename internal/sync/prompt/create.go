package prompt

import (
	"errors"
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	synccreate "github.com/launchdarkly/ldcli/internal/sync/create"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

// runCreate creates one resource from the create flags of add. Add never
// changes a resource that LaunchDarkly has. It starts syncing one that has the
// content of the flags, and refuses one with other content.
func (runner Runner) runCreate(options Options, workspace syncWorkspace, action AddAction) error {
	if options.Scope.KeyKind() == syncdomain.KindVariation {
		return runner.createVariation(options, workspace, action)
	}
	return runner.createAttachment(options, workspace, action)
}

// createAttachment creates a tool or a skill.
func (runner Runner) createAttachment(options Options, workspace syncWorkspace, action AddAction) error {
	client := runner.api(options)
	questions := runner.createQuestions(options, client)
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
		if err := runner.adopt(options, workspace, action, id, input.Matches(created, existing)); err != nil {
			return err
		}
		if name := input.Name; name != nil && existing.Skill != nil && *name != existing.Skill.Name {
			_ = syncconsole.New(options.ErrorOutput).Printf("Note: %s keeps its LaunchDarkly name %q.\n", id.Label(), existing.Skill.Name)
		}
		return nil
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
	file := syncdomain.SyncedAttachment{ProjectKey: id.ProjectKey, Attachment: created}
	row := planAttachment(id, &file.Attachment, nil, "", false)
	if id.Kind == syncdomain.KindSkill {
		row.nameSource = nameFromKey
		if input.Name != nil {
			row.nameSource = nameFromFlag
		}
	}
	return runner.createResource(options, workspace, client, row, func() (synclocal.Creation, error) {
		paths, err := workspace.local.ReplaceAttachments([]syncdomain.SyncedAttachment{file})
		return synclocal.Creation{CreatedPaths: paths}, err
	})
}

// createVariation creates a prompt variation in a config that exists.
func (runner Runner) createVariation(options Options, workspace syncWorkspace, action AddAction) error {
	client := runner.api(options)
	questions := runner.createQuestions(options, client)
	input := *action.Create
	id, canceled, err := input.VariationTarget(client, action.Filter, questions)
	if err != nil || canceled {
		return err
	}
	if err := syncbootstrap.CheckNotSynced(workspace.local, []ResourceID{id}); err != nil {
		return err
	}

	configKey, variationKey, err := id.VariationKeys()
	if err != nil {
		return err
	}
	config, err := client.Config(id.ProjectKey, configKey)
	if err != nil {
		return err
	}
	if index := slices.IndexFunc(config.Variations, func(existing syncdomain.Variation) bool { return existing.Key == variationKey }); index >= 0 {
		matches, err := input.MatchesVariation(id, config.Variations[index])
		if err != nil {
			return err
		}
		return runner.adopt(options, workspace, action, id, matches)
	}
	if slices.Contains(config.ArchivedVariationKeys, variationKey) {
		return fmt.Errorf("%s is archived in LaunchDarkly, and a new variation cannot use its key. Pass another --key.", id.Label())
	}

	variation, canceled, err := input.Variation(client, id, config, questions)
	if err != nil || canceled {
		return err
	}
	file := syncdomain.SyncedResource{
		Kind: syncdomain.KindVariation, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey, Upsert: true, Variation: variation,
	}
	row := planResource(id, &file, ServerResource{ConfigMode: config.Mode}, "", false)
	return runner.createResource(options, workspace, client, row, func() (synclocal.Creation, error) {
		return workspace.local.Add([]synclocal.VariationFile{{
			ProjectKey: id.ProjectKey, ConfigKey: configKey, Upsert: true, Variation: variation,
		}})
	})
}

// createQuestions are the questions of a create. The forms write to the
// output stream, so the check for a terminal uses that stream.
func (runner Runner) createQuestions(options Options, client syncapi.Client) synccreate.Questions {
	return synccreate.Questions{
		Allowed:  !options.NoInput && runner.isTerminal(options.Input, options.Output),
		Input:    options.Input,
		Output:   options.Output,
		Projects: client,
	}
}

// adopt starts syncing a resource that LaunchDarkly already has, with the add
// of a resource that exists. matches is false when a flag that the user passed
// differs from LaunchDarkly, and then adopt refuses.
func (runner Runner) adopt(options Options, workspace syncWorkspace, action AddAction, id ResourceID, matches bool) error {
	if !matches {
		return fmt.Errorf("%s already exists in LaunchDarkly with different content, and add does not change it. "+
			"To sync the LaunchDarkly version, run %s, then edit the file and run %s or ldcli sync all.",
			id.Label(), syncbootstrap.AddCommand(id), syncbootstrap.SyncCommand(id.Kind))
	}
	if err := runner.runAdd(options, workspace, AddAction{Filter: resourceFilter(id), DryRun: action.DryRun}); err != nil {
		return err
	}

	started := "started"
	if action.DryRun {
		started = "will start"
	}
	_ = syncconsole.New(options.ErrorOutput).Printf(
		"Note: %s already exists in LaunchDarkly with this content, so add %s syncing it.\n", id.Label(), started,
	)
	return nil
}

// createResource creates a resource that LaunchDarkly does not have. row is
// the create row of the resource, and write writes its file. It plans the
// create in memory and asks before it writes the file, so that a cancel
// leaves nothing and a watch cannot create the resource before the answer.
func (runner Runner) createResource(
	options Options,
	workspace syncWorkspace,
	client syncapi.Client,
	row PlannedResource,
	write func() (synclocal.Creation, error),
) error {
	path, err := synclocal.ResourcePath(row.ID)
	if err != nil {
		return err
	}
	reviewed := workspaceState{projectKeys: []string{row.ID.ProjectKey}, plan: Plan{Scope: options.Scope, Resources: []PlannedResource{row}}}
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
	if err := syncbootstrap.CheckNotSynced(workspace.local, []ResourceID{row.ID}); err != nil {
		return err
	}
	creation, err := write()
	if err != nil {
		return err
	}
	return runner.applyCreate(options, workspace, client, reviewed, path, creation)
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
	current, err := workspace.loadState(client, options.Scope, resourceFilter(id))
	if err == nil {
		err = checkUnchanged(options, reviewed, current)
	}
	if err != nil {
		return remove(err)
	}
	// The new file of a skill has its name, but only the reviewed row knows
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

// resourceFilter is the filter that keeps only one resource.
func resourceFilter(id ResourceID) syncdomain.Filter {
	if configKey, variationKey, err := id.VariationKeys(); err == nil {
		return syncdomain.Filter{Project: id.ProjectKey, ConfigKey: configKey, Keys: []string{variationKey}}
	}
	return syncdomain.Filter{Project: id.ProjectKey, Keys: []string{id.LookupKey}}
}
