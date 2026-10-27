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
			return repository.GitRepository{
				Root: root, Origin: "git@github.com:launchdarkly/ldcli.git",
			}, true, nil
		},
	}

	workspace, err := resolver.Resolve(filepath.Join(root, "nested"))

	require.NoError(t, err)
	expected, err := canonicalPath(root)
	require.NoError(t, err)
	require.Equal(t, expected, workspace.Root)
	require.Equal(t, "git:github.com/launchdarkly/ldcli", workspace.Source)
}

func TestResolverResolvesRepositorySymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "repository")
	require.NoError(t, os.Symlink(root, link))
	resolver := Resolver{
		findGitRepository: func(string) (repository.GitRepository, bool, error) {
			return repository.GitRepository{
				Root: link, Origin: "git@github.com:launchdarkly/ldcli.git",
			}, true, nil
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
	require.EqualError(t, err, "sync must run inside an initialized Git repository")
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

func TestResolverNormalizesGitOrigin(t *testing.T) {
	tests := map[string]string{
		"SSH shorthand": "git@GitHub.com:launchdarkly/ldcli.git",
		"SSH URL":       "ssh://git@github.com:22/launchdarkly/ldcli.git",
		"HTTPS URL":     "https://token@github.com:443/launchdarkly/ldcli.git",
	}
	for name, origin := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			resolver := Resolver{
				findGitRepository: func(string) (repository.GitRepository, bool, error) {
					return repository.GitRepository{Root: root, Origin: origin}, true, nil
				},
			}

			workspace, err := resolver.Resolve(root)

			require.NoError(t, err)
			require.Equal(t, "git:github.com/launchdarkly/ldcli", workspace.Source)
		})
	}
}

func TestResolverRejectsInvalidOrigin(t *testing.T) {
	root := t.TempDir()
	resolver := Resolver{
		findGitRepository: func(string) (repository.GitRepository, bool, error) {
			return repository.GitRepository{Root: root}, true, nil
		},
	}

	_, err := resolver.Resolve(root)

	require.ErrorContains(t, err, "derive sync source from Git origin")
}
