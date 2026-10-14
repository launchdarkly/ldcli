// Package prompt runs the "ldcli sync prompt" commands. A sync compares the
// local files and LaunchDarkly with the manifest, shows the plan, and applies
// it after the user agrees.
package prompt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncdetach "github.com/launchdarkly/ldcli/internal/sync/detach"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

// syncWorkspace is the Git repository that one command syncs.
type syncWorkspace struct {
	root  string
	local synclocal.Store
	// baselines keeps the sync.lock file and the remote manifest.
	baselines syncmanifest.Baselines
}

// Runner runs the prompt sync commands. Tests replace its function fields.
type Runner struct {
	client     resources.Client
	bootstrap  func(syncbootstrap.Options) error
	detach     func(syncdetach.Options) error
	link       func(synclink.Options) (string, error)
	watch      watchRunner
	isTerminal func(io.Reader, io.Writer) bool
}

// NewRunner creates a runner that calls LaunchDarkly through client.
func NewRunner(client resources.Client) Runner {
	return Runner{
		client:     client,
		bootstrap:  syncbootstrap.Run,
		detach:     syncdetach.Run,
		link:       synclink.Run,
		watch:      watchWorkspace,
		isTerminal: syncinteractive.StreamsAreTerminal,
	}
}

// Run finds the Git repository of the working directory and runs the action.
func (runner Runner) Run(options Options) error {
	if err := validateOptions(options); err != nil {
		return err
	}
	options = options.withDefaults()

	// Every path in a variation file or in the manifest is relative to the
	// repository root, so find the root first.
	resolved, err := syncsource.NewResolver().Resolve(options.WorkingDirectory)
	if err != nil {
		return err
	}
	local := synclocal.NewStore(resolved.Root)
	workspace := syncWorkspace{
		root:      resolved.Root,
		local:     local,
		baselines: syncmanifest.NewBaselineStore(syncmanifest.NewStore(runner.api(options), resolved.Source), local),
	}

	switch action := options.Action.(type) {
	case SyncAction:
		return runner.runSync(options, workspace, action)
	case AddAction:
		return runner.runAdd(options, workspace, action)
	case DetachAction:
		return runner.runDetach(options, workspace, action)
	case LinkAction:
		linked, err := runner.runLink(options, workspace, action)
		if err != nil || !linked {
			return err
		}
	case AttachAction:
		attached, err := runner.runAttach(options, workspace, action)
		if err != nil || !attached {
			return err
		}
	default:
		return fmt.Errorf("unsupported prompt sync action %T", action)
	}

	// Link and attach change only local files. One sync applies the change.
	return runner.runWorkspaceSync(options, workspace, nil)
}

// runSync syncs once, or watches the workspace. A workspace without any sync
// files starts with an add, so that the user can choose the first variations.
func (runner Runner) runSync(options Options, workspace syncWorkspace, action SyncAction) error {
	exists, err := workspace.local.Exists()
	if err != nil {
		return err
	}
	projectKeys, err := workspace.projectKeys()
	if err != nil {
		return err
	}
	if !exists && len(projectKeys) == 0 {
		if err := runner.bootstrap(runner.bootstrapOptions(options, workspace, true, action.DryRun, nil)); err != nil {
			return err
		}
		if !action.Watch {
			return nil
		}
		if exists, err = workspace.local.Exists(); err != nil || !exists {
			return err
		}
	}

	if !action.Watch {
		return runner.runWorkspaceSync(options, workspace, nil)
	}
	ctx, stop := signal.NotifyContext(options.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()
	options.Context = ctx
	return runner.watch(ctx, workspace.root, watchDebounce, func(watcher *sourceWatcher) error {
		return runner.runWorkspaceSync(options, workspace, watcher)
	}, options.ErrorOutput)
}

func (runner Runner) runAdd(options Options, workspace syncWorkspace, action AddAction) error {
	exists, err := workspace.local.Exists()
	if err != nil {
		return err
	}
	return runner.bootstrap(runner.bootstrapOptions(options, workspace, !exists, action.DryRun, action.Variations))
}

func (runner Runner) runDetach(options Options, workspace syncWorkspace, action DetachAction) error {
	projectKeys, err := workspace.projectKeys()
	if err != nil {
		return err
	}
	return runner.detach(syncdetach.Options{
		RepositoryRoot: workspace.root,
		Store:          workspace.local,
		Baselines:      workspace.baselines,
		ProjectKeys:    projectKeys,
		Input:          options.Input,
		Output:         options.Output,
		Selections:     action.Variations,
		NoInput:        options.NoInput,
	})
}

// runLink creates the linked variation file. It reports false when the user
// cancels.
func (runner Runner) runLink(options Options, workspace syncWorkspace, action LinkAction) (bool, error) {
	path, err := runner.link(synclink.Options{
		Catalog:          runner.api(options),
		Store:            workspace.local,
		RepositoryRoot:   workspace.root,
		WorkingDirectory: options.WorkingDirectory,
		File:             action.File,
		Format:           action.Format,
		Input:            options.Input,
		Output:           options.Output,
		Target:           action.Target,
		NoInput:          options.NoInput,
	})
	if err != nil || path == "" {
		return false, err
	}
	_ = syncconsole.New(options.Output).Printf("Linked %s/%s.\n", syncdomain.RootDir, path)
	return true, nil
}

// runAttach adds the attachment to a local variation. It reports false when
// the user cancels.
func (runner Runner) runAttach(options Options, workspace syncWorkspace, action AttachAction) (bool, error) {
	exists, err := workspace.local.Exists()
	if err != nil {
		return false, err
	}
	if !exists {
		return false, errors.New("attach a tool or skill after synchronizing at least one variation")
	}

	attach := attachOptions{
		RepositoryRoot: workspace.root,
		Kind:           action.Kind,
		Key:            action.Key,
		// The attach forms write to the output stream, so check that stream.
		Interactive: !options.NoInput && runner.isTerminal(options.Input, options.Output),
		Input:       options.Input,
		Output:      options.Output,
	}
	if action.Target != nil {
		attach.ProjectKey, attach.VariationID = action.Target.ProjectKey, action.Target.LookupKey
	}
	err = attachToVariation(workspace.local, runner.api(options), attach)
	if errors.Is(err, errAttachmentCanceled) {
		return false, nil
	}
	return err == nil, err
}

func (runner Runner) bootstrapOptions(
	options Options,
	workspace syncWorkspace,
	initial, dryRun bool,
	selections []syncdomain.ResourceID,
) syncbootstrap.Options {
	api := runner.api(options)
	return syncbootstrap.Options{
		Catalog:     api,
		Attachments: api,
		Store:       workspace.local,
		Baselines:   workspace.baselines,
		Input:       options.Input,
		Output:      options.Output,
		Initial:     initial,
		DryRun:      dryRun,
		Selections:  selections,
		NoInput:     options.NoInput,
	}
}

// api returns the LaunchDarkly client for the command.
func (runner Runner) api(options Options) syncapi.Client {
	return syncapi.NewClient(runner.client, options.AccessToken, options.BaseURI)
}

// interactive reports whether the command can ask the user for input.
func (runner Runner) interactive(options Options) bool {
	return !options.NoInput && runner.isTerminal(options.Input, options.ErrorOutput)
}
