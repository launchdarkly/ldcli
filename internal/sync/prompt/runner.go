package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
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
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

// Options contains command input and streams for one prompt synchronization.
type Options struct {
	WorkingDirectory string
	AccessToken      string
	BaseURI          string
	OutputKind       string
	Add              bool
	Detach           bool
	DryRun           bool
	Format           string
	Link             string
	Watch            bool
	Yes              bool
	Context          context.Context
	Input            io.Reader
	Output           io.Writer
	ErrorOutput      io.Writer
	watcher          *sourceWatcher
}

type bootstrapRunner func(syncbootstrap.Options) error
type detachRunner func(syncdetach.Options) error
type linkRunner func(synclink.Options) (string, error)

type syncWorkspace struct {
	root     string
	local    synclocal.Store
	manifest syncmanifest.Store
}

type localFileResourcesByID map[ResourceID]syncdomain.SyncedResource

type attachmentID struct {
	projectKey string
	kind       syncdomain.AttachmentKind
	key        string
}

// Runner coordinates prompt synchronization using existing config APIs.
type Runner struct {
	client     resources.Client
	bootstrap  bootstrapRunner
	detach     detachRunner
	link       linkRunner
	watch      watchRunner
	isTerminal terminalCheck
}

// NewRunner creates a prompt synchronization runner.
func NewRunner(client resources.Client) Runner {
	return Runner{
		client: client, bootstrap: syncbootstrap.Run, detach: syncdetach.Run, link: synclink.Run,
		watch: watchWorkspace, isTerminal: syncinteractive.StreamsAreTerminal,
	}
}

// Run resolves the Git workspace and performs the requested prompt sync flow.
func (runner Runner) Run(options Options) error {
	if err := validateOptions(options); err != nil {
		return err
	}
	// Every path stored in wrappers or the manifest is repository-relative, so
	// resolve the canonical Git root before dispatching any command mode.
	resolvedWorkspace, err := syncsource.NewResolver().Resolve(options.WorkingDirectory)
	if err != nil {
		return err
	}
	workspace := syncWorkspace{
		root:     resolvedWorkspace.Root,
		local:    synclocal.NewStore(resolvedWorkspace.Root),
		manifest: syncmanifest.NewStore(resolvedWorkspace.Root),
	}
	apiClient := syncapi.NewClient(runner.client, options.AccessToken, options.BaseURI)

	if options.Detach {
		return runner.detach(syncdetach.Options{
			RepositoryRoot: workspace.root,
			Store:          workspace.local,
			Manifest:       workspace.manifest,
			Input:          options.Input,
			Output:         options.Output,
		})
	}
	if options.Link != "" {
		path, err := runner.link(synclink.Options{
			Catalog:          apiClient,
			Store:            workspace.local,
			RepositoryRoot:   workspace.root,
			WorkingDirectory: options.WorkingDirectory,
			File:             options.Link,
			Format:           options.Format,
			Input:            options.Input,
			Output:           options.Output,
		})
		if err != nil {
			return err
		}
		if path == "" {
			return nil
		}
		_ = syncconsole.New(options.Output).Printf(
			"Linked %s/%s.\n",
			syncdomain.RootDir,
			path,
		)
	}
	localDirectoryExists, err := workspace.local.Exists()
	if err != nil {
		return err
	}

	if !localDirectoryExists || options.Add {
		if err := runner.bootstrap(syncbootstrap.Options{
			Catalog:     apiClient,
			Attachments: apiClient,
			Store:       workspace.local,
			Manifest:    workspace.manifest,
			Input:       options.Input,
			Output:      options.Output,
			Initial:     !localDirectoryExists,
			DryRun:      options.DryRun,
		}); err != nil {
			return err
		}
		if !options.Watch {
			return nil
		}
		localDirectoryExists, err = workspace.local.Exists()
		if err != nil {
			return err
		}
		if !localDirectoryExists {
			return nil
		}
	}

	if options.Watch {
		ctx := options.Context
		if ctx == nil {
			ctx = context.Background()
		}
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		syncOptions := optionsForWatchSync(ctx, options)
		// Watch owns the retry loop. Each callback still runs the exact same
		// plan, review, revalidation, and execution pipeline as a normal sync.
		return runner.watch(ctx, workspace.root, watchDebounce, func(watcher *sourceWatcher) error {
			syncOptions.watcher = watcher
			return runner.runWorkspaceSync(syncOptions, workspace)
		}, options.ErrorOutput)
	}

	return runner.runWorkspaceSync(options, workspace)
}

