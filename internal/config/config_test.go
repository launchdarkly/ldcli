package config_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/resources"
)

type mockReadFile struct {
	contents   []byte
	successful bool
}

func (m mockReadFile) readFile(name string) ([]byte, error) {
	if !m.successful {
		return nil, errors.New("an error")
	}

	if m.contents != nil {
		return yaml.Marshal(m.contents)
	}

	return yaml.Marshal(map[string]interface{}{
		"access-token": "test-access-token",
	})
}

func TestNewConfigFromFile(t *testing.T) {
	t.Run("with valid input is a valid config", func(t *testing.T) {
		mock := mockReadFile{
			successful: true,
		}

		c, err := config.NewConfigFromFile("test-config-file", mock.readFile)

		require.NoError(t, err)
		assert.Equal(t, "test-access-token", c.AccessToken)
	})

	t.Run("with invalid file is an error", func(t *testing.T) {
		mock := mockReadFile{
			successful: false,
		}

		_, err := config.NewConfigFromFile("test-config-file", mock.readFile)

		assert.EqualError(t, err, "could not read config file")
	})

	t.Run("with invalid formatting in the file contents is an error", func(t *testing.T) {
		mock := mockReadFile{
			contents:   []byte(`invalid`),
			successful: true,
		}

		_, err := config.NewConfigFromFile("test-config-file", mock.readFile)

		assert.EqualError(t, err, "config file is invalid yaml")
	})
}

func TestNewConfig(t *testing.T) {
	t.Run("analytics-opt-out", func(t *testing.T) {
		tests := map[string]struct {
			expected bool
			input    interface{}
		}{
			"when value is true": {
				expected: true,
				input:    true,
			},
			"when value is 1": {
				expected: true,
				input:    1,
			},
			"when value is false": {
				expected: false,
				input:    false,
			},
			"when value is 0": {
				expected: false,
				input:    0,
			},
		}
		for name, tt := range tests {
			tt := tt
			t.Run(fmt.Sprintf("%s is %t", name, tt.expected), func(t *testing.T) {
				rawConfig := map[string]interface{}{
					"analytics-opt-out": tt.input,
				}

				configFile, err := config.NewConfig(rawConfig)

				require.NoError(t, err)
				assert.Equal(t, tt.expected, *configFile.AnalyticsOptOut)
			})
		}
	})

	t.Run("is an error when analytics-opt-out is something else", func(t *testing.T) {
		rawConfig := map[string]interface{}{
			"analytics-opt-out": "something",
		}

		_, err := config.NewConfig(rawConfig)

		assert.EqualError(t, err, "analytics-opt-out must be true or false")
	})

	t.Run("analytics-opt-out", func(t *testing.T) {
		tests := map[string]struct {
			input string
		}{
			"is valid when value is json": {
				input: "json",
			},
			"is valid when value is plaintext": {
				input: "json",
			},
		}
		for name, tt := range tests {
			tt := tt
			t.Run(name, func(t *testing.T) {
				rawConfig := map[string]interface{}{
					"output": tt.input,
				}

				configFile, err := config.NewConfig(rawConfig)

				require.NoError(t, err)
				assert.Equal(t, tt.input, configFile.Output)
			})
		}
	})

	t.Run("is invalid with an invalid output", func(t *testing.T) {
		rawConfig := map[string]interface{}{
			"output": "invalid",
		}

		_, err := config.NewConfig(rawConfig)

		assert.EqualError(t, err, "output is invalid, use 'json' or 'plaintext'")
	})

	t.Run("environment", func(t *testing.T) {
		t.Run("sets the given value", func(t *testing.T) {
			rawConfig := map[string]interface{}{
				"environment": "test-key",
			}

			configFile, err := config.NewConfig(rawConfig)

			require.NoError(t, err)
			assert.Equal(t, "test-key", configFile.Environment)
		})
	})

	t.Run("flag", func(t *testing.T) {
		t.Run("sets the given value", func(t *testing.T) {
			rawConfig := map[string]interface{}{
				"flag": "test-key",
			}

			configFile, err := config.NewConfig(rawConfig)

			require.NoError(t, err)
			assert.Equal(t, "test-key", configFile.Flag)
		})
	})

	t.Run("project", func(t *testing.T) {
		t.Run("sets the given value", func(t *testing.T) {
			rawConfig := map[string]interface{}{
				"project": "test-key",
			}

			configFile, err := config.NewConfig(rawConfig)

			require.NoError(t, err)
			assert.Equal(t, "test-key", configFile.Project)
		})
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
