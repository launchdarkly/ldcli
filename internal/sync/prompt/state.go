package prompt

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
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
// the plan of the resources in the scope that the filter keeps.
func (workspace syncWorkspace) loadState(
	client syncapi.Client,
	scope syncdomain.Scope,
	filter syncdomain.Filter,
) (workspaceState, error) {
	synced, err := workspace.syncedResources()
	if err != nil {
		return workspaceState{}, err
	}
	if err := checkFilter(scope, filter, synced); err != nil {
		return workspaceState{}, err
	}
	projectKeys := projectKeys(synced, filter)
	// The lock is the common ancestor in a three-way comparison of the local
	// files and LaunchDarkly.
	baseline, err := workspace.baselines.Load(projectKeys)
	if err != nil {
		return workspaceState{}, err
	}
	plan, localFiles, err := loadWorkspacePlan(workspace.root, baseline, client, scope, filter)
	if err != nil {
		return workspaceState{}, err
	}
	return workspaceState{projectKeys: projectKeys, baseline: baseline, plan: plan, localFiles: localFiles}, nil
}

// loadWorkspacePlan plans the variations, tools, and skills of the scope. It
// drops each resource that the filter does not keep before it reads
// LaunchDarkly. It also returns the local variations as their files store them.
func loadWorkspacePlan(
	repositoryRoot string,
	baseline syncmanifest.Baseline,
	client syncapi.Client,
	scope syncdomain.Scope,
	filter syncdomain.Filter,
) (Plan, map[ResourceID]syncdomain.SyncedResource, error) {
	variations, err := compileWorkspace(repositoryRoot)
	if err != nil {
		return Plan{}, nil, err
	}
	// A tool that a variation uses can also have a row of its own. The cache
	// reads it from LaunchDarkly once.
	attachments := newAttachmentCache(client)

	plan := Plan{Scope: scope}
	var localFiles map[ResourceID]syncdomain.SyncedResource
	if scope.Includes(syncdomain.KindVariation) {
		var rows Plan
		if rows, localFiles, err = planVariationRows(repositoryRoot, baseline, client, attachments, filter, variations); err != nil {
			return Plan{}, nil, err
		}
		plan.Resources = rows.Resources
	}
	if scope.Includes(syncdomain.KindTool) || scope.Includes(syncdomain.KindSkill) {
		include := func(id ResourceID) bool { return scope.Includes(id.Kind) && filter.Includes(id) }
		rows, err := planAttachmentRows(repositoryRoot, baseline.Lock, attachments, include)
		if err != nil {
			return Plan{}, nil, err
		}
		plan.Resources = append(plan.Resources, rows...)
	}
	referenced, err := planReferencedAttachments(repositoryRoot, baseline.Lock, attachments, plan.Resources)
	if err != nil {
		return Plan{}, nil, err
	}
	plan.Resources = append(plan.Resources, referenced...)
	slices.SortFunc(plan.Resources, func(left, right PlannedResource) int {
		return syncdomain.CompareResourceIDs(left.ID, right.ID)
	})

	checkAttachmentBaselines(plan.Resources, baseline.Lock)
	checkAttachmentDeletes(plan.Resources, variations)
	for index := range plan.Resources {
		resource := &plan.Resources[index]
		resource.SyncedElsewhere = baseline.Stale(resource.ID)
		if resource.ID.Kind != syncdomain.KindVariation {
			resource.usedBy = usersOf(variations, resource.ID)
		}
	}
	if scope.Includes(syncdomain.KindVariation) && !scope.Includes(syncdomain.KindTool) {
		if plan.standalone, err = countStandalone(repositoryRoot, variations, plan.Resources, filter.Project); err != nil {
			return Plan{}, nil, err
		}
	}
	return plan, localFiles, nil
}

// usersOf returns the local variations that use a tool or a skill.
func usersOf(variations []syncdomain.SyncedResource, id ResourceID) []ResourceID {
	var users []ResourceID
	for _, variation := range variations {
		if uses(variation, id) {
			users = append(users, variation.ID())
		}
	}
	return users
}

