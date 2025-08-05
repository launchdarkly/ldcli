package events

import (
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestFilterMatches(t *testing.T) {
	testCases := []struct {
		name     string
		filter   Filter
		event    Base
		expected bool
	}{
		{
			name:     "no filter matches",
			filter:   Filter{},
			event:    Base{Kind: "kind"},
			expected: true,
		},
		{
			name:     "filter kind matches",
			filter:   Filter{Kind: lo.ToPtr("kind")},
			event:    Base{Kind: "kind"},
			expected: true,
		},
		{
			name:     "filter kind does not match",
			filter:   Filter{Kind: lo.ToPtr("kind")},
			event:    Base{Kind: "other"},
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.filter.Matches(tc.event))
		})
	}
}
