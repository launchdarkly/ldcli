package prompt

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncadapter "github.com/launchdarkly/ldcli/internal/sync/adapter"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// executePlan applies each action of the plan, and returns the outcomes and
// the new manifest. The manifest records only the actions that succeed. The
// manifest argument does not change. localFiles has each local variation as
// its file stores it.
func executePlan(
	repositoryRoot string,
	localStore synclocal.Store,
	client syncapi.Client,
	manifest syncmanifest.Manifest,
	plan Plan,
	localFiles map[ResourceID]syncdomain.SyncedResource,
) ([]ResourceOutcome, syncmanifest.Manifest, error) {
	// A conflict choice applies to the full plan. Write nothing until every
	// conflict has a choice, so that a shared attachment cannot change while
	// one of its variations has no choice.
	if err := plan.BlockingError(); err != nil {
		return nil, manifest, err
	}

	next := manifest.Clone()
	outcomes := make([]ResourceOutcome, len(plan.Resources))
	var failures []error
	attachments := newAttachmentResolver(client)
	handled := make(map[int]bool)
	execute := func(index int) {
		if handled[index] {
			return
		}
		resource := plan.Resources[index]
		outcome := ResourceOutcome{ID: resource.ID, Action: resource.Action, Status: OutcomeSucceeded}
		fail := func(err error) {
			outcome.Status, outcome.Error = OutcomeFailed, err.Error()
			failures = append(failures, fmt.Errorf("%s: %w", outcome.ID, err))
		}

		switch {
		case resource.Action == ActionInSync:
			settleUnchangedAttachment(attachments, resource)
			// A relink changes only the descriptor, so the lock records the
			// new mapping digest, adapter behavior, and indexed selections.
			if resource.Source != nil {
				if err := recordSource(&next, resource.ID, resource.Source, resource.IndexedSelections); err != nil {
					fail(err)
				}
			}
		case resource.Action == ActionUpdateManifest:
			settleUnchangedAttachment(attachments, resource)
			if err := recordSuccessfulChange(&next, resource); err != nil {
				fail(err)
			}
		case resource.Action == ActionRemoveManifest:
			next.Remove(resource.ID)
		case resource.Action.changesServer() || resource.Action.changesLocal():
			var pins []syncdomain.AttachmentRef
			var version int
			var err error
			if resource.ID.Kind == syncdomain.KindVariation {
				var localFile *syncdomain.Variation
				if file, ok := localFiles[resource.ID]; ok {
					localFile = &file.Variation
				}
				if resource, err = attachments.settledRow(resource); err == nil {
					pins, err = applyResourceChange(repositoryRoot, localStore, client, attachments, resource, localFile)
				}
			} else {
				version, err = applyAttachmentChange(repositoryRoot, localStore, attachments, resource)
			}
			if err != nil {
				fail(err)
				break
			}
			outcome.pins, outcome.Version = pins, version
			if err := recordSuccessfulChange(&next, resource); err != nil {
				fail(err)
				break
			}
		default:
			outcome.Status, outcome.Error = OutcomeSkipped, "resource is not executable"
		}

		if outcome.Status == OutcomeSucceeded {
			if variation := attachmentManifestState(resource); variation != nil {
				settled, _ := attachments.settledAttachments(resource.ID.ProjectKey, variation.Attachments)
				// A row that synced nothing records only the tools and
				// skills that the lock does not have yet. A tool or skill
				// that this run did not sync keeps its baseline, so that a
				// local edit to it stays a local change.
				if !resource.Action.changesServer() && !resource.Action.changesLocal() {
					settled = slices.DeleteFunc(settled, func(attachment syncdomain.Attachment) bool {
						return next.Has(attachment.ID(resource.ID.ProjectKey))
					})
				}
				if err := recordAttachmentState(&next, repositoryRoot, resource.ID.ProjectKey, settled); err != nil {
					fail(err)
				}
			}
		}
		outcomes[index] = outcome
	}

	// Settle tools and skills before linked prompt pulls. A failed dependency
	// must block its prompt before the prompt changes any repository file.
	var deletions []int
	for index, resource := range plan.Resources {
		if resource.ID.Kind == syncdomain.KindVariation ||
			resource.Action == ActionUpdateLocal && resource.Source != nil {
			continue
		}
		if resource.Action == ActionDeleteLocal {
			deletions = append(deletions, index)
			continue
		}
		execute(index)
	}

	// Resources that can edit the same physical file form one atomic group.
	// An error blocks only that group, so unrelated source files can continue.
	for _, indexes := range sourcePullGroups(repositoryRoot, plan.Resources) {
		resources, replacements, groupErr := settleSourcePulls(plan.Resources, indexes, attachments)
		if groupErr == nil {
			_, groupErr = localStore.ReplaceSources(replacements)
		}
		if groupErr == nil {
			// The written files must compile to the content that LaunchDarkly
			// has for every member, as a managed pull is checked. The group
			// wrote its files as one unit, so one mismatch fails the group
			// before the lock records any member against the new files.
			groupErr = verifySourceGroup(repositoryRoot, resources)
		}
		for position, index := range indexes {
			resource := resources[position]
			outcome := ResourceOutcome{ID: resource.ID, Action: resource.Action, Status: OutcomeSucceeded}
			switch {
			case groupErr != nil:
				outcome.Status, outcome.Error = OutcomeFailed, groupErr.Error()
				if resource.ID.Kind != syncdomain.KindVariation {
					attachments.settle(resource.ID, syncdomain.Attachment{}, groupErr)
				}
			default:
				pins, pinErr := refreshStalePins(client, resource, resource.Server)
				if pinErr != nil {
					outcome.Status, outcome.Error = OutcomeFailed, pinErr.Error()
					failures = append(failures, fmt.Errorf("%s: %w", resource.ID, pinErr))
					break
				}
				outcome.pins = pins
				if err := recordSuccessfulChange(&next, resource); err != nil {
					outcome.Status, outcome.Error = OutcomeFailed, err.Error()
					failures = append(failures, fmt.Errorf("%s: %w", resource.ID, err))
					break
				}
				if resource.ID.Kind != syncdomain.KindVariation && resource.ServerAttachment != nil {
					attachments.settle(resource.ID, *resource.ServerAttachment, nil)
				}
				if resource.Server != nil {
					err := recordAttachmentState(&next, repositoryRoot, resource.ID.ProjectKey, resource.Server.Attachments)
					if err != nil {
						outcome.Status, outcome.Error = OutcomeFailed, err.Error()
						failures = append(failures, fmt.Errorf("%s: %w", resource.ID, err))
					}
				}
			}
			outcomes[index], handled[index] = outcome, true
		}
		if groupErr != nil {
			failures = append(failures, groupErr)
		}
	}

	for index, resource := range plan.Resources {
		if resource.ID.Kind == syncdomain.KindVariation {
			execute(index)
		}
	}

	// A deletion runs last, so that a variation pull can drop its reference.
	for _, index := range deletions {
		execute(index)
	}

	return outcomes, next, errors.Join(failures...)
}

