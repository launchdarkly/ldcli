package sync

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
)

func TestRunPromptUsesSharedFlowForBootstrapAndAdd(t *testing.T) {
	tests := map[string]struct {
		createDirectory bool
		add             bool
		wantInitial     bool
	}{
		"missing directory bootstraps": {
			wantInitial: true,
		},
		"add uses existing directory": {
			createDirectory: true,
			add:             true,
			wantInitial:     false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := initBootstrapRepo(t)
			if test.createDirectory {
				require.NoError(t, os.Mkdir(filepath.Join(root, syncdomain.RootDir), 0o755))
			}
			t.Chdir(root)

			var called bool
			runner := func(options syncbootstrap.Options) error {
				called = true
				assert.Equal(t, test.wantInitial, options.Initial)
				assert.NotNil(t, options.Input)
				assert.NotNil(t, options.Output)
				return nil
			}

			viper.Set(cliflags.AccessTokenFlag, "token")
			viper.Set(cliflags.BaseURIFlag, "https://example.com")
			t.Cleanup(viper.Reset)

			command := newPromptCmd(noopResourceClient{}, runner)
			require.NoError(t, command.Flags().Set(addFlag, boolString(test.add)))
			require.NoError(t, command.RunE(command, nil))
			assert.True(t, called)
		})
	}
}

func TestWriteRequestDebugIncludesQuery(t *testing.T) {
	var output bytes.Buffer
	writeRequestDebug(
		&output,
		"GET",
		"https://example.com/api/v2/projects",
		url.Values{
			"sort":   {"name"},
			"limit":  {"25"},
			"offset": {"50"},
		},
		nil,
	)

	assert.Contains(
		t,
		output.String(),
		"Path: /api/v2/projects?limit=25&offset=50&sort=name",
	)
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

func (noopResourceClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func initBootstrapRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	command := exec.Command("git", "init", "--quiet")
	command.Dir = root
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	command = exec.Command("git", "remote", "add", "origin", "git@github.com:launchdarkly/ldcli.git")
	command.Dir = root
	output, err = command.CombinedOutput()
	require.NoError(t, err, string(output))

	return root
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
