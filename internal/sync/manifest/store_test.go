package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestStoreRoundTripIsDeterministic(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	input := Manifest{
		Resources: []Resource{
			{ResourceKind: syncdomain.KindVariation, ProjectKey: "zeta", LookupKey: "config/b", Fingerprint: fingerprint("b")},
			{ResourceKind: syncdomain.KindVariation, ProjectKey: "alpha", LookupKey: "config/a", Fingerprint: fingerprint("a")},
		},
	}

	require.NoError(t, store.Write(input))
	data, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, FileName))
	require.NoError(t, err)
	require.Equal(t, `formatVersion: 1
resources:
  - resourceKind: variation
    projectKey: alpha
    lookupKey: config/a
    fingerprint: `+fingerprint("a")+`
  - resourceKind: variation
    projectKey: zeta
    lookupKey: config/b
    fingerprint: `+fingerprint("b")+`
`, string(data))

	loaded, exists, err := store.Load()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, []Resource{
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "alpha", LookupKey: "config/a", Fingerprint: fingerprint("a")},
		{ResourceKind: syncdomain.KindVariation, ProjectKey: "zeta", LookupKey: "config/b", Fingerprint: fingerprint("b")},
	}, loaded.Resources)
}

func TestStoreLoadsMissingManifestAsEmpty(t *testing.T) {
	loaded, exists, err := NewStore(t.TempDir()).Load()

	require.NoError(t, err)
	require.False(t, exists)
	require.Equal(t, New(), loaded)
}

func TestStoreRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, syncdomain.RootDir, FileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("formatVersion: 1\nunknown: true\nresources: []\n"), 0o644))

	_, _, err := NewStore(root).Load()
	require.ErrorContains(t, err, "field unknown not found")
}

func TestStoreRejectsMultipleYAMLDocuments(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, syncdomain.RootDir, FileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("formatVersion: 1\nresources: []\n---\nformatVersion: 1\nresources: []\n"), 0o644))

	_, _, err := NewStore(root).Load()
	require.ErrorContains(t, err, "multiple YAML documents")
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
		"format": {
			mutate: func(manifest *Manifest) { manifest.FormatVersion = 2 },
			error:  "unsupported manifest formatVersion",
		},
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
			manifest := Manifest{FormatVersion: FormatVersion, Resources: []Resource{valid}}
			test.mutate(&manifest)
			require.ErrorContains(t, manifest.Validate(), test.error)
		})
	}
}

func TestManifestSupportsDifferentResourceIdentities(t *testing.T) {
	manifest := Manifest{
		FormatVersion: FormatVersion,
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