// optionsForWatchSync clears one-time actions while preserving explicit user
// choices such as --yes for each sync triggered by the watcher.
func optionsForWatchSync(ctx context.Context, options Options) Options {
	options.Add = false
	options.Format = ""
	options.Link = ""
	options.Context = ctx
	return options
}

// runWorkspaceSync plans, reviews, revalidates, and executes one workspace sync.
func (runner Runner) runWorkspaceSync(options Options, workspace syncWorkspace) error {
	var watched *watchedSources
	if options.Watch {
		if options.watcher == nil {
			return fmt.Errorf("watch mode requires an initialized file watcher")
		}
		snapshot, err := sourceSnapshot(workspace.root)
		if err != nil {
			return err
		}
		watched = &watchedSources{
			watcher:  options.watcher,
			snapshot: snapshot,
			debounce: watchDebounce,
		}
	}

	apiClient := syncapi.NewClient(runner.client, options.AccessToken, options.BaseURI)
	// The manifest is the common ancestor in a three-way comparison between
	// current local files and current LaunchDarkly state.
	baseline, _, err := workspace.manifest.Load()
	if err != nil {
		return err
	}
	reviewedPlan, _, err := loadWorkspacePlan(workspace.root, baseline, apiClient)
	if err != nil {
		return err
	}

	if options.DryRun {
		if err := writePlanOutput(options.Output, options.OutputKind, reviewedPlan); err != nil {
			return err
		}
		return reviewedPlan.BlockingError()
	}

	interactive := runner.isTerminal(options.Input, options.ErrorOutput)
	conflictResult, err := resolveConflicts(options, reviewedPlan, options.Input, interactive, watched)
	if err != nil {
		return err
	}
	if conflictResult.sourcesChanged {
		return errRefreshWatchPlan
	}
	if conflictResult.aborted {
		return nil
	}

	resolvedPlan := applyConflictResolutions(reviewedPlan, conflictResult.resolutions)
	shouldContinue, err := reviewAndConfirmPlan(options, resolvedPlan, interactive)
	if err != nil || !shouldContinue {
		return err
	}

	// Re-read both sides after review so no action uses stale state.
	currentManifest, _, err := workspace.manifest.Load()
	if err != nil {
		return err
	}
	currentPlan, currentLocalFiles, err := loadWorkspacePlan(workspace.root, currentManifest, apiClient)
	if err != nil {
		return err
	}
	if !samePlanState(reviewedPlan, currentPlan) {
		if options.Watch {
			return errRefreshWatchPlan
		}
		return fmt.Errorf("sync state changed after review; run sync again")
	}

	// Apply the user's conflict choices to freshly read state, never to the
	// potentially stale objects that were rendered during review.
	currentPlan = applyConflictResolutions(currentPlan, conflictResult.resolutions)
	outcomes, updatedManifest, executionErr := executePlan(
		workspace.root,
		workspace.local,
		apiClient,
		currentManifest,
		currentPlan,
		currentLocalFiles,
	)
	if err := workspace.manifest.Write(updatedManifest); err != nil {
		executionErr = errors.Join(executionErr, err)
	}
	if err := workspace.local.RemoveEmptyDirectories(); err != nil {
		executionErr = errors.Join(executionErr, err)
	}
	if err := writeOutcomeOutput(options.Output, options.OutputKind, outcomes); err != nil {
		executionErr = errors.Join(executionErr, err)
	}
	return executionErr
}

// validateOptions rejects command modes whose side effects or UX conflict.
func validateOptions(options Options) error {
	switch {
	case options.Detach && (options.Add || options.DryRun || options.Link != "" || options.Format != "" || options.Watch || options.Yes):
		return fmt.Errorf("--detach cannot be combined with other sync actions")
	case options.Link == "" && options.Format != "":
		return fmt.Errorf("--format requires --link")
	case options.Link != "" && options.Format == "":
		return fmt.Errorf("--link requires --format")
	case options.Link != "" && (options.Add || options.DryRun):
		return fmt.Errorf("--link cannot be used with --add or --dry-run")
	case options.Watch && options.DryRun:
		return fmt.Errorf("--watch cannot be used with --dry-run")
	}
	if options.Link != "" {
		return syncreference.ValidateFormat(options.Format)
	}
	return nil
}

