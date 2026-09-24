package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/sync/repository"
)

func TestResolverReturnsCanonicalRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	resolver := Resolver{
		findGitRepository: func(string) (repository.GitRepository, bool, error) {
			return repository.GitRepository{Root: root}, true, nil
		},
	}

	workspace, err := resolver.Resolve(filepath.Join(root, "nested"))

	require.NoError(t, err)
	expected, err := canonicalPath(root)
	require.NoError(t, err)
	require.Equal(t, expected, workspace.Root)
}

func TestResolverResolvesRepositorySymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "repository")
	require.NoError(t, os.Symlink(root, link))
	resolver := Resolver{
		findGitRepository: func(string) (repository.GitRepository, bool, error) {
			return repository.GitRepository{Root: link}, true, nil
		},
	}

	workspace, err := resolver.Resolve(link)

	require.NoError(t, err)
	expected, err := canonicalPath(root)
	require.NoError(t, err)
	require.Equal(t, expected, workspace.Root)
}

func TestResolverRequiresGitRepository(t *testing.T) {
	resolver := Resolver{
		findGitRepository: func(string) (repository.GitRepository, bool, error) {
			return repository.GitRepository{}, false, nil
		},
	}

	_, err := resolver.Resolve(".")

	require.ErrorIs(t, err, ErrGitRequired)
	require.EqualError(t, err, "git is required to use sync; initialize a Git repository")
}

func TestResolverReturnsRepositoryError(t *testing.T) {
	expected := errors.New("inspect repository")
	resolver := Resolver{
		findGitRepository: func(string) (repository.GitRepository, bool, error) {
			return repository.GitRepository{}, false, expected
		},
	}

	_, err := resolver.Resolve(".")

	require.ErrorIs(t, err, expected)
}
