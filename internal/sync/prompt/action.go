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

// CommandAction is one sync operation. scope is the scope of the command.
type CommandAction interface {
	validate(scope syncdomain.Scope) error
}

// SyncAction reconciles the workspace once, or each time a watched file
// changes. Filter limits the sync to the resources that it names.
type SyncAction struct {
	Watch  bool
	DryRun bool
	Filter syncdomain.Filter
}

// AddAction writes selected LaunchDarkly resources of the kind of the scope
// as local files. The resources come from positional selectors or from the
// target flags in Filter, which also skip the project and config questions.
type AddAction struct {
	Resources []syncdomain.ResourceID
	Filter    syncdomain.Filter
	DryRun    bool
}

// AttachAction adds a tool or a skill to a local variation, and then syncs.
// ProjectKey skips the project question.
type AttachAction struct {
	Kind       syncdomain.AttachmentKind
	Key        string
	ProjectKey string
	Target     *syncdomain.ResourceID
}

// DetachAction stops syncing selected local resources. With Archive, it
// also archives the variations in LaunchDarkly. The resources come from
// positional selectors or from the target flags in Filter.
type DetachAction struct {
	Resources []syncdomain.ResourceID
	Filter    syncdomain.Filter
	Archive   bool
}

// LinkAction creates a variation whose prompt is an external file, and then
// syncs. ProjectKey skips the project question.
type LinkAction struct {
	File       string
	Format     string
	ProjectKey string
	Target     *synclink.Target
}

func (action SyncAction) validate(scope syncdomain.Scope) error {
	if action.Watch && action.DryRun {
		return errors.New("watch does not support --dry-run")
	}
	return action.Filter.Validate(scope.KeyKind())
}

func (action AddAction) validate(scope syncdomain.Scope) error {
	return validateSelections(action.Resources, action.Filter, scope.KeyKind())
}

func (action DetachAction) validate(scope syncdomain.Scope) error {
	return validateSelections(action.Resources, action.Filter, scope.KeyKind())
}

func (action AttachAction) validate(syncdomain.Scope) error {
	if !action.Kind.Valid() {
		return errors.New("attachment kind must be tool or skill")
	}
	if action.Target != nil && action.Target.Kind != syncdomain.KindVariation {
		return errors.New("attachment target must be a variation")
	}
	return validateProjectFlag(action.ProjectKey, action.Target)
}

func (action LinkAction) validate(syncdomain.Scope) error {
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

// validateSelections rejects positional selectors outside the prompts scope,
// and resources that come both from positional selectors and from target
// flags. kind is the kind that --key names.
func validateSelections(selectors []syncdomain.ResourceID, filter syncdomain.Filter, kind syncdomain.Kind) error {
	if err := filter.Validate(kind); err != nil {
		return err
	}
	switch {
	case len(selectors) == 0:
		return nil
	case kind != syncdomain.KindVariation:
		return fmt.Errorf("only variations have positional selectors. Pass %ss as --project and --key.", kindNoun(kind))
	case !filter.IsZero():
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

// selections returns the resources that positional selectors or --key name.
// kind is the kind that --key names.
func selections(selectors []syncdomain.ResourceID, filter syncdomain.Filter, kind syncdomain.Kind) []syncdomain.ResourceID {
	if len(selectors) != 0 {
		return selectors
	}
	return filter.IDs(kind)
}

// Options are the command input and the streams of one prompt sync command.
type Options struct {
	WorkingDirectory string
	AccessToken      string
	BaseURI          string
	OutputKind       string
	// Scope is the resource kinds that the command syncs, adds, or detaches.
	// The empty scope is prompt variations.
	Scope syncdomain.Scope
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
	return options.Action.validate(options.Scope)
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
