package reference

import (
	"bytes"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestRegisteredFormatsReadAndWriteRealisticSources(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		realPath   string
		format     string
		id         syncdomain.ResourceID
		selectPath string
		fieldMap   map[string]string
		want       string
		update     string
	}{
		{
			name: "Markdown section", fixture: "markdown/agents-instructions.md.golden", format: "markdown",
			id:         syncdomain.VariationID("project", "support", "default"),
			selectPath: `sections[heading="Support agent"]`, fieldMap: map[string]string{"prompt": "body"},
			want: "Ask for an order ID before discussing an order.", update: "Ask for an order ID and region.",
		},
		{
			name: "Agent Skill", fixture: "agent-skill/refunds/skill-instructions.md.golden",
			realPath: ".agents/skills/refunds/SKILL.md", format: "agent-skill",
			id:   syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"},
			want: "Explain the refund policy and escalation rules.", update: "Explain refunds and escalation rules.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := fixtureFile(t, test.fixture)
			documentPath := test.fixture
			if test.realPath != "" {
				documentPath, source = materializeFixture(t, test.fixture, test.realPath)
			}
			reference := syncdomain.Reference{
				File: documentPath, Format: test.format, Select: test.selectPath, Map: test.fieldMap,
			}
			projection, err := ParseSource(sourceBundle(documentPath, source), test.id, syncdomain.VariationModeAgent, reference)
			require.NoError(t, err)
			assert.Equal(t, test.want, primaryText(t, projection))

			desired := projectionWithText(projection, test.update)
			result, err := UpdateSource(sourceBundle(documentPath, source), test.id, syncdomain.VariationModeAgent, reference, desired)
			require.NoError(t, err)
			edits := directEdits(t, result)
			updated := applyConformanceEdits(t, source, edits)
			reparsed, err := ParseSource(sourceBundle(documentPath, updated), test.id, syncdomain.VariationModeAgent, reference)
			require.NoError(t, err)
			assert.Equal(t, test.update, primaryText(t, reparsed))
		})
	}
}

func sourceBundle(path string, data []byte) SourceBundle {
	return SourceBundle{Primary: Document{Path: path, Data: data}}
}

func primaryText(t *testing.T, projection syncdomain.Projection) string {
	t.Helper()
	switch value := projection.(type) {
	case syncdomain.VariationProjection:
		require.NotNil(t, value.Instructions)
		return *value.Instructions
	case syncdomain.ToolProjection:
		require.NotNil(t, value.Description)
		return *value.Description
	case syncdomain.SkillProjection:
		require.NotNil(t, value.Description)
		return *value.Description
	default:
		t.Fatalf("unsupported projection %T", projection)
		return ""
	}
}

func projectionWithText(projection syncdomain.Projection, text string) syncdomain.Projection {
	switch value := projection.(type) {
	case syncdomain.VariationProjection:
		value.Instructions = &text
		value.Messages = nil
		return value
	case syncdomain.ToolProjection:
		value.Description = &text
		return value
	case syncdomain.SkillProjection:
		value.Description = &text
		return value
	default:
		return projection
	}
}

func directEdits(t *testing.T, result UpdateResult) []TextEdit {
	t.Helper()
	var edits []TextEdit
	for field, update := range result.Fields {
		require.Equal(t, CapabilityDirect, update.Capability, field)
		edits = append(edits, update.Edits...)
	}
	require.NotEmpty(t, edits)
	return edits
}

func applyConformanceEdits(t *testing.T, source []byte, edits []TextEdit) []byte {
	t.Helper()
	slices.SortFunc(edits, func(left, right TextEdit) int { return right.Start - left.Start })
	updated := bytes.Clone(source)
	for _, edit := range edits {
		require.Equal(t, edit.Expected, source[edit.Start:edit.End])
		updated = append(append(append([]byte{}, updated[:edit.Start]...), edit.Replacement...), updated[edit.End:]...)
	}
	ascending := slices.Clone(edits)
	slices.SortFunc(ascending, func(left, right TextEdit) int { return left.Start - right.Start })
	beforeOffset, afterOffset := 0, 0
	for _, edit := range ascending {
		length := edit.Start - beforeOffset
		require.Equal(t, source[beforeOffset:edit.Start], updated[afterOffset:afterOffset+length])
		beforeOffset = edit.End
		afterOffset += length + len(edit.Replacement)
	}
	require.Equal(t, source[beforeOffset:], updated[afterOffset:])
	return updated
}