// loadWorkspacePlan keeps local file data separate from the canonical local
// resources used by the three-way plan.
func loadWorkspacePlan(
	repositoryRoot string,
	baseline syncmanifest.Manifest,
	client syncapi.Client,
) (Plan, localFileResourcesByID, error) {
	localFileResources, err := synclocal.CompileWorkspace(repositoryRoot)
	if err != nil {
		return Plan{}, nil, err
	}
	canonicalLocalResources, err := canonicalizeLocalVariationModels(localFileResources, client.ModelConfig)
	if err != nil {
		return Plan{}, nil, err
	}

	localFilesByID := make(localFileResourcesByID, len(localFileResources))
	resourceIDs := make(map[ResourceID]struct{}, len(localFileResources)+len(baseline.Resources))
	for _, resource := range localFileResources {
		id := ResourceID{Kind: resource.Kind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey}
		localFilesByID[id] = resource
		resourceIDs[id] = struct{}{}
	}
	for _, resource := range baseline.Resources {
		if resource.ResourceKind == syncdomain.KindVariation {
			resourceIDs[resource.ID()] = struct{}{}
		}
	}

	serverResources := make(map[ResourceID]ServerResource, len(resourceIDs))
	attachments := newAttachmentHydrator(client)
	for id := range resourceIDs {
		resource, err := readServerResource(client, attachments, id)
		if err != nil {
			return Plan{}, nil, err
		}
		serverResources[id] = resource
	}
	return BuildPlan(baseline, canonicalLocalResources, serverResources), localFilesByID, nil
}

// attachmentHydrator reads each shared dependency once while building a plan.
type attachmentHydrator struct {
	client syncapi.Client
	cache  map[attachmentID]syncdomain.Attachment
}

func newAttachmentHydrator(client syncapi.Client) *attachmentHydrator {
	return &attachmentHydrator{client: client, cache: make(map[attachmentID]syncdomain.Attachment)}
}

func (hydrator *attachmentHydrator) read(projectKey string, kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
	id := attachmentID{projectKey: projectKey, kind: kind, key: key}
	if attachment, ok := hydrator.cache[id]; ok {
		return attachment, nil
	}
	attachment, err := hydrator.client.ReadAttachment(projectKey, kind, key)
	if err != nil {
		return syncdomain.Attachment{}, err
	}
	hydrator.cache[id] = attachment
	return attachment, nil
}

func (hydrator *attachmentHydrator) hydrate(projectKey string, variation *syncdomain.Variation) error {
	variation.Attachments = make([]syncdomain.Attachment, 0, len(variation.Tools)+len(variation.Skills))
	for _, ref := range variation.Tools {
		attachment, err := hydrator.read(projectKey, syncdomain.AttachmentTool, ref.Key)
		if err != nil {
			return err
		}
		variation.Attachments = append(variation.Attachments, attachment)
	}
	for _, ref := range variation.Skills {
		attachment, err := hydrator.read(projectKey, syncdomain.AttachmentSkill, ref.Key)
		if err != nil {
			return err
		}
		variation.Attachments = append(variation.Attachments, attachment)
	}
	return variation.NormalizeAttachments()
}

func hydrateServerAttachments(client syncapi.Client, projectKey string, variation *syncdomain.Variation) error {
	return newAttachmentHydrator(client).hydrate(projectKey, variation)
}

// samePlanState reports whether every reviewed decision still has the same inputs.
func samePlanState(reviewed, current Plan) bool {
	if len(reviewed.Resources) != len(current.Resources) {
		return false
	}
	for index := range reviewed.Resources {
		if !samePlannedResourceState(reviewed.Resources[index], current.Resources[index]) {
			return false
		}
	}
	return true
}

// samePlannedResourceState compares every input that can change a reviewed
// action. Rendered diffs and decoded payload pointers are derived from these values.
func samePlannedResourceState(reviewed, current PlannedResource) bool {
	return reviewed.ID == current.ID &&
		reviewed.Action == current.Action &&
		reviewed.BaselineFingerprint == current.BaselineFingerprint &&
		reviewed.LocalFingerprint == current.LocalFingerprint &&
		reviewed.ServerFingerprint == current.ServerFingerprint &&
		reviewed.ServerMode == current.ServerMode &&
		reviewed.Upsert == current.Upsert &&
		reviewed.ServerHasStaleAttachmentPins == current.ServerHasStaleAttachmentPins &&
		sameAttachmentPins(reviewed.Server, current.Server)
}

// sameAttachmentPins compares exact server references because canonical
// fingerprints intentionally exclude runtime versions.
func sameAttachmentPins(reviewed, current *syncdomain.Variation) bool {
	if reviewed == nil || current == nil {
		return reviewed == current
	}
	return slices.Equal(reviewed.Tools, current.Tools) && slices.Equal(reviewed.Skills, current.Skills)
}
