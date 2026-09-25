package interactive

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChoiceLabelPlacesDescriptionBelowTitle(t *testing.T) {
	assert.Equal(
		t,
		"Support prompt\n  support\n",
		choiceLabel(Choice[string]{
			Title:       "Support prompt",
			Description: "support",
		}),
	)
	assert.Equal(
		t,
		"Support prompt",
		choiceLabel(Choice[string]{Title: "Support prompt"}),
	)
}
