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

	manifestStore := syncmanifest.NewStore(root)
	require.NoError(t, manifestStore.Write(syncmanifest.Manifest{
		FormatVersion: syncmanifest.FormatVersion,
		Resources: []syncmanifest.Resource{{
			ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/manifest-only",
			Fingerprint: testFingerprint(),
		}},
	}))

	resources, _, exists, err := loadResources(root, manifestStore)

	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, []Resource{
		{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/local"},
		{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/manifest-only"},
	}, resources)
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

	manifestStore := syncmanifest.NewStore(root)
	original := syncmanifest.Manifest{
		FormatVersion: syncmanifest.FormatVersion,
		Resources: []syncmanifest.Resource{{
			ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt",
			Fingerprint: testFingerprint(),
		}},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := Resource{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/prompt"}
	err = detachResources(Options{Store: store, Manifest: manifestStore}, original, true, []Resource{resource})

	require.NoError(t, err)
	exists, err := store.VariationExists("project", "config", "prompt")
	require.NoError(t, err)
	assert.False(t, exists)
	_, err = os.Stat(referencePath)
	require.NoError(t, err)
	manifest, exists, err := manifestStore.Load()
	require.NoError(t, err)
	require.True(t, exists)
	assert.Empty(t, manifest.Resources)
}

func TestDetachResourcesRemovesManifestEntryWhenWrapperWasAlreadyDeleted(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	manifestStore := syncmanifest.NewStore(root)
	original := syncmanifest.Manifest{
		FormatVersion: syncmanifest.FormatVersion,
		Resources: []syncmanifest.Resource{{
			ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/deleted",
			Fingerprint: testFingerprint(),
		}},
	}
	require.NoError(t, manifestStore.Write(original))

	resource := Resource{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/deleted"}
	err := detachResources(Options{Store: store, Manifest: manifestStore}, original, true, []Resource{resource})

	require.NoError(t, err)
	manifest, exists, err := manifestStore.Load()
	require.NoError(t, err)
	require.True(t, exists)
	assert.Empty(t, manifest.Resources)
}

func TestDetachResourcesDeletesUnreadableWrapper(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, syncdomain.RootDir, "project", "configs", "config", "broken.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o755))
	require.NoError(t, os.WriteFile(wrapper, []byte("not front matter"), 0o644))

	store := synclocal.NewStore(root)
	manifestStore := syncmanifest.NewStore(root)
	resource := Resource{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/broken"}

	err := detachResources(
		Options{Store: store, Manifest: manifestStore},
		syncmanifest.New(),
		false,
		[]Resource{resource},
	)

	require.NoError(t, err)
	manifest, exists, loadErr := manifestStore.Load()
	require.NoError(t, loadErr)
	require.True(t, exists)
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
		Manifest:       syncmanifest.NewStore(root),
		Input:          bytes.NewBuffer(nil),
		Output:         bytes.NewBuffer(nil),
	})

	require.ErrorContains(t, err, "requires a terminal")
}

func TestRunReportsWhenNoResourcesAreSynced(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	err := Run(Options{
		RepositoryRoot: root,
		Store:          synclocal.NewStore(root),
		Manifest:       syncmanifest.NewStore(root),
		Input:          bytes.NewBuffer(nil),
		Output:         &output,
	})

	require.NoError(t, err)
	assert.Equal(t, "No resources are currently synced.\n", output.String())
}

func testVariation(key string) syncdomain.Variation {
	return syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: key, Name: key, Instructions: "Help."}
}

func testFingerprint() string {
	return "sha256:" + strings.Repeat("0", 64)
}
