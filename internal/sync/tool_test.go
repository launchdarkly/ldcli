package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolParser_Accept(t *testing.T) {
	p := toolParser{}

	assert.True(t, p.Accept("test-tool.v13.json"))
	assert.False(t, p.Accept("nested/test-tool.v13.json"))
	assert.False(t, p.Accept("test-tool.json"))
	assert.False(t, p.Accept("test-tool.v13.yaml"))
}

func TestToolParser_CanonicalizesSchema(t *testing.T) {
	file := File{
		ProjectKey: "proj",
		RelPath:    "search.v2.json",
		Data: []byte(`{
  "key": "search",
  "version": 2,
  "schema": { "b": 1, "a": 2 }
}`),
	}

	first, err := toolParser{}.Parse(file)
	require.NoError(t, err)

	second, err := toolParser{}.Parse(File{
		ProjectKey: "proj",
		RelPath:    "search.v2.json",
		Data:       []byte(`{"schema":{"a":2,"b":1},"version":2,"key":"search"}`),
	})
	require.NoError(t, err)

	assert.Equal(t, first.Fingerprint, second.Fingerprint)
	assert.JSONEq(t, `{"key":"search","version":2,"schema":{"a":2,"b":1}}`, string(first.Payload))
}

func TestToolParser_KeyMismatch(t *testing.T) {
	_, err := toolParser{}.Parse(File{
		ProjectKey: "proj",
		RelPath:    "search.v2.json",
		Data:       []byte(`{"key":"other","version":2,"schema":{}}`),
	})
	require.ErrorContains(t, err, `key "other" does not match filename "search"`)
}

func TestToolParser_VersionMismatch(t *testing.T) {
	_, err := toolParser{}.Parse(File{
		ProjectKey: "proj",
		RelPath:    "search.v2.json",
		Data:       []byte(`{"key":"search","version":3,"schema":{}}`),
	})
	require.ErrorContains(t, err, "version 3 does not match filename v2")
}

func TestToolParser_RejectsUnknownFields(t *testing.T) {
	_, err := toolParser{}.Parse(File{
		ProjectKey: "proj",
		RelPath:    "search.v2.json",
		Data:       []byte(`{"key":"search","version":2,"schema":{},"extra":true}`),
	})
	require.ErrorContains(t, err, "invalid tool file")
}

func TestToolParser_RequiresSchema(t *testing.T) {
	_, err := toolParser{}.Parse(File{
		ProjectKey: "proj",
		RelPath:    "search.v2.json",
		Data:       []byte(`{"key":"search","version":2}`),
	})
	require.ErrorContains(t, err, "schema is required")
}
