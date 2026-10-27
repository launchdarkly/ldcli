package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// SyncManifest is the sync baseline that LaunchDarkly stores for one project
// and one repository source.
type SyncManifest struct {
	Source string                 `json:"source"`
	Items  []SyncManifestResource `json:"items"`
}

// SyncManifestResource is one versioned baseline entry.
type SyncManifestResource struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Fingerprint       string          `json:"fingerprint"`
	Version           int             `json:"version"`
}

// SyncManifestUpsert creates an entry, or updates the entry at Version.
type SyncManifestUpsert struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Fingerprint       string          `json:"fingerprint"`
	Version           int             `json:"version"`
}

// SyncManifestDeletion removes the entry at Version.
type SyncManifestDeletion struct {
	ResourceKind      syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey string          `json:"resourceLookupKey"`
	Version           int             `json:"version"`
}

// GetSyncManifest returns the baseline for one project and source.
func (client Client) GetSyncManifest(projectKey, source string) (SyncManifest, error) {
	response, err := client.read(
		fmt.Sprintf("get sync manifest for project %q", projectKey), projectKey,
		url.Values{"source": {source}}, manifestPath(projectKey)...,
	)
	if err != nil {
		return SyncManifest{}, err
	}
	return decodeJSON[SyncManifest](response, fmt.Sprintf("sync manifest for project %q", projectKey))
}

// PatchSyncManifest applies versioned changes and returns the new baseline.
func (client Client) PatchSyncManifest(
	projectKey string,
	source string,
	upserts []SyncManifestUpsert,
	deletions []SyncManifestDeletion,
) (SyncManifest, error) {
	// The API requires both lists, so send an empty list instead of null.
	if upserts == nil {
		upserts = []SyncManifestUpsert{}
	}
	if deletions == nil {
		deletions = []SyncManifestDeletion{}
	}
	request := mutation{
		method: http.MethodPatch, action: "update", resource: "sync manifest for project", key: projectKey,
		body: struct {
			Source    string                 `json:"source"`
			Upserts   []SyncManifestUpsert   `json:"upserts"`
			Deletions []SyncManifestDeletion `json:"deletions"`
		}{Source: source, Upserts: upserts, Deletions: deletions},
	}
	response, err := client.mutate(request, manifestPath(projectKey)...)
	if err != nil {
		return SyncManifest{}, err
	}

	var manifest SyncManifest
	if err := json.Unmarshal(response, &manifest); err != nil {
		// The API accepted the patch, but the response is not usable. The
		// caller must read the manifest again to find its state.
		return SyncManifest{}, newMutationError("decode updated", request.resource, projectKey, err)
	}
	return manifest, nil
}

func manifestPath(projectKey string) []string {
	return projectPath(projectKey, "configs/sync/manifests")
}
