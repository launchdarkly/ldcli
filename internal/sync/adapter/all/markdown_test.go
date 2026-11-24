package all

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/adaptertest"
)

func TestMarkdownFrontMatterSurvivesBodyUpdates(t *testing.T) {
	tests := []struct {
		name, fixture, realPath, format, metadataPath string
		metadataValue                                 any
	}{
		{name: "Cursor", fixture: "profiles/cursor-rule.mdc", realPath: ".cursor/rules/support.mdc", format: "markdown", metadataPath: "frontmatter.globs", metadataValue: "src/support/**/*.ts"},
		{name: "Copilot", fixture: "profiles/copilot.instructions.md", realPath: ".github/instructions/support.instructions.md", format: "markdown", metadataPath: "frontmatter.applyTo", metadataValue: "src/support/**/*.ts"},
		{name: "Claude", fixture: "profiles/claude-instructions.md.golden", realPath: "CLAUDE.md", format: "markdown", metadataPath: "frontmatter.model", metadataValue: "sonnet"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			documentPath, source := materializeFixture(t, test.fixture, test.realPath)
			id := syncdomain.VariationID("project", "config", "default")
			reference := syncdomain.Reference{File: documentPath, Format: test.format}
			root, err := adapter.ParseDocument(adapter.Document{Path: documentPath, Data: source}, test.format)
			require.NoError(t, err)
			metadata, err := root.Resolve(test.metadataPath)
			require.NoError(t, err)
			assert.Equal(t, test.metadataValue, metadata.Value)

			projection, err := adapter.ParseSource(adaptertest.Bundle(documentPath, source), id, reference)
			require.NoError(t, err)
			prompt := "Updated profile instructions."
			result, err := adapter.UpdateSource(
				adaptertest.Bundle(documentPath, source), id, reference,
				syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &prompt},
			)
			require.NoError(t, err)
			updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
			root, err = adapter.ParseDocument(adapter.Document{Path: documentPath, Data: updated}, test.format)
			require.NoError(t, err)
			metadata, err = root.Resolve(test.metadataPath)
			require.NoError(t, err)
			assert.Equal(t, test.metadataValue, metadata.Value)
			assert.NotNil(t, projection)
		})
	}
}

func TestMarkdownSectionsAndMarkers(t *testing.T) {
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
				File: documentPath, Format: "markdown", Select: test.selectPath,
				Map: map[string]string{"prompt": test.mapPath},
			}
			projection, err := adapter.ParseSource(
				adaptertest.Bundle(reference.File, source), id, reference,
			)
			require.NoError(t, err)
			assert.Equal(t, test.want, *projection.(syncdomain.VariationProjection).Instructions)
			prompt := "Updated selected instructions."
			result, err := adapter.UpdateSource(
				adaptertest.Bundle(reference.File, source), id, reference,
				syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &prompt},
			)
			require.NoError(t, err)
			updated := adaptertest.ApplyChecked(t, source, adaptertest.DirectEdits(t, result))
			assert.Contains(t, string(updated), "## Build\n\nRun `make test`.")
		})
	}
}

func TestMarkdownSectionUpdatePreservesOtherBytes(t *testing.T) {
	source := []byte("# Guide\r\n\r\n## Support\r\n\r\nHelp with orders.\r\n\r\n## Build\r\n\r\nRun make.\r\n")
	id := syncdomain.VariationID("project", "support", "default")
	reference := syncdomain.Reference{
		File: "AGENTS.md", Format: "markdown", Select: `sections[heading="Support"]`,
		Map: map[string]string{"prompt": "body"},
	}
	projection, err := adapter.ParseSource(adaptertest.Bundle("AGENTS.md", source), id, reference)
	require.NoError(t, err)
	assert.Equal(t, "Help with orders.", *projection.(syncdomain.VariationProjection).Instructions)

	next := "Help with orders and refunds."
	update, err := adapter.UpdateSource(
		adaptertest.Bundle("AGENTS.md", source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: &next},
	)
	require.NoError(t, err)
	edit := update.Fields[syncdomain.FieldPrompt].Edits[0]
	updated := adaptertest.ApplyEdits(source, edit)
	assert.Equal(t, "# Guide\r\n\r\n## Support\r\n\r\nHelp with orders and refunds.\r\n\r\n## Build\r\n\r\nRun make.\r\n", string(updated))
}

