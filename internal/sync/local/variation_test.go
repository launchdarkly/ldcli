package local

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestParseManagedPath(t *testing.T) {
	valid := map[string]syncdomain.ResourceID{
		".launchdarkly/project/configs/config/variation.prompt.md": syncdomain.VariationID("project", "config", "variation"),
		".launchdarkly/project/tools/search.json": {
			Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search",
		},
		".launchdarkly/project/skills/support.md": {
			Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "support",
		},
	}
	for file, expected := range valid {
		id, ok := ParseManagedPath(file)
		assert.True(t, ok, file)
		assert.Equal(t, expected, id, file)
	}

	for _, file := range []string{
		".launchdarkly/project/configs/variation.prompt.md",
		".launchdarkly/project/configs/config/nested/variation.prompt.md",
		".launchdarkly/project/configs/config/variation.prompt",
		".launchdarkly/project/configs/config/.prompt.md",
		".launchdarkly/project/tools/search.md",
		".launchdarkly/project/tools/nested/search.json",
		"other/project/configs/config/variation.prompt.md",
		".launchdarkly/manifest.json",
	} {
		_, ok := ParseManagedPath(file)
		assert.False(t, ok, file)
	}
}

func TestParseVariation_UntaggedBodyIsSystemMessage(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/plain.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: completion
key: plain
name: Plain
---

Just say hello.
`),
	}

	resource, err := parseVariation(file)
	require.NoError(t, err)

	payload := resource.Variation
	require.Equal(
		t,
		[]syncdomain.Message{{Role: "system", Content: "Just say hello."}},
		payload.Messages,
	)
}

func TestParseVariation_AgentBodyIsInstructions(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/agent.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: agent
key: agent
name: Agent
---

Use the available capabilities.

<system>This tag is part of the instructions.</system>
`),
	}

	resource, err := parseVariation(file)
	require.NoError(t, err)

	payload := resource.Variation
	assert.Equal(
		t,
		"Use the available capabilities.\n\n<system>This tag is part of the instructions.</system>",
		payload.Instructions,
	)
	assert.Empty(t, payload.Messages)
}

func TestParseVariation_ParsesBOMAndCRLF(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/plain.prompt.md",
		Data: []byte("\ufeff\r\n---\r\n" +
			"formatVersion: 1\r\n" +
			"mode: completion\r\n" +
			"key: plain\r\n" +
			"name: Plain\r\n" +
			"---\r\n\r\n" +
			"Just say hello.\r\n"),
	}

	resource, err := parseVariation(file)
	require.NoError(t, err)

	payload := resource.Variation
	require.Equal(
		t,
		[]syncdomain.Message{{Role: "system", Content: "Just say hello."}},
		payload.Messages,
	)
}

func TestParseVariation_RejectsBodyFieldsInFrontMatter(t *testing.T) {
	for _, field := range []string{
		"instructions: Use the available capabilities.",
		"messages: []",
	} {
		t.Run(field, func(t *testing.T) {
			file := localFile{
				ProjectKey: "proj",
				RelPath:    "cfg/agent.prompt.md",
				Data: []byte(`---
formatVersion: 1
mode: agent
key: agent
name: Agent
` + field + `
---
`),
			}

			_, err := parseVariation(file)
			require.ErrorContains(t, err, "invalid front matter")
		})
	}
}

func TestParseVariation_RejectsUnclosedFrontMatter(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: completion
key: v
name: V
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, "unclosed YAML front matter")
}

func TestParseVariation_MismatchedTags(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/bad.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: completion
key: bad
name: Bad
---

<system>
oops
</user>
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, "unclosed <system> tag")
}

func TestParseVariation_TextOutsideTags(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/bad.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: completion
key: bad
name: Bad
---

hello
<user>
hi
</user>
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, "unexpected text outside message tags")
}

func TestParseVariation_RequiresFormatVersion(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
mode: completion
key: v
name: V
---
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, "formatVersion is required")
}

func TestParseVariation_RequiresMode(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
formatVersion: 1
key: v
name: V
---
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, "mode is required")
}

func TestParseVariation_RejectsUnsupportedMode(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: other
key: v
name: V
---
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, `unsupported mode "other"`)
}

func TestParseVariation_RejectsVariationDescription(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: completion
key: v
name: V
description: Variation description
---
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, "invalid front matter")
}
