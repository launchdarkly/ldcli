package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestUnchangedReadOnlyToolFieldBlocksCompleteResource(t *testing.T) {
	id := ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	oldDescription, newDescription := "Old", "New"
	schema := map[string]any{"type": "object"}
	reference := &syncdomain.Reference{
		File: "tools.ts", Format: "typescript",
		Map: map[string]string{"description": "description", "schema": "inputSchema"},
	}
	server := syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "lookup", Description: &oldDescription, Schema: schema},
	}
	serverProjection, err := syncdomain.ProjectAttachment(server, *reference)
	require.NoError(t, err)
	baseline, err := syncdomain.FingerprintProjection(id, serverProjection)
	require.NoError(t, err)
	local := syncdomain.SyncedAttachment{
		ProjectKey: "project",
		Attachment: syncdomain.Attachment{
			Kind: syncdomain.AttachmentTool,
			Tool: &syncdomain.Tool{Key: "lookup", Description: &newDescription},
		},
		Source: reference,
		Projection: syncdomain.ToolProjection{
			Description: &newDescription,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldDescription: {Capability: syncdomain.CapabilityDirect, Known: true},
				syncdomain.FieldSchema: {
					Capability: syncdomain.CapabilityReadOnly, Reason: "helper call",
				},
			},
		},
	}

	resource := planSyncedAttachment(id, &local, &server, baseline, true)

	assert.Equal(t, ActionError, resource.Action)
	require.NotNil(t, resource.Problem)
	assert.Equal(t, syncdomain.ErrorFieldReadOnly, resource.Problem.Code)
	assert.Contains(t, resource.Problem.Message, `field "schema"`)
	assert.Equal(t, schema, *resource.LocalProjection.(syncdomain.ToolProjection).Schema)
	state, ok := syncdomain.ProjectionFieldState(resource.LocalProjection, syncdomain.FieldSchema)
	require.True(t, ok)
	assert.Equal(t, syncdomain.CapabilityReadOnly, state.Capability)
	assert.False(t, state.Known)

	local.Attachment.Tool.Description = &oldDescription
	local.Projection = syncdomain.ToolProjection{
		Description: &oldDescription,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldDescription: {Capability: syncdomain.CapabilityDirect, Known: true},
			syncdomain.FieldSchema: {
				Capability: syncdomain.CapabilityReadOnly, Reason: "helper call",
			},
		},
	}
	server.Tool.Description = &newDescription
	resource = planSyncedAttachment(id, &local, &server, baseline, true)
	assert.Equal(t, ActionError, resource.Action)
	require.NotNil(t, resource.Problem)
	assert.Equal(t, syncdomain.ErrorFieldReadOnly, resource.Problem.Code)
}

func TestDynamicPromptBlocksVariationPush(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	reference := &syncdomain.Reference{
		File: "agents.py", Format: "python",
		Map: map[string]string{"prompt": "instructions", "model": "model"},
	}
	oldModel, newModel, serverPrompt := "gpt-4", "gpt-5", "Server prompt"
	server := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default",
		Instructions: serverPrompt, Model: map[string]any{"modelName": oldModel},
	}
	serverProjection, err := syncdomain.ProjectVariation(server, *reference)
	require.NoError(t, err)
	baseline, err := syncdomain.FingerprintProjection(id, serverProjection)
	require.NoError(t, err)
	local := syncdomain.SyncedResource{
		Kind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey,
		Source: reference,
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default",
			Model: map[string]any{"modelName": newModel},
		},
		Projection: syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Model: &newModel,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldPrompt: {
					Capability: syncdomain.CapabilityReadOnly, Reason: "f-string",
				},
				syncdomain.FieldModel: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	}

	resource := planResource(
		id, &local, ServerResource{Variation: &server, ConfigMode: syncdomain.VariationModeAgent},
		baseline, true,
	)

	assert.Equal(t, ActionError, resource.Action)
	require.NotNil(t, resource.Problem)
	assert.Equal(t, syncdomain.ErrorFieldReadOnly, resource.Problem.Code)
	assert.Contains(t, resource.Problem.Message, `field "prompt"`)
}

func TestUnresolvedImportSuggestsExactLinkCommand(t *testing.T) {
	id := syncdomain.VariationID("acme", "support", "default")
	reference := &syncdomain.Reference{
		File: "app/agent.py", Format: "python", Map: map[string]string{"prompt": "instructions"},
	}
	server := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default",
		Instructions: "Server prompt",
	}
	local := syncdomain.SyncedResource{
		Kind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey, Source: reference,
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default",
		},
		Projection: syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldPrompt: {
					Capability: syncdomain.CapabilityReadOnly,
					Reason:     "import cannot be resolved", Code: syncdomain.ErrorImportUnresolved,
					File: "app/prompts.py",
				},
			},
		},
	}

	resource := planResource(
		id, &local, ServerResource{Variation: &server, ConfigMode: syncdomain.VariationModeAgent},
		"", false,
	)

	require.NotNil(t, resource.Problem)
	assert.Equal(t, syncdomain.ErrorImportUnresolved, resource.Problem.Code)
	assert.Equal(t,
		"Link the imported value directly: ldcli sync prompts link --file app/prompts.py --format python --project acme --config-key support --key default",
		resource.Problem.Suggestion,
	)
}
