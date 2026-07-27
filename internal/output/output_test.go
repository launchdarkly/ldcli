package output_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/output"
)

func TestNewOutputKind(t *testing.T) {
	t.Run("returns json for valid json input", func(t *testing.T) {
		kind, err := output.NewOutputKind("json")
		require.NoError(t, err)
		assert.Equal(t, output.OutputKindJSON, kind)
	})

	t.Run("returns plaintext for valid plaintext input", func(t *testing.T) {
		kind, err := output.NewOutputKind("plaintext")
		require.NoError(t, err)
		assert.Equal(t, output.OutputKindPlaintext, kind)
	})

	t.Run("returns markdown for valid markdown input", func(t *testing.T) {
		kind, err := output.NewOutputKind("markdown")
		require.NoError(t, err)
		assert.Equal(t, output.OutputKindMarkdown, kind)
	})

	t.Run("returns error for invalid input", func(t *testing.T) {
		kind, err := output.NewOutputKind("xml")
		assert.ErrorIs(t, err, output.ErrInvalidOutputKind)
		assert.Equal(t, output.OutputKindNull, kind)
	})

	t.Run("returns error for empty string", func(t *testing.T) {
		kind, err := output.NewOutputKind("")
		assert.ErrorIs(t, err, output.ErrInvalidOutputKind)
		assert.Equal(t, output.OutputKindNull, kind)
	})
}

func TestCmdOutputSingular(t *testing.T) {
	tests := map[string]struct {
		expected string
		fn       output.PlaintextOutputFn
		input    string
	}{
		"with config file data": {
			expected: "key: value\nkey2: value2",
			fn:       output.ConfigPlaintextOutputFn,
			input:    `{"key": "value", "key2": "value2"}`,
		},
		"with an error with a code and message": {
			expected: "test-message (code: test-code)",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{"code": "test-code", "message": "test-message"}`,
		},
		"with an error with only a code": {
			expected: "an error occurred (code: test-code)",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{"code": "test-code", "message": ""}`,
		},
		"with an error with only a message": {
			expected: "test-message",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{"message": "test-message"}`,
		},
		"with an error without a code or message": {
			expected: "unknown error occurred",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{"message": ""}`,
		},
		"with an error without a response body": {
			expected: "unknown error occurred",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{}`,
		},
		"with an error with a suggestion": {
			expected: "Not Found (code: not_found)\nSuggestion: Check the resource key.",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{"code": "not_found", "message": "Not Found", "suggestion": "Check the resource key."}`,
		},
		"with an error with an empty suggestion": {
			expected: "Internal Server Error (code: internal_server_error)",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{"code": "internal_server_error", "message": "Internal Server Error", "suggestion": ""}`,
		},
		"with an error with only a message and a suggestion": {
			expected: "an error\nSuggestion: Try again.",
			fn:       output.ErrorPlaintextOutputFn,
			input:    `{"message": "an error", "suggestion": "Try again."}`,
		},
		"with a singular resource": {
			expected: "test-name (test-key)",
			fn:       output.SingularPlaintextOutputFn,
			input:    `{"key": "test-key", "name": "test-name"}`,
		},
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			output, err := output.CmdOutputSingular(
				"plaintext",
				[]byte(tt.input),
				tt.fn,
			)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, output)
		})
	}

	t.Run("with json output kind returns raw JSON", func(t *testing.T) {
		input := `{"key": "test-key", "name": "test-name"}`

		result, err := output.CmdOutputSingular(
			"json",
			[]byte(input),
			output.SingularPlaintextOutputFn,
		)

		require.NoError(t, err)
		assert.JSONEq(t, input, result)
	})

	t.Run("with markdown output kind returns plaintext representation", func(t *testing.T) {
		input := `{"key": "test-key", "name": "test-name"}`

		result, err := output.CmdOutputSingular(
			"markdown",
			[]byte(input),
			output.SingularPlaintextOutputFn,
		)

		require.NoError(t, err)
		assert.Equal(t, "test-name (test-key)", result)
	})
}
