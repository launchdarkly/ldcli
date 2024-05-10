package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
