package local

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestStoreReadsWritesAndRemovesLock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
	store := NewStore(root)

	_, err := store.ReadLock()
	require.ErrorIs(t, err, fs.ErrNotExist)

	require.NoError(t, store.WriteLock([]byte("formatVersion: 1\n")))
	content, err := store.ReadLock()
	require.NoError(t, err)
	assert.Equal(t, "formatVersion: 1\n", string(content))

	require.NoError(t, store.WriteLock(nil))
	_, err = store.ReadLock()
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.NoError(t, store.WriteLock(nil))
}

func TestStoreRejectsSymlinkedLock(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
	target := filepath.Join(t.TempDir(), "outside.lock")
	require.NoError(t, os.WriteFile(target, []byte("outside"), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(root, syncdomain.RootDir, lockFileName)))

	err := NewStore(root).WriteLock([]byte("formatVersion: 1\n"))

	require.ErrorContains(t, err, "symbolic links are not supported")
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "outside", string(content))
}
