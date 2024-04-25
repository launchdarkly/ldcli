package config_test

import (
	"ldcli/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {
	t.Run("is valid with a json output", func(t *testing.T) {
		rawConfig := map[string]interface{}{
			"output": "json",
		}

		configFile, err := config.NewConfig(rawConfig)

		require.NoError(t, err)
		assert.Equal(t, "json", configFile.Output)
	})

	t.Run("is valid with a plaintext output", func(t *testing.T) {
		rawConfig := map[string]interface{}{
			"output": "plaintext",
		}

		configFile, err := config.NewConfig(rawConfig)

		require.NoError(t, err)
		assert.Equal(t, "plaintext", configFile.Output)
	})

	t.Run("is invalid with an invalid output", func(t *testing.T) {
		rawConfig := map[string]interface{}{
			"output": "invalid",
		}

		_, err := config.NewConfig(rawConfig)

		assert.EqualError(t, err, "output is invalid")
	})
}
