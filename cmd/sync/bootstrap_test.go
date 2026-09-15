package sync

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
)

func TestRunPromptBootstrapsAndAddsVariations(t *testing.T) {
	tests := map[string]struct {
		createDirectory bool
		add             bool
		wantInitial     bool
	}{
		"missing workspace bootstraps without Git": {
			wantInitial: true,
		},
		"add uses the existing workspace": {
			createDirectory: true,
			add:             true,
			wantInitial:     false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if test.createDirectory {
				require.NoError(t, os.Mkdir(
					filepath.Join(root, syncdomain.RootDir),
					0o755,
				))
			}
			t.Chdir(root)
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			var called bool
			runner := func(options syncbootstrap.Options) error {
				called = true
				assert.Equal(t, test.wantInitial, options.Initial)
				assert.NotNil(t, options.Catalog)
				assert.NotNil(t, options.Input)
				assert.NotNil(t, options.Output)

				return nil
			}

			viper.Set(cliflags.AccessTokenFlag, "token")
			viper.Set(cliflags.BaseURIFlag, "https://example.com")
			t.Cleanup(viper.Reset)

			command := newPromptCmd(noopResourceClient{}, runner)
			require.NoError(t, command.Flags().Set(
				addFlag,
				strconv.FormatBool(test.add),
			))
			require.NoError(t, command.RunE(command, nil))
			assert.True(t, called)
			if test.wantInitial {
				_, err := os.Stat(config.GetConfigFile())
				assert.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}

func TestRunPromptUsesExistingWorkspaceWithoutBootstrap(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(
		filepath.Join(root, syncdomain.RootDir),
		0o755,
	))
	t.Chdir(root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	called := false
	runner := func(syncbootstrap.Options) error {
		called = true

		return nil
	}

	viper.Set(cliflags.AccessTokenFlag, "token")
	viper.Set(cliflags.BaseURIFlag, "https://example.com")
	t.Cleanup(viper.Reset)

	command := newPromptCmd(noopResourceClient{}, runner)
	require.NoError(t, command.RunE(command, nil))
	assert.False(t, called)
}

type noopResourceClient struct{}

var _ resources.Client = noopResourceClient{}

func (noopResourceClient) MakeRequest(
	string,
	string,
	string,
	string,
	url.Values,
	[]byte,
	bool,
) ([]byte, error) {
	return nil, nil
}

func (noopResourceClient) MakeUnauthenticatedRequest(
	string,
	string,
	[]byte,
) ([]byte, error) {
	return nil, nil
}
