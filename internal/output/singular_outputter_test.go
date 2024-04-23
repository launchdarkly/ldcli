package output_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ldcli/internal/output"
)

func TestOutputter_JSON(t *testing.T) {
	input := []byte(`{
		"key": "test-key",
		"name": "test-name",
		"other": "another-value"
	}`)
	output := output.CmdOutput(
		"json",
		output.NewSingularOutputter(input),
	)

	assert.JSONEq(t, output, string(input))
}

func TestOutputter_String(t *testing.T) {
	input := []byte(`{
		"key": "test-key",
		"name": "test-name",
		"other": "another-value"
	}`)
	expected := "test-name (test-key)"
	output := output.CmdOutput(
		"plaintext",
		output.NewSingularOutputter(input),
	)

	assert.Equal(t, expected, output)
}