// countStandalone counts the tool and skill files, by kind, in project (or in
// every project when it is empty), that no local variation uses, and that have
// no row in the plan. --config-key and --key select only variations, so they
// do not change the count. It reads only paths, so an invalid file does not
// stop the count.
func countStandalone(
	repositoryRoot string,
	variations []syncdomain.SyncedResource,
	planned []PlannedResource,
	project string,
) (map[syncdomain.Kind]int, error) {
	files, err := synclocal.SourceFiles(repositoryRoot)
	if err != nil {
		return nil, err
	}
	counts := make(map[syncdomain.Kind]int)
	for _, file := range files {
		id, ok := synclocal.ParseManagedPath(file)
		standalone := ok && id.Kind != syncdomain.KindVariation && (project == "" || id.ProjectKey == project) &&
			len(usersOf(variations, id)) == 0 &&
			!slices.ContainsFunc(planned, func(resource PlannedResource) bool { return resource.ID == id })
		if standalone {
			counts[id.Kind]++
		}
	}
	return counts, nil
}

// planVariationRows reads the variations that the filter keeps from
// LaunchDarkly, and compares both sides with the lock.
func planVariationRows(
	repositoryRoot string,
	baseline syncmanifest.Baseline,
	client syncapi.Client,
	attachments *attachmentCache,
	filter syncdomain.Filter,
	variations []syncdomain.SyncedResource,
) (Plan, map[ResourceID]syncdomain.SyncedResource, error) {
	localFiles := slices.DeleteFunc(slices.Clone(variations), func(resource syncdomain.SyncedResource) bool {
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
		planRestore(repositoryRoot, baseline, &plan.Resources[index])
	}
	return plan, localFilesByID, nil
}

// planAttachmentRows reads each tool and skill that include accepts and that
// has a local file or a lock entry, and compares both sides with the lock.
func planAttachmentRows(
	repositoryRoot string,
	lock syncmanifest.Manifest,
	attachments *attachmentCache,
	include func(ResourceID) bool,
) ([]PlannedResource, error) {
	local, err := synclocal.CompileAttachments(repositoryRoot, include)
	if err != nil {
		return nil, err
	}
	tracked := syncmanifest.New()
	for _, resource := range lock.Resources {
		if resource.ResourceKind != syncdomain.KindVariation && include(resource.ID()) {
			tracked.Resources = append(tracked.Resources, resource)
		}
	}

	ids := make([]ResourceID, 0, len(local)+len(tracked.Resources))
	for _, attachment := range local {
		ids = append(ids, attachment.ID())
	}
	for _, resource := range tracked.Resources {
		ids = append(ids, resource.ID())
	}
	server := make(map[ResourceID]*syncdomain.Attachment, len(ids))
	for _, id := range ids {
		if server[id], err = attachments.read(id); err != nil {
			return nil, err
		}
	}
	return planAttachments(tracked, local, server), nil
}

// planReferencedAttachments plans a row for each tool and skill that a
// variation row writes to a side that does not use it yet, when the tool or
// skill exists on both sides with different content. Without the row, the
// variation row would write its own copy over the other side. The row decides
// the content, and executePlan applies it before the variation rows.
// checkAttachmentBaselines covers the tools and skills that both sides use.
//
// A conflicted variation writes the side that the user chooses, so each side
// of it counts. A row that only conflicts need records them in conflictUses,
// and applyConflictResolutions keeps it only for the chosen side.
func planReferencedAttachments(
	repositoryRoot string,
	lock syncmanifest.Manifest,
	attachments *attachmentCache,
	resources []PlannedResource,
) ([]PlannedResource, error) {
	written := make(map[ResourceID]bool)
	conflictUses := make(map[ResourceID][]conflictUse)
	for _, resource := range resources {
		for _, id := range newReferences(resource) {
			written[id] = true
		}
		if resource.ID.Kind == syncdomain.KindVariation && resource.Action == ActionConflict {
			for _, id := range newAttachments(resource.ID.ProjectKey, resource.Local, resource.Server) {
				conflictUses[id] = append(conflictUses[id], conflictUse{variation: resource.ID, local: true})
			}
			for _, id := range newAttachments(resource.ID.ProjectKey, resource.Server, resource.Local) {
				conflictUses[id] = append(conflictUses[id], conflictUse{variation: resource.ID})
			}
		}
	}
	referenced := func(id ResourceID) bool {
		return (written[id] || len(conflictUses[id]) != 0) &&
			!slices.ContainsFunc(resources, func(resource PlannedResource) bool { return resource.ID == id })
	}

	rows, err := planAttachmentRows(repositoryRoot, lock, attachments, referenced)
	if err != nil {
		return nil, err
	}
	rows = slices.DeleteFunc(rows, func(row PlannedResource) bool {
		return !row.hasLocal() || !row.hasServer() || row.LocalFingerprint == row.ServerFingerprint
	})
	for index := range rows {
		if !written[rows[index].ID] {
			rows[index].conflictUses = conflictUses[rows[index].ID]
		}
	}
	return rows, nil
}

// newReferences returns each tool and skill that a push or a pull writes to a
// side that does not use it yet.
func newReferences(resource PlannedResource) []ResourceID {
	source, target := resource.writtenSides()
	return newAttachments(resource.ID.ProjectKey, source, target)
}

// newAttachments returns each tool and skill that source uses and target does
// not.
func newAttachments(projectKey string, source, target *syncdomain.Variation) []ResourceID {
	if source == nil {
		return nil
	}
	var ids []ResourceID
	for _, attachment := range source.Attachments {
		if target == nil || !hasAttachment(*target, attachment) {
			ids = append(ids, attachment.ID(projectKey))
		}
	}
	return ids
}

// hasAttachment reports whether a variation uses the tool or skill of attachment.
func hasAttachment(variation syncdomain.Variation, attachment syncdomain.Attachment) bool {
	_, ok := variation.Attachment(attachment.Kind, attachment.Key())
	return ok
}

// checkAttachmentBaselines makes sure that a variation row does not move a
// tool or a skill past a change on the other side. sync.lock records the
// state of each tool and skill on its own. A row becomes a conflict when the
// side that it overwrites changed since that state, or when the attachment has
// no state and both sides have different content.
func checkAttachmentBaselines(resources []PlannedResource, lock syncmanifest.Manifest) {
	baselines := make(map[ResourceID]string, len(lock.Resources))
	for _, resource := range lock.Resources {
		baselines[resource.ID()] = resource.Fingerprint
	}

	for index := range resources {
		resource := &resources[index]
		source, target := resource.writtenSides()
		if source == nil || target == nil {
			continue
		}
		for _, id := range resource.changedAttachments {
			kind := syncdomain.AttachmentKind(id.Kind)
			overwritten, ok := target.Attachment(kind, id.LookupKey)
			if !ok {
				continue
			}
			// Without a baseline, sync cannot tell which side changed, so two
			// different versions conflict. A new attachment on one side does not.
			_, unsynced := source.Attachment(kind, id.LookupKey)
			if baseline, tracked := baselines[id]; tracked {
				fingerprint, err := syncdomain.FingerprintAttachment(id.ProjectKey, overwritten)
				unsynced = err != nil || fingerprint != baseline
			}
			if unsynced {
				resource.Action = ActionConflict
				break
			}
		}
	}
}

// checkAttachmentDeletes keeps a tool or a skill that LaunchDarkly deleted
// while a local variation still uses it. The deletion becomes an error, unless
// the variation row stops using it. A conflict can stop using it if
// LaunchDarkly wins, so checkResolvedAttachmentDeletes checks the choice.
func checkAttachmentDeletes(resources []PlannedResource, variations []syncdomain.SyncedResource) {
	variationRows := make(map[ResourceID]PlannedResource)
	for _, resource := range resources {
		if resource.ID.Kind == syncdomain.KindVariation {
			variationRows[resource.ID] = resource
		}
	}

	for index := range resources {
		resource := &resources[index]
		if resource.ID.Kind == syncdomain.KindVariation || resource.Action != ActionDeleteLocal {
			continue
		}
		for _, variation := range variations {
			if !uses(variation, resource.ID) {
				continue
			}
			row, planned := variationRows[variation.ID()]
			if planned && dropsReference(row, resource.ID) {
				continue
			}
			fix := "Run ldcli sync all to pull the variation first."
			if planned {
				fix = "Remove it from the variation first."
			}
			resource.Action = ActionError
			resource.Error = fmt.Sprintf("%s was deleted in LaunchDarkly, but %s still uses it. %s",
				resource.ID.Label(), variation.ID().Label(), fix)
			break
		}
	}
}

// checkResolvedAttachmentDeletes checks the deletions again after the conflict
// choices. A variation whose local version wins still uses its tools and
// skills. The plan has a row for each variation that a conflict choice
// changes, so the check needs no other variations.
func checkResolvedAttachmentDeletes(resources []PlannedResource) {
	var variations []syncdomain.SyncedResource
	for _, resource := range resources {
		if resource.ID.Kind == syncdomain.KindVariation && resource.Local != nil {
			variations = append(variations, syncdomain.SyncedResource{
				Kind: resource.ID.Kind, ProjectKey: resource.ID.ProjectKey, LookupKey: resource.ID.LookupKey, Variation: *resource.Local,
			})
		}
	}
	checkAttachmentDeletes(resources, variations)
}

// dropsReference reports whether a variation row stops using a tool or a
// skill. A deletion does, and so does a pull of a version without the
// reference. A conflict does if LaunchDarkly wins, and the conflict blocks
// the plan until the user chooses.
func dropsReference(row PlannedResource, id ResourceID) bool {
	switch row.Action {
	case ActionDeleteLocal:
		return true
	case ActionUpdateLocal, ActionConflict:
		return !references(row.Server, id)
	default:
		return false
	}
}

// uses reports whether a local variation uses a tool or a skill.
func uses(variation syncdomain.SyncedResource, id ResourceID) bool {
	_, ok := variation.Variation.Attachment(syncdomain.AttachmentKind(id.Kind), id.LookupKey)
	return ok && variation.ProjectKey == id.ProjectKey
}

// references reports whether a server variation references a tool or a skill.
func references(variation *syncdomain.Variation, id ResourceID) bool {
	return variation != nil && slices.ContainsFunc(variation.Refs(syncdomain.AttachmentKind(id.Kind)), func(ref syncdomain.AttachmentRef) bool {
		return ref.Key == id.LookupKey
	})
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
// a resource of the scope that has no local file and no sync.lock entry, so
// that a typo never gives an empty plan that looks like success.
func checkFilter(scope syncdomain.Scope, filter syncdomain.Filter, synced []ResourceID) error {
	if filter.IsZero() {
		return nil
	}
	synced = slices.DeleteFunc(slices.Clone(synced), func(id ResourceID) bool { return !scope.Includes(id.Kind) })
	if !slices.ContainsFunc(synced, func(id ResourceID) bool { return id.ProjectKey == filter.Project }) {
		var commands []string
		for _, kind := range []syncdomain.Kind{syncdomain.KindVariation, syncdomain.KindTool, syncdomain.KindSkill} {
			if scope.Includes(kind) {
				commands = append(commands, fmt.Sprintf("ldcli sync %ss add --project %s", kindNoun(kind), filter.Project))
			}
		}
		return fmt.Errorf("project %q has no synced resources in this repository. To start, run %s.",
			filter.Project, strings.Join(commands, " or "))
	}
	for _, id := range filter.IDs(scope.KeyKind()) {
		if !slices.Contains(synced, id) {
			return fmt.Errorf("%s is not synced in this repository. To start syncing it, run %s.", id.Label(), syncbootstrap.AddCommand(id))
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
// builds a plan, because many variations can share one attachment, and a tool
// or a skill can also have a row of its own.
type attachmentCache struct {
	client  syncapi.Client
	results map[syncdomain.ResourceID]attachmentRead
}

type attachmentRead struct {
	attachment syncdomain.Attachment
	err        error
}

func newAttachmentCache(client syncapi.Client) *attachmentCache {
	return &attachmentCache{client: client, results: make(map[syncdomain.ResourceID]attachmentRead)}
}

// get reads the latest version of an attachment. It keeps each result and
// each 404.
func (cache *attachmentCache) get(id ResourceID) (syncdomain.Attachment, error) {
	if result, ok := cache.results[id]; ok {
		return result.attachment, result.err
	}
	attachment, err := cache.client.ReadAttachment(id.ProjectKey, syncdomain.AttachmentKind(id.Kind), id.LookupKey)
	if err == nil || syncapi.IsNotFound(err) {
		cache.results[id] = attachmentRead{attachment: attachment, err: err}
	}
	return attachment, err
}

// read returns the latest version of an attachment, or nil when LaunchDarkly
// does not have it.
func (cache *attachmentCache) read(id ResourceID) (*syncdomain.Attachment, error) {
	attachment, err := cache.get(id)
	switch {
	case syncapi.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	default:
		return &attachment, nil
	}
}

// hydrate reads every attachment that a server variation references. A
// missing attachment is an error.
func (cache *attachmentCache) hydrate(projectKey string, variation *syncdomain.Variation) error {
	return variation.HydrateAttachments(func(kind syncdomain.AttachmentKind, key string) (syncdomain.Attachment, error) {
		return cache.get(syncdomain.ResourceID{Kind: syncdomain.Kind(kind), ProjectKey: projectKey, LookupKey: key})
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
