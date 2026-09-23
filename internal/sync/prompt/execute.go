package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

type currentResourceState struct {
	localFingerprint  string
	serverFingerprint string
	serverMode        syncdomain.VariationMode
}

// executePlan applies each independently executable resource and advances the
// manifest only for resources that succeed.
func executePlan(
	repositoryRoot string,
	localStore synclocal.Store,
	client syncapi.Client,
	manifest syncmanifest.Manifest,
	plan Plan,
) ([]ResourceOutcome, syncmanifest.Manifest, error) {
	// Keep the reviewed baseline immutable while successful resources advance
	// the result manifest independently.
	manifest.Resources = append([]syncmanifest.Resource(nil), manifest.Resources...)

	outcomes := make([]ResourceOutcome, 0, len(plan.Resources))
	var failures []error

	for _, resource := range plan.Resources {
		outcome := ResourceOutcome{ID: resource.ID, Action: resource.Action, Status: OutcomeSucceeded}

		switch resource.Action {
		case ActionInSync:
			// The manifest already represents this state.
		case ActionUpdateManifest:
			manifest.SetFingerprint(resource.ID, resource.LocalFingerprint)
		case ActionRemoveManifest:
			manifest.Remove(resource.ID)
		case ActionCreateServer, ActionUpdateServer, ActionArchiveServer, ActionUpdateLocal, ActionDeleteLocal:
			if err := applyResourceChange(repositoryRoot, localStore, client, resource); err != nil {
				outcome.Status, outcome.Error = OutcomeFailed, err.Error()
				failures = append(failures, fmt.Errorf("%s/%s: %w", resource.ID.ProjectKey, resource.ID.LookupKey, err))
				break
			}
			recordSuccessfulChange(&manifest, resource)
		default:
			outcome.Status, outcome.Error = OutcomeSkipped, "resource is not executable"
		}

		outcomes = append(outcomes, outcome)
	}

	return outcomes, manifest, errors.Join(failures...)
}

// applyResourceChange verifies the reviewed state and applies one local or
// server mutation.
func applyResourceChange(repositoryRoot string, localStore synclocal.Store, client syncapi.Client, resource PlannedResource) error {
	if err := verifyResourceUnchanged(repositoryRoot, client, resource); err != nil {
		return err
	}
	if changesServer(resource.Action) {
		return applyServerChange(client, resource)
	}
	if err := applyLocalChange(localStore, resource); err != nil {
		return err
	}
	return verifyLocalResult(repositoryRoot, resource)
}

// recordSuccessfulChange updates the manifest to the state selected by the
// completed action.
func recordSuccessfulChange(manifest *syncmanifest.Manifest, resource PlannedResource) {
	switch {
	case resource.Action == ActionArchiveServer || resource.Action == ActionDeleteLocal:
		manifest.Remove(resource.ID)
	case changesServer(resource.Action):
		manifest.SetFingerprint(resource.ID, resource.LocalFingerprint)
	default:
		manifest.SetFingerprint(resource.ID, resource.ServerFingerprint)
	}
}

// verifyResourceUnchanged prevents a reviewed action from using stale local or
// server state.
func verifyResourceUnchanged(repositoryRoot string, client syncapi.Client, reviewed PlannedResource) error {
	current, err := readCurrentResourceState(repositoryRoot, client, reviewed.ID)
	if err != nil {
		return err
	}
	if current.localFingerprint != reviewed.LocalFingerprint ||
		current.serverFingerprint != reviewed.ServerFingerprint ||
		current.serverMode != reviewed.ServerMode {
		return fmt.Errorf("resource changed after review; run sync again")
	}
	return nil
}

