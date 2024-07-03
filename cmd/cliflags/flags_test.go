package cliflags_test

import (
	"testing"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/stretchr/testify/assert"
)

func TestGetBaseURI(t *testing.T) {
	t.Run("without a trailing slash", func(t *testing.T) {
		result := cliflags.GetBaseURI("/test")

		assert.Equal(t, "/test", result)
	})

	t.Run("with a trailing slash", func(t *testing.T) {
		result := cliflags.GetBaseURI("/test/")

		assert.Equal(t, "/test", result)
	})
}
