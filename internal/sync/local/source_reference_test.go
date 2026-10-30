package local

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestWriteDescriptorCreatesUpdatesAndKeepsIdenticalContent(t *testing.T) {
	root := t.TempDir()
	writeDescriptorTestFile(t, root, "first.md", "First.\n")
	writeDescriptorTestFile(t, root, "second.md", "Second.\n")
	id := syncdomain.VariationID("project", "support", "default")
	store := NewStore(root)

	path, err := store.WriteDescriptor(id, Reference{File: "first.md", Format: "markdown"})
	require.NoError(t, err)
	first, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	_, err = store.WriteDescriptor(id, Reference{File: "first.md", Format: "markdown"})
	require.NoError(t, err)
	same, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	assert.Equal(t, first, same)

	_, err = store.WriteDescriptor(id, Reference{File: "second.md", Format: "markdown"})
	require.NoError(t, err)
	updated, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "file: second.md")
}

func writeDescriptorTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
