package local

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestNewSourceReferenceStoresRepositoryRelativePath(t *testing.T) {
	root := t.TempDir()
	workingDirectory := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(filepath.Join(workingDirectory, "prompts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workingDirectory, "prompts", "support.md"), []byte("Help"), 0o644))

	reference, err := NewSourceReference(root, workingDirectory, "prompts/support.md", "markdown", "$", nil)

	require.NoError(t, err)
	require.Equal(t, "app/prompts/support.md", reference.File)
}

func TestNewSourceReferenceRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "prompt.md")
	require.NoError(t, os.WriteFile(outside, []byte("Help"), 0o644))

	_, err := NewSourceReference(root, root, outside, "markdown", "$", nil)
	require.ErrorContains(t, err, "inside the Git repository")

	managed := filepath.Join(root, syncdomain.RootDir, "prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(managed), 0o755))
	require.NoError(t, os.WriteFile(managed, []byte("Help"), 0o644))
	_, err = NewSourceReference(root, root, managed, "markdown", "$", nil)
	require.ErrorContains(t, err, "must be outside")
}

func TestNewSourceReferenceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "prompt.md")
	require.NoError(t, os.WriteFile(outside, []byte("Help"), 0o644))
	link := filepath.Join(root, "prompt.md")
	require.NoError(t, os.Symlink(outside, link))

	_, err := NewSourceReference(root, root, link, "markdown", "$", nil)

	require.ErrorContains(t, err, "inside the Git repository")
}
