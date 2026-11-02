package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// SyncManifestFormatVersion is the remote sync manifest schema.
const SyncManifestFormatVersion = 1

// SyncManifest is the sync baseline that LaunchDarkly stores for one project
// and one repository source.
type SyncManifest struct {
	FormatVersion int                    `json:"formatVersion"`
	Source        string                 `json:"source"`
	Items         []SyncManifestResource `json:"items"`
}

// SyncManifestResource is one versioned baseline entry.
type SyncManifestResource struct {
	ResourceKind           syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey      string          `json:"resourceLookupKey"`
	Fingerprint            string          `json:"fingerprint"`
	Version                int             `json:"version"`
	FingerprintSchema      string          `json:"fingerprintSchema,omitempty"`
	ReferenceDigest        string          `json:"referenceDigest,omitempty"`
	AdapterBehaviorVersion int             `json:"adapterBehaviorVersion,omitempty"`
	AdapterBehaviorDigest  string          `json:"adapterBehaviorDigest,omitempty"`
}

// SyncManifestUpsert creates an entry, or updates the entry at Version.
type SyncManifestUpsert struct {
	ResourceKind           syncdomain.Kind `json:"resourceKind"`
	ResourceLookupKey      string          `json:"resourceLookupKey"`
	Fingerprint            string          `json:"fingerprint"`
	Version                int             `json:"version"`
	FingerprintSchema      string          `json:"fingerprintSchema,omitempty"`
	ReferenceDigest        string          `json:"referenceDigest,omitempty"`
	AdapterBehaviorVersion int             `json:"adapterBehaviorVersion,omitempty"`
	AdapterBehaviorDigest  string          `json:"adapterBehaviorDigest,omitempty"`
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
	manifest, err := decodeJSON[SyncManifest](response, fmt.Sprintf("sync manifest for project %q", projectKey))
	if err != nil {
		return SyncManifest{}, err
	}
	if err := validateSyncManifest(manifest); err != nil {
		return SyncManifest{}, fmt.Errorf("sync manifest for project %q: %w", projectKey, err)
	}
	return manifest, nil
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
			FormatVersion int                    `json:"formatVersion"`
			Source        string                 `json:"source"`
			Upserts       []SyncManifestUpsert   `json:"upserts"`
			Deletions     []SyncManifestDeletion `json:"deletions"`
		}{
			FormatVersion: SyncManifestFormatVersion,
			Source:        source,
			Upserts:       upserts,
			Deletions:     deletions,
		},
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
	if err := validateSyncManifest(manifest); err != nil {
		return SyncManifest{}, newMutationError("decode updated", request.resource, projectKey, err)
	}
	return manifest, nil
}

func validateSyncManifest(manifest SyncManifest) error {
	if manifest.FormatVersion != SyncManifestFormatVersion {
		return fmt.Errorf("unsupported formatVersion %d", manifest.FormatVersion)
	}
	return nil
}

func manifestPath(projectKey string) []string {
	return projectPath(projectKey, "configs/sync/manifests")
}
