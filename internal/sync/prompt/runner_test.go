package prompt

import (
	"context"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	syncdetach "github.com/launchdarkly/ldcli/internal/sync/detach"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
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

func TestRunnerWatchDoesNotRunInitialSync(t *testing.T) {
	root := initGitRepository(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
	runner := NewRunner(noopResourceClient{})
	watchCalled := false
	runner.watch = func(
		_ context.Context,
		repositoryRoot string,
		_ time.Duration,
		_ func(*sourceWatcher) error,
		_ io.Writer,
	) error {
		watchCalled = true
		expectedRoot, err := filepath.EvalSymlinks(root)
		require.NoError(t, err)
		assert.Equal(t, expectedRoot, repositoryRoot)
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: root,
		Watch:            true,
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.NoError(t, err)
	assert.True(t, watchCalled)
}

func TestRunnerLinksBeforeWatching(t *testing.T) {
	root := initGitRepository(t)
	runner := NewRunner(noopResourceClient{})
	linkCalled := false
	runner.link = func(options synclink.Options) (string, error) {
		linkCalled = true
		assert.Equal(t, "prompt.md", options.File)
		assert.Equal(t, syncreference.PlainMarkdown, options.Format)
		require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
		return "project/configs/config/prompt.prompt.md", nil
	}
	watchCalled := false
	runner.watch = func(context.Context, string, time.Duration, func(*sourceWatcher) error, io.Writer) error {
		watchCalled = true
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: root,
		Link:             "prompt.md",
		Format:           syncreference.PlainMarkdown,
		Watch:            true,
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.NoError(t, err)
	assert.True(t, linkCalled)
	assert.True(t, watchCalled)
}

func TestRunnerDetachesWithoutCallingTheAPI(t *testing.T) {
	root := initGitRepository(t)
	runner := NewRunner(noopResourceClient{})
	called := false
	runner.detach = func(options syncdetach.Options) error {
		called = true
		assert.NotZero(t, options.Store)
		assert.NotZero(t, options.Manifest)
		assert.Equal(t, os.Stdin, options.Input)
		assert.Equal(t, io.Discard, options.Output)
		return nil
	}

	err := runner.Run(Options{
		WorkingDirectory: root,
		Detach:           true,
		Input:            os.Stdin,
		Output:           io.Discard,
		ErrorOutput:      io.Discard,
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestValidateOptions(t *testing.T) {
	require.ErrorContains(t, validateOptions(Options{Format: syncreference.PlainMarkdown}), "--format requires --link")
	require.ErrorContains(t, validateOptions(Options{Link: "prompt.md"}), "--link requires --format")
	require.ErrorContains(t, validateOptions(Options{Watch: true, DryRun: true}), "--watch cannot be used with --dry-run")
	require.ErrorContains(t, validateOptions(Options{Detach: true, Add: true}), "--detach cannot be combined")
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
