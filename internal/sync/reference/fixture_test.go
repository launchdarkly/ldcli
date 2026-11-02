package reference

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	require.NoError(t, err)
	return data
}

func materializeFixture(t *testing.T, fixture, realPath string) (string, []byte) {
	t.Helper()
	data := fixtureFile(t, fixture)
	path := filepath.Join(t.TempDir(), filepath.FromSlash(realPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path, data
}
