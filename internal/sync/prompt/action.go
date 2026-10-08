package prompt

import (
	"context"
	"errors"
	"io"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// CommandAction is one prompt sync operation.
type CommandAction interface {
	validate() error
}

// SyncAction reconciles the workspace once, or each time a watched file changes.
type SyncAction struct {
	Watch  bool
	DryRun bool
}

// AddAction writes selected LaunchDarkly variations as local files.
type AddAction struct {
	Variations []syncdomain.ResourceID
	DryRun     bool
}

// AttachAction adds a tool or a skill to a local variation, and then syncs.
type AttachAction struct {
	Kind   syncdomain.AttachmentKind
	Key    string
	Target *syncdomain.ResourceID
}

// DetachAction stops syncing selected local variations. With Archive, it
// also archives them in LaunchDarkly.
type DetachAction struct {
	Variations []syncdomain.ResourceID
	Archive    bool
}

// LinkAction creates a variation whose prompt is an external file, and then syncs.
type LinkAction struct {
	File   string
	Format string
	Target *synclink.Target
}

func (action SyncAction) validate() error {
	if action.Watch && action.DryRun {
		return errors.New("watch does not support --dry-run")
	}
	return nil
}

func (AddAction) validate() error    { return nil }
func (DetachAction) validate() error { return nil }

func (action AttachAction) validate() error {
	if !action.Kind.Valid() {
		return errors.New("attachment kind must be tool or skill")
	}
	if action.Target != nil && action.Target.Kind != syncdomain.KindVariation {
		return errors.New("attachment target must be a variation")
	}
	return nil
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
	return syncreference.ValidateFormat(action.Format)
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
	NoInput     bool
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
