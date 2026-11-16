package prompt

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncadapter "github.com/launchdarkly/ldcli/internal/sync/adapter"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// Action is the one change that reconciles a resource.
type Action string

const (
	ActionInSync         Action = "in_sync"
	ActionCreateServer   Action = "create_server"
	ActionUpdateServer   Action = "update_server"
	ActionUpdateLocal    Action = "update_local"
	ActionDeleteLocal    Action = "delete_local"
	ActionUpdateManifest Action = "update_manifest"
	ActionRemoveManifest Action = "remove_manifest"
	ActionConflict       Action = "conflict"
	ActionError          Action = "error"
)

// changesServer reports whether the action writes to LaunchDarkly.
func (action Action) changesServer() bool {
	return action == ActionCreateServer || action == ActionUpdateServer
}

// changesLocal reports whether the action writes a local file.
func (action Action) changesLocal() bool {
	return action == ActionUpdateLocal || action == ActionDeleteLocal
}

// ResourceID is the identity of a synchronized resource.
type ResourceID = syncdomain.ResourceID

// ServerResource is a variation in LaunchDarkly and the mode of its config.
// Variation is nil when the variation does not exist. The config owns the
// mode, and the variation API cannot change it.
type ServerResource struct {
	Variation  *syncdomain.Variation
	ConfigMode syncdomain.VariationMode
	// ConfigMissing is true when LaunchDarkly has no config for the
	// variation. ConfigMode is then the mode that a create gives the config.
	ConfigMissing bool
}

// PlannedResource is the local and the server state of one resource, and the
// action that reconciles them. A variation row has Local and Server. A tool or
// skill row has LocalAttachment and ServerAttachment. A nil side does not exist.
type PlannedResource struct {
	ID                  ResourceID
	Action              Action
	Upsert              bool
	BaselineFingerprint string
	LocalFingerprint    string
	ServerFingerprint   string
	ServerMode          syncdomain.VariationMode
	Direction           syncdomain.SyncDirection
	Local               *syncdomain.Variation
	Server              *syncdomain.Variation
	LocalAttachment     *syncdomain.Attachment
	ServerAttachment    *syncdomain.Attachment
	Source              *syncdomain.Reference
	SourceDependencies  []string
	IndexedSelections   []syncdomain.IndexedSelection
	LocalProjection     syncdomain.Projection
	ServerProjection    syncdomain.Projection
	// SyncedElsewhere is true when another working copy synced the resource
	// after this working copy wrote its sync.lock file. The plan is still
	// correct, because it compares against this working copy's lock.
	SyncedElsewhere bool
	// ServerHasStaleAttachmentPins is true when LaunchDarkly pins an older
	// version of a tool or skill than its latest version.
	ServerHasStaleAttachmentPins bool
	Diff                         variationDiffFields
	Error                        string
	Problem                      *syncdomain.Problem
	// changedAttachments are the tools and skills whose content differs
	// between the local file and LaunchDarkly.
	changedAttachments []ResourceID
	// attachmentConflict is true when the row is a conflict because a shared
	// tool or skill changed on the side that the row would overwrite.
	attachmentConflict bool
	// conflictUses are the conflicted variations that newly use the tool or
	// skill of this row on one side, when no push or pull needs the row.
	conflictUses []conflictUse
	// usedBy are the local variations that use the tool or skill of this row.
	usedBy []ResourceID
	// nameSource is where the name of a local skill comes from: a name in its
	// file, its key, or a flag of the add that creates it.
	nameSource string
	// directionOverride is true when the LaunchDarkly sync setting of the
	// resource chose the side that wins, instead of the three-way comparison.
	directionOverride bool
	// createsConfig is true when the create of the variation also creates
	// its config, because LaunchDarkly has none.
	createsConfig bool
}

func setProblem(resource *PlannedResource, problem *syncdomain.Problem) {
	resource.Action = ActionError
	resource.Problem = problem
	if problem != nil {
		resource.Error = problem.Message
	}
}

