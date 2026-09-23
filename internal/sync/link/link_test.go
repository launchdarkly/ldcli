package link

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
			Key: "claude", Name: "Claude", Version: 4,
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
  modelName: claude
  parameters:
    temperature: 0.2
---
`, string(wrapper))

	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	require.Contains(t, string(resources[0].Payload), `"instructions":"Be helpful."`)
	require.Contains(t, string(resources[0].Payload), `"modelName":"claude"`)
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
		ModelConfig: syncapi.ModelConfig{Key: "claude"},
		Key:         "prompt",
		Name:        "Prompt",
	})

	require.ErrorContains(t, err, `variation "prompt" already exists`)
}

func TestAskValueUsesDefaultsAndEnteredValues(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("\nCustom name\n"))
	var output bytes.Buffer

	key, err := askValue(input, &output, "Variation key", "prompt")
	require.NoError(t, err)
	name, err := askValue(input, &output, "Variation name", "Prompt")
	require.NoError(t, err)

	require.Equal(t, "prompt", key)
	require.Equal(t, "Custom name", name)
	require.Contains(t, output.String(), "Variation key [prompt]")
}