// verifySourceGroup checks the local result of every member of a document
// group, and returns the first mismatch with the resource that it belongs to.
func verifySourceGroup(repositoryRoot string, resources []PlannedResource) error {
	for _, resource := range resources {
		if err := verifyLocalResult(repositoryRoot, resource); err != nil {
			return fmt.Errorf("%s: %w", resource.ID, err)
		}
	}
	return nil
}

// settleSourcePulls resolves the attachment versions of each variation in a
// document group and builds its source replacements. On an error, every row
// keeps its planned state, so the caller can report the group as failed.
func settleSourcePulls(
	resources []PlannedResource,
	indexes []int,
	attachments *attachmentResolver,
) ([]PlannedResource, []synclocal.SourceReplacement, error) {
	settled := make([]PlannedResource, len(indexes))
	replacements := make([]synclocal.SourceReplacement, 0, len(indexes))
	var groupErr error
	for position, index := range indexes {
		resource := resources[index]
		settled[position] = resource
		if groupErr != nil {
			continue
		}
		if resource.ID.Kind == syncdomain.KindVariation {
			if resource, groupErr = attachments.settledRow(resource); groupErr != nil {
				continue
			}
			settled[position] = resource
		}
		replacement, err := sourcePullReplacement(resource)
		if err != nil {
			groupErr = err
			continue
		}
		replacements = append(replacements, replacement)
	}
	return settled, replacements, groupErr
}

