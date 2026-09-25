package console

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriter(t *testing.T) {
	var output bytes.Buffer
	writer := New(&output)

	require.NoError(t, writer.Write("Loading"))
	require.NoError(t, writer.Printf(" %d", 2))
	require.NoError(t, writer.Line(" pages"))
	require.NoError(t, writer.WriteBytes([]byte("Done\n")))

	assert.Equal(t, "Loading 2 pages\nDone\n", output.String())
}
