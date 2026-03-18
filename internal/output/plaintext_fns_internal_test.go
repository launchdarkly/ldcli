package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorPlaintextOutputFn(t *testing.T) {
	t.Run("with code and message", func(t *testing.T) {
		r := resource{"code": "conflict", "message": "resource already exists"}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "resource already exists (code: conflict)", result)
	})

	t.Run("with only a message", func(t *testing.T) {
		r := resource{"message": "something went wrong"}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "something went wrong", result)
	})

	t.Run("with only a code", func(t *testing.T) {
		r := resource{"code": "not_found", "message": ""}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "an error occurred (code: not_found)", result)
	})

	t.Run("with neither code nor message", func(t *testing.T) {
		r := resource{}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "unknown error occurred", result)
	})

	t.Run("with empty message and nil code", func(t *testing.T) {
		r := resource{"message": ""}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "unknown error occurred", result)
	})

	t.Run("with suggestion appends suggestion line", func(t *testing.T) {
		r := resource{"code": "not_found", "message": "Not Found", "suggestion": "Try ldcli flags list."}

		result := ErrorPlaintextOutputFn(r)

		assert.Equal(t, "Not Found (code: not_found)\nSuggestion: Try ldcli flags list.", result)
	})

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

func TestMultiplePlaintextOutputFn(t *testing.T) {
	tests := map[string]struct {
		resource resource
		expected string
	}{
		"with a name and key": {
			resource: resource{
				"key":  "test-key",
				"name": "test-name",
			},
			expected: "* test-name (test-key)",
		},
		"with only a key": {
			resource: resource{
				"key": "test-key",
			},
			expected: "* test-key",
		},
		"with an email and ID": {
			resource: resource{
				"_id":   "test-id",
				"email": "test-email",
			},
			expected: "* test-email (test-id)",
		},
		"without any valid field": {
			resource: resource{
				"other": "other-value",
			},
			expected: "* cannot read resource",
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			out := MultiplePlaintextOutputFn(tt.resource)

			assert.Equal(t, tt.expected, out)
		})
	}
}

func TestConfigPlaintextOutputFn(t *testing.T) {
	tests := map[string]struct {
		resource resource
		expected string
	}{
		"with multiple keys sorts alphabetically": {
			resource: resource{
				"zeta":  "last",
				"alpha": "first",
				"mid":   "middle",
			},
			expected: "alpha: first\nmid: middle\nzeta: last",
		},
		"with single key": {
			resource: resource{
				"key": "value",
			},
			expected: "key: value",
		},
		"with empty resource": {
			resource: resource{},
			expected: "",
		},
		"with non-string values": {
			resource: resource{
				"count":   float64(42),
				"enabled": true,
			},
			expected: "count: 42\nenabled: true",
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			out := ConfigPlaintextOutputFn(tt.resource)

			assert.Equal(t, tt.expected, out)
		})
	}
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
		"with non-string name and key does not panic": {
			resource: resource{
				"key":  float64(123),
				"name": true,
			},
			expected: "true (123)",
		},
		"with non-string email and id does not panic": {
			resource: resource{
				"_id":   float64(999),
				"email": float64(42),
			},
			expected: "42 (999)",
		},
		"with non-string key only does not panic": {
			resource: resource{
				"key": float64(456),
			},
			expected: "456",
		},
		"with non-string name only does not panic": {
			resource: resource{
				"name": float64(789),
			},
			expected: "789",
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
