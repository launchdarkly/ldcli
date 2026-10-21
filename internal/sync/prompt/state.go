package prompt

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

// workspaceState is everything that one plan depends on. The plan compares
// canonical local variations. localFiles keeps each variation as its file
// stores it, so that a local write keeps the form of the file.
type workspaceState struct {
	projectKeys []string
	baseline    syncmanifest.Baseline
	plan        Plan
	localFiles  map[ResourceID]syncdomain.SyncedResource
	// noSettings are the projects without sync settings.
	noSettings []string
}

// loadState reads the baseline, the local files, and LaunchDarkly, and builds
// the plan.
func (workspace syncWorkspace) loadState(client syncapi.Client) (workspaceState, error) {
	projectKeys, err := workspace.projectKeys()
	if err != nil {
		return workspaceState{}, err
	}
	// The lock is the common ancestor in a three-way comparison of the local
	// files and LaunchDarkly.
	baseline, err := workspace.baselines.Load(projectKeys)
	if err != nil {
		return workspaceState{}, err
	}
	loaded, err := loadWorkspacePlan(workspace.root, baseline, client)
	if err != nil {
		return workspaceState{}, err
	}
	return workspaceState{
		projectKeys: projectKeys, baseline: baseline, plan: loaded.plan, localFiles: loaded.localFiles,
		noSettings: loaded.noSettings,
	}, nil
}

// warnUnavailableSettings warns once in a run about each project without sync settings.
func (workspace syncWorkspace) warnUnavailableSettings(output io.Writer, state workspaceState) {
	for _, projectKey := range state.noSettings {
		if !workspace.warnedSettings[projectKey] {
			workspace.warnedSettings[projectKey] = true
			_ = syncconsole.New(output).Write(syncapi.SettingsUnavailableWarning(projectKey, "Sync uses ui-and-code."))
		}
	}
}

// loadedPlan is the plan and the inputs that loadWorkspacePlan read.
type loadedPlan struct {
	plan       Plan
	localFiles map[ResourceID]syncdomain.SyncedResource
	noSettings []string
}

// loadWorkspacePlan reads the local variations, their LaunchDarkly versions,
// and their sources of truth, and compares both sides with the lock. It also
// returns the local variations as their files store them.
func loadWorkspacePlan(
	repositoryRoot string,
	baseline syncmanifest.Baseline,
	client syncapi.Client,
) (loadedPlan, error) {
	localFiles, err := compileWorkspace(repositoryRoot)
	if err != nil {
		return loadedPlan{}, err
	}
	local, err := canonicalizeLocalVariationModels(localFiles, client.ModelConfig)
	if err != nil {
		return loadedPlan{}, err
	}

	localFilesByID := make(map[ResourceID]syncdomain.SyncedResource, len(localFiles))
	ids := make(map[ResourceID]struct{}, len(localFiles)+len(baseline.Lock.Resources))
	for _, resource := range localFiles {
		localFilesByID[resource.ID()] = resource
		ids[resource.ID()] = struct{}{}
	}
	for _, resource := range baseline.Lock.Resources {
		if resource.ResourceKind == syncdomain.KindVariation {
			ids[resource.ID()] = struct{}{}
		}
	}

	attachments := newAttachmentCache(client)
	server := make(map[ResourceID]ServerResource, len(ids))
	for id := range ids {
		resource, err := readServerResource(client, attachments, id)
		if err != nil {
			return loadedPlan{}, err
		}
		server[id] = resource
	}
	codeOnly, noSettings, err := resolveCodeOnly(client, ids)
	if err != nil {
		return loadedPlan{}, err
	}
	for id := range codeOnly {
		resource := server[id]
		resource.CodeOnly = true
		server[id] = resource
	}
	plan := BuildPlan(baseline.Lock, local, server)
	for index := range plan.Resources {
		plan.Resources[index].SyncedElsewhere = baseline.Stale(plan.Resources[index].ID)
		planRestore(repositoryRoot, baseline, &plan.Resources[index])
	}
	return loadedPlan{plan: plan, localFiles: localFilesByID, noSettings: noSettings}, nil
}

// resolveCodeOnly returns the code-only variations. It also returns the
// projects that have no sync settings.
func resolveCodeOnly(
	client syncapi.Client,
	ids map[ResourceID]struct{},
) (map[ResourceID]bool, []string, error) {
	byProject := make(map[string][]ResourceID)
	for id := range ids {
		byProject[id.ProjectKey] = append(byProject[id.ProjectKey], id)
	}
	projectKeys := slices.Sorted(maps.Keys(byProject))

	codeOnly := make(map[ResourceID]bool)
	var noSettings []string
	for _, projectKey := range projectKeys {
		projectIDs := byProject[projectKey]
		slices.SortFunc(projectIDs, syncdomain.CompareResourceIDs)
		sources, err := client.ResolveSourceOfTruth(projectKey, projectIDs)
		switch {
		case errors.Is(err, syncapi.ErrSyncSettingsUnavailable):
			noSettings = append(noSettings, projectKey)
		case err != nil:
			return nil, nil, err
		}
		for id, source := range sources {
			if source == syncdomain.SourceOfTruthCodeOnly {
				codeOnly[id] = true
			}
		}
	}
	return codeOnly, noSettings, nil
}

