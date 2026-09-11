package local

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestIsVariationFile(t *testing.T) {
	assert.True(t, isVariationFile("my-config/my-variation.prompt.md"))
	assert.False(t, isVariationFile("my-variation.prompt.md"))
	assert.False(t, isVariationFile("my-config/nested/my-variation.prompt.md"))
	assert.False(t, isVariationFile("my-config/my-variation.prompt"))
	assert.False(t, isVariationFile("my-config/my-variation.md"))
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

	var payload syncdomain.Variation
	require.NoError(t, unmarshalPayload(resource, &payload))
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

	var payload syncdomain.Variation
	require.NoError(t, unmarshalPayload(resource, &payload))
	assert.Equal(
		t,
		"Use the available capabilities.\n\n<system>This tag is part of the instructions.</system>",
		payload.Instructions,
	)
	assert.Empty(t, payload.Messages)
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

func TestParseVariation_RejectsUnknownFrontMatter(t *testing.T) {
	file := localFile{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: completion
key: v
name: V
mystery: true
---
`),
	}

	_, err := parseVariation(file)
	require.ErrorContains(t, err, "invalid front matter")
}

func unmarshalPayload(resource syncdomain.SyncedResource, destination any) error {
	return json.Unmarshal(resource.Payload, destination)
}
