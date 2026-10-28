package awsdevopsagent

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A file is not wrapped, so each prompt must leave the next lines unread.
func TestReadLineLeavesLaterLinesInAFile(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString("1\nmy-space\ny\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.Equal(t, "1", readLine(r))
	assert.Equal(t, "my-space", readLine(r))
	assert.Equal(t, "y", readLine(r))
}