func blockUnsupportedFields(resource *PlannedResource) {
	if resource.Source == nil || resource.LocalProjection == nil {
		return
	}
	states := resource.LocalProjection.States()
	// The first field in name order is the one that the problem reports, so
	// the same plan always gives the same problem.
	for _, field := range slices.Sorted(maps.Keys(states)) {
		state := states[field]
		if state.Capability == syncdomain.CapabilityDirect {
			continue
		}
		code := state.Code
		if code == "" {
			code = syncdomain.ErrorFieldReadOnly
		}
		suggestion := "Map only syncable fields, or link the value's real source."
		if code == syncdomain.ErrorImportUnresolved && state.File != "" {
			suggestion = "Link the imported value directly: " +
				sourceLinkCommand(resource.ID, state.File, resource.Source.Format)
		}
		setProblem(resource, syncdomain.NewProblem(
			code,
			fmt.Sprintf("field %q is %s: %s", field, state.Capability, state.Reason),
			suggestion,
		).WithResource(resource.ID).WithFile(resource.Source.File, 0, 0))
		return
	}
}

func sourceLinkCommand(id ResourceID, file, format string) string {
	if id.Kind == syncdomain.KindVariation {
		configKey, variationKey, _ := id.VariationKeys()
		return fmt.Sprintf(
			"ldcli sync prompts link --file %s --format %s --project %s --config-key %s --key %s",
			file, format, id.ProjectKey, configKey, variationKey,
		)
	}
	return fmt.Sprintf(
		"ldcli sync %ss link --file %s --format %s --project %s --key %s",
		id.Kind, file, format, id.ProjectKey, id.LookupKey,
	)
}

// The sources of the name of a skill that sync creates.
const (
	nameFromFlag        = "flag"
	nameFromFrontMatter = "frontMatter"
	nameFromKey         = "key"
)

// hasLocal reports whether the resource has a local file.
func (resource PlannedResource) hasLocal() bool {
	return resource.Local != nil || resource.LocalAttachment != nil
}

// hasServer reports whether LaunchDarkly has the resource.
func (resource PlannedResource) hasServer() bool {
	return resource.Server != nil || resource.ServerAttachment != nil
}

// writtenSides returns the variation that a variation row writes, and the
// variation that it replaces. Both are nil for a row that writes none.
func (resource PlannedResource) writtenSides() (source, target *syncdomain.Variation) {
	switch {
	case resource.ID.Kind != syncdomain.KindVariation:
		return nil, nil
	case resource.Action.changesServer():
		return resource.Local, resource.Server
	case resource.Action == ActionUpdateLocal:
		return resource.Server, resource.Local
	default:
		return nil, nil
	}
}

// conflictUse is a conflicted variation that newly uses a tool or skill on one
// side. local is true when that side is the local file.
type conflictUse struct {
	variation ResourceID
	local     bool
}

// Plan is the sync decision for each resource, in identity order.
type Plan struct {
	Resources []PlannedResource
	// Scope is the resource kinds that the plan covers. The empty scope is
	// prompt variations.
	Scope syncdomain.Scope
	// standalone counts the tool and skill files, by kind, that no local
	// variation uses. Only a prompts plan counts them, because it leaves them
	// out.
	standalone map[syncdomain.Kind]int
	// ForceConfirmation makes link confirm a descriptor-only change.
	ForceConfirmation bool
}

