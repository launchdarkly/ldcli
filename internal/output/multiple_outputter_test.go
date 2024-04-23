package output_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/output"
)

func TestMultipleOutputter_JSON(t *testing.T) {
	input := []byte(`{
		"items": [
			{
				"key": "test-key1",
				"name": "test-name1",
				"other": "another-value2"
			},
			{
				"key": "test-key2",
				"name": "test-name2",
				"other": "another-value2"
			}
		]
	}`)
	output, err := output.CmdOutput(
		"json",
		output.NewMultipleOutputterFn(input),
	)

	require.NoError(t, err)
	assert.JSONEq(t, output, string(input))
}

func TestMultipleOutputter_String(t *testing.T) {
	input := []byte(`{
		"items": [
			{
				"key": "test-key1",
				"name": "test-name1",
				"other": "another-value2"
			},
			{
				"key": "test-key2",
				"name": "test-name2",
				"other": "another-value2"
			}
		]
	}`)
	expected := "* test-name1 (test-key1)\n* test-name2 (test-key2)"
	output, err := output.CmdOutput(
		"plaintext",
		output.NewMultipleOutputterFn(input),
	)

	require.NoError(t, err)
	assert.Equal(t, expected, output)
}
