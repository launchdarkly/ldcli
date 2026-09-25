package link

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

func TestCreateWritesLinkedVariationWrapper(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "prompts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompts", "support-agent.md"), []byte("Be helpful.\n"), 0o644))

	path, err := Create(Options{
		Store:            synclocal.NewStore(root),
		RepositoryRoot:   root,
		WorkingDirectory: root,
		File:             "prompts/support-agent.md",
		Format:           syncreference.PlainMarkdown,
	}, Selection{
		Project: syncapi.Project{Key: "production", Name: "Production"},
		Config:  syncapi.Config{Key: "support", Name: "Support", Mode: syncdomain.VariationModeAgent},
		ModelConfig: syncapi.ModelConfig{
			Key: "claude", ID: "claude-3-5-sonnet-20241022", Name: "Claude", Version: 4,
			Params: map[string]any{"temperature": 0.2}, CustomParams: map[string]any{"region": "us-east"},
		},
		Key:  "support-agent",
		Name: "Support agent",
	})

	require.NoError(t, err)
	require.Equal(t, "production/configs/support/support-agent.prompt.md", path)
	wrapper, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, filepath.FromSlash(path)))
	require.NoError(t, err)
	require.Equal(t, `---
formatVersion: 1
upsert: true
ref:
  file: prompts/support-agent.md
  format: plain-markdown
mode: agent
key: support-agent
name: Support agent
modelConfigKey: claude
modelConfigVersion: 4
model:
  custom:
    region: us-east
  modelName: claude-3-5-sonnet-20241022
  parameters:
    temperature: 0.2
---
`, string(wrapper))

	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	require.Contains(t, string(resources[0].Payload), `"instructions":"Be helpful."`)
	require.Contains(t, string(resources[0].Payload), `"modelName":"claude-3-5-sonnet-20241022"`)
}

func TestCreateRejectsExistingServerVariation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Help"), 0o644))

	_, err := Create(Options{
		Store:            synclocal.NewStore(root),
		RepositoryRoot:   root,
		WorkingDirectory: root,
		File:             "prompt.md",
		Format:           syncreference.PlainMarkdown,
	}, Selection{
		Project: syncapi.Project{Key: "production"},
		Config: syncapi.Config{
			Key: "support", Mode: syncdomain.VariationModeAgent,
			Variations: []syncdomain.Variation{{Key: "prompt"}},
		},
		ModelConfig: syncapi.ModelConfig{Key: "claude", ID: "claude-3-5-sonnet-20241022"},
		Key:         "prompt",
		Name:        "Prompt",
	})

	require.ErrorContains(t, err, `variation "prompt" already exists`)
}

func TestCreateValidatesDestinationBeforeUpdatingLinkedFile(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "prompt.md")
	require.NoError(t, os.WriteFile(sourcePath, nil, 0o644))
	options := Options{
		Store:            synclocal.NewStore(root),
		RepositoryRoot:   root,
		WorkingDirectory: root,
		File:             "prompt.md",
		Format:           syncreference.PlainMarkdown,
	}
	prompt, err := readLinkedPrompt(options)
	require.NoError(t, err)
	prompt, err = addMissingPromptContent(prompt, syncdomain.VariationModeAgent, "prompt", "Prompt", "Help.")
	require.NoError(t, err)

	_, err = createLinkedPrompt(options, Selection{
		Project: syncapi.Project{Key: "production"},
		Config: syncapi.Config{
			Key: "support", Mode: syncdomain.VariationModeAgent,
			Variations: []syncdomain.Variation{{Key: "prompt"}},
		},
		ModelConfig: syncapi.ModelConfig{Key: "claude", ID: "claude-3-5-sonnet-20241022"},
		Key:         "prompt",
		Name:        "Prompt",
	}, prompt)

	require.ErrorContains(t, err, `variation "prompt" already exists`)
	content, readErr := os.ReadFile(sourcePath)
	require.NoError(t, readErr)
	require.Empty(t, content)
}

func TestRequiredValueRejectsBlankInput(t *testing.T) {
	require.Error(t, requiredValue("variation name")("  "))
	require.NoError(t, requiredValue("variation name")("Custom name"))
}
