package output_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/output"
)

func TestOutputter_JSON(t *testing.T) {
	input := []byte(`{
		"key": "test-key",
		"name": "test-name",
		"other": "another-value"
	}`)
	output, err := output.CmdOutput(
		"json",
		output.NewSingularOutputterFn(input),
	)

	require.NoError(t, err)
	assert.JSONEq(t, output, string(input))
}

func TestOutputter_String(t *testing.T) {
	input := []byte(`{
		"key": "test-key",
		"name": "test-name",
		"other": "another-value"
	}`)
	expected := "test-name (test-key)"
	output, err := output.CmdOutput(
		"plaintext",
		output.NewSingularOutputterFn(input),
	)

	require.NoError(t, err)
	assert.Equal(t, expected, output)
}
