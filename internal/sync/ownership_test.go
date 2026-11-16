package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecideOwnership(t *testing.T) {
	id := VariationID("p", "support", "default")
	exact := id.LookupKey
	wildcard := "support/*"

	tests := []struct {
		name      string
		ownership Ownership
		want      OwnershipAction
	}{
		{
			name:      "exact code_canonical rule is already code-owned",
			ownership: Ownership{Direction: SyncDirectionCodeCanonical, MatchedPattern: exact},
			want:      OwnershipNothingToDo,
		},
		{
			name:      "wildcard code_canonical rule is claimed with an exact rule",
			ownership: Ownership{Direction: SyncDirectionCodeCanonical, MatchedPattern: wildcard},
			want:      OwnershipClaim,
		},
		{
			name:      "explicit server_canonical rule is managed in LaunchDarkly",
			ownership: Ownership{Direction: SyncDirectionServerCanonical, MatchedPattern: exact},
			want:      OwnershipManagedInLaunchDarkly,
		},
		{
			name:      "no rule with direction both is claimed",
			ownership: Ownership{Direction: SyncDirectionBoth},
			want:      OwnershipClaim,
		},
		{
			name:      "no rule with direction server_canonical is claimed",
			ownership: Ownership{Direction: SyncDirectionServerCanonical},
			want:      OwnershipClaim,
		},
		{
			name:      "rule with legacy direction both is claimed",
			ownership: Ownership{Direction: SyncDirectionBoth, MatchedPattern: exact},
			want:      OwnershipClaim,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, DecideOwnership(id, test.ownership))
		})
	}
}
