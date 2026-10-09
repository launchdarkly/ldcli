package prompt

import (
	"errors"
	"fmt"

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
	outcomes := make([]ResourceOutcome, 0, len(plan.Resources))
	var failures []error
	attachments := newAttachmentResolver(client)
	fail := func(outcome *ResourceOutcome, err error) {
		outcome.Status, outcome.Error = OutcomeFailed, err.Error()
		failures = append(failures, fmt.Errorf("%s: %w", outcome.ID, err))
	}

	for _, resource := range plan.Resources {
		outcome := ResourceOutcome{ID: resource.ID, Action: resource.Action, Status: OutcomeSucceeded}
		switch {
		case resource.Action == ActionInSync:
		case resource.Action == ActionUpdateManifest:
			next.SetFingerprint(resource.ID, resource.LocalFingerprint)
		case resource.Action == ActionRemoveManifest:
			next.Remove(resource.ID)
		case resource.Action.changesServer() || resource.Action.changesLocal():
			var localFile *syncdomain.Variation
			if file, ok := localFiles[resource.ID]; ok {
				localFile = &file.Variation
			}
			if err := applyResourceChange(repositoryRoot, localStore, client, attachments, resource, localFile); err != nil {
				fail(&outcome, err)
				break
			}
			recordSuccessfulChange(&next, resource)
		default:
			outcome.Status, outcome.Error = OutcomeSkipped, "resource is not executable"
		}

		if outcome.Status == OutcomeSucceeded {
			if variation := attachmentManifestState(resource); variation != nil {
				if err := next.SetAttachments(resource.ID.ProjectKey, variation.Attachments); err != nil {
					fail(&outcome, err)
				}
			}
		}
		outcomes = append(outcomes, outcome)
	}

	if len(failures) == 0 {
		variations, err := compileWorkspace(repositoryRoot)
		if err != nil {
			failures = append(failures, err)
		} else {
			next.RemoveUnusedAttachments(variations)
			next.SetRefs(variations)
		}
	}
	return outcomes, next, errors.Join(failures...)
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
	case resource.Action.changesServer():
		manifest.SetFingerprint(resource.ID, resource.LocalFingerprint)
	default:
		manifest.SetFingerprint(resource.ID, resource.ServerFingerprint)
	}
}

// applyResourceChange applies one action that writes to LaunchDarkly or to a
// local file. localFile is the variation as its file stores it, or nil.
func applyResourceChange(
	repositoryRoot string,
	localStore synclocal.Store,
	client syncapi.Client,
	attachments *attachmentResolver,
	resource PlannedResource,
	localFile *syncdomain.Variation,
) error {
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
		return err
	}
	if err := verifyLocalResult(repositoryRoot, resource); err != nil {
		return err
	}

	// The local file now matches LaunchDarkly. If LaunchDarkly pins an older
	// attachment version, update the pin too.
	if resource.ServerHasStaleAttachmentPins && server != nil {
		configKey, _, err := resource.ID.VariationKeys()
		if err != nil {
			return err
		}
		pinned, err := server.PinnedToLatest()
		if err != nil {
			return err
		}
		return client.UpdateVariation(resource.ID.ProjectKey, configKey, pinned)
	}
	return nil
}

// applyServerChange writes one variation to LaunchDarkly. If the write fails
// without a response, it reads the variation to find whether the write
// succeeded.
func applyServerChange(client syncapi.Client, attachments *attachmentResolver, resource PlannedResource) error {
	configKey, variationKey, err := resource.ID.VariationKeys()
	if err != nil {
		return err
	}
	projectKey := resource.ID.ProjectKey

	var writeErr error
	switch resource.Action {
	case ActionCreateServer:
		variation, err := attachments.resolveVariation(projectKey, *resource.Local)
		if err != nil {
			return err
		}
		writeErr = client.CreateVariation(projectKey, configKey, variation)
	case ActionUpdateServer:
		variation, err := attachments.resolveVariation(projectKey, withExplicitDetach(*resource.Local, resource.Server))
		if err != nil {
			return err
		}
		writeErr = client.UpdateVariation(projectKey, configKey, variation)
	default:
		return fmt.Errorf("action %q does not change LaunchDarkly", resource.Action)
	}
	if writeErr == nil || !syncapi.MutationMayHaveSucceeded(writeErr) {
		return writeErr
	}

	state, err := client.ReadVariation(projectKey, configKey, variationKey)
	if err != nil {
		return errors.Join(writeErr, fmt.Errorf("verify server variation: %w", err))
	}
	actual := ""
	if state.Exists {
		if err := newAttachmentCache(client).hydrate(projectKey, &state.Variation); err != nil {
			return errors.Join(writeErr, err)
		}
		if actual, err = syncdomain.FingerprintVariation(projectKey, resource.ID.LookupKey, state.Variation); err != nil {
			return errors.Join(writeErr, err)
		}
	}
	switch actual {
	case resource.LocalFingerprint:
		return nil
	case resource.ServerFingerprint:
		return writeErr
	default:
		return fmt.Errorf("server variation changed concurrently after an uncertain write: %w", writeErr)
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
			Ref:             resource.restoreRef,
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
// action wrote. For ActionUpdateLocal, resource.Server is in the form of the
// local file.
func verifyLocalResult(repositoryRoot string, resource PlannedResource) error {
	actual, err := readLocalFingerprint(repositoryRoot, resource.ID)
	if err != nil {
		return err
	}

	expected := ""
	if resource.Action == ActionUpdateLocal {
		expected, err = syncdomain.FingerprintVariation(resource.ID.ProjectKey, resource.ID.LookupKey, *resource.Server)
		if err != nil {
			return err
		}
	}
	if actual != expected {
		return errors.New("local variation did not match the expected state after sync")
	}
	return nil
}

// readLocalFingerprint returns the fingerprint of one local variation, or an
// empty string when the variation has no local file.
func readLocalFingerprint(repositoryRoot string, id ResourceID) (string, error) {
	variations, err := compileWorkspace(repositoryRoot)
	if err != nil {
		return "", err
	}
	for _, variation := range variations {
		if variation.ID() == id {
			return syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation.Variation)
		}
	}
	return "", nil
}
