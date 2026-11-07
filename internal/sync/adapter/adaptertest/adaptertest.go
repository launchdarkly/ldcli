// Package adaptertest holds the helpers that adapter tests share: bundles,
// fixtures, edit application with byte identity checks, and projection text
// access.
package adaptertest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
)

// Bundle returns a bundle with one primary document and no dependencies.
func Bundle(path string, data []byte) adapter.SourceBundle {
	return adapter.SourceBundle{Primary: adapter.Document{Path: path, Data: data}}
}

// Fixture reads one file below the testdata directory of the test package.
func Fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	require.NoError(t, err)
	return data
}

// Materialize writes fixture data to realPath in a temporary directory. Tests
// use it for files whose real name activates a coding tool, such as SKILL.md.
func Materialize(t *testing.T, data []byte, realPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), filepath.FromSlash(realPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// ApplyEdits applies edits to source from the highest offset to the lowest.
func ApplyEdits(source []byte, edits ...adapter.TextEdit) []byte {
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(left, right adapter.TextEdit) int { return right.Start - left.Start })
	result := bytes.Clone(source)
	for _, edit := range ordered {
		result = slices.Concat(result[:edit.Start], edit.Replacement, result[edit.End:])
	}
	return result
}

// ApplyChecked applies edits and proves that every byte outside the edits is
// unchanged and that each edit expected the bytes that it replaced.
func ApplyChecked(t *testing.T, source []byte, edits []adapter.TextEdit) []byte {
	t.Helper()
	for _, edit := range edits {
		require.Equal(t, edit.Expected, source[edit.Start:edit.End])
	}
	updated := ApplyEdits(source, edits...)
	ascending := slices.Clone(edits)
	slices.SortFunc(ascending, func(left, right adapter.TextEdit) int { return left.Start - right.Start })
	beforeOffset, afterOffset := 0, 0
	for _, edit := range ascending {
		length := edit.Start - beforeOffset
		require.Equal(t, source[beforeOffset:edit.Start], updated[afterOffset:afterOffset+length])
		beforeOffset = edit.End
		afterOffset += length + len(edit.Replacement)
	}
	require.Equal(t, source[beforeOffset:], updated[afterOffset:])
	return updated
}

// DirectEdits returns the edits of an update and fails if a field is not direct.
func DirectEdits(t *testing.T, result adapter.UpdateResult) []adapter.TextEdit {
	t.Helper()
	var edits []adapter.TextEdit
	for field, update := range result.Fields {
		require.Equal(t, adapter.CapabilityDirect, update.Capability, field)
		edits = append(edits, update.Edits...)
	}
	require.NotEmpty(t, edits)
	return edits
}

// PrimaryText returns the main text of a projection: the instructions of a
// variation, or the description of a tool or a skill.
func PrimaryText(t *testing.T, projection syncdomain.Projection) string {
	t.Helper()
	switch value := projection.(type) {
	case syncdomain.VariationProjection:
		require.NotNil(t, value.Instructions)
		return *value.Instructions
	case syncdomain.ToolProjection:
		require.NotNil(t, value.Description)
		return *value.Description
	case syncdomain.SkillProjection:
		require.NotNil(t, value.Description)
		return *value.Description
	default:
		t.Fatalf("unsupported projection %T", projection)
		return ""
	}
}

// WithText returns a copy of the projection with new main text.
func WithText(projection syncdomain.Projection, text string) syncdomain.Projection {
	switch value := projection.(type) {
	case syncdomain.VariationProjection:
		value.Instructions = &text
		value.Messages = nil
		return value
	case syncdomain.ToolProjection:
		value.Description = &text
		return value
	case syncdomain.SkillProjection:
		value.Description = &text
		return value
	default:
		return projection
	}
}

// ModelParameterTarget returns the map key of one model parameter.
func ModelParameterTarget(key string) string {
	encoded, _ := json.Marshal(key)
	return fmt.Sprintf("model.parameters[%s]", encoded)
}

// AssertJSONEqual compares two values by their JSON encoding.
func AssertJSONEqual(t *testing.T, expected, actual any) {
	t.Helper()
	expectedJSON, err := json.Marshal(expected)
	require.NoError(t, err)
	actualJSON, err := json.Marshal(actual)
	require.NoError(t, err)
	assert.JSONEq(t, string(expectedJSON), string(actualJSON))
}

// StringPointer returns a pointer to value.
func StringPointer(value string) *string { return &value }
