package bootstrap

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

func TestVariationChoicesSortsAndOmitsExistingVariations(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	_, err := store.Bootstrap([]synclocal.VariationFile{{
		ProjectKey: "project",
		ConfigKey:  "config",
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeCompletion,
			Key:  "existing",
			Name: "Existing",
		},
	}})
	require.NoError(t, err)
	config := syncapi.Config{
		Key:  "config",
		Mode: syncdomain.VariationModeCompletion,
		Variations: []syncdomain.Variation{
			{Key: "z", Name: "Zulu"},
			{Key: "existing", Name: "Alpha"},
			{Key: "a", Name: "Alpha"},
		},
	}

	choices, existingCount, err := variationChoices(store, "project", config)
	require.NoError(t, err)
	assert.Equal(t, 1, existingCount)
	require.Len(t, choices, 2)
	assert.Equal(t, "a", choices[0].Value.Key)
	assert.Equal(t, "z", choices[1].Value.Key)
}

func TestVariationChoicesReturnsLocalInspectionError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(
		filepath.Join(root, syncdomain.RootDir),
		0o755,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, syncdomain.RootDir, "project"),
		[]byte("not a directory"),
		0o644,
	))

	config := syncapi.Config{
		Key: "config",
		Variations: []syncdomain.Variation{{
			Key: "variation",
		}},
	}

	_, _, err := variationChoices(synclocal.NewStore(root), "project", config)
	require.ErrorContains(t, err, "inspect variation variation")
}

func TestRunRequiresTerminal(t *testing.T) {
	err := Run(Options{
		Catalog: &fakeCatalog{},
		Store:   synclocal.NewStore(t.TempDir()),
		Input:   bytes.NewBuffer(nil),
		Output:  bytes.NewBuffer(nil),
		Initial: true,
	})

	require.ErrorContains(
		t,
		err,
		"interactive prompt selection requires a terminal",
	)
}

func TestFinishSelectionDryRunDoesNotCreateFiles(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer

	err := finishSelection(Options{
		Store:   synclocal.NewStore(root),
		Output:  &output,
		Initial: true,
		DryRun:  true,
	}, []synclocal.VariationFile{{
		ProjectKey: "project",
		ConfigKey:  "config",
		Upsert:     true,
		Variation: syncdomain.Variation{
			Key:          "variation",
			Name:         "Variation",
			Mode:         syncdomain.VariationModeAgent,
			Instructions: "Be helpful.",
		},
	}})

	require.NoError(t, err)
	assert.Equal(
		t,
		`============================================================
File 1 of 1
Would create: .launchdarkly/project/configs/config/variation.prompt.md
------------------------------------------------------------
---
formatVersion: 1
upsert: true
mode: agent
key: variation
name: Variation
---

Be helpful.
============================================================
`,
		output.String(),
	)
	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFinishSelectionWritesInitialManifest(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "variation", Name: "Variation", Instructions: "Be helpful.",
	}

	err := finishSelection(Options{
		Store:    synclocal.NewStore(root),
		Manifest: syncmanifest.NewStore(root),
		Output:   &output,
		Initial:  true,
	}, []synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Upsert: true, Variation: variation,
	}})

	require.NoError(t, err)
	manifest, exists, err := syncmanifest.NewStore(root).Load()
	require.NoError(t, err)
	require.True(t, exists)
	require.Len(t, manifest.Resources, 1)
	expected, err := syncdomain.FingerprintVariation("project", "config/variation", variation)
	require.NoError(t, err)
	assert.Equal(t, expected, manifest.Resources[0].Fingerprint)
}

func TestFinishSelectionAddsMultipleVersionedVariationsToExistingManifest(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	manifestStore := syncmanifest.NewStore(root)
	first := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "first", Name: "First", Instructions: "First.",
	}
	second := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "second", Name: "Second", Instructions: "Second.",
		ModelConfigKey: "model", ModelConfigVersion: 2,
	}
	third := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "third", Name: "Third", Instructions: "Third.",
		ModelConfigKey: "model", ModelConfigVersion: 3,
	}
	require.NoError(t, finishSelection(Options{
		Store: store, Manifest: manifestStore, Output: io.Discard, Initial: true,
	}, []synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: first,
	}}))

	require.NoError(t, finishSelection(Options{
		Store: store, Manifest: manifestStore, Output: io.Discard,
	}, []synclocal.VariationFile{
		{ProjectKey: "project", ConfigKey: "config", Variation: second},
		{ProjectKey: "project", ConfigKey: "config", Variation: third},
	}))

	manifest, exists, err := manifestStore.Load()
	require.NoError(t, err)
	require.True(t, exists)
	require.Len(t, manifest.Resources, 3)
	assert.Equal(t, "config/first", manifest.Resources[0].LookupKey)
	assert.Equal(t, "config/second", manifest.Resources[1].LookupKey)
	assert.Equal(t, "config/third", manifest.Resources[2].LookupKey)
}

func TestFinishSelectionRollsBackFilesWhenManifestWriteFails(t *testing.T) {
	root := t.TempDir()
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "variation", Name: "Variation", Instructions: "Be helpful.",
	}

	err := finishSelection(Options{
		Store:    synclocal.NewStore(root),
		Manifest: failingManifestStore{},
		Output:   io.Discard,
		Initial:  true,
	}, []synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: variation,
	}})

	require.ErrorContains(t, err, "write manifest")
	_, statErr := os.Stat(filepath.Join(root, syncdomain.RootDir))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestWriteSummary(t *testing.T) {
	var output bytes.Buffer

	writeSummary(
		&output,
		true,
		2,
	)
	assert.Equal(
		t,
		"Bootstrapped 2 variation files in .launchdarkly.\n",
		output.String(),
	)

	output.Reset()
	writeNoChangeSummary(&output, false)
	assert.Contains(t, output.String(), "No variations added")
}

func TestWritePreviewsPrintsEveryFile(t *testing.T) {
	var output bytes.Buffer

	writePreviews(&output, []synclocal.RenderedVariationFile{
		{Path: "project/configs/config/first.prompt.md", Content: []byte("first\n")},
		{Path: "project/configs/config/second.prompt.md", Content: []byte("second\n")},
	})

	assert.Equal(
		t,
		`============================================================
File 1 of 2
Would create: .launchdarkly/project/configs/config/first.prompt.md
------------------------------------------------------------
first
============================================================

============================================================
File 2 of 2
Would create: .launchdarkly/project/configs/config/second.prompt.md
------------------------------------------------------------
second
============================================================
`,
		output.String(),
	)
}

type fakeCatalog struct{}

var _ Catalog = &fakeCatalog{}

func (*fakeCatalog) Projects() ([]syncapi.Project, error) {
	return nil, nil
}

func (*fakeCatalog) Configs(string) ([]syncapi.Config, error) {
	return nil, nil
}

type failingManifestStore struct{}

func (failingManifestStore) Load() (syncmanifest.Manifest, bool, error) {
	return syncmanifest.New(), false, nil
}

func (failingManifestStore) Write(syncmanifest.Manifest) error {
	return errors.New("write manifest")
}