func sourcePullReplacement(resource PlannedResource) (synclocal.SourceReplacement, error) {
	// A source edit can replace a value but not remove it. If LaunchDarkly
	// has no value for a mapped field, the pull cannot make the source match,
	// so it stops here instead of recording a state that the next plan
	// would see as a local edit.
	if field, missing := missingServerField(resource.LocalProjection, resource.ServerProjection); missing {
		return synclocal.SourceReplacement{}, syncdomain.NewProblem(
			syncdomain.ErrorSourceNotWritable,
			fmt.Sprintf("LaunchDarkly has no value for mapped field %q, so the pull cannot clear it", field),
			"Set the field in LaunchDarkly, or remove it from the map and link again.",
		).WithResource(resource.ID).WithFile(resource.Source.File, 0, 0)
	}
	projection, err := syncdomain.ChangedProjection(resource.LocalProjection, resource.ServerProjection)
	if err != nil {
		return synclocal.SourceReplacement{}, err
	}
	var relationshipAttachments []syncdomain.Attachment
	if resource.Server != nil {
		relationshipAttachments = slices.Clone(resource.Server.Attachments)
	}
	return synclocal.SourceReplacement{
		ID: resource.ID, Reference: *resource.Source,
		Projection: projection, ExpectedFingerprint: resource.LocalFingerprint,
		ExpectedProjection:        resource.LocalProjection,
		ExpectedIndexedSelections: resource.IndexedSelections,
		RelationshipAttachments:   relationshipAttachments,
	}, nil
}

// resolvedSourcePath returns the path of the file that a repository-relative
// source path resolves to, or the path itself when it does not resolve.
func resolvedSourcePath(repositoryRoot, file string) string {
	resolved, err := filepath.EvalSymlinks(filepath.Join(repositoryRoot, filepath.FromSlash(file)))
	if err != nil {
		return file
	}
	return resolved
}

// missingServerField returns the first mapped field, in name order, that the
// local projection has a value for and the server projection does not.
func missingServerField(local, server syncdomain.Projection) (syncdomain.Field, bool) {
	return missingField(local, server)
}

// checkSourcePushClears stops a push whose source has no value for a mapped
// field that LaunchDarkly has a value for. A push merges the mapped values
// onto the LaunchDarkly copy and cannot clear a field, so the push would
// record a state that the next plan would pull back into the source.
func checkSourcePushClears(resource PlannedResource) error {
	if field, missing := missingField(resource.ServerProjection, resource.LocalProjection); missing {
		return syncdomain.NewProblem(
			syncdomain.ErrorSourceNotWritable,
			fmt.Sprintf("the source has no value for mapped field %q, so the push cannot clear it", field),
			"Set the field in the source, or remove it from the map and link again.",
		).WithResource(resource.ID).WithFile(resource.Source.File, 0, 0)
	}
	return nil
}

// missingField returns the first mapped field, in name order, that the
// first projection has a value for and the second does not.
func missingField(from, to syncdomain.Projection) (syncdomain.Field, bool) {
	if from == nil || to == nil {
		return "", false
	}
	for _, field := range slices.Sorted(maps.Keys(to.States())) {
		_, fromOK := from.Value(field)
		_, toOK := to.Value(field)
		if fromOK && !toOK {
			return field, true
		}
	}
	return "", false
}

