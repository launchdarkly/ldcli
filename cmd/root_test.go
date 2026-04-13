package cmd_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestCreate(t *testing.T) {
	t.Run("with valid flags prints version", func(t *testing.T) {
		args := []string{
			"--version",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `ldcli version test`)
	})
}

func TestOutputFlags(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{"key": "test-key", "name": "test-name"}`),
	}

	t.Run("--json returns raw JSON output", func(t *testing.T) {
		args := []string{
			"flags", "toggle-on",
			"--access-token", "abcd1234",
			"--environment", "test-env",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--json",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `"key": "test-key"`)
		assert.NotContains(t, string(output), "Successfully updated")
	})

	t.Run("--output json returns raw JSON output", func(t *testing.T) {
		args := []string{
			"flags", "toggle-on",
			"--access-token", "abcd1234",
			"--environment", "test-env",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `"key": "test-key"`)
		assert.NotContains(t, string(output), "Successfully updated")
	})

	t.Run("--output plaintext returns human-readable output", func(t *testing.T) {
		args := []string{
			"flags", "toggle-on",
			"--access-token", "abcd1234",
			"--environment", "test-env",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "plaintext",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), "Successfully updated\n\nKey:")
		assert.Contains(t, string(output), "test-key")
	})
}

// getenv is passed to NewRootCommand for FORCE_TTY / LD_FORCE_TTY only; nil uses os.Getenv.
func newRootCmdWithTerminal(t *testing.T, isTerminal func() bool, getenv func(string) string) *cmd.RootCmd {
	t.Helper()
	rootCmd, err := cmd.NewRootCommand(
		config.NewService(&resources.MockClient{}),
		analytics.NoopClientFn{}.Tracker(),
		cmd.APIClients{},
		"test",
		false,
		isTerminal,
		getenv,
	)
	require.NoError(t, err)
	return rootCmd
}

func execNonTTYCmd(t *testing.T, mockClient *resources.MockClient, extraArgs ...string) []byte {
	t.Helper()
	return execNonTTYCmdGetenv(t, mockClient, nil, extraArgs...)
}

// getenv is forwarded to NewRootCommand for FORCE_TTY / LD_FORCE_TTY only (nil uses os.Getenv).
func execNonTTYCmdGetenv(t *testing.T, mockClient *resources.MockClient, getenv func(string) string, extraArgs ...string) []byte {
	t.Helper()
	rootCmd, err := cmd.NewRootCommand(
		config.NewService(&resources.MockClient{}),
		analytics.NoopClientFn{}.Tracker(),
		cmd.APIClients{ResourcesClient: mockClient},
		"test",
		false,
		func() bool { return false },
		getenv,
	)
	require.NoError(t, err)

	c := rootCmd.Cmd()
	b := bytes.NewBufferString("")
	c.SetOut(b)
	args := []string{
		"flags", "toggle-on",
		"--access-token", "abcd1234",
		"--environment", "test-env",
		"--flag", "test-flag",
		"--project", "test-proj",
	}
	args = append(args, extraArgs...)
	c.SetArgs(args)

	err = c.Execute()
	require.NoError(t, err)

	out, err := io.ReadAll(b)
	require.NoError(t, err)
	return out
}

func TestOutputDefaultsAndOverrides(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{"key": "test-key", "name": "test-name"}`),
	}

	t.Run("non-TTY defaults to json output", func(t *testing.T) {
		rootCmd := newRootCmdWithTerminal(t, func() bool { return false }, func(string) string { return "" })

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "json", f.DefValue)
	})

	t.Run("TTY defaults to plaintext output", func(t *testing.T) {
		rootCmd := newRootCmdWithTerminal(t, func() bool { return true }, func(string) string { return "" })

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("explicit --output plaintext overrides non-TTY", func(t *testing.T) {
		out := execNonTTYCmd(t, mockClient, "--output", "plaintext")
		assert.Contains(t, string(out), "Successfully updated")
	})

	t.Run("non-TTY without explicit flag returns JSON", func(t *testing.T) {
		out := execNonTTYCmd(t, mockClient)
		assert.Contains(t, string(out), `"key"`)
		assert.NotContains(t, string(out), "Successfully updated")
	})

	t.Run("--json overrides non-TTY default", func(t *testing.T) {
		out := execNonTTYCmd(t, mockClient, "--json")
		assert.Contains(t, string(out), `"key"`)
		assert.NotContains(t, string(out), "Successfully updated")
	})

	t.Run("LD_OUTPUT=plaintext overrides non-TTY default", func(t *testing.T) {
		t.Setenv("LD_OUTPUT", "plaintext")

		out := execNonTTYCmd(t, mockClient)
		assert.Contains(t, string(out), "Successfully updated")
		assert.Contains(t, string(out), "Key:")
		assert.Contains(t, string(out), "test-key")
	})

	t.Run("FORCE_TTY=0 yields plaintext DefValue when non-TTY", func(t *testing.T) {
		getenv := func(k string) string {
			if k == "FORCE_TTY" {
				return "0"
			}
			return ""
		}
		rootCmd := newRootCmdWithTerminal(t, func() bool { return false }, getenv)

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("FORCE_TTY=1 yields plaintext DefValue when non-TTY", func(t *testing.T) {
		getenv := func(k string) string {
			if k == "FORCE_TTY" {
				return "1"
			}
			return ""
		}
		rootCmd := newRootCmdWithTerminal(t, func() bool { return false }, getenv)

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("LD_FORCE_TTY=1 yields plaintext DefValue when non-TTY", func(t *testing.T) {
		getenv := func(k string) string {
			if k == "LD_FORCE_TTY" {
				return "1"
			}
			return ""
		}
		rootCmd := newRootCmdWithTerminal(t, func() bool { return false }, getenv)

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("LD_FORCE_TTY=1 yields plaintext output when non-TTY", func(t *testing.T) {
		getenv := func(k string) string {
			if k == "LD_FORCE_TTY" {
				return "1"
			}
			return ""
		}
		out := execNonTTYCmdGetenv(t, mockClient, getenv)
		assert.Contains(t, string(out), "Successfully updated")
		assert.Contains(t, string(out), "test-name (test-key)")
	})

	t.Run("FORCE_TTY=1 yields plaintext output when non-TTY", func(t *testing.T) {
		getenv := func(k string) string {
			if k == "FORCE_TTY" {
				return "1"
			}
			return ""
		}
		out := execNonTTYCmdGetenv(t, mockClient, getenv)
		assert.Contains(t, string(out), "Successfully updated")
		assert.Contains(t, string(out), "test-name (test-key)")
	})
}

func TestNewRootCommandNilIsTerminalRejects(t *testing.T) {
	_, err := cmd.NewRootCommand(
		config.NewService(&resources.MockClient{}),
		analytics.NoopClientFn{}.Tracker(),
		cmd.APIClients{},
		"test",
		false,
		nil,
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "isTerminal")
}

func TestConfigOutputPrecedenceNonTTY(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	mockClient := &resources.MockClient{
		Response: []byte(`{"key": "test-key", "name": "test-name"}`),
	}

	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)

	ldcliDir := filepath.Join(cfgRoot, "ldcli")
	require.NoError(t, os.MkdirAll(ldcliDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ldcliDir, "config.yml"), []byte("output: plaintext\n"), 0o644))

	rootCmd, err := cmd.NewRootCommand(
		config.NewService(&resources.MockClient{}),
		analytics.NoopClientFn{}.Tracker(),
		cmd.APIClients{ResourcesClient: mockClient},
		"test",
		true,
		func() bool { return false },
		nil,
	)
	require.NoError(t, err)

	c := rootCmd.Cmd()
	b := bytes.NewBufferString("")
	c.SetOut(b)
	c.SetArgs([]string{
		"flags", "toggle-on",
		"--access-token", "abcd1234",
		"--environment", "test-env",
		"--flag", "test-flag",
		"--project", "test-proj",
	})
	require.NoError(t, c.Execute())

	out, err := io.ReadAll(b)
	require.NoError(t, err)
	assert.Contains(t, string(out), "Successfully updated")
	assert.Contains(t, string(out), "test-name (test-key)")
}
