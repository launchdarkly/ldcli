package prompt

import (
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// Action is the one change that reconciles a resource.
type Action string

const (
	ActionInSync         Action = "in_sync"
	ActionCreateServer   Action = "create_server"
	ActionUpdateServer   Action = "update_server"
	ActionUpdateLocal    Action = "update_local"
	ActionUpdateBoth     Action = "update_both"
	ActionDeleteLocal    Action = "delete_local"
	ActionUpdateManifest Action = "update_manifest"
	ActionRemoveManifest Action = "remove_manifest"
	ActionConflict       Action = "conflict"
	ActionError          Action = "error"
)

// changesServer reports whether the action writes to LaunchDarkly.
func (action Action) changesServer() bool {
	return action == ActionCreateServer || action == ActionUpdateServer || action == ActionUpdateBoth
}

// changesLocal reports whether the action writes a local file.
func (action Action) changesLocal() bool {
	return action == ActionUpdateLocal || action == ActionDeleteLocal || action == ActionUpdateBoth
}

// ResourceID is the identity of a synchronized resource.
type ResourceID = syncdomain.ResourceID

// ServerResource is a variation in LaunchDarkly and the mode of its config.
// Variation is nil when the variation does not exist. The config owns the
// mode, and the variation API cannot change it.
type ServerResource struct {
	Variation  *syncdomain.Variation
	ConfigMode syncdomain.VariationMode
	CodeOnly   bool
}

// PlannedResource is the local and the server state of one resource, and the
// action that reconciles them.
type PlannedResource struct {
	ID                  ResourceID
	Action              Action
	Upsert              bool
	BaselineFingerprint string
	LocalFingerprint    string
	ServerFingerprint   string
	ServerMode          syncdomain.VariationMode
	Local               *syncdomain.Variation
	Server              *syncdomain.Variation
	CodeOnly            bool
	// Merged is the local prompt with the other settings of LaunchDarkly. Only
	// a code-only variation that exists on both sides has it.
	Merged            *syncdomain.Variation
	MergedFingerprint string
	RevertsServer     bool
	// SyncedElsewhere is true when another working copy synced the resource
	// after this working copy wrote its sync.lock file. The plan is still
	// correct, because it compares against this working copy's lock.
	SyncedElsewhere bool
	// ServerHasStaleAttachmentPins is true when LaunchDarkly pins an older
	// version of a tool or skill than its latest version.
	ServerHasStaleAttachmentPins bool
	Diff                         variationDiffFields
	// LocalDiff is the change to the local file of an ActionUpdateBoth.
	LocalDiff variationDiffFields
	Error     string
	// changedAttachments are the tools and skills whose content differs
	// between the local file and LaunchDarkly.
	changedAttachments []ResourceID
	// restoreRef is the link that a restored variation file keeps.
	restoreRef *syncdomain.Reference
	// swappedFingerprint is the fingerprint of the local settings with the LaunchDarkly prompt.
	swappedFingerprint string
}

// Plan is the sync decision for each resource, in identity order.
type Plan struct {
	Resources []PlannedResource
}

// BuildPlan compares the baseline with the local and the server state. The
// server map must have an entry for each resource in local and in baseline.
func BuildPlan(baseline syncmanifest.Manifest, local []syncdomain.SyncedResource, server map[ResourceID]ServerResource) Plan {
	localByID := make(map[ResourceID]syncdomain.SyncedResource, len(local))
	for _, resource := range local {
		localByID[resource.ID()] = resource
	}
	baselineByID := make(map[ResourceID]string, len(baseline.Resources))
	for _, resource := range baseline.Resources {
		if resource.ResourceKind == syncdomain.KindVariation {
			baselineByID[resource.ID()] = resource.Fingerprint
		}
	}

	ids := make([]ResourceID, 0, len(localByID)+len(baselineByID))
	for id := range localByID {
		ids = append(ids, id)
	}
	for id := range baselineByID {
		if _, isLocal := localByID[id]; !isLocal {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, syncdomain.CompareResourceIDs)

	plan := Plan{Resources: make([]PlannedResource, 0, len(ids))}
	for _, id := range ids {
		localResource, isLocal := localByID[id]
		var localState *syncdomain.SyncedResource
		if isLocal {
			localState = &localResource
		}
		baselineFingerprint, tracked := baselineByID[id]
		plan.Resources = append(plan.Resources, planResource(id, localState, server[id], baselineFingerprint, tracked))
	}
	return plan
}

// planResource fingerprints both sides of one resource and chooses its action.
// local is nil when the resource has no local file.
func planResource(
	id ResourceID,
	local *syncdomain.SyncedResource,
	server ServerResource,
	baselineFingerprint string,
	tracked bool,
) PlannedResource {
	resource := PlannedResource{
		ID: id, BaselineFingerprint: baselineFingerprint, Server: server.Variation, ServerMode: server.ConfigMode,
		CodeOnly: server.CodeOnly,
	}
	fail := func(message string) PlannedResource {
		resource.Action, resource.Error = ActionError, message
		return resource
	}

	var err error
	if local != nil {
		resource.Upsert = local.Upsert
		variation := local.Variation
		if variation.Mode != server.ConfigMode {
			return fail(fmt.Sprintf("local mode %q does not match config mode %q", variation.Mode, server.ConfigMode))
		}
		resource.Local = &variation
		if resource.LocalFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation); err != nil {
			return fail(err.Error())
		}
	}
	if server.Variation != nil {
		if resource.ServerFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, *server.Variation); err != nil {
			return fail(fmt.Sprintf("invalid server variation: %s", err))
		}
	}

	if resource.CodeOnly && resource.Local != nil && server.Variation != nil {
		merged := server.Variation.WithPromptOf(*resource.Local)
		swapped := resource.Local.WithPromptOf(*server.Variation)
		resource.MergedFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, merged)
		if err == nil {
			resource.swappedFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, swapped)
		}
		if err != nil {
			return fail(fmt.Sprintf("invalid merged variation: %s", err))
		}
		resource.Merged = &merged
	}

	resource.Action = chooseAction(tracked, resource)
	if resource.Action == ActionError {
		resource.Error = "variation does not exist in LaunchDarkly; set upsert: true to create it"
	}
	resource.RevertsServer = revertsServer(resource)
	// The fingerprints do not include versions. If LaunchDarkly pins an older
	// attachment version, sync updates the pin even when the content matches.
	currentPins, latestPins, stalePins := attachmentPinDiff(resource.Server)
	resource.ServerHasStaleAttachmentPins = stalePins
	if stalePins && (resource.Action == ActionInSync || resource.Action == ActionUpdateManifest) {
		resource.Action = ActionUpdateServer
	}

	// The diff shows only changes in behavior. The raw JSON can differ when
	// the fingerprints match, for example because the API adds defaults.
	switch {
	case resource.Action == ActionUpdateBoth:
		resource.Diff = variationDiff(resource.Server, resource.Merged)
		resource.LocalDiff = variationDiff(resource.Local, resource.Merged)
	case resource.LocalFingerprint != resource.ServerFingerprint:
		resource.Diff = variationDiff(resource.Server, resource.Local)
	}
	if stalePins && resource.Action != ActionConflict {
		if resource.Diff == nil {
			resource.Diff = variationDiffFields{}
		}
		resource.Diff["attachment versions"] = variationFieldDiff{Before: currentPins, After: latestPins}
	}
	resource.changedAttachments = changedAttachmentIDs(resource)
	return resource
}