// sourcePullGroups groups the pull rows that can edit one file. Two
// descriptors can name one file two ways, for example through a symbolic
// link, so a file is known by its resolved path.
func sourcePullGroups(repositoryRoot string, resources []PlannedResource) [][]int {
	var indexes []int
	var files []map[string]struct{}
	for index, resource := range resources {
		if resource.Action != ActionUpdateLocal || resource.Source == nil {
			continue
		}
		indexes = append(indexes, index)
		groupFiles := map[string]struct{}{resolvedSourcePath(repositoryRoot, resource.Source.File): {}}
		for _, dependency := range resource.SourceDependencies {
			groupFiles[resolvedSourcePath(repositoryRoot, dependency)] = struct{}{}
		}
		files = append(files, groupFiles)
	}
	parent := make([]int, len(indexes))
	for index := range parent {
		parent[index] = index
	}
	var root func(int) int
	root = func(index int) int {
		if parent[index] != index {
			parent[index] = root(parent[index])
		}
		return parent[index]
	}
	for left := range files {
		for right := left + 1; right < len(files); right++ {
			overlap := false
			for file := range files[left] {
				if _, overlap = files[right][file]; overlap {
					break
				}
			}
			if overlap {
				parent[root(right)] = root(left)
			}
		}
	}
	positions := make(map[int]int)
	var groups [][]int
	for position, index := range indexes {
		groupRoot := root(position)
		group, ok := positions[groupRoot]
		if !ok {
			group = len(groups)
			positions[groupRoot] = group
			groups = append(groups, nil)
		}
		groups[group] = append(groups[group], index)
	}
	return groups
}

func settleUnchangedAttachment(resolver *attachmentResolver, resource PlannedResource) {
	if resource.ID.Kind == syncdomain.KindVariation || resource.ServerAttachment == nil {
		return
	}
	attachment := *resource.ServerAttachment
	if resource.Source != nil && resource.LocalProjection != nil {
		if merged, err := syncdomain.ApplyAttachmentProjection(attachment, resource.LocalProjection); err == nil {
			merged.Version = attachment.Version
			attachment = merged
		}
	}
	resolver.settle(resource.ID, attachment, nil)
}

func recordAttachmentState(
	manifest *syncmanifest.Manifest,
	repositoryRoot, projectKey string,
	attachments []syncdomain.Attachment,
) error {
	if len(attachments) == 0 {
		return manifest.SetAttachments(projectKey, attachments)
	}
	ids := make(map[ResourceID]struct{}, len(attachments))
	for _, attachment := range attachments {
		ids[attachment.ID(projectKey)] = struct{}{}
	}
	// The linked tools and skills compile before the manifest changes, so a
	// compile error leaves the manifest as it was instead of recording a
	// fingerprint of the wrong schema.
	local, err := synclocal.CompileAttachments(repositoryRoot, func(id ResourceID) bool {
		_, included := ids[id]
		return included
	})
	if err != nil {
		return err
	}
	if err := manifest.SetAttachments(projectKey, attachments); err != nil {
		return err
	}
	settled := make(map[ResourceID]syncdomain.Attachment, len(attachments))
	for _, attachment := range attachments {
		settled[attachment.ID(projectKey)] = attachment
	}
	for _, attachment := range local {
		if attachment.Source == nil {
			continue
		}
		// The baseline is the content that LaunchDarkly has after the
		// action, seen through the local mapping. The repository copy can
		// hold an edit that no tool or skill row synced in this run, and
		// that edit must stay a local change.
		synced, ok := settled[attachment.ID()]
		if !ok {
			continue
		}
		fingerprint, err := syncdomain.SyncedAttachment{
			ProjectKey: projectKey, Attachment: synced, Source: attachment.Source,
		}.Fingerprint()
		if err != nil {
			return err
		}
		manifest.SetFingerprint(attachment.ID(), fingerprint)
		if err := recordSource(manifest, attachment.ID(), attachment.Source, attachment.IndexedSelections); err != nil {
			return err
		}
	}
	return nil
}

// attachmentToPublish returns the tool or skill that a push sends. A linked
// row merges its mapped fields onto the plan-time LaunchDarkly copy and keeps
// its projection, so the publish merges them again onto the latest copy and
// an edit to an unowned field in LaunchDarkly survives.
func attachmentToPublish(resource PlannedResource) (syncdomain.Attachment, error) {
	local := *resource.LocalAttachment
	if resource.Source == nil || resource.ServerAttachment == nil {
		return local, nil
	}
	if err := checkSourcePushClears(resource); err != nil {
		return syncdomain.Attachment{}, err
	}
	merged, err := syncdomain.ApplyAttachmentProjection(*resource.ServerAttachment, resource.LocalProjection)
	if err != nil {
		return syncdomain.Attachment{}, err
	}
	merged.Projection = resource.LocalProjection
	return merged, nil
}

