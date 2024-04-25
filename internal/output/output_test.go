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

func TestCmdOutputMultiple(t *testing.T) {
	tests := map[string]struct {
		expected string
		fn       output.PlaintextOutputFn
		input    string
	}{
		"with multiple emails not as items property": {
			expected: "* test-email1 (test-id1)\n* test-email2 (test-id2)",
			fn:       output.MultipleEmailPlaintextOutputFn,
			input:    `[{"_id": "test-id1", "email": "test-email1"}, {"_id": "test-id2", "email": "test-email2"}]`,
		},
		"with multiple items": {
			expected: "* test-name1 (test-key1)\n* test-name2 (test-key2)",
			fn:       output.MultiplePlaintextOutputFn,
			input:    `{"items": [{"key": "test-key1", "name": "test-name1"}, {"key": "test-key2", "name": "test-name2"}]}`,
		},
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			output, err := output.CmdOutputMultiple(
				"plaintext",
				[]byte(tt.input),
				tt.fn,
			)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, output)
		})
	}
}
