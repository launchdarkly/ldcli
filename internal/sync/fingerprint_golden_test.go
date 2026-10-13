package sync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// LaunchDarkly stores these fingerprints in remote manifests. If one of these
// values changes, every existing workspace reports drift after an upgrade.
func TestFingerprintsAreStableAcrossReleases(t *testing.T) {
	description := "Search documentation"
	tool := Attachment{
		Kind: AttachmentTool, Version: 4,
		Tool: &Tool{
			Key: "search", Description: &description,
			Schema: map[string]any{"type": "object"}, Tags: []string{"b", "a"},
		},
	}
	skill := Attachment{
		Kind: AttachmentSkill, Version: 2,
		Skill: &Skill{Key: "support", Name: "Support", Description: "Guidance", Markdown: "# Support\n"},
	}
	agent := Variation{
		Mode: VariationModeAgent, Key: "default", Name: "Default",
		Instructions:   "  Help the user.\r\n",
		ModelConfigKey: "claude", ModelConfigVersion: 3,
		Model:        map[string]any{"modelName": "claude", "parameters": map[string]any{}},
		OutputFormat: map[string]any{"type": "json"},
		Tools:        []AttachmentRef{{Key: "search", Version: 4}},
		Skills:       []AttachmentRef{{Key: "support", Version: 2}},
		Attachments:  []Attachment{skill, tool},
	}
	completion := Variation{
		Mode: VariationModeCompletion, Key: "chat", Name: "Chat",
		Messages: []Message{{Role: "system", Content: "Be concise."}, {Role: "user", Content: "Hi"}},
	}

	agentFingerprint, err := FingerprintVariation("project", "config/default", agent)
	require.NoError(t, err)
	completionFingerprint, err := FingerprintVariation("project", "config/chat", completion)
	require.NoError(t, err)
	toolFingerprint, err := FingerprintAttachment("project", tool)
	require.NoError(t, err)
	skillFingerprint, err := FingerprintAttachment("project", skill)
	require.NoError(t, err)

	require.Equal(t, "sha256:e764603e2fd4ff3ee1b16a85866c72d693e70f7d343f43e984d193ebebd206cb", agentFingerprint)
	require.Equal(t, "sha256:87c2a750ed78d8eb20ef28ce535057c7abc9691b7d822db4e4470839e30df928", completionFingerprint)
	require.Equal(t, "sha256:cadfcd880d050ddd8334d1b9ce22f08c563c85cb05910be95c22885273db916e", toolFingerprint)
	require.Equal(t, "sha256:b1daa2342cf1945b988512e7957f47faf933c4da026260d183dfe4eb57fba7f7", skillFingerprint)
}
