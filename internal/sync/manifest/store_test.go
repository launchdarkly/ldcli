package manifest

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type patchCall struct {
	projectKey string
	upserts    []syncapi.SyncManifestUpsert
	deletions  []syncapi.SyncManifestDeletion
}

type manifestClient struct {
	manifests      map[string]syncapi.SyncManifest
	patchResponses []syncapi.SyncManifest
	patchErr       error
	patches        []patchCall
}

func (client *manifestClient) GetSyncManifest(projectKey, _ string) (syncapi.SyncManifest, error) {
	return client.manifests[projectKey], nil
}

func (client *manifestClient) PatchSyncManifest(
	projectKey string,
	_ string,
	upserts []syncapi.SyncManifestUpsert,
	deletions []syncapi.SyncManifestDeletion,
) (syncapi.SyncManifest, error) {
	client.patches = append(client.patches, patchCall{
		projectKey: projectKey,
		upserts:    append([]syncapi.SyncManifestUpsert(nil), upserts...),
		deletions:  append([]syncapi.SyncManifestDeletion(nil), deletions...),
	})
	if client.patchErr != nil {
		return syncapi.SyncManifest{}, client.patchErr
	}
	response := client.patchResponses[0]
	client.patchResponses = client.patchResponses[1:]
	return response, nil
}

func TestStoreLoadsProjectManifests(t *testing.T) {
	client := &manifestClient{manifests: map[string]syncapi.SyncManifest{
		"zeta": {
			Source: "git:example/repo",
			Items: []syncapi.SyncManifestResource{{
				ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "config/b",
				Fingerprint: fingerprint("b"), Version: 4,
			}},
		},
		"alpha": {
			Source: "git:example/repo",
			Items: []syncapi.SyncManifestResource{{
				ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "config/a",
				Fingerprint: fingerprint("a"), Version: 2,
			}},
		},
	}}

	loaded, err := NewStore(client, "git:example/repo").Load([]string{"zeta", "alpha", "alpha"})

	require.NoError(t, err)
	require.Equal(t, []Resource{
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "alpha", LookupKey: "config/a", Fingerprint: fingerprint("a"), Version: 2},
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "zeta", LookupKey: "config/b", Fingerprint: fingerprint("b"), Version: 4},
	}, loaded.Resources)
}