// planRestore checks a variation whose local file is missing. The restored
// file must keep the link that sync.lock records. When sync cannot restore
// the link safely, the resource becomes an error and sync writes nothing.
func planRestore(repositoryRoot string, baseline syncmanifest.Baseline, resource *PlannedResource) {
	if resource.Action != ActionUpdateLocal || resource.Local != nil {
		return
	}
	fail := func(problem, fix string) {
		resource.Action = ActionError
		resource.Error = "the variation file is missing, and " + problem + ". " + fix
	}
	ref := baseline.Lock.Ref(resource.ID)
	if ref == nil {
		return
	}

	content, err := synclocal.ReadReference(repositoryRoot, *ref)
	if err != nil {
		fail(fmt.Sprintf("its linked file %q is not available (%s)", ref.File, err),
			"Restore the files from Git, or stop syncing the variation with detach")
		return
	}
	// The restore writes LaunchDarkly's prompt to the linked file. Allow that
	// only when the file already has that prompt, so that no local edit is lost.
	linked := *resource.Server
	if err := syncreference.ApplyToVariation(ref.Format, content, &linked); err == nil {
		fingerprint, err := syncdomain.FingerprintVariation(resource.ID.ProjectKey, resource.ID.LookupKey, linked)
		if err == nil && fingerprint == resource.ServerFingerprint {
			resource.restoreRef = ref
			return
		}
	}
	fail(fmt.Sprintf("its linked file %q differs from LaunchDarkly", ref.File),
		"Restore the variation file from Git, and then run sync again")
}

// readServerResource reads one variation from LaunchDarkly with the content
// of its tools and skills.
func readServerResource(client syncapi.Client, attachments *attachmentCache, id syncdomain.ResourceID) (ServerResource, error) {
	configKey, variationKey, err := id.VariationKeys()
	if err != nil {
		return ServerResource{}, err
	}
	state, err := client.ReadVariation(id.ProjectKey, configKey, variationKey)
	if err != nil {
		return ServerResource{}, err
	}

	resource := ServerResource{ConfigMode: state.ConfigMode}
	if state.Exists {
		if err := attachments.hydrate(id.ProjectKey, &state.Variation); err != nil {
			return ServerResource{}, err
		}
		resource.Variation = &state.Variation
	}
	return resource, nil
}

// projectKeys returns each project that has a local sync file or an entry in
// the sync.lock file. A project whose files are all missing is still in the
// lock, so sync can restore its files.
func (workspace syncWorkspace) projectKeys() ([]string, error) {
	files, err := synclocal.SourceFiles(workspace.root)
	if err != nil {
		return nil, err
	}
	lock, err := syncmanifest.ReadLock(workspace.local)
	if err != nil {
		return nil, err
	}

	projectKeys := lock.ProjectKeys()
	for _, file := range files {
		if id, ok := synclocal.ParseManagedPath(file); ok {
			projectKeys = append(projectKeys, id.ProjectKey)
		}
	}
	slices.Sort(projectKeys)
	return slices.Compact(projectKeys), nil
}

// compileWorkspace reads the local variations. A workspace without a managed
// directory has no variations.
func compileWorkspace(repositoryRoot string) ([]syncdomain.SyncedResource, error) {
	resources, err := synclocal.CompileWorkspace(repositoryRoot)
	if errors.Is(err, synclocal.ErrNoDirectory) {
		return nil, nil
	}
	return resources, err
}

// attachmentCache reads each tool and skill from LaunchDarkly once while it
// builds a plan, because many variations can share one attachment.
type attachmentCache struct {
	client      syncapi.Client
	attachments map[syncdomain.ResourceID]syncdomain.Attachment
}

func newAttachmentCache(client syncapi.Client) *attachmentCache {
	return &attachmentCache{client: client, attachments: make(map[syncdomain.ResourceID]syncdomain.Attachment)}
}

func (cache *attachmentCache) hydrate(projectKey string, variation *syncdomain.Variation) error {
	return variation.HydrateAttachments(func(kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
		id := syncdomain.ResourceID{Kind: syncdomain.Kind(kind), ProjectKey: projectKey, LookupKey: key}
		if attachment, ok := cache.attachments[id]; ok {
			return attachment, nil
		}
		attachment, err := cache.client.ReadAttachment(projectKey, kind, key)
		if err != nil {
			return syncdomain.Attachment{}, err
		}
		cache.attachments[id] = attachment
		return attachment, nil
	})
}

// samePlanState reports whether each reviewed decision still has the same input.
func samePlanState(reviewed, current Plan) bool {
	return slices.EqualFunc(reviewed.Resources, current.Resources, samePlannedResourceState)
}

// samePlannedResourceState compares every input that can change the action of
// a reviewed resource. The diff and the decoded variations derive from them.
func samePlannedResourceState(reviewed, current PlannedResource) bool {
	return reviewed.ID == current.ID &&
		reviewed.Action == current.Action &&
		reviewed.BaselineFingerprint == current.BaselineFingerprint &&
		reviewed.LocalFingerprint == current.LocalFingerprint &&
		reviewed.ServerFingerprint == current.ServerFingerprint &&
		reviewed.MergedFingerprint == current.MergedFingerprint &&
		reviewed.ServerMode == current.ServerMode &&
		reviewed.CodeOnly == current.CodeOnly &&
		reviewed.Upsert == current.Upsert &&
		reviewed.ServerHasStaleAttachmentPins == current.ServerHasStaleAttachmentPins &&
		sameAttachmentPins(reviewed.Server, current.Server)
}

// sameAttachmentPins compares the exact versions that LaunchDarkly pins. The
// fingerprints do not include versions.
func sameAttachmentPins(reviewed, current *syncdomain.Variation) bool {
	if reviewed == nil || current == nil {
		return reviewed == current
	}
	return slices.Equal(reviewed.Tools, current.Tools) && slices.Equal(reviewed.Skills, current.Skills)
}
