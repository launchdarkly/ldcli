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
		assert.Contains(t, string(output), "Successfully updated")
		assert.Contains(t, string(output), "test-name (test-key)")
	})
}

func newRootCmdWithTerminal(t *testing.T, isTerminal func() bool) *cmd.RootCmd {
	t.Helper()
	rootCmd, err := cmd.NewRootCommand(
		config.NewService(&resources.MockClient{}),
		analytics.NoopClientFn{}.Tracker(),
		cmd.APIClients{},
		"test",
		false,
		isTerminal,
	)
	require.NoError(t, err)
	return rootCmd
}

func execNonTTYCmd(t *testing.T, mockClient *resources.MockClient, extraArgs ...string) []byte {
	t.Helper()
	rootCmd, err := cmd.NewRootCommand(
		config.NewService(&resources.MockClient{}),
		analytics.NoopClientFn{}.Tracker(),
		cmd.APIClients{ResourcesClient: mockClient},
		"test",
		false,
		func() bool { return false },
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
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")

		rootCmd := newRootCmdWithTerminal(t, func() bool { return false })

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "json", f.DefValue)
	})

	t.Run("TTY defaults to plaintext output", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")

		rootCmd := newRootCmdWithTerminal(t, func() bool { return true })

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("nil isTerminal defaults to json output", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")

		rootCmd := newRootCmdWithTerminal(t, nil)

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "json", f.DefValue)
	})

	t.Run("explicit --output plaintext overrides non-TTY", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")

		out := execNonTTYCmd(t, mockClient, "--output", "plaintext")
		assert.Contains(t, string(out), "Successfully updated")
	})

	t.Run("non-TTY without explicit flag returns JSON", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")

		out := execNonTTYCmd(t, mockClient)
		assert.Contains(t, string(out), `"key"`)
		assert.NotContains(t, string(out), "Successfully updated")
	})

	t.Run("--json overrides non-TTY default", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")

		out := execNonTTYCmd(t, mockClient, "--json")
		assert.Contains(t, string(out), `"key"`)
		assert.NotContains(t, string(out), "Successfully updated")
	})

	t.Run("LD_OUTPUT=plaintext overrides non-TTY default", func(t *testing.T) {
		t.Setenv("LD_OUTPUT", "plaintext")
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")

		out := execNonTTYCmd(t, mockClient)
		assert.Contains(t, string(out), "Successfully updated")
		assert.Contains(t, string(out), "test-name (test-key)")
	})

	t.Run("FORCE_TTY=0 yields plaintext DefValue when non-TTY", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "0")
		t.Setenv("LD_FORCE_TTY", "")

		rootCmd := newRootCmdWithTerminal(t, func() bool { return false })

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("FORCE_TTY=1 yields plaintext DefValue when non-TTY", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "1")
		t.Setenv("LD_FORCE_TTY", "")

		rootCmd := newRootCmdWithTerminal(t, func() bool { return false })

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("LD_FORCE_TTY=1 yields plaintext DefValue when non-TTY", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "1")

		rootCmd := newRootCmdWithTerminal(t, func() bool { return false })

		f := rootCmd.Cmd().PersistentFlags().Lookup(cliflags.OutputFlag)
		require.NotNil(t, f)
		assert.Equal(t, "plaintext", f.DefValue)
	})

	t.Run("LD_FORCE_TTY=1 yields plaintext output when non-TTY", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "1")

		out := execNonTTYCmd(t, mockClient)
		assert.Contains(t, string(out), "Successfully updated")
		assert.Contains(t, string(out), "test-name (test-key)")
	})
}

func TestConfigOutputPrecedenceNonTTY(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	mockClient := &resources.MockClient{
		Response: []byte(`{"key": "test-key", "name": "test-name"}`),
	}

	cfgRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)
	t.Setenv("FORCE_TTY", "")
	t.Setenv("LD_FORCE_TTY", "")

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
