package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorPlaintextOutputFn(t *testing.T) {
	t.Run("with a non-string message does not panic", func(t *testing.T) {
		r := resource{"message": float64(404)}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "404", result)
	})

	t.Run("with a non-string code renders via fmt formatting", func(t *testing.T) {
		r := resource{"code": 123, "message": "an error"}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "an error (code: 123)", result)
	})

	t.Run("with non-string message and code does not panic", func(t *testing.T) {
		r := resource{"code": true, "message": 42}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "42 (code: true)", result)
	})
}

func TestSingularPlaintextOutputFn(t *testing.T) {
	tests := map[string]struct {
		resource resource
		expected string
	}{
		"with a name and key": {
			resource: resource{
				"key":  "test-key",
				"name": "test-name",
			},
			expected: "test-name (test-key)",
		},
		"with only a key": {
			resource: resource{
				"key": "test-key",
			},
			expected: "test-key",
		},
		"with an ID and email": {
			resource: resource{
				"_id":   "test-id",
				"email": "test-email",
				"name":  "test-name",
			},
			expected: "test-email (test-id)",
		},
		"with a name and ID": {
			resource: resource{
				"_id":  "test-id",
				"name": "test-name",
			},
			expected: "test-name (test-id)",
		},
		"with only an ID": {
			resource: resource{
				"_id": "test-id",
			},
			expected: "test-id",
		},
		"without any valid field": {
			resource: resource{
				"other": "other-value",
			},
			expected: "cannot read resource",
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			out := SingularPlaintextOutputFn(tt.resource)

			assert.Equal(t, tt.expected, out)
		})
	}
}
