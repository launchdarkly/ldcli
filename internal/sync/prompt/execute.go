package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// attachmentResolver versions shared dependencies once per execution and
// reuses both successful results and failures across every consumer.
type attachmentResolver struct {
	client   syncapi.Client
	resolved map[attachmentID]syncdomain.Attachment
	failures map[attachmentID]error
}

func newAttachmentResolver(client syncapi.Client) *attachmentResolver {
	return &attachmentResolver{
		client:   client,
		resolved: make(map[attachmentID]syncdomain.Attachment),
		failures: make(map[attachmentID]error),
	}
}

func (resolver *attachmentResolver) resolveVariation(projectKey string, variation syncdomain.Variation) (syncdomain.Variation, error) {
	// A value copy still shares slice backing arrays with the reviewed plan;
	// clone pins before assigning server versions so review state stays immutable.
	variation.Tools = slices.Clone(variation.Tools)
	variation.Skills = slices.Clone(variation.Skills)
	if err := resolver.resolveReferences(projectKey, syncdomain.AttachmentTool, variation.Tools, variation); err != nil {
		return syncdomain.Variation{}, err
	}
	if err := resolver.resolveReferences(projectKey, syncdomain.AttachmentSkill, variation.Skills, variation); err != nil {
		return syncdomain.Variation{}, err
	}
	return variation, nil
}

func (resolver *attachmentResolver) resolveReferences(
	projectKey string,
	kind syncdomain.AttachmentKind,
	references []syncdomain.AttachmentRef,
	variation syncdomain.Variation,
) error {
	for index := range references {
		local, ok := variation.Attachment(kind, references[index].Key)
		if !ok {
			return fmt.Errorf("%s %q content is missing", kind, references[index].Key)
		}
		resolved, err := resolver.resolve(projectKey, local)
		if err != nil {
			return err
		}
		references[index].Version = resolved.Version
	}
	return nil
}

func (resolver *attachmentResolver) resolve(projectKey string, local syncdomain.Attachment) (syncdomain.Attachment, error) {
	id := attachmentID{projectKey: projectKey, kind: local.Kind, key: local.Key()}
	if err, failed := resolver.failures[id]; failed {
		return syncdomain.Attachment{}, err
	}
	if attachment, ok := resolver.resolved[id]; ok {
		return attachment, nil
	}

	remote, err := resolver.client.ReadAttachment(projectKey, local.Kind, local.Key())
	var mutationErr error
	switch {
	case err == nil && sameAttachmentContent(local, remote):
		resolver.resolved[id] = remote
		return remote, nil
	case err == nil:
		mutationErr = resolver.client.UpdateAttachment(projectKey, local)
	case syncapi.IsNotFound(err) && local.Kind == syncdomain.AttachmentTool && local.Upsert:
		mutationErr = resolver.client.CreateAttachment(projectKey, local)
	case syncapi.IsNotFound(err) && local.Kind == syncdomain.AttachmentTool:
		err = fmt.Errorf(
			"tool %q does not exist in LaunchDarkly; add \"upsert\": true to its local JSON file to create it during sync",
			local.Key(),
		)
		resolver.failures[id] = err
		return syncdomain.Attachment{}, err
	default:
		resolver.failures[id] = err
		return syncdomain.Attachment{}, err
	}

	if mutationErr != nil && !syncapi.MutationMayHaveSucceeded(mutationErr) {
		resolver.failures[id] = mutationErr
		return syncdomain.Attachment{}, mutationErr
	}

	// Version allocation belongs to LaunchDarkly, so trust only a subsequent
	// read. It also verifies writes whose response was lost or malformed.
	observed, err := resolver.client.ReadAttachment(projectKey, local.Kind, local.Key())
	if err != nil {
		err = errors.Join(mutationErr, err)
		resolver.failures[id] = err
		return syncdomain.Attachment{}, err
	}
	if !sameAttachmentContent(local, observed) {
		err = mutationErr
		if err == nil {
			err = fmt.Errorf("%s %q changed concurrently", local.Kind, local.Key())
		}
		resolver.failures[id] = err
		return syncdomain.Attachment{}, err
	}
	resolver.resolved[id] = observed
	return observed, nil
}

