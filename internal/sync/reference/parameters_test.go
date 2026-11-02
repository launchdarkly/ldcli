package reference

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestJSONIndividualModelParameterMappingsRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		source      any
		replacement any
	}{
		{name: "temperature", key: "temperature", source: 0.2, replacement: 0.7},
		{name: "top k", key: "top_k", source: 20, replacement: 40},
		{name: "top p", key: "top_p", source: 0.9, replacement: 0.8},
		{name: "max tokens", key: "max_tokens", source: 256, replacement: 512},
		{name: "future provider key", key: "Provider.Future-Key", source: "v1", replacement: "v2"},
		{name: "string", key: "response_style", source: "brief", replacement: "detailed"},
		{name: "boolean", key: "reasoning", source: true, replacement: false},
		{name: "null", key: "seed", source: nil, replacement: true},
		{
			name: "nested object", key: "provider",
			source:      map[string]any{"mode": "fast"},
			replacement: map[string]any{"mode": "careful", "retry": true},
		},
		{
			name: "array", key: "stop",
			source:      []any{"END"},
			replacement: []any{"DONE", map[string]any{"token": 2}},
		},
	}
	id := syncdomain.VariationID("project", "config", "default")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := json.Marshal(map[string]any{"prompt": "Help", "value": test.source})
			require.NoError(t, err)
			target := modelParameterTarget(test.key)
			reference := syncdomain.Reference{
				File: "agent.json", Format: "json",
				Map: map[string]string{"prompt": "prompt", target: "value"},
			}

			projection, err := ParseSource(
				sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
			)
			require.NoError(t, err)
			value, present := syncdomain.ProjectionFieldValue(projection, syncdomain.Field(target))
			assert.True(t, present)
			assertJSONValueEqual(t, test.source, value)

			parameters := map[string]any{test.key: test.replacement}
			update, err := UpdateSource(
				sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
				syncdomain.VariationProjection{
					Mode: syncdomain.VariationModeAgent, Parameters: &parameters,
					FieldStates: map[syncdomain.Field]syncdomain.FieldState{
						syncdomain.Field(target): {Capability: syncdomain.CapabilityDirect, Known: true},
					},
				},
			)
			require.NoError(t, err)
			require.Len(t, update.Fields[syncdomain.Field(target)].Edits, 1)
			updated := applyParameterEdits(source, update.Fields[syncdomain.Field(target)].Edits...)
			reparsed, err := ParseSource(
				sourceBundle(reference.File, updated), id, syncdomain.VariationModeAgent, reference,
			)
			require.NoError(t, err)
			value, present = syncdomain.ProjectionFieldValue(reparsed, syncdomain.Field(target))
			assert.True(t, present)
			assertJSONValueEqual(t, test.replacement, value)
		})
	}
}

func TestJSONWholeModelParametersOwnsCanonicalMap(t *testing.T) {
	source := []byte(`{"prompt":"Help","parameters":{"temperature":0.2,"tools":[{"name":"search"}]}}`)
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "agent.json", Format: "json",
		Map: map[string]string{"prompt": "prompt", "model.parameters": "parameters"},
	}
	projection, err := ParseSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
	)
	require.NoError(t, err)
	parameters := projection.(syncdomain.VariationProjection).Parameters
	require.NotNil(t, parameters)
	assert.Equal(t, map[string]any{"temperature": 0.2}, *parameters)

	replacement := map[string]any{
		"top_k":  30,
		"nested": map[string]any{"enabled": true},
		"stop":   []any{"END"},
		"seed":   nil,
	}
	update, err := UpdateSource(
		sourceBundle(reference.File, source), id, syncdomain.VariationModeAgent, reference,
		syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Parameters: &replacement,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldModelParameters: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, update.Fields[syncdomain.FieldModelParameters].Edits, 1)
	updated := applyParameterEdits(source, update.Fields[syncdomain.FieldModelParameters].Edits...)
	assert.JSONEq(t, `{"prompt":"Help","parameters":{"top_k":30,"nested":{"enabled":true},"stop":["END"],"seed":null}}`, string(updated))
}

func TestNormalizeReferenceRejectsOverlappingOrInvalidParameterTargets(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	tests := []struct {
		name    string
		targets map[string]string
		code    syncdomain.ErrorCode
		message string
	}{
		{
			name: "whole and child overlap",
			targets: map[string]string{
				"prompt": "prompt", "model.parameters": "parameters",
				`model.parameters["temperature"]`: "temperature",
			},
			code: syncdomain.ErrorInvalidReference, message: "overlaps",
		},
		{
			name: "same exact key through JSON escape",
			targets: map[string]string{
				"prompt": "prompt", `model.parameters["top_k"]`: "top_k",
				`model.parameters["top\u005fk"]`: "other",
			},
			code: syncdomain.ErrorInvalidReference, message: "same model parameter",
		},
		{
			name: "expanded tools child",
			targets: map[string]string{
				"prompt": "prompt", `model.parameters["tools"]`: "tools",
			},
			code: syncdomain.ErrorInvalidReference, message: "duplicates the tools field",
		},
		{
			name: "child needs exact bracket key",
			targets: map[string]string{
				"prompt": "prompt", "model.parameters.temperature": "temperature",
			},
			code: syncdomain.ErrorInvalidReference, message: "unsupported mapped field",
		},
		{
			name: "target uses shared path syntax",
			targets: map[string]string{
				"prompt": "prompt", `model.parameters["temperature"`: "temperature",
			},
			code: syncdomain.ErrorInvalidPathSyntax, message: "invalid path",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeReference(id, syncdomain.Reference{
				File: "agent.json", Format: "json", Map: test.targets,
			})
			require.Error(t, err)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			assert.Equal(t, test.code, problem.Code)
			assert.ErrorContains(t, err, test.message)
		})
	}
}

func modelParameterTarget(key string) string {
	encoded, _ := json.Marshal(key)
	return fmt.Sprintf("model.parameters[%s]", encoded)
}

func assertJSONValueEqual(t *testing.T, expected, actual any) {
	t.Helper()
	expectedJSON, err := json.Marshal(expected)
	require.NoError(t, err)
	actualJSON, err := json.Marshal(actual)
	require.NoError(t, err)
	assert.JSONEq(t, string(expectedJSON), string(actualJSON))
}

func applyParameterEdits(source []byte, edits ...TextEdit) []byte {
	updated := append([]byte(nil), source...)
	for _, edit := range edits {
		next := make([]byte, 0, len(updated)-edit.End+edit.Start+len(edit.Replacement))
		next = append(next, updated[:edit.Start]...)
		next = append(next, edit.Replacement...)
		next = append(next, updated[edit.End:]...)
		updated = next
	}
	return updated
}
