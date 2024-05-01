package output_test

import (
	"ldcli/internal/output"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
}
