package manifest

import (
	"errors"
	"fmt"
	"slices"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

// The API accepts at most this number of upserts and deletions in one patch.
const manifestBatchLimit = 100

type client interface {
	GetSyncManifest(projectKey, source string) (syncapi.SyncManifest, error)
	PatchSyncManifest(
		projectKey string,
		source string,
		upserts []syncapi.SyncManifestUpsert,
		deletions []syncapi.SyncManifestDeletion,
	) (syncapi.SyncManifest, error)
}

// Store keeps the manifest in LaunchDarkly. LaunchDarkly stores one remote
// manifest for each project and repository source.
type Store struct {
	client client
	source string
}

// NewStore creates a store for one repository source.
func NewStore(client client, source string) Store {
	return Store{client: client, source: source}
}

// Load reads the remote manifest of each project and combines them.
func (store Store) Load(projectKeys []string) (Manifest, error) {
	manifest := New()
	for _, projectKey := range uniqueSorted(projectKeys) {
		remote, err := store.client.GetSyncManifest(projectKey, store.source)
		if err != nil {
			return Manifest{}, err
		}
		resources, err := store.resourcesFromRemote(projectKey, remote)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Resources = append(manifest.Resources, resources...)
	}
	manifest.Sort()
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate sync manifest: %w", err)
	}
	return manifest, nil
}

// Update sends the difference between previous and next to LaunchDarkly. The
// previous manifest must come from Load, so that each entry has its remote
// version. Update returns the new remote state.
func (store Store) Update(previous, next Manifest) (Manifest, error) {
	if err := previous.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate previous sync manifest: %w", err)
	}
	for _, resource := range previous.Resources {
		if resource.Version == 0 {
			return Manifest{}, fmt.Errorf("sync manifest resource %s is missing its remote version", resource.ID())
		}
	}
	if err := next.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate updated sync manifest: %w", err)
	}

	result := New()
	for _, projectKey := range projectKeys(previous, next) {
		before := previous.project(projectKey)
		after := next.project(projectKey)
		upserts, deletions := changes(before, after)
		if len(upserts) == 0 && len(deletions) == 0 {
			result.Resources = append(result.Resources, withVersions(after, before)...)
			continue
		}

		remote, err := store.patch(projectKey, upserts, deletions)
		if err != nil {
			return Manifest{}, err
		}
		resources, err := store.resourcesFromRemote(projectKey, remote)
		if err != nil {
			return Manifest{}, err
		}
		result.Resources = append(result.Resources, resources...)
	}
	result.Sort()
	return result, nil
}

// patch sends the changes in batches and returns the remote manifest after
// the last batch. If a batch fails without a response, patch reads the
// manifest to find whether LaunchDarkly applied it.
func (store Store) patch(
	projectKey string,
	upserts []syncapi.SyncManifestUpsert,
	deletions []syncapi.SyncManifestDeletion,
) (syncapi.SyncManifest, error) {
	var latest syncapi.SyncManifest
	for len(upserts) != 0 || len(deletions) != 0 {
		batchUpserts := upserts[:min(len(upserts), manifestBatchLimit)]
		batchDeletions := deletions[:min(len(deletions), manifestBatchLimit)]
		upserts = upserts[len(batchUpserts):]
		deletions = deletions[len(batchDeletions):]

		remote, err := store.client.PatchSyncManifest(projectKey, store.source, batchUpserts, batchDeletions)
		switch {
		case err == nil:
		case syncapi.IsConflict(err):
			return syncapi.SyncManifest{}, fmt.Errorf(
				"sync manifest for project %q changed in LaunchDarkly; run sync again: %w", projectKey, err,
			)
		case !syncapi.MutationMayHaveSucceeded(err):
			return syncapi.SyncManifest{}, err
		default:
			var readErr error
			remote, readErr = store.client.GetSyncManifest(projectKey, store.source)
			if readErr != nil || !changesApplied(remote, batchUpserts, batchDeletions) {
				return syncapi.SyncManifest{}, errors.Join(err, readErr)
			}
		}
		latest = remote
	}
	return latest, nil
}

