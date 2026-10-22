package prompt

import (
	"errors"
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
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
}

// loadState reads the baseline, the local files, and LaunchDarkly, and builds
// the plan of the resources that the filter keeps.
func (workspace syncWorkspace) loadState(client syncapi.Client, filter syncdomain.Filter) (workspaceState, error) {
	synced, err := workspace.syncedResources()
	if err != nil {
		return workspaceState{}, err
	}
	if err := checkFilter(filter, synced); err != nil {
		return workspaceState{}, err
	}
	projectKeys := projectKeys(synced, filter)
	// The lock is the common ancestor in a three-way comparison of the local
	// files and LaunchDarkly.
	baseline, err := workspace.baselines.Load(projectKeys)
	if err != nil {
		return workspaceState{}, err
	}
	plan, localFiles, err := loadWorkspacePlan(workspace.root, baseline, client, filter)
	if err != nil {
		return workspaceState{}, err
	}
	return workspaceState{projectKeys: projectKeys, baseline: baseline, plan: plan, localFiles: localFiles}, nil
}

// loadWorkspacePlan reads the local variations and their LaunchDarkly
// versions, and compares both with the lock. It also returns the local
// variations as their files store them. It drops each variation that the
// filter does not keep before it reads LaunchDarkly.
func loadWorkspacePlan(
	repositoryRoot string,
	baseline syncmanifest.Baseline,
	client syncapi.Client,
	filter syncdomain.Filter,
) (Plan, map[ResourceID]syncdomain.SyncedResource, error) {
	localFiles, err := compileWorkspace(repositoryRoot)
	if err != nil {
		return Plan{}, nil, err
	}
	localFiles = slices.DeleteFunc(localFiles, func(resource syncdomain.SyncedResource) bool {
		return !filter.Includes(resource.ID())
	})
	local, err := canonicalizeLocalVariationModels(localFiles, client.ModelConfig)
	if err != nil {
		return Plan{}, nil, err
	}

	tracked := syncmanifest.New()
	for _, resource := range baseline.Lock.Resources {
		if resource.ResourceKind == syncdomain.KindVariation && filter.Includes(resource.ID()) {
			tracked.Resources = append(tracked.Resources, resource)
		}
	}
	localFilesByID := make(map[ResourceID]syncdomain.SyncedResource, len(localFiles))
	ids := make(map[ResourceID]struct{}, len(localFiles)+len(tracked.Resources))
	for _, resource := range localFiles {
		localFilesByID[resource.ID()] = resource
		ids[resource.ID()] = struct{}{}
	}
	for _, resource := range tracked.Resources {
		ids[resource.ID()] = struct{}{}
	}

	attachments := newAttachmentCache(client)
	server := make(map[ResourceID]ServerResource, len(ids))
	for id := range ids {
		resource, err := readServerResource(client, attachments, id)
		if err != nil {
			return Plan{}, nil, err
		}
		server[id] = resource
	}
	plan := BuildPlan(tracked, local, server)
	for index := range plan.Resources {
		plan.Resources[index].SyncedElsewhere = baseline.Stale(plan.Resources[index].ID)
		planRestore(repositoryRoot, baseline, &plan.Resources[index])
	}
	return plan, localFilesByID, nil
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

// syncedResources returns the identity of each resource that has a local sync
// file or an entry in the sync.lock file. It reads only local files, so that a
// filter that names a resource that is not synced fails before any
// LaunchDarkly read.
func (workspace syncWorkspace) syncedResources() ([]ResourceID, error) {
	files, err := synclocal.SourceFiles(workspace.root)
	if err != nil {
		return nil, err
	}
	lock, err := syncmanifest.ReadLock(workspace.local)
	if err != nil {
		return nil, err
	}

	var synced []ResourceID
	for _, resource := range lock.Resources {
		synced = append(synced, resource.ID())
	}
	for _, file := range files {
		if id, ok := synclocal.ParseManagedPath(file); ok {
			synced = append(synced, id)
		}
	}
	return synced, nil
}

// projectKeys returns each project of a synced resource that the filter keeps.
// A project whose files are all missing is still in the lock, so sync can
// restore its files.
func projectKeys(synced []ResourceID, filter syncdomain.Filter) []string {
	var keys []string
	for _, id := range synced {
		if filter.Includes(id) {
			keys = append(keys, id.ProjectKey)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// checkFilter returns an error when the filter names a project, a config, or
// a variation that has no local file and no sync.lock entry, so that a typo
// never gives an empty plan that looks like success.
func checkFilter(filter syncdomain.Filter, synced []ResourceID) error {
	if filter.IsZero() {
		return nil
	}
	if !slices.ContainsFunc(synced, func(id ResourceID) bool { return id.ProjectKey == filter.Project }) {
		return fmt.Errorf("project %q has no synced resources in this repository. "+
			"To start, run ldcli sync prompts add --project %s.", filter.Project, filter.Project)
	}
	for _, key := range filter.Keys {
		if id := syncdomain.VariationID(filter.Project, filter.ConfigKey, key); !slices.Contains(synced, id) {
			return fmt.Errorf("%s is not synced in this repository. To start syncing it, "+
				"run ldcli sync prompts add --project %s --config-key %s --key %s.",
				id.Label(), filter.Project, filter.ConfigKey, key)
		}
	}
	if !slices.ContainsFunc(synced, filter.Includes) {
		return fmt.Errorf("config %q in project %q has no synced variations in this repository. "+
			"To start, run ldcli sync prompts add --project %s --config-key %s.",
			filter.ConfigKey, filter.Project, filter.Project, filter.ConfigKey)
	}
	return nil
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
		reviewed.ServerMode == current.ServerMode &&
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
