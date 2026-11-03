package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

func TestMarkdownRejectsUnsafeMarkersAndFrontMatter(t *testing.T) {
	inputs := []string{
		"<!-- launchdarkly:begin one -->\n<!-- launchdarkly:begin two -->",
		"<!-- launchdarkly:end one -->",
		"<!-- launchdarkly:begin one -->",
		"---\ninvalid: [\n---\nbody",
		"---\ndescription: value",
	}
	for _, input := range inputs {
		_, err := parse(adapter.Document{Path: "AGENTS.md", Data: []byte(input)}, false)
		require.Error(t, err, input)
	}

	root, err := parse(adapter.Document{Path: "AGENTS.md", Data: []byte("\ufeff# Instructions")}, false)
	require.NoError(t, err)
	assert.Equal(t, "# Instructions", root.Fields["body"].Value)
	assert.Empty(t, root.Fields["frontmatter"].Fields)
	assert.Empty(t, root.Fields["markers"].Items)
}

func TestMarkdownAndAgentSkillSafetyForms(t *testing.T) {
	selectPath, fieldMap := markdownAdapter{}.Defaults(syncdomain.KindTool)
	assert.Equal(t, "$", selectPath)
	assert.Nil(t, fieldMap)
	selectPath, fieldMap = agentSkillAdapter{}.Defaults(syncdomain.KindVariation)
	assert.Equal(t, "$", selectPath)
	assert.Nil(t, fieldMap)

	_, err := parse(adapter.Document{Path: "README.md", Data: []byte{0xff}}, false)
	require.Error(t, err)
	_, err = parseAgentSkill(adapter.Document{Path: "SKILL.md", Data: []byte{0xff}})
	require.Error(t, err)

	for name, input := range map[string]string{
		"missing front matter":  "# Body",
		"unclosed front matter": "---\ndescription: value",
		"invalid YAML":          "---\ndescription: [\n---\nBody",
		"front matter scalar":   "---\nvalue\n---\nBody",
		"missing description":   "---\nname: skill\n---\nBody",
		"description type":      "---\ndescription: [value]\n---\nBody",
		"empty body":            "---\ndescription: value\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseAgentSkill(adapter.Document{Path: "SKILL.md", Data: []byte(input)})
			require.Error(t, err)
		})
	}

	root, err := parseAgentSkill(adapter.Document{Path: "SKILL.md", Data: []byte("---\ndescription: |\n  long text\n---\nBody\n")})
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["frontmatter"].Fields["description"].Capability)

	headings := sections([]byte("    # indented\n````\n# hidden\n````\n## Visible ##\nbody\n"), 0)
	require.Len(t, headings, 1)
	assert.Equal(t, "Visible", headings[0].Fields["heading"].Value)

	exact := exactNode([]byte("body\n"), 0, 5)
	encoded, err := exact.Encode("next")
	require.NoError(t, err)
	assert.Equal(t, "next\n", string(encoded))

	region, bodyStart, err := splitFrontMatter([]byte("\ufeff---\r\ndescription: value\r\n---\r\n\r\nBody"))
	require.NoError(t, err)
	assert.NotEmpty(t, region.Data)
	assert.Greater(t, bodyStart, 0)
}
