package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferenceRelationshipsPreserveOwnershipAndExactKeys(t *testing.T) {
	empty := []AttachmentRef{}
	keys := []AttachmentRef{{Key: "Tool.Case_sensitive"}}
	variation := Variation{
		Mode:  VariationModeAgent,
		Tools: []AttachmentRef{{Key: "Tool.Case_sensitive", Version: 9}},
	}

	unowned, err := ProjectVariation(variation, Reference{})
	require.NoError(t, err)
	assert.Nil(t, unowned.Tools)
	_, owned := unowned.FieldStates[FieldTools]
	assert.False(t, owned)

	ownedEmpty, err := ProjectVariation(variation, Reference{Tools: &empty})
	require.NoError(t, err)
	require.NotNil(t, ownedEmpty.Tools)
	assert.Equal(t, []string{"Tool.Case_sensitive"}, *ownedEmpty.Tools)

	ownedKeys, err := ProjectVariation(variation, Reference{Tools: &keys})
	require.NoError(t, err)
	require.NotNil(t, ownedKeys.Tools)
	assert.Equal(t, []string{"Tool.Case_sensitive"}, *ownedKeys.Tools)

	emptyKeys := []string{}
	applied := ApplyVariationProjection(Variation{}, VariationProjection{
		Tools: &emptyKeys,
		FieldStates: map[Field]FieldState{
			FieldTools: {Capability: CapabilityDirect, Known: true},
		},
	})
	require.NotNil(t, applied.Tools)
	assert.Empty(t, applied.Tools)
	applied = ApplyVariationProjection(Variation{}, ownedKeys)
	assert.Equal(t, []AttachmentRef{{Key: "Tool.Case_sensitive"}}, applied.Tools)
}

func TestReferenceDigestExcludesRelationships(t *testing.T) {
	first := []AttachmentRef{{Key: "search"}}
	second := []AttachmentRef{{Key: "lookup"}}
	base := Reference{File: "prompt.md", Format: "markdown"}

	left, err := ReferenceDigest(Reference{
		File: base.File, Format: base.Format, Tools: &first,
	})
	require.NoError(t, err)
	right, err := ReferenceDigest(Reference{
		File: base.File, Format: base.Format, Tools: &second,
	})
	require.NoError(t, err)
	assert.Equal(t, left, right)
}

func TestValidateAttachmentRefsAllowsCompletionToolsAndRejectsSkills(t *testing.T) {
	require.NoError(t, ValidateAttachmentRefs(
		VariationModeCompletion,
		[]AttachmentRef{{Key: "search"}},
		nil,
	))

	err := ValidateAttachmentRefs(
		VariationModeCompletion,
		nil,
		[]AttachmentRef{{Key: "support"}},
	)
	require.EqualError(t, err, "skills can only be attached to agent-mode configs")
}
