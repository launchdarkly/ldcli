package reference

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestMarkdownProfilesPreserveActivationMetadata(t *testing.T) {
	tests := []struct {
		name, fixture, realPath, format, metadataPath string
		metadataValue                                 any
	}{
		{name: "Cursor", fixture: "profiles/cursor-rule.mdc", realPath: ".cursor/rules/support.mdc", format: "cursor", metadataPath: "frontmatter.globs", metadataValue: "src/support/**/*.ts"},
		{name: "Copilot", fixture: "profiles/copilot.instructions.md", realPath: ".github/instructions/support.instructions.md", format: "copilot", metadataPath: "frontmatter.applyTo", metadataValue: "src/support/**/*.ts"},
		{name: "Claude", fixture: "profiles/claude-instructions.md.golden", realPath: "CLAUDE.md", format: "claude-markdown", metadataPath: "frontmatter.model", metadataValue: "sonnet"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			documentPath, source := materializeFixture(t, test.fixture, test.realPath)
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{File: documentPath, Format: test.format}
			root, err := parseSourceDocument(Document{Path: documentPath, Data: source}, test.format)
			require.NoError(t, err)
			metadata, err := resolveSourcePath(root, test.metadataPath)
			require.NoError(t, err)
			assert.Equal(t, test.metadataValue, metadata.value)

			projection, err := ParseSource(sourceBundle(documentPath, source), id, syncdomain.VariationModeAgent, reference)
			require.NoError(t, err)
			prompt := "Updated profile instructions."
			result, err := UpdateSource(
				sourceBundle(documentPath, source), id, syncdomain.VariationModeAgent, reference,
				syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &prompt},
			)
			require.NoError(t, err)
			updated := applyConformanceEdits(t, source, directEdits(t, result))
			root, err = parseSourceDocument(Document{Path: documentPath, Data: updated}, test.format)
			require.NoError(t, err)
			metadata, err = resolveSourcePath(root, test.metadataPath)
			require.NoError(t, err)
			assert.Equal(t, test.metadataValue, metadata.value)
			assert.NotNil(t, projection)
		})
	}
}

func TestAgentsMarkdownSectionsAndMarkers(t *testing.T) {
	documentPath, source := materializeFixture(t, "profiles/agents-instructions.md.golden", "AGENTS.md")
	id := syncdomain.VariationID("project", "config", "default")
	tests := []struct {
		name, selectPath, mapPath, want string
	}{
		{name: "section", selectPath: `sections[heading="Support"]`, mapPath: "body", want: "Ask for an order ID.\n\n<!-- launchdarkly:begin refunds -->\nExplain the refund policy.\n<!-- launchdarkly:end refunds -->"},
		{name: "marker", selectPath: `markers[id="refunds"]`, mapPath: "body", want: "Explain the refund policy."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference := syncdomain.Reference{
				File: documentPath, Format: "agents-markdown", Select: test.selectPath,
				Map: map[string]string{"prompt": test.mapPath},
			}
			projection, err := ParseSource(
				sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
			)
			require.NoError(t, err)
			assert.Equal(t, test.want, *projection.(syncdomain.VariationProjection).Instructions)
			prompt := "Updated selected instructions."
			result, err := UpdateSource(
				sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
				syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &prompt},
			)
			require.NoError(t, err)
			updated := applyConformanceEdits(t, source, directEdits(t, result))
			assert.Contains(t, string(updated), "## Build\n\nRun `make test`.")
		})
	}
}
