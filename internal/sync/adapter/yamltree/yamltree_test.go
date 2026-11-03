package yamltree

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestScalarSpanHelpers(t *testing.T) {
	_, _, _, _, err := ScalarSpan(Region{}, &yaml.Node{}, false)
	require.Error(t, err)
	assert.Equal(t, `"line\nnext"`, quoteYAML("line\nnext", "'old'"))
	assert.Equal(t, "'it''s'", quoteYAML("it's", "'old'"))
	_, err = EncodeString("bad\x01", "old")
	require.Error(t, err)

	assert.Equal(t, len(`"unterminated`), yamlQuotedEnd([]byte(`"unterminated`)))
	assert.Equal(t, len(`'it''s'`), yamlQuotedEnd([]byte(`'it''s'`)))
	assert.Equal(t, "value, next", string(yamlPlainScalar([]byte("value, next # comment"), false)))
	assert.Equal(t, "value", string(yamlPlainScalar([]byte("value, next]"), true)))
}