// attachmentManifestState returns the variation whose attachments the
// manifest records after the action, or nil.
func attachmentManifestState(resource PlannedResource) *syncdomain.Variation {
	switch resource.Action {
	case ActionInSync, ActionUpdateManifest, ActionCreateServer, ActionUpdateServer:
		return resource.Local
	case ActionUpdateLocal:
		return resource.Server
	default:
		return nil
	}
}

// recordSuccessfulChange records the state that a completed action chose.
// The content hash and the descriptor binding are one record, so an error
// of the binding fails the row.
func recordSuccessfulChange(manifest *syncmanifest.Manifest, resource PlannedResource) error {
	switch {
	case resource.Action == ActionDeleteLocal:
		manifest.Remove(resource.ID)
	case resource.Action == ActionUpdateManifest:
		manifest.SetFingerprint(resource.ID, resource.LocalFingerprint)
	case resource.Action.changesServer():
		manifest.SetFingerprint(resource.ID, resource.LocalFingerprint)
	default:
		manifest.SetFingerprint(resource.ID, resource.ServerFingerprint)
	}
	return recordSource(manifest, resource.ID, resource.Source, resource.IndexedSelections)
}

// recordSource records the mapping digest, the adapter behavior, and the
// indexed selections of a descriptor-backed resource in the manifest.
func recordSource(
	manifest *syncmanifest.Manifest,
	id ResourceID,
	source *syncdomain.Reference,
	selections []syncdomain.IndexedSelection,
) error {
	behavior := ""
	if source != nil {
		var err error
		if behavior, err = syncadapter.Behavior(source.Format); err != nil {
			return err
		}
	}
	return manifest.SetSource(id, source, behavior, selections)
}

// applyResourceChange applies one action that writes to LaunchDarkly or to a
// local file. localFile is the variation as its file stores it, or nil. It
// returns the tool and skill versions that the variation pins when the action
// writes the variation to LaunchDarkly.
func applyResourceChange(
	repositoryRoot string,
	localStore synclocal.Store,
	client syncapi.Client,
	attachments *attachmentResolver,
	resource PlannedResource,
	localFile *syncdomain.Variation,
) ([]syncdomain.AttachmentRef, error) {
	if resource.Action.changesServer() {
		return applyServerChange(client, attachments, resource)
	}

	// The local write uses the form of the local file, which can leave out
	// model values from the model config. A pin update below needs the
	// complete variation, so keep it.
	server := resource.Server
	if resource.Action == ActionUpdateLocal {
		variation := variationForLocalFile(*resource.Server, localFile, resource.Local)
		resource.Server = &variation
	}
	if err := applyLocalChange(localStore, resource); err != nil {
		return nil, err
	}
	if err := verifyLocalResult(repositoryRoot, resource); err != nil {
		return nil, err
	}

	// The local file now matches LaunchDarkly. If LaunchDarkly pins an older
	// attachment version, update the pin too.
	return refreshStalePins(client, resource, server)
}

// refreshStalePins moves the tool and skill pins of a variation in
// LaunchDarkly to the latest versions after a pull, when LaunchDarkly pins
// older versions. server is the complete variation that LaunchDarkly has. It
// returns the pins that it wrote, or nil when the pins are current.
func refreshStalePins(
	client syncapi.Client,
	resource PlannedResource,
	server *syncdomain.Variation,
) ([]syncdomain.AttachmentRef, error) {
	if !resource.ServerHasStaleAttachmentPins || server == nil {
		return nil, nil
	}
	configKey, _, err := resource.ID.VariationKeys()
	if err != nil {
		return nil, err
	}
	pinned, err := server.PinnedToLatest()
	if err != nil {
		return nil, err
	}
	return pinsOf(pinned), client.UpdateVariation(resource.ID.ProjectKey, configKey, pinned)
}