func sameAttachmentContent(left, right syncdomain.Attachment) bool {
	leftJSON, _ := json.Marshal(syncdomain.CanonicalAttachment(left))
	rightJSON, _ := json.Marshal(syncdomain.CanonicalAttachment(right))
	return string(leftJSON) == string(rightJSON)
}

// variationForServerUpdate distinguishes omitted attachment fields from an
// explicit request to detach every existing item.
func variationForServerUpdate(local syncdomain.Variation, server *syncdomain.Variation) syncdomain.Variation {
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

func variationPinnedToLatest(variation syncdomain.Variation) syncdomain.Variation {
	// A value copy still shares slice backing arrays with the reviewed plan;
	// clone pins before assigning latest versions.
	variation.Tools = slices.Clone(variation.Tools)
	variation.Skills = slices.Clone(variation.Skills)
	for index := range variation.Tools {
		if attachment, ok := variation.Attachment(syncdomain.AttachmentTool, variation.Tools[index].Key); ok {
			variation.Tools[index].Version = attachment.Version
		}
	}
	for index := range variation.Skills {
		if attachment, ok := variation.Attachment(syncdomain.AttachmentSkill, variation.Skills[index].Key); ok {
			variation.Skills[index].Version = attachment.Version
		}
	}
	return variation
}

// executePlan applies each independently executable resource and advances the
// manifest only for resources that succeed.
func executePlan(
	repositoryRoot string,
	localStore synclocal.Store,
	client syncapi.Client,
	manifest syncmanifest.Manifest,
	plan Plan,
	localFiles localFileResourcesByID,
) ([]ResourceOutcome, syncmanifest.Manifest, error) {
	// Conflict resolution is a plan-wide decision. Refuse every mutation until
	// all conflicts have a direction so a shared attachment cannot advance
	// while one of its consumers remains unresolved.
	if err := plan.BlockingError(); err != nil {
		return nil, manifest, err
	}

	// Keep the reviewed baseline immutable while successful resources advance
	// the result manifest independently.
	manifest.Resources = append([]syncmanifest.Resource(nil), manifest.Resources...)

	outcomes := make([]ResourceOutcome, 0, len(plan.Resources))
	var failures []error
	attachments := newAttachmentResolver(client)

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
			if err := applyResourceChange(
				repositoryRoot,
				localStore,
				client,
				attachments,
				resource,
				localFiles[resource.ID],
			); err != nil {
				outcome.Status, outcome.Error = OutcomeFailed, err.Error()
				failures = append(failures, fmt.Errorf("%s/%s: %w", resource.ID.ProjectKey, resource.ID.LookupKey, err))
				break
			}
			recordSuccessfulChange(&manifest, resource)
		default:
			outcome.Status, outcome.Error = OutcomeSkipped, "resource is not executable"
		}

		if outcome.Status == OutcomeSucceeded {
			if variation := attachmentManifestState(resource); variation != nil {
				if err := manifest.SetAttachments(resource.ID.ProjectKey, variation.Attachments); err != nil {
					outcome.Status, outcome.Error = OutcomeFailed, err.Error()
					failures = append(failures, err)
				}
			}
		}
		outcomes = append(outcomes, outcome)
	}

	if len(failures) == 0 {
		if err := pruneAttachmentManifest(repositoryRoot, &manifest); err != nil {
			failures = append(failures, err)
		}
	}
	return outcomes, manifest, errors.Join(failures...)
}

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

func pruneAttachmentManifest(repositoryRoot string, manifest *syncmanifest.Manifest) error {
	resources, err := synclocal.CompileWorkspace(repositoryRoot)
	if errors.Is(err, synclocal.ErrNoDirectory) {
		resources = nil
		err = nil
	}
	if err != nil {
		return err
	}
	referenced := make(map[syncdomain.ResourceID]struct{})
	for _, resource := range resources {
		for _, attachment := range resource.Attachments {
			referenced[syncdomain.ResourceID{
				Kind:       syncdomain.Kind(attachment.Kind),
				ProjectKey: resource.ProjectKey,
				LookupKey:  attachment.Key(),
			}] = struct{}{}
		}
	}
	manifest.RemoveUnreferencedAttachments(referenced)
	return nil
}

