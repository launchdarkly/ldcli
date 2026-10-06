package manifest

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

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

// Store persists synchronization baselines through LaunchDarkly.
type Store struct {
	client client
	source string
}

// NewStore creates a remote manifest store for one repository source.
func NewStore(client client, source string) Store {
	return Store{client: client, source: source}
}

// Load combines the project-scoped remote manifests used by the workspace.
func (store Store) Load(projectKeys []string) (Manifest, error) {
	projectKeys = append([]string(nil), projectKeys...)
	slices.Sort(projectKeys)
	projectKeys = slices.Compact(projectKeys)

	manifest := New()
	for _, projectKey := range projectKeys {
		remote, err := store.client.GetSyncManifest(projectKey, store.source)
		if err != nil {
			return Manifest{}, err
		}
		resources, err := resourcesFromRemote(projectKey, store.source, remote)
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

// Update applies the difference between two aggregate manifest states.
func (store Store) Update(previous, next Manifest) (Manifest, error) {
	if err := previous.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate previous sync manifest: %w", err)
	}
	for _, resource := range previous.Resources {
		if resource.Version == 0 {
			return Manifest{}, fmt.Errorf(
				"sync manifest resource %s/%s is missing its remote version",
				resource.ProjectKey,
				resource.LookupKey,
			)
		}
	}
	if err := next.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate updated sync manifest: %w", err)
	}

	result := New()
	for _, projectKey := range manifestProjects(previous, next) {
		before := projectResources(previous, projectKey)
		after := projectResources(next, projectKey)
		upserts, deletions := manifestChanges(before, after)

		if len(upserts) == 0 && len(deletions) == 0 {
			result.Resources = append(result.Resources, retainVersions(before, after)...)
			continue
		}

		remote, err := store.apply(projectKey, upserts, deletions)
		if err != nil {
			return Manifest{}, err
		}
		resources, err := resourcesFromRemote(projectKey, store.source, remote)
		if err != nil {
			return Manifest{}, err
		}
		result.Resources = append(result.Resources, resources...)
	}

	result.Sort()
	return result, nil
}

func (store Store) apply(
	projectKey string,
	upserts []syncapi.SyncManifestUpsert,
	deletions []syncapi.SyncManifestDeletion,
) (syncapi.SyncManifest, error) {
	var latest syncapi.SyncManifest
	for len(upserts) != 0 || len(deletions) != 0 {
		upsertCount := min(len(upserts), manifestBatchLimit)
		deletionCount := min(len(deletions), manifestBatchLimit)
		batchUpserts := upserts[:upsertCount]
		batchDeletions := deletions[:deletionCount]

		remote, err := store.client.PatchSyncManifest(
			projectKey,
			store.source,
			batchUpserts,
			batchDeletions,
		)
		if err != nil {
			if syncapi.IsConflict(err) {
				return syncapi.SyncManifest{}, fmt.Errorf(
					"sync manifest for project %q changed in LaunchDarkly; run sync again: %w",
					projectKey,
					err,
				)
			}
			if !syncapi.MutationMayHaveSucceeded(err) {
				return syncapi.SyncManifest{}, err
			}

			remote, readErr := store.client.GetSyncManifest(projectKey, store.source)
			if readErr != nil || !changesApplied(remote, batchUpserts, batchDeletions) {
				return syncapi.SyncManifest{}, errors.Join(err, readErr)
			}
		}

		latest = remote
		upserts = upserts[upsertCount:]
		deletions = deletions[deletionCount:]
	}
	return latest, nil
}