// latestLinkedVariation reads the variation that a linked push merges its
// mapped fields onto. The mapped fields must still have the content that the
// review saw, or the push would write over a change that the plan did not
// compare.
func latestLinkedVariation(
	client syncapi.Client,
	resource PlannedResource,
	configKey, variationKey string,
) (syncdomain.Variation, error) {
	projectKey := resource.ID.ProjectKey
	state, err := client.ReadVariation(projectKey, configKey, variationKey)
	if err != nil {
		return syncdomain.Variation{}, err
	}
	if !state.Exists {
		return syncdomain.Variation{}, linkedResourceDeleted(&resource)
	}
	if err := newAttachmentCache(client).hydrate(projectKey, &state.Variation); err != nil {
		return syncdomain.Variation{}, err
	}
	projection, err := syncdomain.ProjectVariation(state.Variation, *resource.Source)
	if err != nil {
		return syncdomain.Variation{}, err
	}
	fingerprint, err := syncdomain.FingerprintProjection(resource.ID, projection)
	if err != nil {
		return syncdomain.Variation{}, err
	}
	if fingerprint != resource.ServerFingerprint {
		return syncdomain.Variation{}, syncdomain.NewProblem(
			syncdomain.ErrorStateChanged, "the mapped fields changed in LaunchDarkly after review", "Run sync again.",
		).WithResource(resource.ID)
	}
	return state.Variation, nil
}

// applyServerChange writes one variation to LaunchDarkly. If the write fails
// without a response, or a create gets a 409 because the variation exists
// now, it reads the variation to find whether LaunchDarkly has the content.
func applyServerChange(
	client syncapi.Client,
	attachments *attachmentResolver,
	resource PlannedResource,
) ([]syncdomain.AttachmentRef, error) {
	configKey, variationKey, err := resource.ID.VariationKeys()
	if err != nil {
		return nil, err
	}
	projectKey := resource.ID.ProjectKey

	var variation syncdomain.Variation
	var writeErr error
	switch resource.Action {
	case ActionCreateServer:
		if variation, err = attachments.resolveVariation(projectKey, *resource.Local); err != nil {
			return nil, err
		}
		writeErr = client.CreateVariation(projectKey, configKey, variation)
	case ActionUpdateServer:
		local := *resource.Local
		server := resource.Server
		if resource.Source != nil && resource.Server != nil {
			projection, ok := resource.LocalProjection.(syncdomain.VariationProjection)
			if !ok {
				return nil, fmt.Errorf("source projection for %s is %T", resource.ID.Label(), resource.LocalProjection)
			}
			if err := checkSourcePushClears(resource); err != nil {
				return nil, err
			}
			// The push merges the mapped fields onto the latest LaunchDarkly
			// copy, as a tool or skill publish does, so an edit to an
			// unowned field after the review survives.
			latest, err := latestLinkedVariation(client, resource, configKey, variationKey)
			if err != nil {
				return nil, err
			}
			server = &latest
			local = syncdomain.ApplyVariationProjection(latest, projection)
			local = withOwnedRelationshipContent(local, *resource.Local, *resource.Source)
		}
		local = withExplicitDetach(local, server)
		if variation, err = attachments.resolveVariation(projectKey, local); err != nil {
			return nil, err
		}
		writeErr = client.UpdateVariation(projectKey, configKey, variation)
	default:
		return nil, fmt.Errorf("action %q does not change LaunchDarkly", resource.Action)
	}
	if writeErr == nil {
		return pinsOf(variation), nil
	}
	createConflict := resource.Action == ActionCreateServer && syncapi.IsConflict(writeErr)
	if !syncapi.MutationMayHaveSucceeded(writeErr) && !createConflict {
		return nil, writeErr
	}

	state, err := client.ReadVariation(projectKey, configKey, variationKey)
	if err != nil {
		return nil, errors.Join(writeErr, fmt.Errorf("verify server variation: %w", err))
	}
	actual := ""
	if state.Exists {
		if err := newAttachmentCache(client).hydrate(projectKey, &state.Variation); err != nil {
			return nil, errors.Join(writeErr, err)
		}
		if resource.Source != nil {
			var projection syncdomain.VariationProjection
			projection, err = syncdomain.ProjectVariation(state.Variation, *resource.Source)
			if err == nil {
				actual, err = syncdomain.FingerprintProjection(resource.ID, projection)
			}
		} else {
			actual, err = syncdomain.FingerprintVariation(projectKey, resource.ID.LookupKey, state.Variation)
		}
		if err != nil {
			return nil, errors.Join(writeErr, err)
		}
	}
	switch actual {
	case resource.LocalFingerprint:
		return pinsOf(variation), nil
	case resource.ServerFingerprint:
		return nil, writeErr
	default:
		return nil, fmt.Errorf("server variation changed concurrently after an uncertain write: %w", writeErr)
	}
}

