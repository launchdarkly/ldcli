package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/repository"
)

func TestResolverUsesGitSourceWhenAvailable(t *testing.T) {
	root := t.TempDir()
	gitSource, err := syncdomain.NewSource(
		syncdomain.SourceTypeGit,
		"github.com/launchdarkly/ldcli",
	)
	require.NoError(t, err)

	resolver := NewResolver()
	resolver.findGitSource = func(string) (repository.GitRepository, bool, error) {
		return repository.GitRepository{Root: root, Source: gitSource}, true, nil
	}

	workspace, err := resolver.Resolve(filepath.Join(root, "nested"))

	require.NoError(t, err)
	assert.Equal(t, requireCanonicalPath(t, root), workspace.Root)
	assert.Equal(t, gitSource, workspace.Source)
}

func TestResolverRequiresGit(t *testing.T) {
	resolver := NewResolver()
	resolver.findGitSource = func(string) (repository.GitRepository, bool, error) {
		return repository.GitRepository{}, false, nil
	}

	_, err := resolver.Resolve(t.TempDir())

	require.ErrorIs(t, err, ErrGitRequired)
	require.ErrorContains(t, err, "git is required to use sync")
}

func TestResolverCanonicalizesSymlinkedWorkspace(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.Symlink(root, link))
	gitSource, err := syncdomain.NewSource(
		syncdomain.SourceTypeGit,
		"github.com/launchdarkly/ldcli",
	)
	require.NoError(t, err)
	resolver := NewResolver()
	resolver.findGitSource = func(string) (repository.GitRepository, bool, error) {
		return repository.GitRepository{Root: link, Source: gitSource}, true, nil
	}

	workspace, err := resolver.Resolve(link)

	require.NoError(t, err)
	assert.Equal(t, requireCanonicalPath(t, root), workspace.Root)
	assert.Equal(t, gitSource, workspace.Source)
}

func TestResolverReturnsGitErrors(t *testing.T) {
	resolver := NewResolver()
	resolver.findGitSource = func(string) (repository.GitRepository, bool, error) {
		return repository.GitRepository{}, false, errors.New("invalid origin")
	}

	_, err := resolver.Resolve(t.TempDir())

	require.ErrorContains(t, err, "invalid origin")
}

func requireCanonicalPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := canonicalPath(path)
	require.NoError(t, err)

	return resolved
}
