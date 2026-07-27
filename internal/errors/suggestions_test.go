package errors_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/errors"
)

func TestSuggestionForStatus(t *testing.T) {
	t.Run("401 returns access token suggestion with baseURI", func(t *testing.T) {
		result := errors.SuggestionForStatus(401, "https://app.launchdarkly.com")

		assert.Contains(t, result, "ldcli login")
		assert.Contains(t, result, "LD_ACCESS_TOKEN")
		assert.Contains(t, result, "https://app.launchdarkly.com/settings/authorization")
		assert.NotContains(t, result, "{baseURI}")
	})

	t.Run("403 returns permission suggestion", func(t *testing.T) {
		result := errors.SuggestionForStatus(403, "https://app.launchdarkly.com")

		assert.Contains(t, result, "permission")
		assert.Contains(t, result, "custom role")
	})

	t.Run("404 returns resource not found suggestion", func(t *testing.T) {
		result := errors.SuggestionForStatus(404, "https://app.launchdarkly.com")

		assert.Contains(t, result, "not found")
		assert.Contains(t, result, "ldcli projects list")
		assert.Contains(t, result, "ldcli flags list")
	})

	t.Run("409 returns conflict suggestion", func(t *testing.T) {
		result := errors.SuggestionForStatus(409, "https://app.launchdarkly.com")

		assert.Contains(t, result, "Conflict")
		assert.Contains(t, result, "latest version")
	})

	t.Run("429 returns rate limit suggestion", func(t *testing.T) {
		result := errors.SuggestionForStatus(429, "https://app.launchdarkly.com")

		assert.Contains(t, result, "Rate limited")
		assert.Contains(t, result, "retry")
	})

	t.Run("unknown status code returns empty string", func(t *testing.T) {
		result := errors.SuggestionForStatus(502, "https://app.launchdarkly.com")

		assert.Empty(t, result)
	})

	t.Run("200 returns empty string", func(t *testing.T) {
		result := errors.SuggestionForStatus(200, "https://app.launchdarkly.com")

		assert.Empty(t, result)
	})

	t.Run("replaces baseURI placeholder with actual value", func(t *testing.T) {
		result := errors.SuggestionForStatus(401, "https://custom.ld.example.com")

		assert.Contains(t, result, "https://custom.ld.example.com/settings/authorization")
		assert.NotContains(t, result, "{baseURI}")
	})

	t.Run("empty baseURI does not panic", func(t *testing.T) {
		result := errors.SuggestionForStatus(401, "")

		assert.Contains(t, result, "ldcli login")
		assert.Contains(t, result, "/settings/authorization")
	})
}
