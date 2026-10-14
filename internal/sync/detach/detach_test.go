package detach

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

func TestLoadResourcesUnionsLocalAndManifestResources(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: testVariation("local"),
	}})
	require.NoError(t, err)

	manifestStore := newMemoryManifestStore()
	require.NoError(t, manifestStore.Write(syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{
			{
				ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/manifest-only",
				Fingerprint: testFingerprint(),
			},
			{
				ResourceKind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search",
				Fingerprint: testFingerprint(),
			},
		},
	}))

	resources, _, err := loadResources(root, manifestStore, []string{"project"})

	require.NoError(t, err)
	assert.Equal(t, []syncdomain.ResourceID{
		{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/local"},
		{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/manifest-only"},
	}, resources)
}

func TestDetachResourcesPrunesUnreferencedAttachmentManifestEntries(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	description := "Search documentation"
	variation := testVariation("prompt")
	variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	variation.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Description: &description, Schema: map[string]any{"type": "object"}},
	}}
	_, err := store.Add([]synclocal.VariationFile{{ProjectKey: "project", ConfigKey: "config", Variation: variation}})
	require.NoError(t, err)

	manifestStore := newMemoryManifestStore()
	original := syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{
			{
				ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt",
				Fingerprint: testFingerprint(),
			},
			{
				ResourceKind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search",
				Fingerprint: testFingerprint(),
			},
		},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt"}
	err = detachResources(
		Options{RepositoryRoot: root, Store: store, Baselines: manifestStore},
		syncmanifest.Baseline{Lock: original},
		[]syncdomain.ResourceID{resource},
	)

	require.NoError(t, err)
	baseline, err := manifestStore.Load([]string{"project"})
	manifest := baseline.Lock
	require.NoError(t, err)
	assert.Empty(t, manifest.Resources)
}

func TestDetachResourcesRemovesWrapperAndManifestButKeepsReferencedFile(t *testing.T) {
	root := t.TempDir()
	referencePath := filepath.Join(root, "prompts", "prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(referencePath), 0o755))
	require.NoError(t, os.WriteFile(referencePath, []byte("Keep me.\n"), 0o644))

	store := synclocal.NewStore(root)
	variation := testVariation("prompt")
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: variation,
		Ref: &synclocal.Reference{File: "prompts/prompt.md", Format: syncreference.PlainMarkdown},
	}})
	require.NoError(t, err)

	manifestStore := newMemoryManifestStore()
	original := syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{{
			ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt",
			Fingerprint: testFingerprint(),
		}},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt"}
	err = detachResources(Options{Store: store, Baselines: manifestStore}, syncmanifest.Baseline{Lock: original}, []syncdomain.ResourceID{resource})

	require.NoError(t, err)
	exists, err := store.VariationExists("project", "config", "prompt")
	require.NoError(t, err)
	assert.False(t, exists)
	_, err = os.Stat(referencePath)
	require.NoError(t, err)
	baseline, err := manifestStore.Load([]string{"project"})
	manifest := baseline.Lock
	require.NoError(t, err)
	assert.Empty(t, manifest.Resources)
}

func TestDetachResourcesRemovesManifestEntryWhenWrapperWasAlreadyDeleted(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	manifestStore := newMemoryManifestStore()
	original := syncmanifest.Manifest{
		Resources: []syncmanifest.Resource{{
			ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/deleted",
			Fingerprint: testFingerprint(),
		}},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/deleted"}
	err := detachResources(Options{Store: store, Baselines: manifestStore}, syncmanifest.Baseline{Lock: original}, []syncdomain.ResourceID{resource})

	require.NoError(t, err)
	baseline, err := manifestStore.Load([]string{"project"})
	manifest := baseline.Lock
	require.NoError(t, err)
	assert.Empty(t, manifest.Resources)
}

func TestDetachResourcesDeletesUnreadableWrapper(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "broken.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("not front matter"), 0o644))

	store := synclocal.NewStore(root)
	manifestStore := newMemoryManifestStore()
	resource := syncdomain.ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/broken"}

	err := detachResources(
		Options{Store: store, Baselines: manifestStore},
		syncmanifest.Baseline{Lock: syncmanifest.New()},
		[]syncdomain.ResourceID{resource},
	)

	require.NoError(t, err)
	baseline, loadErr := manifestStore.Load([]string{"project"})
	manifest := baseline.Lock
	require.NoError(t, loadErr)
	assert.Empty(t, manifest.Resources)
	_, statErr := os.Stat(wrapper)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunRequiresTerminalWhenResourcesExist(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: testVariation("prompt"),
	}})
	require.NoError(t, err)

	err = Run(Options{
		RepositoryRoot: root,
		Store:          store,
		Baselines:      newMemoryManifestStore(),
		Input:          bytes.NewBuffer(nil),
		Output:         bytes.NewBuffer(nil),
	})

	require.ErrorContains(t, err, "requires a terminal")
}

func TestRunUsesExplicitSelectionsWithoutTerminal(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: testVariation("prompt"),
	}})
	require.NoError(t, err)
	selection := syncdomain.ResourceID{
		Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt",
	}
	manifestStore := newMemoryManifestStore()

	err = Run(Options{
		RepositoryRoot: root,
		Store:          store,
		Baselines:      manifestStore,
		Input:          bytes.NewBuffer(nil),
		Output:         bytes.NewBuffer(nil),
		Selections:     []syncdomain.ResourceID{selection},
		NoInput:        true,
	})

	require.NoError(t, err)
	exists, err := store.VariationExists("project", "config", "prompt")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, []string{"project"}, manifestStore.loadedProjectKeys)
}

func TestRunReportsWhenNoResourcesAreSynced(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	err := Run(Options{
		RepositoryRoot: root,
		Store:          synclocal.NewStore(root),
		Baselines:      newMemoryManifestStore(),
		Input:          bytes.NewBuffer(nil),
		Output:         &output,
	})

	require.NoError(t, err)
	assert.Equal(t, "No resources are currently synced.\n", output.String())
}

func TestRunRejectsExplicitSelectionWhenNoResourcesAreSynced(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	err := Run(Options{
		RepositoryRoot: root,
		Store:          synclocal.NewStore(root),
		Baselines:      newMemoryManifestStore(),
		Input:          bytes.NewBuffer(nil),
		Output:         &output,
		Selections:     []syncdomain.ResourceID{syncdomain.VariationID("production", "support", "default")},
		NoInput:        true,
	})

	require.ErrorContains(t, err, "variation production/support/default is not synced")
	assert.Empty(t, output.String())
}

type memoryManifestStore struct {
	manifest          syncmanifest.Manifest
	loadedProjectKeys []string
}

func newMemoryManifestStore() *memoryManifestStore {
	return &memoryManifestStore{manifest: syncmanifest.New()}
}

func (store *memoryManifestStore) Load(projectKeys []string) (syncmanifest.Baseline, error) {
	store.loadedProjectKeys = append([]string(nil), projectKeys...)
	return syncmanifest.Baseline{Lock: store.manifest}, nil
}

func (store *memoryManifestStore) Save(_ syncmanifest.Baseline, next syncmanifest.Manifest) (syncmanifest.Baseline, error) {
	store.manifest = next
	return syncmanifest.Baseline{Lock: next}, nil
}

func (store *memoryManifestStore) Write(manifest syncmanifest.Manifest) error {
	store.manifest = manifest
	return nil
}

func testVariation(key string) syncdomain.Variation {
	return syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: key, Name: key, Instructions: "Help."}
}

func testFingerprint() string {
	return "sha256:" + strings.Repeat("0", 64)
}
