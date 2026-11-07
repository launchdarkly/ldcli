package prompt

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

type emptySettingsTransport struct{}

var _ resources.Client = emptySettingsTransport{}

func (emptySettingsTransport) MakeRequest(
	string, string, string, string, url.Values, []byte, bool,
) ([]byte, error) {
	return []byte(`{"items":[]}`), nil
}

func (emptySettingsTransport) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestEnforceDirectionUsesLaunchDarklySetting(t *testing.T) {
	local := testVariation("local")
	server := testVariation("server")
	source := &syncdomain.Reference{File: "agents.py", Format: "python"}

	codeCanonical := PlannedResource{
		ID: testResourceID(), Action: ActionUpdateLocal, Local: &local, Server: &server, Source: source,
	}
	enforceDirection(&codeCanonical, syncdomain.SyncDirectionCodeCanonical)
	assert.Equal(t, ActionUpdateServer, codeCanonical.Action)

	serverCanonical := PlannedResource{
		ID: testResourceID(), Action: ActionUpdateServer, Local: &local, Server: &server, Source: source,
	}
	enforceDirection(&serverCanonical, syncdomain.SyncDirectionServerCanonical)
	assert.Equal(t, ActionUpdateLocal, serverCanonical.Action)
}

func TestDeletedLinkedResourceRequiresDetach(t *testing.T) {
	resource := PlannedResource{
		ID: testResourceID(), Action: ActionDeleteLocal,
		Source: &syncdomain.Reference{File: "agents.py", Format: "python"},
	}

	enforceDirection(&resource, syncdomain.SyncDirectionBoth)

	assert.Equal(t, ActionError, resource.Action)
	require.NotNil(t, resource.Problem)
	assert.Equal(t, syncdomain.ErrorResourceDeleted, resource.Problem.Code)
	assert.Equal(t,
		"Run ldcli sync prompts detach --project production --config-key support --key default.",
		resource.Problem.Suggestion,
	)

	resource.Action, resource.Error, resource.Problem = ActionDeleteLocal, "", nil
	enforceDirection(&resource, syncdomain.SyncDirectionCodeCanonical)
	assert.Equal(t, ActionError, resource.Action)
	assert.Equal(t, syncdomain.ErrorResourceDeleted, resource.Problem.Code)

	toolID := ResourceID{Kind: syncdomain.KindTool, ProjectKey: "acme", LookupKey: "refund"}
	tool := PlannedResource{
		ID: toolID, Action: ActionDeleteLocal,
		Source: &syncdomain.Reference{File: "tools.json", Format: "json"},
	}
	enforceDirection(&tool, syncdomain.SyncDirectionCodeCanonical)
	assert.Equal(t,
		"Run ldcli sync tools detach --project acme --key refund.",
		tool.Problem.Suggestion,
	)
}

func TestMissingToolSettingIsActionable(t *testing.T) {
	id := ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	resources := []PlannedResource{{
		ID: id, Source: &syncdomain.Reference{File: "tools.json", Format: "json"},
	}}
	client := syncapi.NewClient(emptySettingsTransport{}, "token", "https://example.com")

	err := applySourceDirections(client, resources)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorSyncSettingUnavailable, problem.Code)
	assert.Equal(t, id.Label(), problem.Resource)
	assert.Contains(t, problem.Suggestion, "does not provide a tool sync setting")
}

func TestFormatOneBaselineWithoutOptionalMetadataRemainsReadable(t *testing.T) {
	resource := PlannedResource{
		ID:     testResourceID(),
		Action: ActionInSync,
		Source: &syncdomain.Reference{
			File: "agents.py", Format: "python", Select: `agents[id="support"]`,
			Map: map[string]string{"prompt": "instructions"},
		},
	}
	baseline := syncmanifest.Resource{
		ResourceKind: resource.ID.Kind,
		ProjectKey:   resource.ID.ProjectKey,
		LookupKey:    resource.ID.LookupKey,
		Fingerprint:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	checkSourceBaseline(&resource, baseline, true)

	assert.Equal(t, ActionInSync, resource.Action)
	assert.Nil(t, resource.Problem)
}

func TestRecordAttachmentStateKeepsProjectionFingerprint(t *testing.T) {
	root := t.TempDir()
	source := `import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool("lookup", {
  description: "Look up an order.",
  inputSchema: { type: "object" },
}, handler);
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "tools.ts"), []byte(source), 0o644))
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	reference := syncdomain.Reference{
		File: "tools.ts", Format: "typescript", Select: `tools[id="lookup"]`,
		Map: map[string]string{"description": "description"},
	}
	content, err := synclocal.RenderDescriptor(id, reference)
	require.NoError(t, err)
	descriptor, err := synclocal.ReferencePath(id)
	require.NoError(t, err)
	descriptor = filepath.Join(root, filepath.FromSlash(descriptor))
	require.NoError(t, os.MkdirAll(filepath.Dir(descriptor), 0o755))
	require.NoError(t, os.WriteFile(descriptor, content, 0o644))
	description := "Look up an order."
	attachment := syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{
			Key: "lookup", Description: &description, Schema: map[string]any{"type": "object"},
		},
	}
	manifest := syncmanifest.New()

	err = recordAttachmentState(&manifest, root, "project", []syncdomain.Attachment{attachment})

	require.NoError(t, err)
	require.Len(t, manifest.Resources, 1)
	assert.Equal(t, syncdomain.ProjectionFingerprintSchema, manifest.Resources[0].FingerprintSchema)
	full, err := syncdomain.FingerprintAttachment("project", attachment)
	require.NoError(t, err)
	assert.NotEqual(t, full, manifest.Resources[0].Fingerprint)
}