func withOwnedRelationshipContent(
	merged, authored syncdomain.Variation,
	reference syncdomain.Reference,
) syncdomain.Variation {
	attachments := slices.DeleteFunc(slices.Clone(merged.Attachments), func(attachment syncdomain.Attachment) bool {
		return reference.Tools != nil && attachment.Kind == syncdomain.AttachmentTool ||
			reference.Skills != nil && attachment.Kind == syncdomain.AttachmentSkill
	})
	for _, attachment := range authored.Attachments {
		if reference.Tools != nil && attachment.Kind == syncdomain.AttachmentTool ||
			reference.Skills != nil && attachment.Kind == syncdomain.AttachmentSkill {
			attachments = append(attachments, attachment)
		}
	}
	merged.Attachments = attachments
	merged.SortAttachments()
	return merged
}

// pinsOf returns the tool and skill references of a variation with their versions.
func pinsOf(variation syncdomain.Variation) []syncdomain.AttachmentRef {
	return slices.Concat(variation.Tools, variation.Skills)
}

// applyAttachmentChange applies the action of one tool or skill row, and
// returns the version that LaunchDarkly has after the action.
func applyAttachmentChange(
	repositoryRoot string,
	localStore synclocal.Store,
	attachments *attachmentResolver,
	resource PlannedResource,
) (int, error) {
	switch resource.Action {
	case ActionCreateServer, ActionUpdateServer:
		// A create row exists only with upsert, or after the user chose the
		// local side of a conflict in which LaunchDarkly has no copy. Both
		// allow the create, but not the update of a copy that appears later.
		local, err := attachmentToPublish(resource)
		if err != nil {
			return 0, err
		}
		local.Upsert = local.Upsert || resource.Action == ActionCreateServer
		attachments.review(resource)
		published, err := attachments.resolve(resource.ID.ProjectKey, local, resource.Action == ActionCreateServer)
		return published.Version, err
	case ActionUpdateLocal:
		server := syncdomain.SyncedAttachment{ProjectKey: resource.ID.ProjectKey, Attachment: *resource.ServerAttachment}
		_, err := localStore.ReplaceAttachments([]syncdomain.SyncedAttachment{server})
		if err == nil {
			err = verifyLocalResult(repositoryRoot, resource)
		}
		attachments.settle(resource.ID, server.Attachment, err)
		return server.Attachment.Version, err
	case ActionDeleteLocal:
		// The pull of a variation that drops the reference ran first. If that
		// pull failed, the variation still uses the file, so keep it.
		variations, err := compileWorkspace(repositoryRoot)
		if err != nil {
			return 0, err
		}
		if index := slices.IndexFunc(variations, func(variation syncdomain.SyncedResource) bool {
			return uses(variation, resource.ID)
		}); index >= 0 {
			return 0, fmt.Errorf("%s still uses it", variations[index].ID().Label())
		}
		if _, err := localStore.DeleteResources([]syncdomain.ResourceID{resource.ID}); err != nil {
			return 0, err
		}
		return 0, verifyLocalResult(repositoryRoot, resource)
	default:
		return 0, fmt.Errorf("action %q does not change a tool or a skill", resource.Action)
	}
}