// applyResourceChange applies one local or server mutation from the plan
// revalidated after review.
func applyResourceChange(
	repositoryRoot string,
	localStore synclocal.Store,
	client syncapi.Client,
	attachments *attachmentResolver,
	resource PlannedResource,
	localFile syncdomain.SyncedResource,
) error {
	if changesServer(resource.Action) {
		return applyServerChange(client, attachments, resource)
	}
	// Local files can omit inherited model fields. Keep the complete server
	// form for any attachment pin refresh that follows the local write.
	serverVariation := resource.Server
	if resource.Action == ActionUpdateLocal {
		variation, err := variationForLocalFile(*resource.Server, resource.Local, localFile)
		if err != nil {
			return err
		}
		resource.Server = &variation
	}
	if err := applyLocalChange(localStore, resource); err != nil {
		return err
	}
	if err := verifyLocalResult(repositoryRoot, resource); err != nil {
		return err
	}
	if resource.ServerHasStaleAttachmentPins && serverVariation != nil {
		configKey, _, err := splitVariationLookupKey(resource.ID.LookupKey)
		if err != nil {
			return err
		}
		return client.UpdateVariation(resource.ID.ProjectKey, configKey, variationPinnedToLatest(*serverVariation))
	}
	return nil
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

// applyServerChange performs one variation mutation through the existing
// public config APIs.
func applyServerChange(client syncapi.Client, attachments *attachmentResolver, resource PlannedResource) error {
	configKey, variationKey, err := splitVariationLookupKey(resource.ID.LookupKey)
	if err != nil {
		return err
	}

	var mutationErr error
	switch resource.Action {
	case ActionCreateServer:
		variation, err := attachments.resolveVariation(resource.ID.ProjectKey, *resource.Local)
		if err != nil {
			return err
		}
		mutationErr = client.CreateVariation(resource.ID.ProjectKey, configKey, variation)
	case ActionUpdateServer:
		variation, err := attachments.resolveVariation(
			resource.ID.ProjectKey,
			variationForServerUpdate(*resource.Local, resource.Server),
		)
		if err != nil {
			return err
		}
		mutationErr = client.UpdateVariation(resource.ID.ProjectKey, configKey, variation)
	case ActionArchiveServer:
		mutationErr = client.ArchiveVariation(resource.ID.ProjectKey, configKey, variationKey)
	}
	if mutationErr == nil {
		return nil
	}
	if !syncapi.MutationMayHaveSucceeded(mutationErr) {
		return mutationErr
	}

	// A network error can hide a successful write, so re-read only when the
	// mutation result is uncertain.
	state, readErr := client.ReadVariation(resource.ID.ProjectKey, configKey, variationKey)
	if readErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("verify server variation: %w", readErr))
	}

	actualFingerprint := ""
	if state.Exists {
		if err := hydrateServerAttachments(client, resource.ID.ProjectKey, &state.Variation); err != nil {
			return errors.Join(mutationErr, err)
		}
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

	expectedFingerprint := ""
	if resource.Action == ActionUpdateLocal {
		expectedFingerprint, err = syncdomain.FingerprintVariation(
			resource.ID.ProjectKey,
			resource.ID.LookupKey,
			*resource.Server,
		)
		if err != nil {
			return err
		}
	}
	if actualFingerprint != expectedFingerprint {
		return fmt.Errorf("local variation did not match the expected state after sync")
	}
	return nil
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
		variation.Attachments = resource.Attachments
		return syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, variation)
	}
	return "", nil
}

// readServerResource reads one supported resource and hydrates its shared
// dependencies through the plan-local cache.
func readServerResource(client syncapi.Client, attachments *attachmentHydrator, id ResourceID) (ServerResource, error) {
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
		if err := attachments.hydrate(id.ProjectKey, &state.Variation); err != nil {
			return ServerResource{}, err
		}
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
