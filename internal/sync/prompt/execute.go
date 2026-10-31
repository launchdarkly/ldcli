package prompt

import (
	"errors"
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
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

	var sourceReplacements []synclocal.SourceReplacement
	var sourceIndexes []int
	for index, resource := range plan.Resources {
		if resource.Action != ActionUpdateLocal || resource.Source == nil {
			continue
		}
		projection, err := syncdomain.ChangedProjection(resource.LocalProjection, resource.ServerProjection)
		if err != nil {
			return nil, manifest, err
		}
		sourceReplacements = append(sourceReplacements, synclocal.SourceReplacement{
			ID: resource.ID, Mode: resource.ServerMode, Reference: *resource.Source,
			Projection: projection, ExpectedFingerprint: resource.LocalFingerprint,
			ExpectedProjection:        resource.LocalProjection,
			ExpectedIndexedSelections: resource.IndexedSelections,
		})
		sourceIndexes = append(sourceIndexes, index)
	}
	if len(sourceReplacements) != 0 {
		_, sourceErr := localStore.ReplaceSources(sourceReplacements)
		for _, index := range sourceIndexes {
			resource := plan.Resources[index]
			outcome := ResourceOutcome{ID: resource.ID, Action: resource.Action, Status: OutcomeSucceeded}
			if sourceErr != nil {
				outcome.Status, outcome.Error = OutcomeFailed, sourceErr.Error()
			} else {
				recordSuccessfulChange(&next, resource)
			}
			outcomes[index], handled[index] = outcome, true
		}
		if sourceErr != nil {
			failures = append(failures, sourceErr)
		}
	}
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
		case resource.Action == ActionUpdateManifest:
			recordSuccessfulChange(&next, resource)
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
			recordSuccessfulChange(&next, resource)
		default:
			outcome.Status, outcome.Error = OutcomeSkipped, "resource is not executable"
		}

		if outcome.Status == OutcomeSucceeded {
			if variation := attachmentManifestState(resource); variation != nil {
				settled, _ := attachments.settledAttachments(resource.ID.ProjectKey, variation.Attachments)
				if err := recordAttachmentState(&next, repositoryRoot, resource.ID.ProjectKey, settled); err != nil {
					fail(err)
				}
			}
		}
		outcomes[index] = outcome
	}

	// The plan puts tools and skills before the variations that pin them. A
	// deletion of a tool or a skill runs last instead, so that the pull of a
	// variation that drops the reference runs first.
	var deletions []int
	for index, resource := range plan.Resources {
		if resource.ID.Kind != syncdomain.KindVariation && resource.Action == ActionDeleteLocal {
			deletions = append(deletions, index)
			continue
		}
		execute(index)
	}
	for _, index := range deletions {
		execute(index)
	}

	return outcomes, next, errors.Join(failures...)
}

func recordAttachmentState(
	manifest *syncmanifest.Manifest,
	repositoryRoot, projectKey string,
	attachments []syncdomain.Attachment,
) error {
	if err := manifest.SetAttachments(projectKey, attachments); err != nil {
		return err
	}
	if len(attachments) == 0 {
		return nil
	}
	ids := make(map[ResourceID]struct{}, len(attachments))
	for _, attachment := range attachments {
		ids[attachment.ID(projectKey)] = struct{}{}
	}
	local, err := synclocal.CompileAttachments(repositoryRoot, func(id ResourceID) bool {
		_, included := ids[id]
		return included
	})
	if err != nil {
		return err
	}
	for _, attachment := range local {
		if attachment.Source == nil {
			continue
		}
		projection, err := syncdomain.ProjectAttachment(attachment.Attachment, *attachment.Source)
		if err != nil {
			return err
		}
		fingerprint, err := syncdomain.FingerprintProjection(attachment.ID(), projection)
		if err != nil {
			return err
		}
		manifest.SetFingerprint(attachment.ID(), fingerprint)
		if err := manifest.SetSourceMetadata(attachment.ID(), attachment.Source); err != nil {
			return err
		}
		manifest.SetIndexedSelections(attachment.ID(), attachment.IndexedSelections)
	}
	return nil
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
func recordSuccessfulChange(manifest *syncmanifest.Manifest, resource PlannedResource) {
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
	_ = manifest.SetSourceMetadata(resource.ID, resource.Source)
	if resource.Source != nil {
		manifest.SetIndexedSelections(resource.ID, resource.IndexedSelections)
	}
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
	if resource.ServerHasStaleAttachmentPins && server != nil {
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
	return nil, nil
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
		if resource.Source != nil && resource.Server != nil {
			projection, ok := resource.LocalProjection.(syncdomain.VariationProjection)
			if !ok {
				return nil, fmt.Errorf("source projection for %s is %T", resource.ID.Label(), resource.LocalProjection)
			}
			local = syncdomain.ApplyVariationProjection(*resource.Server, projection)
		}
		local = withExplicitDetach(local, resource.Server)
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
		local := *resource.LocalAttachment
		if resource.Source != nil && resource.ServerAttachment != nil {
			var err error
			local, err = syncdomain.ApplyAttachmentProjection(*resource.ServerAttachment, resource.LocalProjection)
			if err != nil {
				return 0, err
			}
		}
		local.Upsert = local.Upsert || resource.Action == ActionCreateServer
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
				value := variationForSourceMode(variation.Variation, mode)
				projection := variation.Projection
				if typed, ok := projection.(syncdomain.VariationProjection); ok && typed.Mode != value.Mode {
					var converted syncdomain.VariationProjection
					converted, err = syncdomain.ProjectVariation(value, *variation.Source)
					converted.FieldStates = typed.FieldStates
					projection = converted
				}
				if err == nil && expected != nil {
					projection, err = syncdomain.FillReadOnlyFields(projection, expected)
				}
				if err != nil {
					return "", err
				}
				return syncdomain.FingerprintProjection(id, projection)
			}
			return syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation.Variation)
		}
	}
	return "", nil
}