// BuildPlan compares the baseline with the local and the server state. The
// server map must have an entry for each resource in local and in baseline.
func BuildPlan(baseline syncmanifest.Manifest, local []syncdomain.SyncedResource, server map[ResourceID]ServerResource) Plan {
	localByID := make(map[ResourceID]syncdomain.SyncedResource, len(local))
	for _, resource := range local {
		localByID[resource.ID()] = resource
	}
	baselineByID := make(map[ResourceID]syncmanifest.Resource, len(baseline.Resources))
	for _, resource := range baseline.Resources {
		if resource.ResourceKind == syncdomain.KindVariation {
			baselineByID[resource.ID()] = resource
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
		baselineResource, tracked := baselineByID[id]
		if localState != nil {
			tracked = baselineApplies(baselineResource, tracked, localState.Source)
		}
		row := planResource(id, localState, server[id], baselineResource.Fingerprint, tracked)
		checkSourceBaseline(&row, baselineResource, tracked)
		blockMissingDescriptor(&row, localState == nil, baselineResource, tracked)
		plan.Resources = append(plan.Resources, row)
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
		createsConfig: server.ConfigMissing,
	}
	fail := func(message string) PlannedResource {
		resource.Action, resource.Error = ActionError, message
		return resource
	}

	var err error
	if local != nil {
		resource.Upsert = local.Upsert
		resource.Source = local.Source
		resource.SourceDependencies = slices.Clone(local.Dependencies)
		resource.IndexedSelections = slices.Clone(local.IndexedSelections)
		variation := local.Variation
		if local.Source != nil {
			// The source implies a mode. The config decides the real one.
			projection, ok := local.Projection.(syncdomain.VariationProjection)
			if !ok {
				return fail(fmt.Sprintf("source returned %T for a variation", local.Projection))
			}
			if projection, err = projection.ForMode(server.ConfigMode); err != nil {
				return fail(err.Error())
			}
			variation = syncdomain.ApplyVariationProjection(variation, projection)
			resource.LocalProjection = projection
		}
		if variation.Mode != server.ConfigMode {
			return fail(fmt.Sprintf("local mode %q does not match config mode %q", variation.Mode, server.ConfigMode))
		}
		if err := syncdomain.ValidateAttachmentRefs(variation.Mode, variation.Tools, variation.Skills); err != nil {
			return fail(err.Error())
		}
		resource.Local = &variation
		if local.Source == nil {
			if resource.LocalFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation); err != nil {
				return fail(err.Error())
			}
			if server.Variation != nil {
				// A linked tool or skill of a managed variation compares on
				// the fields that the link maps, so the LaunchDarkly copy is
				// reduced to them here, for the fingerprint and for every
				// later check of the row.
				aligned, err := alignLinkedAttachments(*server.Variation, variation.Attachments)
				if err != nil {
					return fail(fmt.Sprintf("invalid server variation: %s", err))
				}
				server.Variation, resource.Server = &aligned, &aligned
			}
		}
	}
	if server.Variation != nil {
		if local != nil && local.Source != nil {
			resource.ServerProjection, err = syncdomain.ProjectVariation(*server.Variation, *local.Source)
			if err == nil {
				resource.ServerFingerprint, err = syncdomain.FingerprintProjection(id, resource.ServerProjection)
			}
		} else {
			resource.ServerFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, *server.Variation)
		}
		if err != nil {
			return fail(fmt.Sprintf("invalid server variation: %s", err))
		}
	}
	if resource.Source != nil && resource.LocalProjection != nil {
		if resource.ServerProjection != nil {
			resource.LocalProjection, err = syncdomain.FillReadOnlyFields(
				resource.LocalProjection, resource.ServerProjection,
			)
		}
		if err == nil {
			resource.LocalFingerprint, err = syncdomain.FingerprintProjection(id, resource.LocalProjection)
		}
		if err != nil {
			return fail(err.Error())
		}
	}

	resource.Action = chooseAction(tracked, resource)
	if resource.Action == ActionError {
		resource.Error = missingVariationError(id, server.ConfigMissing, resource.Source != nil)
	}
	if resource.Action != ActionCreateServer {
		resource.createsConfig = false
	}
	if resource.Action == ActionCreateServer {
		_, variationKey, _ := id.VariationKeys()
		rejectInvalidCreate(&resource, syncdomain.ValidateCreate(id.Kind, variationKey, local.Variation.Name, ""))
	}
	blockUnsupportedFields(&resource)
	// The fingerprints do not include versions. If LaunchDarkly pins an older
	// attachment version, sync updates the pin even when the content matches.
	currentPins, latestPins, stalePins := attachmentPinDiff(resource.Server)
	resource.ServerHasStaleAttachmentPins = stalePins
	upgradeStalePins(&resource)

	// The diff shows only changes in behavior. The raw JSON can differ when
	// the fingerprints match, for example because the API adds defaults.
	if resource.LocalFingerprint != resource.ServerFingerprint {
		if resource.Source != nil {
			resource.Diff = projectionDiff(resource.ServerProjection, resource.LocalProjection)
		} else {
			resource.Diff = variationDiff(resource.Server, resource.Local)
		}
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

// planAttachments plans one row for each tool and skill that has a local file
// or an entry in the baseline. server must have an entry for each of them,
// which is nil when LaunchDarkly does not have the attachment.
func planAttachments(
	baseline syncmanifest.Manifest,
	local []syncdomain.SyncedAttachment,
	server map[ResourceID]*syncdomain.Attachment,
) []PlannedResource {
	localByID := make(map[ResourceID]*syncdomain.SyncedAttachment, len(local))
	for index := range local {
		localByID[local[index].ID()] = &local[index]
	}
	baselineByID := make(map[ResourceID]syncmanifest.Resource, len(baseline.Resources))
	for _, resource := range baseline.Resources {
		if resource.ResourceKind != syncdomain.KindVariation {
			baselineByID[resource.ID()] = resource
		}
	}

	ids := slices.Collect(maps.Keys(localByID))
	for id := range baselineByID {
		if _, isLocal := localByID[id]; !isLocal {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, syncdomain.CompareResourceIDs)

	resources := make([]PlannedResource, 0, len(ids))
	for _, id := range ids {
		baselineResource, tracked := baselineByID[id]
		if local := localByID[id]; local != nil {
			tracked = baselineApplies(baselineResource, tracked, local.Source)
		}
		row := planSyncedAttachment(id, localByID[id], server[id], baselineResource.Fingerprint, tracked)
		checkSourceBaseline(&row, baselineResource, tracked)
		blockMissingDescriptor(&row, localByID[id] == nil, baselineResource, tracked)
		resources = append(resources, row)
	}
	return resources
}

// missingVariationError is the error of a local variation that LaunchDarkly
// does not have and that the plan cannot create. A managed file creates it
// with upsert. A descriptor has no upsert, so a link with --upsert creates
// it, and the config too when LaunchDarkly has none.
func missingVariationError(id ResourceID, configMissing, linked bool) string {
	configKey, variationKey, _ := id.VariationKeys()
	missing := "variation does not exist in LaunchDarkly"
	create := "set upsert: true to create it"
	if configMissing {
		missing = fmt.Sprintf("config %q does not exist in LaunchDarkly", configKey)
		create = "set upsert: true in the variation file to create the config and the variation"
	}
	if !linked {
		return missing + "; " + create
	}
	create = "Run link again with --upsert to create it, or create it in LaunchDarkly and run sync again."
	if configMissing {
		create = "Run link again with --upsert to create the config and the variation, or create them in LaunchDarkly and run sync again."
	}
	return fmt.Sprintf(
		"%s. %s To stop syncing it, run ldcli sync prompts detach --project %s --config-key %s --key %s.",
		missing, create, id.ProjectKey, configKey, variationKey,
	)
}

// baselineApplies reports whether a lock entry is a baseline for the local
// side. A managed file and a source projection have fingerprints of
// different schemas, so a lock entry without a projection schema says
// nothing about a linked resource, and the link plans it as a first sync.
func baselineApplies(baseline syncmanifest.Resource, tracked bool, source *syncdomain.Reference) bool {
	return tracked && (source == nil || baseline.FingerprintSchema != "")
}

// blockMissingDescriptor stops the row of a linked resource whose descriptor
// is gone. A restore would write a managed copy and drop the mapping, so the
// user restores the descriptor or detaches the resource.
func blockMissingDescriptor(resource *PlannedResource, missingLocal bool, baseline syncmanifest.Resource, tracked bool) {
	if !missingLocal || !tracked || baseline.FingerprintSchema == "" || resource.Action == ActionError {
		return
	}
	setProblem(resource, syncdomain.NewProblem(
		syncdomain.ErrorLinkedSourceMissing,
		"the descriptor of a linked resource is missing",
		"Restore the .ref.yaml descriptor, or run "+detachCommand(resource.ID)+".",
	).WithResource(resource.ID))
}

func checkSourceBaseline(resource *PlannedResource, baseline syncmanifest.Resource, tracked bool) {
	if resource.Source == nil || !tracked || resource.Action == ActionError {
		return
	}
	digest, err := syncdomain.ReferenceDigest(*resource.Source)
	behavior, behaviorErr := syncadapter.Behavior(resource.Source.Format)
	schemaChanged := baseline.FingerprintSchema != "" &&
		baseline.FingerprintSchema != syncdomain.ProjectionFingerprintSchema
	referenceChanged := baseline.ReferenceDigest != "" && baseline.ReferenceDigest != digest
	adapterChanged := baseline.AdapterBehavior != "" && baseline.AdapterBehavior != behavior
	switch {
	case err != nil:
		resource.Action, resource.Error = ActionError, err.Error()
	case behaviorErr != nil:
		resource.Action, resource.Error = ActionError, behaviorErr.Error()
	case schemaChanged || referenceChanged:
		setProblem(resource, syncdomain.NewProblem(
			syncdomain.ErrorBindingStale,
			"the source descriptor changed",
			"Run link again to review the mapping.",
		).WithResource(resource.ID))
	case adapterChanged:
		setProblem(resource, syncdomain.NewProblem(
			syncdomain.ErrorBindingStale,
			"adapter behavior changed",
			"Review the descriptor, then run link again.",
		).WithResource(resource.ID))
	case baseline.IndexedSelections != nil && !slices.Equal(baseline.IndexedSelections, resource.IndexedSelections):
		setProblem(resource, syncdomain.NewProblem(
			syncdomain.ErrorIndexedSelectionChanged,
			"an indexed source selection changed",
			"Review the descriptor and run link again. Use a keyed selector when the item has a stable key.",
		).WithResource(resource.ID).WithFile(resource.Source.File, 0, 0))
	}
}

// planAttachment fingerprints both sides of one tool or skill and chooses its
// action with the same rules as a variation.
func planAttachment(
	id ResourceID,
	local, server *syncdomain.Attachment,
	baselineFingerprint string,
	tracked bool,
) PlannedResource {
	var synced *syncdomain.SyncedAttachment
	if local != nil {
		synced = &syncdomain.SyncedAttachment{ProjectKey: id.ProjectKey, Attachment: *local}
	}
	return planSyncedAttachment(id, synced, server, baselineFingerprint, tracked)
}

func planSyncedAttachment(
	id ResourceID,
	local *syncdomain.SyncedAttachment,
	server *syncdomain.Attachment,
	baselineFingerprint string,
	tracked bool,
) PlannedResource {
	resource := PlannedResource{
		ID: id, BaselineFingerprint: baselineFingerprint, ServerAttachment: server,
	}
	fail := func(message string) PlannedResource {
		resource.Action, resource.Error = ActionError, message
		return resource
	}

	var err error
	if local != nil {
		resource.Upsert = local.Attachment.Upsert
		resource.LocalAttachment = &local.Attachment
		resource.Source = local.Source
		resource.SourceDependencies = slices.Clone(local.Dependencies)
		resource.IndexedSelections = slices.Clone(local.IndexedSelections)
		if resource.Source != nil {
			resource.LocalProjection = local.Projection
		} else {
			resource.LocalFingerprint, err = syncdomain.FingerprintAttachment(id.ProjectKey, local.Attachment)
		}
		if err != nil {
			return fail(err.Error())
		}
	}
	if local != nil && local.Attachment.Skill != nil {
		resource.nameSource = nameFromKey
		if local.Attachment.Skill.Name != "" {
			resource.nameSource = nameFromFrontMatter
		}
	}
	if server != nil {
		if resource.Source != nil {
			resource.ServerProjection, err = syncdomain.ProjectAttachment(*server, *resource.Source)
			if err == nil {
				resource.ServerFingerprint, err = syncdomain.FingerprintProjection(id, resource.ServerProjection)
			}
		} else {
			resource.ServerFingerprint, err = syncdomain.FingerprintAttachment(id.ProjectKey, *server)
		}
		if err != nil {
			return fail(fmt.Sprintf("invalid server %s: %s", id.Kind, err))
		}
	}
	if resource.Source != nil && resource.LocalProjection != nil {
		if resource.ServerProjection != nil {
			resource.LocalProjection, err = syncdomain.FillReadOnlyFields(
				resource.LocalProjection, resource.ServerProjection,
			)
		}
		if err == nil {
			resource.LocalFingerprint, err = syncdomain.FingerprintProjection(id, resource.LocalProjection)
		}
		if err != nil {
			return fail(err.Error())
		}
	}

	resource.Action = chooseAction(tracked, resource)
	switch resource.Action {
	case ActionError:
		resource.Error = missingAttachmentError(id, resource.Source != nil)
	case ActionCreateServer:
		rejectInvalidCreate(&resource, validateAttachmentCreate(local.Attachment))
	}
	blockUnsupportedFields(&resource)
	if resource.LocalFingerprint != resource.ServerFingerprint {
		if resource.Source != nil {
			resource.Diff = projectionDiff(resource.ServerProjection, resource.LocalProjection)
		} else {
			resource.Diff = attachmentDiff(server, resource.LocalAttachment)
		}
	}
	resource.changedAttachments = changedAttachmentIDs(resource)
	return resource
}

// missingAttachmentError explains how to create a tool or skill that exists
// only in a local file, or how to stop syncing it.
func missingAttachmentError(id ResourceID, linked bool) string {
	missing := missingAttachment(id)
	if linked {
		// A descriptor has no upsert, so a link with --upsert creates the
		// resource, or the user creates it in LaunchDarkly.
		missing = fmt.Sprintf("%s %q does not exist in LaunchDarkly. Run link again with --upsert to create it, or create it in LaunchDarkly and run sync again.",
			id.Kind, id.LookupKey)
	}
	return fmt.Sprintf("%s To stop syncing it, run ldcli sync %ss detach --project %s --key %s.",
		missing, id.Kind, id.ProjectKey, id.LookupKey)
}

// missingAttachment says that LaunchDarkly does not have the tool or skill
// of a local file without upsert, and names the command that creates it.
func missingAttachment(id ResourceID) string {
	consent := `add "upsert": true to its file`
	if id.Kind == syncdomain.KindSkill {
		consent = "add upsert: true to its front matter"
	}
	return fmt.Sprintf("%s %q does not exist in LaunchDarkly. To create it, %s and run ldcli sync %ss --project %s --key %s.",
		id.Kind, id.LookupKey, consent, id.Kind, id.ProjectKey, id.LookupKey)
}

// validateAttachmentCreate checks a new tool or skill against the create rules
// of LaunchDarkly.
func validateAttachmentCreate(attachment syncdomain.Attachment) error {
	var name, description string
	switch {
	case attachment.Tool != nil && attachment.Tool.Description != nil:
		description = *attachment.Tool.Description
	case attachment.Skill != nil:
		name, description = attachment.Skill.CreateName(), attachment.Skill.Description
	}
	return syncdomain.ValidateCreate(syncdomain.Kind(attachment.Kind), attachment.Key(), name, description)
}

// rejectInvalidCreate turns a create that LaunchDarkly would reject into an
// error, so that the apply does not stop halfway.
func rejectInvalidCreate(resource *PlannedResource, err error) {
	if err != nil {
		resource.Action, resource.Error = ActionError, err.Error()
	}
}

// chooseAction compares each fingerprint with the baseline to find which side
// changed. It returns ActionError for a new local resource without upsert.
func chooseAction(tracked bool, resource PlannedResource) Action {
	localExists := resource.hasLocal()
	serverExists := resource.hasServer()

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

// upgradeStalePins turns a row that has nothing else to do into a push, when
// LaunchDarkly pins older tool or skill versions than the latest.
func upgradeStalePins(resource *PlannedResource) {
	if resource.ServerHasStaleAttachmentPins &&
		(resource.Action == ActionInSync || resource.Action == ActionUpdateManifest) {
		resource.Action = ActionUpdateServer
	}
}

// RequiresConfirmation reports whether the plan changes a local file or
// LaunchDarkly. A change to the manifest only does not need a confirmation.
func (plan Plan) RequiresConfirmation() bool {
	if plan.ForceConfirmation {
		return true
	}
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

// HasChanges reports whether the plan has work to do.
func (plan Plan) HasChanges() bool {
	return slices.ContainsFunc(plan.Resources, func(resource PlannedResource) bool {
		return resource.Action != ActionInSync
	})
}

// BlockingError returns one error for each conflict or invalid resource, in
// plan order. A plan row shows only the first clause of an error, so this
// error carries the full message of every row.
func (plan Plan) BlockingError() error {
	var blocking []error
	for _, resource := range plan.Resources {
		switch resource.Action {
		case ActionConflict:
			blocking = append(blocking, fmt.Errorf("cannot sync conflicted resource %s", resource.ID.Label()))
		case ActionError:
			if resource.Problem != nil {
				blocking = append(blocking, fmt.Errorf("cannot sync %s: %w", resource.ID.Label(), resource.Problem))
			} else {
				blocking = append(blocking, fmt.Errorf("cannot sync %s: %s", resource.ID.Label(), resource.Error))
			}
		}
	}
	return errors.Join(blocking...)
}
