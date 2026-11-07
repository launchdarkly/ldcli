package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjectionIncludesComparesValuesWithoutFieldStates(t *testing.T) {
	want, have := "wanted", "other"
	expected := VariationProjection{Instructions: &want}

	assert.False(t, ProjectionIncludes(VariationProjection{Instructions: &have}, expected),
		"a projection without field states still compares the fields that have a value")
	assert.True(t, ProjectionIncludes(VariationProjection{Instructions: &want}, expected))
	assert.True(t, ProjectionIncludes(VariationProjection{}, VariationProjection{}))
}
