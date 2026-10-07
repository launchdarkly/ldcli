package prompt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
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
	syncrepository "github.com/launchdarkly/ldcli/internal/sync/repository"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

// CommandAction is one focused prompt sync operation.
type CommandAction interface {
	promptSyncAction()
}

// SyncAction reconciles the workspace once or whenever watched files change.
type SyncAction struct {
	Watch  bool
	DryRun bool
}

// AddAction materializes selected LaunchDarkly variations as local files.
type AddAction struct {
	Variations []syncdomain.ResourceID
	DryRun     bool
}

// AttachAction adds one tool or skill reference to a managed variation.
type AttachAction struct {
	Kind   syncdomain.AttachmentKind
	Key    string
	Target *syncdomain.ResourceID
}

// DetachAction stops managing selected local variations.
type DetachAction struct {
	Variations []syncdomain.ResourceID
}

// LinkAction creates a managed variation backed by an external prompt file.
type LinkAction struct {
	File   string
	Format string
	Target *synclink.Target
}

func (SyncAction) promptSyncAction()   {}
func (AddAction) promptSyncAction()    {}
func (AttachAction) promptSyncAction() {}
func (DetachAction) promptSyncAction() {}
func (LinkAction) promptSyncAction()   {}

// Options contains command input and streams for one prompt synchronization.
type Options struct {
	WorkingDirectory string
	AccessToken      string
	BaseURI          string
	OutputKind       string
	Action           CommandAction
	ConflictPolicy   ConflictPolicy
	Yes              bool
	NoInput          bool
	Context          context.Context
	Input            io.Reader
	Output           io.Writer
	ErrorOutput      io.Writer
}

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

func (options Options) watching() bool {
	action, ok := options.Action.(SyncAction)
	return ok && action.Watch
}

type bootstrapRunner func(syncbootstrap.Options) error
type detachRunner func(syncdetach.Options) error
type linkRunner func(synclink.Options) (string, error)

type manifestStore interface {
	Load(projectKeys []string) (syncmanifest.Manifest, error)
	Update(previous, next syncmanifest.Manifest) (syncmanifest.Manifest, error)
}

