package dev_server_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
)

func TestStartServerCmd(t *testing.T) {
	baseArgs := []string{
		"dev-server", "start",
		"--access-token", "test-token",
		"--project", "test-proj",
		"--source", "staging",
	}

	t.Run("calls RunServer with parsed context JSON", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			append(baseArgs, "--context", `{"kind":"user","key":"test-user"}`),
		)

		require.NoError(t, err)
		assert.True(t, mockClient.RunServerCalled)
		require.NotNil(t, mockClient.RunServerParams.InitialProjectSettings.Context)
		assert.Equal(t, "test-user", mockClient.RunServerParams.InitialProjectSettings.Context.Key())
	})

	t.Run("calls RunServer with parsed override JSON", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			append(baseArgs, "--override", `{"my-flag": true}`),
		)

		require.NoError(t, err)
		assert.True(t, mockClient.RunServerCalled)
		assert.Len(t, mockClient.RunServerParams.InitialProjectSettings.Overrides, 1)
	})

	t.Run("returns error for malformed context JSON", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			append(baseArgs, "--context", `not-valid-json`),
		)

		require.Error(t, err)
		assert.False(t, mockClient.RunServerCalled)
	})
}

func TestStartServerCmdSdkInitTimeout(t *testing.T) {
	baseArgs := []string{
		"dev-server", "start",
		"--access-token", "test-token",
		"--project", "test-proj",
		"--source", "staging",
	}

	t.Run("defaults to 5s", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			baseArgs,
		)

		require.NoError(t, err)
		assert.True(t, mockClient.RunServerCalled)
		assert.Equal(t, 5*time.Second, adapters.DefaultSdkInitTimeout)
		assert.Equal(t, adapters.DefaultSdkInitTimeout, mockClient.RunServerParams.SdkInitTimeout)
	})

	t.Run("passes a custom timeout from the flag", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			append(baseArgs, "--sdk-init-timeout", "15s"),
		)

		require.NoError(t, err)
		assert.True(t, mockClient.RunServerCalled)
		assert.Equal(t, 15*time.Second, mockClient.RunServerParams.SdkInitTimeout)
	})

	t.Run("reads the timeout from the LD_SDK_INIT_TIMEOUT environment variable", func(t *testing.T) {
		t.Setenv("LD_SDK_INIT_TIMEOUT", "20s")

		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			baseArgs,
		)

		require.NoError(t, err)
		assert.True(t, mockClient.RunServerCalled)
		assert.Equal(t, 20*time.Second, mockClient.RunServerParams.SdkInitTimeout)
	})

	t.Run("returns error for a zero timeout", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			append(baseArgs, "--sdk-init-timeout", "0"),
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "--sdk-init-timeout must be greater than zero")
		assert.False(t, mockClient.RunServerCalled)
	})

	t.Run("returns error for a negative timeout", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			append(baseArgs, "--sdk-init-timeout", "-1s"),
		)

		require.Error(t, err)
		assert.False(t, mockClient.RunServerCalled)
	})

	t.Run("returns error for an unparseable timeout", func(t *testing.T) {
		mockClient := &dev_server.MockClient{}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{DevClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			append(baseArgs, "--sdk-init-timeout", "soon"),
		)

		require.Error(t, err)
		assert.False(t, mockClient.RunServerCalled)
	})
}
