package sdks_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ldcli/internal/sdks"
)

func TestReplaceFlagKey(t *testing.T) {
	tests := map[string]struct {
		body     string
		expected string
	}{
		"replaces placeholder my-flag-key": {
			body:     "# title ```const featureFlagKey = \"my-flag-key\"```",
			expected: "# title ```const featureFlagKey = \"real-flag-key\"```",
		},
		"replaces placeholder my-flag": {
			body:     "# title ```const featureFlagKey = \"my-flag\"```",
			expected: "# title ```const featureFlagKey = \"real-flag-key\"```",
		},
		"replaces placeholder my-boolean-flag": {
			body:     "# title ```const featureFlagKey = \"my-boolean-flag\"```",
			expected: "# title ```const featureFlagKey = \"real-flag-key\"```",
		},
		"replaces placeholder FLAG_KEY": {
			body:     "# title ```const featureFlagKey = \"my-boolean-flag\"```",
			expected: "# title ```const featureFlagKey = \"real-flag-key\"```",
		},
		"replaces placeholder <flag key>": {
			body:     "# title ```hello_erlang_server:get(<<\"FLAG_KEY\">>)```",
			expected: "# title ```hello_erlang_server:get(<<\"real-flag-key\">>)```",
		},
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			updated := sdks.ReplaceFlagKey(tt.body, "real-flag-key")

			assert.Equal(t, string(tt.expected), string(updated))
		})
	}
}

func TestReplaceSDKKey(t *testing.T) {
	tests := map[string]struct {
		body     string
		expected string
	}{
		"replaces placeholder 1234567890abcdef": {
			body:     "# title ```const sdkKey = \"1234567890abcdef\"```",
			expected: "# title ```const sdkKey = \"real-sdk-key\"```",
		},
		"replaces placeholder myClientSideID": {
			body:     "# title ```const sdkKey = \"myClientSideID\"```",
			expected: "# title ```const sdkKey = \"real-sdk-key\"```",
		},
		"replaces placeholder mobile-key-from-launch-darkly-website": {
			body:     "# title ```const sdkKey = \"mobile-key-from-launch-darkly-website\"```",
			expected: "# title ```const sdkKey = \"real-sdk-key\"```",
		},
		"replaces placeholder YOUR_SDK_KEY": {
			body:     "# title ```const sdkKey = \"YOUR_SDK_KEY\"```",
			expected: "# title ```const sdkKey = \"real-sdk-key\"```",
		},
		"replaces placeholder Your LaunchDarkly SDK key": {
			body:     "# title ```const sdkKey = \"Your LaunchDarkly SDK key\"```",
			expected: "# title ```const sdkKey = \"real-sdk-key\"```",
		},
	}
	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			updated := sdks.ReplaceSDKKey(tt.body, "real-sdk-key")

			assert.Equal(t, string(tt.expected), string(updated))
		})
	}
}