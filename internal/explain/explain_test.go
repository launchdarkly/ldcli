package explain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/explain"
)

func TestDefaultRegistry_HasCuratedCoverage(t *testing.T) {
	r := explain.DefaultRegistry()
	assert.True(t, r.HasCurated([]string{"flags", "update"}))
	assert.True(t, r.HasCurated([]string{"flags", "list"}))
	assert.False(t, r.HasCurated([]string{"nope"}))
}

func TestRegistry_ResolveUnknownReturnsErrCommandNotFound(t *testing.T) {
	r := explain.NewRegistry()
	_, err := r.Resolve([]string{"some", "thing"})
	require.Error(t, err)
	assert.ErrorIs(t, err, explain.ErrCommandNotFound)
}

// TestFlagsUpdateSnapshot is a soft snapshot. It compares the JSON-rendered
// explanation to a checked-in golden file. Run with `-update` to refresh.
//
// We deliberately keep the assertion soft (key-by-key) rather than a strict
// byte-equality test: the structure must be stable for agents, but field
// reordering of nested examples is not a regression.
func TestFlagsUpdateSnapshot(t *testing.T) {
	r := explain.DefaultRegistry()
	exp, err := r.Resolve([]string{"flags", "update"})
	require.NoError(t, err)

	out, err := explain.RenderJSON(exp)
	require.NoError(t, err)

	goldenPath := filepath.Join("testdata", "flags_update.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
		require.NoError(t, os.WriteFile(goldenPath, []byte(out), 0o644))
		t.Logf("wrote golden file: %s", goldenPath)
		return
	}

	goldenBytes, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Skipf("golden file %s not present; run with UPDATE_GOLDEN=1 to create", goldenPath)
	}
	require.NoError(t, err)

	var got, want map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.NoError(t, json.Unmarshal(goldenBytes, &want))

	for _, key := range []string{"command", "summary", "httpMethod", "path", "operationId"} {
		assert.Equal(t, want[key], got[key], "top-level field %q drifted from golden", key)
	}
}