func chooseAction(tracked bool, resource PlannedResource) Action {
	action := chooseUIAndCodeAction(tracked, resource)
	if !resource.CodeOnly {
		return action
	}
	switch {
	case action == ActionUpdateLocal && resource.Local != nil:
		return chooseCodeOnlyMerge(resource, false)
	case action == ActionDeleteLocal, action == ActionConflict && resource.Server == nil:
		// The variation was archived in LaunchDarkly, so sync creates it again.
		return ActionCreateServer
	case action == ActionConflict:
		return chooseCodeOnlyMerge(resource, true)
	default:
		return action
	}
}

// chooseCodeOnlyMerge compares the merged variation with each side. bothChanged
// is true when the local file also changed, so a settings difference is a conflict.
func chooseCodeOnlyMerge(resource PlannedResource, bothChanged bool) Action {
	promptDiffers := resource.MergedFingerprint != resource.ServerFingerprint
	settingsDiffer := resource.MergedFingerprint != resource.LocalFingerprint
	tracked := resource.BaselineFingerprint != ""
	switch {
	case bothChanged && tracked && resource.MergedFingerprint == resource.BaselineFingerprint:
		// LaunchDarkly changed only the prompt, and the local file changed only the other settings.
		return ActionUpdateServer
	case bothChanged && tracked && resource.swappedFingerprint == resource.BaselineFingerprint:
		// The local file changed only the prompt, and LaunchDarkly changed only the other settings.
		return ActionUpdateBoth
	case settingsDiffer && bothChanged:
		return ActionConflict
	case promptDiffers && settingsDiffer:
		return ActionUpdateBoth
	case promptDiffers:
		return ActionUpdateServer
	case settingsDiffer:
		return ActionUpdateLocal
	default:
		return ActionUpdateManifest
	}
}