type syncWorkspace struct {
	root     string
	local    synclocal.Store
	manifest manifestStore
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
	if options.Action == nil {
		options.Action = SyncAction{}
	}
	// Every path stored in wrappers or the manifest is repository-relative, so
	// resolve the canonical Git root before dispatching any command mode.
	resolvedWorkspace, err := syncsource.NewResolver().Resolve(options.WorkingDirectory)
	if err != nil {
		return err
	}
	apiClient := syncapi.NewClient(runner.client, options.AccessToken, options.BaseURI)
	workspace := syncWorkspace{
		root:     resolvedWorkspace.Root,
		local:    synclocal.NewStore(resolvedWorkspace.Root),
		manifest: syncmanifest.NewStore(apiClient, resolvedWorkspace.Source),
	}
	localDirectoryExists, err := workspace.local.Exists()
	if err != nil {
		return err
	}

	switch action := options.Action.(type) {
	case DetachAction:
		projectKeys, err := discoverProjectKeys(workspace.root)
		if err != nil {
			return err
		}
		return runner.detach(syncdetach.Options{
			RepositoryRoot: workspace.root,
			Store:          workspace.local,
			Manifest:       workspace.manifest,
			ProjectKeys:    projectKeys,
			Input:          options.Input,
			Output:         options.Output,
			Selections:     action.Variations,
			NoInput:        options.NoInput,
		})
	case LinkAction:
		path, err := runner.link(synclink.Options{
			Catalog:          apiClient,
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
		localDirectoryExists = true
	case AttachAction:
		if !localDirectoryExists {
			return fmt.Errorf("attach a tool or skill after synchronizing at least one variation")
		}
		projectKey, variationID := "", ""
		if action.Target != nil {
			projectKey = action.Target.ProjectKey
			variationID = action.Target.LookupKey
		}
		if err := attachToVariation(workspace.local, apiClient, attachOptions{
			RepositoryRoot: workspace.root,
			ProjectKey:     projectKey,
			VariationID:    variationID,
			Kind:           action.Kind,
			Key:            action.Key,
			Interactive:    !options.NoInput && runner.isTerminal(options.Input, options.Output),
			Input:          options.Input,
			Output:         options.Output,
		}); err != nil {
			if errors.Is(err, errAttachmentCanceled) {
				return nil
			}
			return err
		}
	case AddAction:
		return runner.bootstrap(syncbootstrap.Options{
			Catalog:     apiClient,
			Attachments: apiClient,
			Store:       workspace.local,
			Manifest:    workspace.manifest,
			Input:       options.Input,
			Output:      options.Output,
			Initial:     !localDirectoryExists,
			DryRun:      action.DryRun,
			Selections:  action.Variations,
			NoInput:     options.NoInput,
		})
	case SyncAction:
	default:
		return fmt.Errorf("unsupported prompt sync action %T", action)
	}

	projectKeys, err := discoverProjectKeys(workspace.root)
	if err != nil {
		return err
	}
	syncAction, isSync := options.Action.(SyncAction)
	if !localDirectoryExists && len(projectKeys) == 0 {
		if err := runner.bootstrap(syncbootstrap.Options{
			Catalog:     apiClient,
			Attachments: apiClient,
			Store:       workspace.local,
			Manifest:    workspace.manifest,
			Input:       options.Input,
			Output:      options.Output,
			Initial:     !localDirectoryExists,
			DryRun:      isSync && syncAction.DryRun,
			NoInput:     options.NoInput,
		}); err != nil {
			return err
		}
		if !isSync || !syncAction.Watch {
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

	if isSync && syncAction.Watch {
		ctx := options.Context
		if ctx == nil {
			ctx = context.Background()
		}
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		options.Context = ctx
		return runner.watch(ctx, workspace.root, watchDebounce, func(watcher *sourceWatcher) error {
			return runner.runWorkspaceSync(options, workspace, watcher)
		}, options.ErrorOutput)
	}

	return runner.runWorkspaceSync(options, workspace, nil)
}

// runWorkspaceSync plans, reviews, revalidates, and executes one workspace sync.
func (runner Runner) runWorkspaceSync(options Options, workspace syncWorkspace, watcher *sourceWatcher) error {
	var watched *watchedSources
	if watcher != nil {
		snapshot, err := sourceSnapshot(workspace.root)
		if err != nil {
			return err
		}
		watched = &watchedSources{
			watcher:  watcher,
			snapshot: snapshot,
			debounce: watchDebounce,
		}
	}

	apiClient := syncapi.NewClient(runner.client, options.AccessToken, options.BaseURI)
	reviewedProjectKeys, err := discoverProjectKeys(workspace.root)
	if err != nil {
		return err
	}
	// The manifest is the common ancestor in a three-way comparison between
	// current local files and current LaunchDarkly state.
	baseline, err := workspace.manifest.Load(reviewedProjectKeys)
	if err != nil {
		return err
	}
	reviewedPlan, _, err := loadWorkspacePlan(workspace.root, baseline, apiClient)
	if err != nil {
		return err
	}

	if options.dryRun() {
		if err := writePlanOutput(options.Output, options.OutputKind, reviewedPlan); err != nil {
			return err
		}
		return reviewedPlan.BlockingError()
	}

	interactive := !options.NoInput && runner.isTerminal(options.Input, options.ErrorOutput)
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
	if err != nil {
		return err
	}
	if !shouldContinue {
		if !resolvedPlan.HasChanges() {
			return cleanupOrphanedAttachments(options, workspace.local, interactive)
		}
		return nil
	}

	// Re-read both sides after review so no action uses stale state.
	currentProjectKeys, err := discoverProjectKeys(workspace.root)
	if err != nil {
		return err
	}
	if !slices.Equal(reviewedProjectKeys, currentProjectKeys) {
		if options.watching() {
			return errRefreshWatchPlan
		}
		return fmt.Errorf("sync projects changed after review; run sync again")
	}
	currentManifest, err := workspace.manifest.Load(currentProjectKeys)
	if err != nil {
		return err
	}
	currentPlan, currentLocalFiles, err := loadWorkspacePlan(workspace.root, currentManifest, apiClient)
	if err != nil {
		return err
	}
	if !samePlanState(reviewedPlan, currentPlan) {
		if options.watching() {
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
	if _, err := workspace.manifest.Update(currentManifest, updatedManifest); err != nil {
		executionErr = errors.Join(executionErr, err)
	}
	if err := workspace.local.RemoveEmptyDirectories(); err != nil {
		executionErr = errors.Join(executionErr, err)
	}
	if err := writeOutcomeOutput(options.Output, options.OutputKind, outcomes); err != nil {
		executionErr = errors.Join(executionErr, err)
	}
	if executionErr == nil {
		executionErr = cleanupOrphanedAttachments(options, workspace.local, interactive)
	}
	return executionErr
}

// validateOptions rejects command modes whose side effects or UX conflict.
func validateOptions(options Options) error {
	switch action := options.Action.(type) {
	case nil:
	case SyncAction:
		if action.Watch && action.DryRun {
			return fmt.Errorf("watch does not support --dry-run")
		}
	case AddAction:
	case AttachAction:
		if action.Kind != syncdomain.AttachmentTool && action.Kind != syncdomain.AttachmentSkill {
			return fmt.Errorf("attachment kind must be tool or skill")
		}
		if action.Target != nil && action.Target.Kind != syncdomain.KindVariation {
			return fmt.Errorf("attachment target must be a variation")
		}
	case DetachAction:
	case LinkAction:
		if action.File == "" {
			return fmt.Errorf("linked file is required")
		}
		if action.Format == "" {
			return fmt.Errorf("--format is required")
		}
		if err := syncreference.ValidateFormat(action.Format); err != nil {
			return err
		}
		if action.Target != nil && action.Target.ModelConfigKey == "" {
			return fmt.Errorf("--model-config-key is required with --to")
		}
	default:
		return fmt.Errorf("unsupported prompt sync action %T", action)
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
	if errors.Is(err, synclocal.ErrNoDirectory) {
		localFileResources = nil
		err = nil
	}
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

func discoverProjectKeys(repositoryRoot string) ([]string, error) {
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return nil, err
	}
	deleted, err := syncrepository.DeletedPaths(repositoryRoot)
	if err != nil {
		return nil, err
	}

	var projectKeys []string
	for _, file := range append(files, deleted...) {
		if projectKey, ok := projectKeyFromManagedPath(file); ok {
			projectKeys = append(projectKeys, projectKey)
		}
	}
	slices.Sort(projectKeys)
	return slices.Compact(projectKeys), nil
}

func projectKeyFromManagedPath(file string) (string, bool) {
	parts := strings.Split(file, "/")
	if len(parts) < 4 || parts[0] != syncdomain.RootDir || parts[1] == "" {
		return "", false
	}
	switch {
	case len(parts) == 5 && parts[2] == "configs" && strings.HasSuffix(parts[4], ".prompt.md"):
	case len(parts) == 4 && parts[2] == "tools" && strings.HasSuffix(parts[3], ".json"):
	case len(parts) == 4 && parts[2] == "skills" && strings.HasSuffix(parts[3], ".md"):
	default:
		return "", false
	}
	return parts[1], true
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
