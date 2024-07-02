package config_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/resources"
)

type mockReadFile struct {
	contents []byte
}

func (m mockReadFile) readFile(name string) ([]byte, error) {
	if m.contents != nil {
		return m.contents, nil
	}

	return yaml.Marshal(map[string]interface{}{
		"access-token": "test-access-token",
	})
}

func TestService_VerifyAccessToken(t *testing.T) {
	t.Run("is valid with a valid access token", func(t *testing.T) {
		service := config.NewService(&resources.MockClient{})

		isValid := service.VerifyAccessToken("valid-access-token", "http://test.com")

		assert.True(t, isValid)
	})

	t.Run("is invalid with an invalid access token", func(t *testing.T) {
		service := config.NewService(&resources.MockClient{
			StatusCode: http.StatusUnauthorized,
			Err:        errors.New("invalid access token"),
		})

		isValid := service.VerifyAccessToken("invalid-access-token", "http://test.com")

		assert.False(t, isValid)
	})
}

func TestNew(t *testing.T) {
	mock := mockReadFile{
		contents: []byte(`
access-token: test-access-token
analytics-opt-out: true
`,
		),
	}

	c, err := config.New("test-config", mock.readFile)

	require.NoError(t, err)
	assert.Equal(t, "test-access-token", c.AccessToken)
	assert.True(t, *c.AnalyticsOptOut)
}

func TestUpdate(t *testing.T) {
	c, err := config.New("test", mockReadFile{}.readFile)
	require.NoError(t, err)

	t.Run("updates valid fields", func(t *testing.T) {
		result, updatedFields, err := c.Update(
			[]string{
				"access-token", "test-access-token",
				"analytics-opt-out", "true",
				"base-uri", "http://test.com",
				"environment", "test-environment",
				"flag", "test-flag",
				"output", "plaintext",
				"project", "test-project",
			},
		)

		require.NoError(t, err)
		assert.Equal(t, "test-access-token", result.AccessToken)
		assert.True(t, *result.AnalyticsOptOut)
		assert.Equal(t, "http://test.com", result.BaseURI)
		assert.Equal(t, "test-environment", result.Environment)
		assert.Equal(t, "test-flag", result.Flag)
		assert.Equal(t, "plaintext", result.Output)
		assert.Equal(t, "test-project", result.Project)
		assert.Equal(
			t,
			[]string{
				"access-token",
				"analytics-opt-out",
				"base-uri",
				"environment",
				"flag",
				"output",
				"project",
			},
			updatedFields,
		)
	})

	t.Run("with an invalid output flag", func(t *testing.T) {
		_, _, err = c.Update([]string{"output", "invalid"})

		assert.EqualError(t, err, "output is invalid. Use 'json' or 'plaintext'")
	})

	t.Run("with an invalid analytics-opt-out flag", func(t *testing.T) {
		_, _, err = c.Update([]string{"analytics-opt-out", "invalid"})

		assert.EqualError(t, err, "analytics-opt-out must be true or false")
	})

	t.Run("with an invalid amount of flags", func(t *testing.T) {
		_, _, err = c.Update([]string{"access-token"})

		assert.EqualError(t, err, "flag needs an argument: --set")
	})

	t.Run("with an invalid flag", func(t *testing.T) {
		_, _, err = c.Update([]string{"invalid-flag", "val"})

		assert.EqualError(t, err, "invalid-flag is not a valid configuration option")
	})
}

func TestRemove(t *testing.T) {
	c, err := config.New("test", mockReadFile{}.readFile)
	require.NoError(t, err)

	t.Run("with a valid flag is a no-op", func(t *testing.T) {
		c, err = c.Remove("access-token")

		require.NoError(t, err)
		assert.Equal(t, "test-access-token", c.AccessToken)
	})

	t.Run("with an invalid flag is an error", func(t *testing.T) {
		c, err = c.Remove("invalid")

		assert.EqualError(t, err, "invalid is not a valid configuration option")
	})
}