// applyServerChange performs one variation mutation through the existing
// public config APIs.
func applyServerChange(client syncapi.Client, resource PlannedResource) error {
	configKey, variationKey, err := splitVariationLookupKey(resource.ID.LookupKey)
	if err != nil {
		return err
	}

	var mutationErr error
	switch resource.Action {
	case ActionCreateServer:
		mutationErr = client.CreateVariation(resource.ID.ProjectKey, configKey, *resource.Local)
	case ActionUpdateServer:
		mutationErr = client.UpdateVariation(resource.ID.ProjectKey, configKey, *resource.Local)
	case ActionArchiveServer:
		mutationErr = client.ArchiveVariation(resource.ID.ProjectKey, configKey, variationKey)
	}
	if mutationErr == nil {
		return nil
	}

	// A network error can hide a successful write, so re-read only when the
	// mutation result is uncertain.
	state, readErr := client.ReadVariation(resource.ID.ProjectKey, configKey, variationKey)
	if readErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("verify server variation: %w", readErr))
	}

	actualFingerprint := ""
	if state.Exists {
		actualFingerprint, readErr = syncdomain.FingerprintVariation(resource.ID.ProjectKey, resource.ID.LookupKey, state.Variation)
		if readErr != nil {
			return errors.Join(mutationErr, readErr)
		}
	}
	expectedFingerprint := resource.LocalFingerprint
	if resource.Action == ActionArchiveServer {
		expectedFingerprint = ""
	}

	switch actualFingerprint {
	case expectedFingerprint:
		return nil
	case resource.ServerFingerprint:
		return mutationErr
	default:
		return fmt.Errorf("server variation changed concurrently after an uncertain write: %w", mutationErr)
	}
}

// verifyLocalResult confirms that a local file mutation produced the selected
// server state.
func verifyLocalResult(repositoryRoot string, resource PlannedResource) error {
	actualFingerprint, err := readLocalFingerprint(repositoryRoot, resource.ID)
	if err != nil {
		return err
	}

	expectedFingerprint := resource.ServerFingerprint
	if resource.Action == ActionDeleteLocal {
		expectedFingerprint = ""
	}
	if actualFingerprint != expectedFingerprint {
		return fmt.Errorf("local variation did not match the expected state after sync")
	}
	return nil
}

// readCurrentResourceState reads the local and server fingerprints used for
// optimistic concurrency checks.
func readCurrentResourceState(repositoryRoot string, client syncapi.Client, id ResourceID) (currentResourceState, error) {
	localFingerprint, err := readLocalFingerprint(repositoryRoot, id)
	if err != nil {
		return currentResourceState{}, err
	}

	serverResource, err := readServerResource(client, id)
	if err != nil {
		return currentResourceState{}, err
	}
	serverFingerprint := ""
	if serverResource.Variation != nil {
		serverFingerprint, err = syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, *serverResource.Variation)
	}
	return currentResourceState{
		localFingerprint: localFingerprint, serverFingerprint: serverFingerprint, serverMode: serverResource.ConfigMode,
	}, err
}

// readLocalFingerprint returns the current fingerprint for one local resource,
// or an empty fingerprint when the resource does not exist.
func readLocalFingerprint(repositoryRoot string, id ResourceID) (string, error) {
	localResources, err := synclocal.CompileWorkspace(repositoryRoot)
	if err != nil {
		return "", err
	}

	for _, resource := range localResources {
		if resource.Kind != id.Kind || resource.ProjectKey != id.ProjectKey || resource.LookupKey != id.LookupKey {
			continue
		}
		var variation syncdomain.Variation
		if err := json.Unmarshal(resource.Payload, &variation); err != nil {
			return "", err
		}
		return syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation)
	}
	return "", nil
}

// readServerResource reads one supported resource from LaunchDarkly.
func readServerResource(client syncapi.Client, id ResourceID) (ServerResource, error) {
	if id.Kind != syncdomain.KindVariation {
		return ServerResource{}, fmt.Errorf("unsupported sync resource kind %q", id.Kind)
	}
	configKey, variationKey, err := splitVariationLookupKey(id.LookupKey)
	if err != nil {
		return ServerResource{}, err
	}
	state, err := client.ReadVariation(id.ProjectKey, configKey, variationKey)
	if err != nil {
		return ServerResource{}, err
	}

	resource := ServerResource{ConfigMode: state.ConfigMode}
	if state.Exists {
		resource.Variation = &state.Variation
	}
	return resource, nil
}

// splitVariationLookupKey separates a config key from its variation key.
func splitVariationLookupKey(lookupKey string) (string, string, error) {
	configKey, variationKey, ok := strings.Cut(lookupKey, "/")
	if !ok || configKey == "" || variationKey == "" || strings.Contains(variationKey, "/") {
		return "", "", fmt.Errorf("invalid variation lookup key %q", lookupKey)
	}
	return configKey, variationKey, nil
}

// changesServer reports whether an action mutates LaunchDarkly.
func changesServer(action Action) bool {
	return action == ActionCreateServer || action == ActionUpdateServer || action == ActionArchiveServer
}
