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

	resolver := NewResolver(filepath.Join(t.TempDir(), "config.yml"))
	resolver.findGitSource = func(string) (repository.GitRepository, bool, error) {
		return repository.GitRepository{Root: root, Source: gitSource}, true, nil
	}
	resolver.ensureInstallationID = func(string) (string, error) {
		t.Fatal("Git source must not create an installation ID")

		return "", nil
	}

	workspace, err := resolver.Resolve(filepath.Join(root, "nested"))

	require.NoError(t, err)
	assert.Equal(t, requireCanonicalPath(t, root), workspace.Root)
	assert.Equal(t, gitSource, workspace.Source)
}

func TestResolverUsesStableLocalSourceFromWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
	nested := filepath.Join(root, "services", "api")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	resolver := localResolver("installation-id")

	first, err := resolver.Resolve(nested)
	require.NoError(t, err)
	second, err := resolver.Resolve(root)
	require.NoError(t, err)

	expectedRoot := requireCanonicalPath(t, root)
	assert.Equal(t, expectedRoot, first.Root)
	assert.Equal(t, first, second)
	assert.Equal(t, syncdomain.SourceTypeLocal, first.Source.Type())
	assert.Equal(
		t,
		localSourceIdentifier("installation-id", expectedRoot),
		first.Source.Identifier(),
	)
	assert.NotContains(t, first.Source.Identifier(), expectedRoot)

	_, err = os.Stat(filepath.Join(root, syncdomain.RootDir, "source.yaml"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestResolverUsesCurrentDirectoryBeforeBootstrap(t *testing.T) {
	root := t.TempDir()
	resolver := localResolver("installation-id")

	workspace, err := resolver.Resolve(root)

	require.NoError(t, err)
	expectedRoot := requireCanonicalPath(t, root)
	assert.Equal(t, expectedRoot, workspace.Root)
	assert.Equal(
		t,
		localSourceIdentifier("installation-id", expectedRoot),
		workspace.Source.Identifier(),
	)
}

func TestResolverCanonicalizesSymlinkedWorkspace(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))

	link := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.Symlink(root, link))

	workspace, err := localResolver("installation-id").Resolve(link)

	require.NoError(t, err)
	expectedRoot := requireCanonicalPath(t, root)
	assert.Equal(t, expectedRoot, workspace.Root)
	assert.Equal(
		t,
		localSourceIdentifier("installation-id", expectedRoot),
		workspace.Source.Identifier(),
	)
}

func TestResolverChangesLocalIdentityWhenWorkspaceMoves(t *testing.T) {
	first, err := localResolver("installation-id").Resolve(t.TempDir())
	require.NoError(t, err)
	second, err := localResolver("installation-id").Resolve(t.TempDir())
	require.NoError(t, err)

	assert.NotEqual(t, first.Source.Identifier(), second.Source.Identifier())
}

func TestResolverReturnsGitAndConfigErrors(t *testing.T) {
	t.Run("invalid Git source", func(t *testing.T) {
		resolver := localResolver("installation-id")
		resolver.findGitSource = func(string) (repository.GitRepository, bool, error) {
			return repository.GitRepository{}, false, errors.New("invalid origin")
		}

		_, err := resolver.Resolve(t.TempDir())
		require.ErrorContains(t, err, "invalid origin")
	})

	t.Run("installation ID", func(t *testing.T) {
		resolver := localResolver("installation-id")
		resolver.ensureInstallationID = func(string) (string, error) {
			return "", errors.New("config unavailable")
		}

		_, err := resolver.Resolve(t.TempDir())
		require.ErrorContains(t, err, "config unavailable")
	})
}

func localResolver(installationID string) Resolver {
	resolver := NewResolver("config.yml")
	resolver.findGitSource = func(string) (repository.GitRepository, bool, error) {
		return repository.GitRepository{}, false, nil
	}
	resolver.ensureInstallationID = func(string) (string, error) {
		return installationID, nil
	}

	return resolver
}

func requireCanonicalPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := canonicalPath(path)
	require.NoError(t, err)

	return resolved
}