func TestStoreUpdatesWithRemoteVersions(t *testing.T) {
	client := &manifestClient{patchResponses: []syncapi.SyncManifest{{
		Source: "git:example/repo",
		Items: []syncapi.SyncManifestResource{
			{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "config/existing", Fingerprint: fingerprint("c"), Version: 4},
			{ResourceKind: syncdomain.KindTool, ResourceLookupKey: "search", Fingerprint: fingerprint("d"), Version: 1},
		},
	}}}
	store := NewStore(client, "git:example/repo")
	previous := Manifest{Resources: []Resource{
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/existing", Fingerprint: fingerprint("a"), Version: 3},
		{ResourceKind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "old", Fingerprint: fingerprint("b"), Version: 2},
	}}
	next := Manifest{Resources: []Resource{
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/existing", Fingerprint: fingerprint("c"), Version: 3},
		{ResourceKind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search", Fingerprint: fingerprint("d")},
	}}

	updated, err := store.Update(previous, next)

	require.NoError(t, err)
	require.Len(t, client.patches, 1)
	assert.Equal(t, []syncapi.SyncManifestUpsert{
		{ResourceKind: syncdomain.KindTool, ResourceLookupKey: "search", Fingerprint: fingerprint("d"), Version: 0},
		{ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "config/existing", Fingerprint: fingerprint("c"), Version: 3},
	}, client.patches[0].upserts)
	assert.Equal(t, []syncapi.SyncManifestDeletion{{
		ResourceKind: syncdomain.KindSkill, ResourceLookupKey: "old", Version: 2,
	}}, client.patches[0].deletions)
	assert.Equal(t, 4, updated.Resources[1].Version)
}

func TestStoreReportsOptimisticConflict(t *testing.T) {
	client := &manifestClient{patchErr: errors.New(`{"code":"conflict","statusCode":409}`)}
	store := NewStore(client, "git:example/repo")
	previous := Manifest{Resources: []Resource{{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/variation",
		Fingerprint: fingerprint("a"), Version: 1,
	}}}
	next := previous
	next.Resources = append([]Resource(nil), previous.Resources...)
	next.Resources[0].Fingerprint = fingerprint("b")

	_, err := store.Update(previous, next)

	require.ErrorContains(t, err, `sync manifest for project "project" changed in LaunchDarkly`)
}

func TestStoreReadsBackAnUncertainPatchThatSucceeded(t *testing.T) {
	applied := syncapi.SyncManifest{
		Source: "git:example/repo",
		Items: []syncapi.SyncManifestResource{{
			ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "config/variation",
			Fingerprint: fingerprint("b"), Version: 2,
		}},
	}
	client := &manifestClient{
		manifests: map[string]syncapi.SyncManifest{"project": applied},
		patchErr:  uncertainPatchError(t),
	}
	previous := Manifest{Resources: []Resource{{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/variation",
		Fingerprint: fingerprint("a"), Version: 1,
	}}}
	next := previous.Clone()
	next.Resources[0].Fingerprint = fingerprint("b")

	updated, err := NewStore(client, "git:example/repo").Update(previous, next)

	require.NoError(t, err)
	require.Equal(t, []Resource{{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/variation",
		Fingerprint: fingerprint("b"), Version: 2,
	}}, updated.Resources)
}

// uncertainPatchError returns the error that the API client reports when a
// patch fails before LaunchDarkly sends a response.
func uncertainPatchError(t *testing.T) error {
	t.Helper()
	transport := &failingTransport{err: errors.New("connection reset")}
	_, err := syncapi.NewClient(transport, "token", "https://example.com").PatchSyncManifest("project", "source", nil, nil)
	require.True(t, syncapi.MutationMayHaveSucceeded(err))
	return err
}

type failingTransport struct {
	err error
}

func (transport *failingTransport) MakeRequest(string, string, string, string, url.Values, []byte, bool) ([]byte, error) {
	return nil, transport.err
}

func (transport *failingTransport) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, transport.err
}

func TestManifestValidation(t *testing.T) {
	valid := Resource{
		ResourceKind: syncdomain.KindVariation,
		ProjectKey:   "project",
		LookupKey:    "config/variation",
		Fingerprint:  fingerprint("a"),
	}

	tests := map[string]struct {
		mutate func(*Manifest)
		error  string
	}{
		"kind": {
			mutate: func(manifest *Manifest) { manifest.Resources[0].ResourceKind = "" },
			error:  "invalid resource kind",
		},
		"project traversal": {
			mutate: func(manifest *Manifest) { manifest.Resources[0].ProjectKey = ".." },
			error:  "invalid project key",
		},
		"lookup traversal": {
			mutate: func(manifest *Manifest) { manifest.Resources[0].LookupKey = "../variation" },
			error:  "invalid lookup key",
		},
		"empty lookup segment": {
			mutate: func(manifest *Manifest) { manifest.Resources[0].LookupKey = "config//variation" },
			error:  "invalid lookup key",
		},
		"fingerprint": {
			mutate: func(manifest *Manifest) { manifest.Resources[0].Fingerprint = "not-a-hash" },
			error:  "invalid fingerprint",
		},
		"duplicate": {
			mutate: func(manifest *Manifest) { manifest.Resources = append(manifest.Resources, manifest.Resources[0]) },
			error:  "duplicate manifest resource",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			manifest := Manifest{Resources: []Resource{valid}}
			test.mutate(&manifest)
			require.ErrorContains(t, manifest.Validate(), test.error)
		})
	}
}

func TestManifestSupportsDifferentResourceIdentities(t *testing.T) {
	manifest := Manifest{
		Resources: []Resource{
			{ResourceKind: "tool", ProjectKey: "project", LookupKey: "weather", Fingerprint: fingerprint("a")},
			{ResourceKind: "skill", ProjectKey: "project", LookupKey: "support/summarize/v2", Fingerprint: fingerprint("b")},
		},
	}

	require.NoError(t, manifest.Validate())
}

func fingerprint(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}