func TestMarkdownSectionsIgnoreCodeAndKeepHeadingHashes(t *testing.T) {
	source := []byte("    ## Not a heading\n\n````\n## Fenced\n````\n\n## C#\n\nLanguage guidance.\n")
	id := syncdomain.VariationID("project", "guide", "default")
	reference := syncdomain.Reference{
		File: "AGENTS.md", Format: "markdown", Select: `sections[heading="C#"]`,
		Map: map[string]string{"prompt": "body"},
	}

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)

	require.NoError(t, err)
	assert.Equal(t, "Language guidance.", *projection.(syncdomain.VariationProjection).Instructions)
}

func TestMarkdownRepresentsOneCompletionSystemMessage(t *testing.T) {
	source := []byte("Current system prompt.\n")
	id := syncdomain.VariationID("project", "completion", "default")
	reference := syncdomain.Reference{File: "prompt.md", Format: "markdown"}

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
	require.NoError(t, err)
	completion, err := projection.(syncdomain.VariationProjection).ForMode(syncdomain.VariationModeCompletion)
	require.NoError(t, err)
	require.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Current system prompt."}}, *completion.Messages)

	desired := []syncdomain.Message{{Role: syncdomain.RoleSystem, Content: "Updated system prompt."}}
	update, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeCompletion, Messages: &desired},
	)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityDirect, update.Fields[syncdomain.FieldPrompt].Capability)
}

func TestAgentSkillMapsDescriptionAndBody(t *testing.T) {
	source := []byte("---\nname: refunds\ndescription: Explain refunds.\nlicense: Apache-2.0\n---\n\n# Refunds\n\nAsk for the order ID.\n")
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}
	reference := syncdomain.Reference{File: ".agents/skills/refunds/SKILL.md", Format: "agent-skill"}

	projection, err := adapter.ParseSource(adaptertest.Bundle(reference.File, source), id, reference)
	require.NoError(t, err)
	skill := projection.(syncdomain.SkillProjection)
	assert.Equal(t, "Explain refunds.", *skill.Description)
	assert.Equal(t, "# Refunds\n\nAsk for the order ID.\n", *skill.Markdown)

	description := "Explain refunds and escalation."
	markdown := "# Refunds\n\nEscalate large refunds.\n"
	update, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.SkillProjection{Description: &description, Markdown: &markdown},
	)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityDirect, update.Fields[syncdomain.FieldDescription].Capability)
	assert.Equal(t, adapter.CapabilityDirect, update.Fields[syncdomain.FieldMarkdown].Capability)
}

func TestAgentSkillDescriptionUpdatePreservesInlineComment(t *testing.T) {
	source := []byte("---\nname: refunds\ndescription: \"Explain refunds.\" # catalog text\n---\n\n# Refunds\n")
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}
	reference := syncdomain.Reference{File: "SKILL.md", Format: "agent-skill"}
	description := "Explain refunds and escalation."

	update, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, source), id, reference,
		syncdomain.SkillProjection{Description: &description},
	)

	require.NoError(t, err)
	updated := adaptertest.ApplyEdits(source, update.Fields[syncdomain.FieldDescription].Edits...)
	assert.Contains(t, string(updated), `description: "Explain refunds and escalation." # catalog text`)
}

func TestAgentSkillRejectsInvalidFiles(t *testing.T) {
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}
	reference := syncdomain.Reference{File: "SKILL.md", Format: "agent-skill"}
	tests := []struct {
		name, source, message string
	}{
		{name: "missing front matter", source: "# Skill\n", message: "missing YAML front matter"},
		{name: "unclosed front matter", source: "---\ndescription: Help\n", message: "unclosed YAML front matter"},
		{name: "missing description", source: "---\nname: refunds\n---\n\n# Skill\n", message: "path_not_found"},
		{name: "missing body", source: "---\ndescription: Help\n---\n", message: "skill Markdown is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.ParseSource(
				adaptertest.Bundle(reference.File, []byte(test.source)), id, reference,
			)
			require.Error(t, err)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			if test.message == "path_not_found" {
				assert.Equal(t, syncdomain.ErrorPathNotFound, problem.Code)
			} else {
				assert.Equal(t, syncdomain.ErrorInvalidSource, problem.Code)
				assert.Contains(t, err.Error(), test.message)
			}
		})
	}
}
