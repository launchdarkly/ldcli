package prompt

import (
	"encoding/json"
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// Action describes the one change needed to reconcile a resource.
type Action string

const (
	ActionInSync         Action = "in_sync"
	ActionCreateServer   Action = "create_server"
	ActionUpdateServer   Action = "update_server"
	ActionArchiveServer  Action = "archive_server"
	ActionUpdateLocal    Action = "update_local"
	ActionDeleteLocal    Action = "delete_local"
	ActionUpdateManifest Action = "update_manifest"
	ActionRemoveManifest Action = "remove_manifest"
	ActionConflict       Action = "conflict"
	ActionError          Action = "error"
)

// ResourceID is the shared identity of a synchronized resource.
type ResourceID = syncdomain.ResourceID

// ServerResource contains a variation and the mode owned by its parent config.
// The variation APIs cannot change that mode.
type ServerResource struct {
	Variation  *syncdomain.Variation
	ConfigMode syncdomain.VariationMode
}

// PlannedResource contains the compared local/server state and the action
// selected for one resource.
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
	Diff                variationDiffFields
	Error               string
}

// Plan contains sync decisions in deterministic resource order.
type Plan struct {
	Resources []PlannedResource
}

// BuildPlan compares the committed baseline with current local and server
// state. Server must contain an entry, with a nil Variation for absence, for
// every candidate resource.
func BuildPlan(baseline syncmanifest.Manifest, local []syncdomain.SyncedResource, server map[ResourceID]ServerResource) Plan {
	localByID := make(map[ResourceID]syncdomain.SyncedResource, len(local))
	resourceIDs := make(map[ResourceID]struct{}, len(local)+len(baseline.Resources))

	for _, resource := range local {
		id := ResourceID{Kind: resource.Kind, ProjectKey: resource.ProjectKey, LookupKey: resource.LookupKey}
		localByID[id] = resource
		resourceIDs[id] = struct{}{}
	}

	baselineByID := make(map[ResourceID]string, len(baseline.Resources))
	for _, resource := range baseline.Resources {
		id := resource.ID()
		baselineByID[id] = resource.Fingerprint
		resourceIDs[id] = struct{}{}
	}

	orderedIDs := make([]ResourceID, 0, len(resourceIDs))
	for id := range resourceIDs {
		orderedIDs = append(orderedIDs, id)
	}
	slices.SortFunc(orderedIDs, syncdomain.CompareResourceIDs)

	plan := Plan{Resources: make([]PlannedResource, 0, len(orderedIDs))}
	for _, id := range orderedIDs {
		localResource, localExists := localByID[id]
		baselineFingerprint, tracked := baselineByID[id]
		plan.Resources = append(plan.Resources, buildPlannedResource(
			id, localResource, localExists, server[id], baselineFingerprint, tracked,
		))
	}

	return plan
}

// buildPlannedResource validates one local/server pair before choosing its action.
func buildPlannedResource(
	id ResourceID,
	localResource syncdomain.SyncedResource,
	localExists bool,
	serverResource ServerResource,
	baselineFingerprint string,
	tracked bool,
) PlannedResource {
	resource := PlannedResource{
		ID: id, BaselineFingerprint: baselineFingerprint, Server: serverResource.Variation, ServerMode: serverResource.ConfigMode,
	}

	if localExists {
		resource.Upsert = localResource.Upsert
		var variation syncdomain.Variation
		if err := json.Unmarshal(localResource.Payload, &variation); err != nil {
			resource.Action, resource.Error = ActionError, fmt.Sprintf("decode local variation: %s", err)
			return resource
		}
		if variation.Mode != serverResource.ConfigMode {
			resource.Action = ActionError
			resource.Error = fmt.Sprintf("local mode %q does not match config mode %q", variation.Mode, serverResource.ConfigMode)
			return resource
		}
		resource.Local = &variation
	}

	var err error
	if resource.Local != nil {
		resource.LocalFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, *resource.Local)
		if err != nil {
			resource.Action, resource.Error = ActionError, err.Error()
			return resource
		}
	}
	if resource.Server != nil {
		resource.ServerFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, *resource.Server)
		if err != nil {
			resource.Action, resource.Error = ActionError, fmt.Sprintf("invalid server variation: %s", err)
			return resource
		}
	}

	resource.Action = chooseAction(tracked, resource)
	if resource.Action == ActionError {
		resource.Error = "variation does not exist in LaunchDarkly; set upsert: true to create it"
	}
	resource.Diff = variationDiff(resource.Server, resource.Local)
	return resource
}

// RequiresConfirmation reports whether the plan changes local or server
// resources. Manifest-only bookkeeping is safe to perform without prompting.
func (plan Plan) RequiresConfirmation() bool {
	for _, resource := range plan.Resources {
		switch resource.Action {
		case ActionCreateServer, ActionUpdateServer, ActionArchiveServer, ActionUpdateLocal, ActionDeleteLocal:
			return true
		}
	}
	return false
}

// BlockingError returns a readable error for conflicts or invalid resources.
func (plan Plan) BlockingError() error {
	for _, resource := range plan.Resources {
		switch resource.Action {
		case ActionConflict:
			return fmt.Errorf("cannot sync conflicted resource %s/%s", resource.ID.ProjectKey, resource.ID.LookupKey)
		case ActionError:
			return fmt.Errorf("cannot sync %s/%s: %s", resource.ID.ProjectKey, resource.ID.LookupKey, resource.Error)
		}
	}
	return nil
}

// HasChanges reports whether synchronization has work to perform.
func (plan Plan) HasChanges() bool {
	for _, resource := range plan.Resources {
		if resource.Action != ActionInSync {
			return true
		}
	}
	return false
}

// chooseAction compares local and server fingerprints with the manifest
// baseline to determine which side changed.
func chooseAction(tracked bool, resource PlannedResource) Action {
	localExists := resource.Local != nil
	serverExists := resource.Server != nil

	// Without a baseline there is no direction to infer. Adopt identical state,
	// honor explicit local upsert, and require a choice for divergent content.
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

	localUnchanged := resource.LocalFingerprint == resource.BaselineFingerprint
	serverUnchanged := resource.ServerFingerprint == resource.BaselineFingerprint
	switch {
	// Neither side moved from the common ancestor.
	case localUnchanged && serverUnchanged:
		return ActionInSync
	// Both sides independently reached the same state, including deletion.
	case resource.LocalFingerprint == resource.ServerFingerprint:
		if !localExists && !serverExists {
			return ActionRemoveManifest
		}
		return ActionUpdateManifest
	// Only local moved, so local is authoritative for this run.
	case !localUnchanged && serverUnchanged:
		if !localExists {
			return ActionArchiveServer
		}
		if serverExists {
			return ActionUpdateServer
		}
		return ActionConflict
	// Only LaunchDarkly moved, so pull or mirror its deletion locally.
	case localUnchanged && !serverUnchanged:
		if !serverExists {
			return ActionDeleteLocal
		}
		if localExists {
			return ActionUpdateLocal
		}
		return ActionConflict
	// Both sides moved to different states.
	default:
		return ActionConflict
	}
}

// variationDiff builds the structured diff rendered during plan review.
func variationDiff(before, after *syncdomain.Variation) variationDiffFields {
	if before == nil && after == nil {
		return nil
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if before == nil {
		beforeJSON = nil
	}
	if after == nil {
		afterJSON = nil
	}
	if string(beforeJSON) == string(afterJSON) {
		return nil
	}

	return variationDiffFields{
		"variation": {Before: beforeJSON, After: afterJSON},
	}
}