// withExplicitDetach sends an empty reference list when the local variation
// has no tools or skills but LaunchDarkly has some. The API treats an absent
// list as unchanged.
func withExplicitDetach(local syncdomain.Variation, server *syncdomain.Variation) syncdomain.Variation {
	if server == nil {
		return local
	}
	if local.Tools == nil && len(server.Tools) != 0 {
		local.Tools = []syncdomain.AttachmentRef{}
	}
	if local.Skills == nil && len(server.Skills) != 0 {
		local.Skills = []syncdomain.AttachmentRef{}
	}
	return local
}

// applyLocalChange writes the LaunchDarkly state of one variation to its
// local file, or deletes the file.
func applyLocalChange(store synclocal.Store, resource PlannedResource) error {
	configKey, variationKey, err := resource.ID.VariationKeys()
	if err != nil {
		return err
	}

	switch resource.Action {
	case ActionUpdateLocal:
		_, err := store.ReplaceVariations([]synclocal.VariationReplacement{{
			ProjectKey:      resource.ID.ProjectKey,
			ConfigKey:       configKey,
			CreateIfMissing: resource.Local == nil,
			Variation:       *resource.Server,
		}})
		return err
	case ActionDeleteLocal:
		_, err := store.DeleteVariations([]synclocal.VariationDeletion{{
			ProjectKey: resource.ID.ProjectKey, ConfigKey: configKey, VariationKey: variationKey,
		}})
		return err
	default:
		return fmt.Errorf("action %q does not change a local file", resource.Action)
	}
}

// verifyLocalResult makes sure that the local file has the state that the
// action wrote. For ActionUpdateLocal on a variation, resource.Server is in
// the form of the local file.
func verifyLocalResult(repositoryRoot string, resource PlannedResource) error {
	actual, err := readLocalFingerprint(
		repositoryRoot, resource.ID, resource.ServerMode, resource.ServerProjection,
	)
	if err != nil {
		return err
	}

	expected := ""
	switch {
	case resource.Action != ActionUpdateLocal:
	case resource.Source != nil:
		// A linked resource compares the projection of its mapped fields.
		expected = resource.ServerFingerprint
	case resource.ID.Kind == syncdomain.KindVariation:
		expected, err = syncdomain.FingerprintVariation(resource.ID.ProjectKey, resource.ID.LookupKey, *resource.Server)
		if err != nil {
			return err
		}
	default:
		expected = resource.ServerFingerprint
	}
	if actual != expected {
		return fmt.Errorf("local %s did not match the expected state after sync", resource.ID.Kind)
	}
	return nil
}

// readLocalFingerprint returns the fingerprint of one local resource, or an
// empty string when the resource has no local file.
func readLocalFingerprint(
	repositoryRoot string,
	id ResourceID,
	mode syncdomain.VariationMode,
	expected syncdomain.Projection,
) (string, error) {
	if id.Kind != syncdomain.KindVariation {
		attachments, err := synclocal.CompileAttachments(repositoryRoot, func(candidate ResourceID) bool { return candidate == id })
		if err != nil || len(attachments) == 0 {
			return "", err
		}
		if attachments[0].Source != nil {
			projection := attachments[0].Projection
			if expected != nil {
				projection, err = syncdomain.FillReadOnlyFields(projection, expected)
			}
			if err != nil {
				return "", err
			}
			return syncdomain.FingerprintProjection(id, projection)
		}
		return syncdomain.FingerprintAttachment(id.ProjectKey, attachments[0].Attachment)
	}
	variations, err := compileWorkspace(repositoryRoot)
	if err != nil {
		return "", err
	}
	for _, variation := range variations {
		if variation.ID() == id {
			if variation.Source != nil {
				typed, ok := variation.Projection.(syncdomain.VariationProjection)
				if !ok {
					return "", fmt.Errorf("source returned %T for a variation", variation.Projection)
				}
				projection, err := typed.ForMode(mode)
				if err != nil {
					return "", err
				}
				var aligned syncdomain.Projection = projection
				if expected != nil {
					if aligned, err = syncdomain.FillReadOnlyFields(aligned, expected); err != nil {
						return "", err
					}
				}
				return syncdomain.FingerprintProjection(id, aligned)
			}
			return syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation.Variation)
		}
	}
	return "", nil
}
