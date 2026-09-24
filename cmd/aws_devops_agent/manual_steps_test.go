package awsdevopsagent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyReaderReadsOneKeyAtATime(t *testing.T) {
	keys := newKeyReader(strings.NewReader("S\rx"))
	defer keys.close()

	assert.Equal(t, byte(skipKey), keys.next())
	assert.Equal(t, byte('\r'), keys.next())
	assert.Equal(t, byte('x'), keys.next())
	assert.Equal(t, byte(0), keys.next())
}

func TestKeyReaderReadsALineWithoutEchoing(t *testing.T) {
	keys := newKeyReader(strings.NewReader("api-xx\x7f1 \r"))
	defer keys.close()

	assert.Equal(t, "api-x1", keys.line())
}

func TestKeyReaderEchoesTheLineItReadsBack(t *testing.T) {
	keys := newKeyReader(strings.NewReader("owner/rx\x7fepo\r"))
	defer keys.close()

	var echoed strings.Builder

	assert.Equal(t, "owner/repo", keys.echoLine(&echoed))
	assert.Equal(t, "owner/rx\b \bepo", echoed.String())
}

func TestKeyReaderReadsAnEmptyLine(t *testing.T) {
	keys := newKeyReader(strings.NewReader("\r"))
	defer keys.close()

	assert.Empty(t, keys.line())
}
