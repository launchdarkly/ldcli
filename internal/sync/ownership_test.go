package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOwnershipManagedInLaunchDarkly(t *testing.T) {
	tests := map[string]struct {
		ownership Ownership
		want      bool
	}{
		"an explicit LaunchDarkly rule": {
			ownership: Ownership{Direction: SyncDirectionServerCanonical, MatchedPattern: "config/variation"},
			want:      true,
		},
		"a wildcard LaunchDarkly rule": {
			ownership: Ownership{Direction: SyncDirectionServerCanonical, MatchedPattern: "config/*"},
			want:      true,
		},
		"the default with no rule": {
			ownership: Ownership{Direction: SyncDirectionServerCanonical},
		},
		"a code rule": {
			ownership: Ownership{Direction: SyncDirectionCodeCanonical, MatchedPattern: "config/variation"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, test.ownership.ManagedInLaunchDarkly())
		})
	}
}
