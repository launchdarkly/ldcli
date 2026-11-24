package local

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestDeleteDefinitionsKeepsExternalSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	writeDescriptorForTest(t, root, id, Reference{File: "prompt.md", Format: "markdown"})

	paths, err := NewStore(root).DeleteDefinitions([]syncdomain.ResourceID{id})

	require.NoError(t, err)
	assert.Equal(t, []string{"project/configs/support/default.ref.yaml"}, paths)
	require.FileExists(t, filepath.Join(root, "prompt.md"))
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "configs", "support", "default.ref.yaml"))
}
