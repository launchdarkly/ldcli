// Package prompt runs the "ldcli sync prompts" commands. A sync compares the
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

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncdetach "github.com/launchdarkly/ldcli/internal/sync/detach"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
	syncrepository "github.com/launchdarkly/ldcli/internal/sync/repository"
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
	// cliVersion is the ldcli version that each LaunchDarkly request names.
	cliVersion string
	bootstrap  func(syncbootstrap.Options) error
	detach     func(syncdetach.Options) error
	link       func(synclink.Options) (string, error)
	watch      watchRunner
	isTerminal func(io.Reader, io.Writer) bool
	// revisions finds the commit and the pull request of the repository.
	revisions syncrepository.RevisionReader
}

// NewRunner creates a runner. Each LaunchDarkly request names cliVersion in
// its User-Agent header.
func NewRunner(cliVersion string) Runner {
	return Runner{
		cliVersion: cliVersion,
		bootstrap:  syncbootstrap.Run,
		detach:     syncdetach.Run,
		link:       synclink.Run,
		watch:      watchWorkspace,
		isTerminal: syncinteractive.StreamsAreTerminal,
		revisions:  syncrepository.NewRevisionReader(),
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
	// Each write to LaunchDarkly names the repository, so that LaunchDarkly
	// can tell a sync write from another write.
	options.syncSource = resolved.Source
	options.revision = runner.revision(resolved.Root)
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
	case SourceOfTruthAction:
		return runner.runSourceOfTruth(options, workspace, action)
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
		if err := runner.bootstrap(runner.bootstrapOptions(options, workspace, true, action.DryRun, "", nil)); err != nil {
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
		// HEAD can change while the watch runs, so find it for each cycle.
		cycle := options
		cycle.revision = runner.revision(workspace.root)
		return runner.runWorkspaceSync(cycle, workspace, watcher)
	}, options.ErrorOutput)
}

func (runner Runner) runAdd(options Options, workspace syncWorkspace, action AddAction) error {
	exists, err := workspace.local.Exists()
	if err != nil {
		return err
	}
	return runner.bootstrap(runner.bootstrapOptions(options, workspace, !exists, action.DryRun, action.Direction, action.Variations))
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
		Archive:        action.Archive,
		Archiver:       runner.api(options),
		Yes:            options.Yes,
		Context:        options.Context,
		Settings:       runner.api(options),
		ErrorOutput:    options.ErrorOutput,
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
	direction syncdomain.Direction,
	selections []syncdomain.ResourceID,
) syncbootstrap.Options {
	api := runner.api(options)
	return syncbootstrap.Options{
		Settings:    api,
		Direction:   direction,
		ErrorOutput: options.ErrorOutput,
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
	return syncapi.NewClient(syncapi.Options{
		BaseURI:     options.BaseURI,
		AccessToken: options.AccessToken,
		CLIVersion:  runner.cliVersion,
		Sync:        syncapi.SyncIdentity{Source: options.syncSource, Commit: options.revision.Commit},
	})
}

// revision returns the commit and the pull request of the repository at root.
// A runner without a revision reader knows neither.
func (runner Runner) revision(root string) syncrepository.Revision {
	if runner.revisions == nil {
		return syncrepository.Revision{}
	}
	return runner.revisions.Revision(root)
}

// interactive reports whether the command can ask the user for input.
func (runner Runner) interactive(options Options) bool {
	return !options.NoInput && runner.isTerminal(options.Input, options.ErrorOutput)
}
