package config_test

import (
	"fmt"
	"github.com/launchdarkly/ldcli/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