func resourcesFromRemote(projectKey, source string, remote syncapi.SyncManifest) ([]Resource, error) {
	if remote.Source != "" && remote.Source != source {
		return nil, fmt.Errorf(
			"sync manifest for project %q returned source %q instead of %q",
			projectKey,
			remote.Source,
			source,
		)
	}

	resources := make([]Resource, 0, len(remote.Items))
	for _, item := range remote.Items {
		if item.Version < 1 {
			return nil, fmt.Errorf(
				"sync manifest resource %s/%s returned invalid version %d",
				projectKey,
				item.ResourceLookupKey,
				item.Version,
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

func manifestProjects(manifests ...Manifest) []string {
	var projectKeys []string
	for _, manifest := range manifests {
		for _, resource := range manifest.Resources {
			projectKeys = append(projectKeys, resource.ProjectKey)
		}
	}
	slices.Sort(projectKeys)
	return slices.Compact(projectKeys)
}

func projectResources(manifest Manifest, projectKey string) []Resource {
	var resources []Resource
	for _, resource := range manifest.Resources {
		if resource.ProjectKey == projectKey {
			resources = append(resources, resource)
		}
	}
	return resources
}

func manifestChanges(
	previous []Resource,
	next []Resource,
) ([]syncapi.SyncManifestUpsert, []syncapi.SyncManifestDeletion) {
	before := make(map[identity]Resource, len(previous))
	for _, resource := range previous {
		before[resourceIdentity(resource)] = resource
	}

	var upserts []syncapi.SyncManifestUpsert
	remaining := make(map[identity]Resource, len(previous))
	for id, resource := range before {
		remaining[id] = resource
	}

	for _, resource := range next {
		id := resourceIdentity(resource)
		current, exists := before[id]
		delete(remaining, id)
		if exists && current.Fingerprint == resource.Fingerprint {
			continue
		}
		version := 0
		if exists {
			version = current.Version
		}
		upserts = append(upserts, syncapi.SyncManifestUpsert{
			ResourceKind:      resource.ResourceKind,
			ResourceLookupKey: resource.LookupKey,
			Fingerprint:       resource.Fingerprint,
			Version:           version,
		})
	}

	var deletions []syncapi.SyncManifestDeletion
	for _, resource := range remaining {
		deletions = append(deletions, syncapi.SyncManifestDeletion{
			ResourceKind:      resource.ResourceKind,
			ResourceLookupKey: resource.LookupKey,
			Version:           resource.Version,
		})
	}
	slices.SortFunc(upserts, func(left, right syncapi.SyncManifestUpsert) int {
		if left.ResourceKind != right.ResourceKind {
			return strings.Compare(string(left.ResourceKind), string(right.ResourceKind))
		}
		return strings.Compare(left.ResourceLookupKey, right.ResourceLookupKey)
	})
	slices.SortFunc(deletions, func(left, right syncapi.SyncManifestDeletion) int {
		if left.ResourceKind != right.ResourceKind {
			return strings.Compare(string(left.ResourceKind), string(right.ResourceKind))
		}
		return strings.Compare(left.ResourceLookupKey, right.ResourceLookupKey)
	})
	return upserts, deletions
}

type identity struct {
	kind      string
	lookupKey string
}

func resourceIdentity(resource Resource) identity {
	return identity{kind: string(resource.ResourceKind), lookupKey: resource.LookupKey}
}

func retainVersions(previous, next []Resource) []Resource {
	versions := make(map[identity]int, len(previous))
	for _, resource := range previous {
		versions[resourceIdentity(resource)] = resource.Version
	}
	result := append([]Resource(nil), next...)
	for index := range result {
		result[index].Version = versions[resourceIdentity(result[index])]
	}
	return result
}

func changesApplied(
	remote syncapi.SyncManifest,
	upserts []syncapi.SyncManifestUpsert,
	deletions []syncapi.SyncManifestDeletion,
) bool {
	current := make(map[identity]syncapi.SyncManifestResource, len(remote.Items))
	for _, item := range remote.Items {
		current[identity{kind: string(item.ResourceKind), lookupKey: item.ResourceLookupKey}] = item
	}
	for _, upsert := range upserts {
		item, ok := current[identity{kind: string(upsert.ResourceKind), lookupKey: upsert.ResourceLookupKey}]
		if !ok || item.Fingerprint != upsert.Fingerprint || item.Version <= upsert.Version {
			return false
		}
	}
	for _, deletion := range deletions {
		if _, ok := current[identity{kind: string(deletion.ResourceKind), lookupKey: deletion.ResourceLookupKey}]; ok {
			return false
		}
	}
	return true
}