// revertsServer reports whether the action undoes an archive or a prompt change in LaunchDarkly.
func revertsServer(resource PlannedResource) bool {
	switch {
	case !resource.CodeOnly || !resource.Action.changesServer():
		return false
	case resource.Server == nil:
		return resource.BaselineFingerprint != ""
	case resource.ServerFingerprint == resource.BaselineFingerprint:
		return false
	default:
		// When the swapped variation matches the baseline, LaunchDarkly did not change the prompt.
		return resource.MergedFingerprint != resource.ServerFingerprint &&
			resource.swappedFingerprint != resource.BaselineFingerprint
	}
}

// chooseUIAndCodeAction compares each fingerprint with the baseline to find which side
// changed. It returns ActionError for a new local variation without upsert.
func chooseUIAndCodeAction(tracked bool, resource PlannedResource) Action {
	localExists := resource.Local != nil
	serverExists := resource.Server != nil

	// Without a baseline, sync cannot tell which side changed. It adopts equal
	// state, creates a new local variation that has upsert, and reports a
	// conflict for different content.
	if !tracked {
		switch {
		case localExists && serverExists && resource.LocalFingerprint == resource.ServerFingerprint:
			return ActionUpdateManifest
		case localExists && serverExists:
			return ActionConflict
		case localExists && resource.Upsert:
			return ActionCreateServer
		case localExists:
			return ActionError
		default:
			return ActionInSync
		}
	}

	// A missing local file means that this working copy does not have the
	// variation, for example after a Git pull or a branch switch. Sync restores
	// the file. Only "detach --archive" archives a variation.
	if !localExists && serverExists {
		return ActionUpdateLocal
	}

	// A tracked side that exists has a fingerprint, so "unchanged" also means
	// that the side exists.
	localUnchanged := resource.LocalFingerprint == resource.BaselineFingerprint
	serverUnchanged := resource.ServerFingerprint == resource.BaselineFingerprint
	switch {
	case localUnchanged && serverUnchanged:
		return ActionInSync
	case resource.LocalFingerprint == resource.ServerFingerprint:
		// Both sides changed to the same state, which can be a deletion.
		if !localExists {
			return ActionRemoveManifest
		}
		return ActionUpdateManifest
	case serverUnchanged:
		// Only the local file changed, so LaunchDarkly follows it.
		return ActionUpdateServer
	case localUnchanged && !serverExists:
		// The variation was archived in LaunchDarkly, so the local file goes too.
		return ActionDeleteLocal
	case localUnchanged:
		// Only LaunchDarkly changed, so the local file follows it.
		return ActionUpdateLocal
	default:
		return ActionConflict
	}
}

// RequiresConfirmation reports whether the plan changes a local file or
// LaunchDarkly. A change to the manifest only does not need a confirmation.
func (plan Plan) RequiresConfirmation() bool {
	return slices.ContainsFunc(plan.Resources, func(resource PlannedResource) bool {
		return resource.Action.changesServer() || resource.Action.changesLocal()
	})
}

// HasDestructiveActions reports whether the plan deletes a local file.
func (plan Plan) HasDestructiveActions() bool {
	return slices.ContainsFunc(plan.Resources, func(resource PlannedResource) bool {
		return resource.Action == ActionDeleteLocal
	})
}

// HasOverwrites reports whether the plan reverts a change made in LaunchDarkly.
func (plan Plan) HasOverwrites() bool {
	return slices.ContainsFunc(plan.Resources, func(resource PlannedResource) bool {
		return resource.RevertsServer
	})
}

// HasChanges reports whether the plan has work to do.
func (plan Plan) HasChanges() bool {
	return slices.ContainsFunc(plan.Resources, func(resource PlannedResource) bool {
		return resource.Action != ActionInSync
	})
}

// BlockingError returns an error for the first conflict or invalid resource.
func (plan Plan) BlockingError() error {
	for _, resource := range plan.Resources {
		switch resource.Action {
		case ActionConflict:
			return fmt.Errorf("cannot sync conflicted resource %s", resource.ID)
		case ActionError:
			return fmt.Errorf("cannot sync %s: %s", resource.ID, resource.Error)
		}
	}
	return nil
}
