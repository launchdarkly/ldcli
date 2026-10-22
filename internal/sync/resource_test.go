package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGroupByProjectSortsProjectsAndKeepsOrder(t *testing.T) {
	betaFirst, alphaFirst, betaSecond := VariationID("beta", "c", "z"), VariationID("alpha", "c", "v"), VariationID("beta", "c", "a")

	var projects []string
	groups := map[string][]ResourceID{}
	for projectKey, ids := range GroupByProject([]ResourceID{betaFirst, alphaFirst, betaSecond}) {
		projects = append(projects, projectKey)
		groups[projectKey] = ids
	}

	assert.Equal(t, []string{"alpha", "beta"}, projects)
	assert.Equal(t, []ResourceID{alphaFirst}, groups["alpha"])
	assert.Equal(t, []ResourceID{betaFirst, betaSecond}, groups["beta"])
}
