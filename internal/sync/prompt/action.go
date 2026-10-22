package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// CommandAction is one prompt sync operation.
type CommandAction interface {
	validate() error
}

// SyncAction reconciles the workspace once, or each time a watched file
// changes. Scope is the resource kinds that the sync plans, and Filter limits
// the sync to the resources that it names.
type SyncAction struct {
	Watch  bool
	DryRun bool
	Scope  syncdomain.Scope
	Filter syncdomain.Filter
}

// AddAction writes selected LaunchDarkly variations as local files. The
// variations come from positional selectors or from the target flags in
// Filter, which also skip the project and config questions.
type AddAction struct {
	Variations []syncdomain.ResourceID
	Filter     syncdomain.Filter
	DryRun     bool
}

// AttachAction adds a tool or a skill to a local variation, and then syncs.
// ProjectKey skips the project question.
type AttachAction struct {
	Kind       syncdomain.AttachmentKind
	Key        string
	ProjectKey string
	Target     *syncdomain.ResourceID
}

// DetachAction stops syncing selected local variations. With Archive, it
// also archives them in LaunchDarkly. The variations come from positional
// selectors or from the target flags in Filter.
type DetachAction struct {
	Variations []syncdomain.ResourceID
	Filter     syncdomain.Filter
	Archive    bool
}

// LinkAction creates a variation whose prompt is an external file, and then
// syncs. ProjectKey skips the project question.
type LinkAction struct {
	File       string
	Format     string
	ProjectKey string
	Target     *synclink.Target
}

func (action SyncAction) validate() error {
	if action.Watch && action.DryRun {
		return errors.New("watch does not support --dry-run")
	}
	return action.Filter.Validate(action.Scope.KeyKind())
}

func (action AddAction) validate() error {
	return validateSelections(action.Variations, action.Filter)
}

func (action DetachAction) validate() error {
	return validateSelections(action.Variations, action.Filter)
}

func (action AttachAction) validate() error {
	if !action.Kind.Valid() {
		return errors.New("attachment kind must be tool or skill")
	}
	if action.Target != nil && action.Target.Kind != syncdomain.KindVariation {
		return errors.New("attachment target must be a variation")
	}
	return validateProjectFlag(action.ProjectKey, action.Target)
}

func (action LinkAction) validate() error {
	switch {
	case action.File == "":
		return errors.New("linked file is required")
	case action.Format == "":
		return errors.New("--format is required")
	case action.Target != nil && action.Target.ModelConfigKey == "":
		return errors.New("--model-config-key is required with --to")
	}
	var target *syncdomain.ResourceID
	if action.Target != nil {
		target = &action.Target.Variation
	}
	if err := validateProjectFlag(action.ProjectKey, target); err != nil {
		return err
	}
	return syncreference.ValidateFormat(action.Format)
}

// validateSelections rejects variations that come both from positional
// selectors and from target flags.
func validateSelections(selectors []syncdomain.ResourceID, filter syncdomain.Filter) error {
	if err := filter.Validate(syncdomain.KindVariation); err != nil {
		return err
	}
	if len(selectors) != 0 && !filter.IsZero() {
		return errors.New("pass variations as --project, --config-key, and --key, " +
			"or as project-key/config-key/variation-key arguments, not both")
	}
	return nil
}

// validateProjectFlag rejects an unsafe --project, or one that names another
// project than --to.
func validateProjectFlag(projectKey string, target *syncdomain.ResourceID) error {
	if err := (syncdomain.Filter{Project: projectKey}).Validate(syncdomain.KindVariation); err != nil {
		return err
	}
	if projectKey != "" && target != nil && target.ProjectKey != projectKey {
		return fmt.Errorf("--to %q names project %q, but --project is %q", target.String(), target.ProjectKey, projectKey)
	}
	return nil
}

// selections returns the variations that positional selectors or --key name.
func selections(selectors []syncdomain.ResourceID, filter syncdomain.Filter) []syncdomain.ResourceID {
	if len(selectors) != 0 {
		return selectors
	}
	return filter.IDs(syncdomain.KindVariation)
}

// Options are the command input and the streams of one prompt sync command.
type Options struct {
	WorkingDirectory string
	AccessToken      string
	BaseURI          string
	OutputKind       string
	// Action is the operation to run. A nil action is a SyncAction.
	Action         CommandAction
	ConflictPolicy ConflictPolicy
	// Yes applies the plan without a confirmation prompt.
	Yes bool
	// NoInput makes each prompt for missing input an error.
	NoInput bool
	// Diff shows the full diff of each planned change, and the content of
	// each file that add creates.
	Diff        bool
	Context     context.Context
	Input       io.Reader
	Output      io.Writer
	ErrorOutput io.Writer
}

// validateOptions rejects an action whose flags conflict.
func validateOptions(options Options) error {
	if options.Action == nil {
		return nil
	}
	return options.Action.validate()
}

// withDefaults returns the options with a context and an action.
func (options Options) withDefaults() Options {
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Action == nil {
		options.Action = SyncAction{}
	}
	return options
}

// dryRun reports whether the action previews changes without writing them.
func (options Options) dryRun() bool {
	switch action := options.Action.(type) {
	case SyncAction:
		return action.DryRun
	case AddAction:
		return action.DryRun
	default:
		return false
	}
}

// watching reports whether the action is a watch.
func (options Options) watching() bool {
	action, ok := options.Action.(SyncAction)
	return ok && action.Watch
}

// syncAction returns the sync that the command runs. After an attach or a
// link, it is a sync of every resource.
func (options Options) syncAction() SyncAction {
	action, _ := options.Action.(SyncAction)
	return action
}