func (store Store) resourcesFromRemote(projectKey string, remote syncapi.SyncManifest) ([]Resource, error) {
	if remote.Source != "" && remote.Source != store.source {
		return nil, fmt.Errorf(
			"sync manifest for project %q returned source %q instead of %q", projectKey, remote.Source, store.source,
		)
	}

	resources := make([]Resource, 0, len(remote.Items))
	for _, item := range remote.Items {
		if item.Version < 1 {
			return nil, fmt.Errorf(
				"sync manifest resource %s/%s returned invalid version %d", projectKey, item.ResourceLookupKey, item.Version,
			)
		}
		resources = append(resources, Resource{
			ResourceKind: item.ResourceKind,
			ProjectKey:   projectKey,
			LookupKey:    item.ResourceLookupKey,
			Fingerprint:  item.Fingerprint,
			Version:      item.Version,
		})
	}
	return resources, nil
}

// changes compares two states of one project. The results are in identity order.
func changes(before, after []Resource) ([]syncapi.SyncManifestUpsert, []syncapi.SyncManifestDeletion) {
	before, after = sorted(before), sorted(after)
	remaining := make(map[syncdomain.ResourceID]Resource, len(before))
	for _, resource := range before {
		remaining[resource.ID()] = resource
	}

	var upserts []syncapi.SyncManifestUpsert
	for _, resource := range after {
		current, exists := remaining[resource.ID()]
		delete(remaining, resource.ID())
		if exists && current.Fingerprint == resource.Fingerprint {
			continue
		}
		upserts = append(upserts, syncapi.SyncManifestUpsert{
			ResourceKind:      resource.ResourceKind,
			ResourceLookupKey: resource.LookupKey,
			Fingerprint:       resource.Fingerprint,
			Version:           current.Version,
		})
	}

	var deletions []syncapi.SyncManifestDeletion
	for _, resource := range before {
		if _, removed := remaining[resource.ID()]; removed {
			deletions = append(deletions, syncapi.SyncManifestDeletion{
				ResourceKind:      resource.ResourceKind,
				ResourceLookupKey: resource.LookupKey,
				Version:           resource.Version,
			})
		}
	}
	return upserts, deletions
}

// changesApplied reports whether the remote manifest contains every change.
func changesApplied(
	remote syncapi.SyncManifest,
	upserts []syncapi.SyncManifestUpsert,
	deletions []syncapi.SyncManifestDeletion,
) bool {
	type key struct {
		kind      syncdomain.Kind
		lookupKey string
	}
	current := make(map[key]syncapi.SyncManifestResource, len(remote.Items))
	for _, item := range remote.Items {
		current[key{item.ResourceKind, item.ResourceLookupKey}] = item
	}
	for _, upsert := range upserts {
		item, ok := current[key{upsert.ResourceKind, upsert.ResourceLookupKey}]
		if !ok || item.Fingerprint != upsert.Fingerprint || item.Version <= upsert.Version {
			return false
		}
	}
	for _, deletion := range deletions {
		if _, ok := current[key{deletion.ResourceKind, deletion.ResourceLookupKey}]; ok {
			return false
		}
	}
	return true
}

// withVersions copies the remote version of each entry in before to the
// matching entry in after.
func withVersions(after, before []Resource) []Resource {
	versions := make(map[syncdomain.ResourceID]int, len(before))
	for _, resource := range before {
		versions[resource.ID()] = resource.Version
	}
	result := slices.Clone(after)
	for index := range result {
		result[index].Version = versions[result[index].ID()]
	}
	return result
}

// project returns the entries of one project.
func (manifest Manifest) project(projectKey string) []Resource {
	var resources []Resource
	for _, resource := range manifest.Resources {
		if resource.ProjectKey == projectKey {
			resources = append(resources, resource)
		}
	}
	return resources
}

func sorted(resources []Resource) []Resource {
	manifest := Manifest{Resources: slices.Clone(resources)}
	manifest.Sort()
	return manifest.Resources
}

func projectKeys(manifests ...Manifest) []string {
	var keys []string
	for _, manifest := range manifests {
		for _, resource := range manifest.Resources {
			keys = append(keys, resource.ProjectKey)
		}
	}
	return uniqueSorted(keys)
}

func uniqueSorted(values []string) []string {
	values = slices.Clone(values)
	slices.Sort(values)
	return slices.Compact(values)
}
