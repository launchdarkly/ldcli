package sync

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVariationParser_Accept(t *testing.T) {
	p := variationParser{}

	assert.True(t, p.Accept("my-config/my-variation.prompt"))
	assert.False(t, p.Accept("my-variation.prompt"))
	assert.False(t, p.Accept("my-config/nested/my-variation.prompt"))
	assert.False(t, p.Accept("my-config/my-variation.md"))
}

func TestVariationParser_UntaggedBodyIsSystemMessage(t *testing.T) {
	file := File{
		ProjectKey: "proj",
		RelPath:    "cfg/plain.prompt",
		Data: []byte(`---
formatVersion: 1
key: plain
name: Plain
---

Just say hello.
`),
	}

	resource, err := variationParser{}.Parse(file)
	require.NoError(t, err)

	var payload variationPayload
	require.NoError(t, unmarshalPayload(resource, &payload))
	require.Equal(t, []message{{Role: "system", Content: "Just say hello."}}, payload.Messages)
}

func TestVariationParser_MismatchedTags(t *testing.T) {
	file := File{
		ProjectKey: "proj",
		RelPath:    "cfg/bad.prompt",
		Data: []byte(`---
formatVersion: 1
key: bad
name: Bad
---

<system>
oops
</user>
`),
	}

	_, err := variationParser{}.Parse(file)
	require.ErrorContains(t, err, "unclosed <system> tag")
}

func TestVariationParser_TextOutsideTags(t *testing.T) {
	file := File{
		ProjectKey: "proj",
		RelPath:    "cfg/bad.prompt",
		Data: []byte(`---
formatVersion: 1
key: bad
name: Bad
---

hello
<user>
hi
</user>
`),
	}

	_, err := variationParser{}.Parse(file)
	require.ErrorContains(t, err, "unexpected text outside message tags")
}

func TestVariationParser_RequiresFormatVersion(t *testing.T) {
	file := File{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt",
		Data: []byte(`---
key: v
name: V
---
`),
	}

	_, err := variationParser{}.Parse(file)
	require.ErrorContains(t, err, "formatVersion is required")
}

func TestVariationParser_RejectsUnknownFrontMatter(t *testing.T) {
	file := File{
		ProjectKey: "proj",
		RelPath:    "cfg/v.prompt",
		Data: []byte(`---
formatVersion: 1
key: v
name: V
mystery: true
---
`),
	}

	_, err := variationParser{}.Parse(file)
	require.ErrorContains(t, err, "invalid front matter")
}

func unmarshalPayload(resource SyncedResource, dest any) error {
	return json.Unmarshal(resource.Payload, dest)
}
