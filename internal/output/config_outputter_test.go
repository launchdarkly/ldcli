package output_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/output"
)

func TestConfigOutputter_JSON(t *testing.T) {
	input := []byte(`{
		"access-token": "test-access-token",
		"base-uri": "test-base-uri"
	}`)
	output, err := output.CmdOutput(
		"json",
		output.NewConfigOutputterFn(input),
	)

	require.NoError(t, err)
	assert.JSONEq(t, output, string(input))
}

func TestConfigOutputter_String(t *testing.T) {
	input := []byte(`{
		"access-token": "test-access-token",
		"base-uri": "test-base-uri"
	}`)
	expected := "access-token: test-access-token\nbase-uri: test-base-uri"
	output, err := output.CmdOutput(
		"plaintext",
		output.NewConfigOutputterFn(input),
	)

	require.NoError(t, err)
	assert.Equal(t, expected, output)
}
