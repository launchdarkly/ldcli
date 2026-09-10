package local

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestVariationParser_Accept(t *testing.T) {
	parser := variationParser{}

	assert.True(t, parser.accept("my-config/my-variation.prompt.md"))
	assert.False(t, parser.accept("my-variation.prompt.md"))
	assert.False(t, parser.accept("my-config/nested/my-variation.prompt.md"))
	assert.False(t, parser.accept("my-config/my-variation.prompt"))
	assert.False(t, parser.accept("my-config/my-variation.md"))
}

func TestVariationParser_UntaggedBodyIsSystemMessage(t *testing.T) {
	file := file{
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

	resource, err := variationParser{}.parse(file)
	require.NoError(t, err)

	var payload syncdomain.Variation
	require.NoError(t, unmarshalPayload(resource, &payload))
	require.Equal(
		t,
		[]syncdomain.Message{{Role: "system", Content: "Just say hello."}},
		payload.Messages,
	)
}

func TestVariationParser_AgentBodyIsInstructions(t *testing.T) {
	file := file{
		ProjectKey: "proj",
		RelPath:    "cfg/agent.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: agent
key: agent
name: Agent
---

Use the available tools.

<system>This tag is part of the instructions.</system>
`),
	}

	resource, err := variationParser{}.parse(file)
	require.NoError(t, err)

	var payload syncdomain.Variation
	require.NoError(t, unmarshalPayload(resource, &payload))
	assert.Equal(
		t,
		"Use the available tools.\n\n<system>This tag is part of the instructions.</system>",
		payload.Instructions,
	)
	assert.Empty(t, payload.Messages)
}

func TestVariationParser_RejectsInstructionsInFrontMatter(t *testing.T) {
	file := file{
		ProjectKey: "proj",
		RelPath:    "cfg/agent.prompt.md",
		Data: []byte(`---
formatVersion: 1
mode: agent
key: agent
name: Agent
instructions: Use the available tools.
---
`),
	}

	_, err := variationParser{}.parse(file)
	require.ErrorContains(t, err, "field instructions not found")
}

func TestVariationParser_MismatchedTags(t *testing.T) {
	file := file{
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

	_, err := variationParser{}.parse(file)
	require.ErrorContains(t, err, "unclosed <system> tag")
}

func TestVariationParser_TextOutsideTags(t *testing.T) {
	file := file{
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

	_, err := variationParser{}.parse(file)
	require.ErrorContains(t, err, "unexpected text outside message tags")
}

func TestVariationParser_RequiresFormatVersion(t *testing.T) {
	file := file{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
key: v
name: V
---
`),
	}

	_, err := variationParser{}.parse(file)
	require.ErrorContains(t, err, "formatVersion is required")
}

func TestVariationParser_RequiresMode(t *testing.T) {
	file := file{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt.md",
		Data: []byte(`---
formatVersion: 1
key: v
name: V
---
`),
	}

	_, err := variationParser{}.parse(file)
	require.ErrorContains(t, err, "mode is required")
}

func TestVariationParser_RejectsUnsupportedMode(t *testing.T) {
	file := file{
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

	_, err := variationParser{}.parse(file)
	require.ErrorContains(t, err, `unsupported mode "other"`)
}

func TestVariationParser_RejectsUnknownFrontMatter(t *testing.T) {
	file := file{
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

	_, err := variationParser{}.parse(file)
	require.ErrorContains(t, err, "invalid front matter")
}

func unmarshalPayload(resource syncdomain.SyncedResource, destination any) error {
	return json.Unmarshal(resource.Payload, destination)
}
