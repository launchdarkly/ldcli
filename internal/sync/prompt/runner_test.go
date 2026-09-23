package prompt

import (
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

func TestRunnerBootstrapsMissingWorkspaceAndAddsToExistingWorkspace(t *testing.T) {
	tests := map[string]struct {
		createDirectory bool
		add             bool
		dryRun          bool
		wantInitial     bool
	}{
		"missing workspace": {
			wantInitial: true,
		},
		"missing workspace dry run": {
			dryRun:      true,
			wantInitial: true,
		},
		"add to existing workspace": {
			createDirectory: true,
			add:             true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initGitRepository(t)
			if test.createDirectory {
				require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
			}

			called := false
			runner := NewRunner(noopResourceClient{})
			runner.bootstrap = func(options syncbootstrap.Options) error {
				called = true
				assert.Equal(t, test.wantInitial, options.Initial)
				assert.Equal(t, test.dryRun, options.DryRun)
				assert.NotNil(t, options.Catalog)
				assert.NotNil(t, options.Input)
				assert.NotNil(t, options.Output)
				return nil
			}

			err := runner.Run(Options{
				WorkingDirectory: root,
				AccessToken:      "token",
				BaseURI:          "https://example.com",
				Add:              test.add,
				DryRun:           test.dryRun,
				Input:            os.Stdin,
				Output:           io.Discard,
				ErrorOutput:      io.Discard,
			})

			require.NoError(t, err)
			assert.True(t, called)
		})
	}
}

func TestRunnerRequiresGit(t *testing.T) {
	runner := NewRunner(noopResourceClient{})
	called := false
	runner.bootstrap = func(syncbootstrap.Options) error {
		called = true
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: t.TempDir(),
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.ErrorIs(t, err, syncsource.ErrGitRequired)
	assert.False(t, called)
}

type noopResourceClient struct{}

var _ resources.Client = noopResourceClient{}

func (noopResourceClient) MakeRequest(string, string, string, string, url.Values, []byte, bool) ([]byte, error) {
	return nil, nil
}

func (noopResourceClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func initGitRepository(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", "git@github.com:launchdarkly/example.git"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	return root
}
