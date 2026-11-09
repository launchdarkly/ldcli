package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectionFingerprintIgnoresLineEndingsAndSurroundingSpace(t *testing.T) {
	id := VariationID("project", "support", "default")
	reference := Reference{File: "prompt.md", Format: "markdown", Map: map[string]string{string(FieldPrompt): "body"}}
	clean := Variation{Mode: VariationModeAgent, Key: "default", Name: "Default", Instructions: "Ask for an order ID.\nKeep it short."}
	noisy := clean
	noisy.Instructions = "\n\nAsk for an order ID.\r\nKeep it short.  \n"

	left, err := ProjectVariation(clean, reference)
	require.NoError(t, err)
	right, err := ProjectVariation(noisy, reference)
	require.NoError(t, err)
	leftFingerprint, err := FingerprintProjection(id, left)
	require.NoError(t, err)
	rightFingerprint, err := FingerprintProjection(id, right)
	require.NoError(t, err)
	assert.Equal(t, leftFingerprint, rightFingerprint)

	messages := VariationProjection{Mode: VariationModeCompletion}
	set, err := messages.set(FieldMessages, []Message{{Role: RoleSystem, Content: "Hello\r\n"}}, directState)
	require.NoError(t, err)
	assert.Equal(t, "Hello", (*set.(VariationProjection).Messages)[0].Content)
}

func TestForModeKeepsEmptyPromptsEmpty(t *testing.T) {
	empty := ""
	agent := VariationProjection{Mode: VariationModeAgent, Instructions: &empty}
	completion, err := agent.ForMode(VariationModeCompletion)
	require.NoError(t, err)
	assert.Empty(t, *completion.Messages)

	back, err := completion.ForMode(VariationModeAgent)
	require.NoError(t, err)
	assert.Equal(t, "", *back.Instructions)

	user := VariationProjection{Mode: VariationModeCompletion, Messages: &[]Message{{Role: RoleUser, Content: "Hi"}}}
	_, err = user.ForMode(VariationModeAgent)
	require.Error(t, err)
}

func TestProjectionsMarkAbsentServerValuesAsUnknown(t *testing.T) {
	reference := Reference{File: "agent.py", Format: "python", Map: map[string]string{
		string(FieldPrompt): "instructions", string(FieldModel): "model", `model.parameters["temperature"]`: "temperature",
	}}
	projection, err := ProjectVariation(Variation{Mode: VariationModeAgent, Key: "default", Name: "Default"}, reference)
	require.NoError(t, err)
	assert.True(t, projection.States()[FieldPrompt].Known)
	assert.False(t, projection.States()[FieldModel].Known)
	assert.False(t, projection.States()[Field(`model.parameters["temperature"]`)].Known)

	tool := Attachment{Kind: AttachmentTool, Tool: &Tool{Key: "search", Schema: map[string]any{"type": "object"}}}
	attachment, err := ProjectAttachment(tool, Reference{File: "tools.json", Format: "json", Map: map[string]string{
		string(FieldDescription): "description", string(FieldSchema): "schema",
	}})
	require.NoError(t, err)
	assert.False(t, attachment.States()[FieldDescription].Known)
	assert.True(t, attachment.States()[FieldSchema].Known)
}
