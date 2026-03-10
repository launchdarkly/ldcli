package dev_server_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/dev_server"
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
