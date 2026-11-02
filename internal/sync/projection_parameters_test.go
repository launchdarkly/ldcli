package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelParameterKeyPreservesExactKeys(t *testing.T) {
	tests := []struct {
		target string
		key    string
		valid  bool
	}{
		{target: `model.parameters["temperature"]`, key: "temperature", valid: true},
		{target: `model.parameters["top_k"]`, key: "top_k", valid: true},
		{target: `model.parameters["top_p"]`, key: "top_p", valid: true},
		{target: `model.parameters["max_tokens"]`, key: "max_tokens", valid: true},
		{target: `model.parameters["Provider.Future-Key"]`, key: "Provider.Future-Key", valid: true},
		{target: `model.parameters["Case\u0053ensitive"]`, key: "CaseSensitive", valid: true},
		{target: `model.parameters[temperature]`},
		{target: `model.parameters.temperature`},
		{target: `model.parameters["temperature"].nested`},
	}
	for _, test := range tests {
		t.Run(test.target, func(t *testing.T) {
			key, valid := ModelParameterKey(Field(test.target))
			assert.Equal(t, test.valid, valid)
			assert.Equal(t, test.key, key)
		})
	}
}

func TestProjectVariationPreservesJSONModelParameterValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value any
	}{
		{name: "temperature", key: "temperature", value: 0.2},
		{name: "top k", key: "top_k", value: 40.0},
		{name: "top p", key: "top_p", value: 0.9},
		{name: "max tokens", key: "max_tokens", value: 1024.0},
		{name: "future provider key", key: "Provider.Future-Key", value: "fast"},
		{name: "string", key: "response_style", value: "brief"},
		{name: "boolean", key: "reasoning", value: true},
		{name: "null", key: "seed", value: nil},
		{name: "nested object", key: "provider", value: map[string]any{"mode": "fast", "retry": true}},
		{name: "array", key: "stop", value: []any{"END", map[string]any{"token": 2.0}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := Field(`model.parameters["` + test.key + `"]`)
			variation := Variation{
				Mode:  VariationModeAgent,
				Model: map[string]any{"parameters": map[string]any{test.key: test.value}},
			}
			projection, err := ProjectVariation(variation, Reference{
				Map: map[string]string{string(target): "value"},
			})
			require.NoError(t, err)
			require.NotNil(t, projection.Parameters)
			assert.Equal(t, test.value, (*projection.Parameters)[test.key])
			value, present := ProjectionFieldValue(projection, target)
			assert.True(t, present)
			assert.Equal(t, test.value, value)
		})
	}
}

func TestApplyVariationProjectionUsesWholeOrIndividualParameterOwnership(t *testing.T) {
	base := Variation{
		Mode: VariationModeAgent,
		Model: map[string]any{"parameters": map[string]any{
			"temperature": 0.2,
			"top_k":       20.0,
			"keep":        true,
		}},
	}
	tests := []struct {
		name       string
		projection VariationProjection
		want       map[string]any
	}{
		{
			name: "individual mapping merges one key",
			projection: VariationProjection{
				Parameters:  &map[string]any{"top_k": 50.0},
				FieldStates: map[Field]FieldState{`model.parameters["top_k"]`: {Known: true}},
			},
			want: map[string]any{"temperature": 0.2, "top_k": 50.0, "keep": true},
		},
		{
			name: "individual mapping preserves null",
			projection: VariationProjection{
				Parameters:  &map[string]any{"top_p": nil},
				FieldStates: map[Field]FieldState{`model.parameters["top_p"]`: {Known: true}},
			},
			want: map[string]any{"temperature": 0.2, "top_k": 20.0, "top_p": nil, "keep": true},
		},
		{
			name: "whole mapping replaces canonical parameters",
			projection: VariationProjection{
				Parameters:  &map[string]any{"max_tokens": 512.0},
				FieldStates: map[Field]FieldState{FieldModelParameters: {Known: true}},
			},
			want: map[string]any{"max_tokens": 512.0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			applied := ApplyVariationProjection(base, test.projection)
			assert.Equal(t, test.want, applied.Model["parameters"])
		})
	}
}

func TestModelParameterProjectionChangeAndFingerprint(t *testing.T) {
	id := VariationID("project", "config", "default")
	target := Field(`model.parameters["temperature"]`)
	state := map[Field]FieldState{target: {Capability: CapabilityDirect, Known: true}}
	localParameters := map[string]any{"temperature": 0.2}
	serverParameters := map[string]any{"temperature": 0.7}
	local := VariationProjection{Mode: VariationModeAgent, Parameters: &localParameters, FieldStates: state}
	server := VariationProjection{Mode: VariationModeAgent, Parameters: &serverParameters, FieldStates: state}

	localFingerprint, err := FingerprintProjection(id, local)
	require.NoError(t, err)
	serverFingerprint, err := FingerprintProjection(id, server)
	require.NoError(t, err)
	assert.NotEqual(t, localFingerprint, serverFingerprint)

	changed, err := ChangedProjection(local, server)
	require.NoError(t, err)
	value, present := ProjectionFieldValue(changed, target)
	assert.True(t, present)
	assert.Equal(t, 0.7, value)
	assert.False(t, ProjectionIncludes(local, server))
	assert.True(t, ProjectionIncludes(server, changed))
}

func TestModelParameterProjectionExcludesExpandedTools(t *testing.T) {
	variation := Variation{
		Mode:  VariationModeAgent,
		Tools: []AttachmentRef{{Key: "search"}},
		Model: map[string]any{"parameters": map[string]any{
			"temperature": 0.2,
			"tools":       []any{map[string]any{"name": "search"}},
		}},
	}
	projection, err := ProjectVariation(variation, Reference{
		Map: map[string]string{string(FieldModelParameters): "parameters"},
	})
	require.NoError(t, err)
	require.NotNil(t, projection.Parameters)
	assert.Equal(t, map[string]any{"temperature": 0.2}, *projection.Parameters)

	parameters := map[string]any{
		"temperature": 0.3,
		"tools":       []any{map[string]any{"name": "search"}},
	}
	applied := ApplyVariationProjection(variation, VariationProjection{
		Parameters: &parameters,
		FieldStates: map[Field]FieldState{
			FieldModelParameters: {Known: true},
		},
	})
	assert.Equal(t, map[string]any{"temperature": 0.3}, applied.Model["parameters"])
}
