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

func TestRunUsesExplicitTargetWithoutTerminal(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Be helpful.\n"), 0o644))
	target := syncdomain.ResourceID{
		Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/support-agent",
	}

	path, err := Run(Options{
		Catalog: &fakeCatalog{
			config: syncapi.Config{Key: "support", Mode: syncdomain.VariationModeAgent},
			model:  syncapi.ModelConfig{Key: "claude", ID: "claude-3", Version: 2},
		},
		Store:            synclocal.NewStore(root),
		RepositoryRoot:   root,
		WorkingDirectory: root,
		File:             "prompt.md",
		Format:           syncreference.PlainMarkdown,
		NoInput:          true,
		Target: &Target{
			Variation: target, ModelConfigKey: "claude",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "production/configs/support/support-agent.prompt.md", path)
	wrapper, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, filepath.FromSlash(path)))
	require.NoError(t, err)
	require.Contains(t, string(wrapper), "name: Support agent")
}

func TestRunRejectsBlankExplicitMetadata(t *testing.T) {
	tests := map[string]Target{
		"name": {
			Name: " ",
		},
		"content": {
			Content: "\t",
		},
	}

	for field, target := range tests {
		t.Run(field, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), nil, 0o644))
			target.Variation = syncdomain.ResourceID{
				Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/support-agent",
			}
			target.ModelConfigKey = "claude"

			_, err := Run(Options{
				Catalog:          &fakeCatalog{},
				Store:            synclocal.NewStore(root),
				RepositoryRoot:   root,
				WorkingDirectory: root,
				File:             "prompt.md",
				Format:           syncreference.PlainMarkdown,
				Target:           &target,
			})

			require.ErrorContains(t, err, "--"+field+" cannot be blank")
		})
	}
}

func TestCreateLinkedPromptWritesLinkedVariationWrapper(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "prompts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompts", "support-agent.md"), []byte("Be helpful.\n"), 0o644))

	path, err := create(Options{
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
	require.Equal(t, "Be helpful.", resources[0].Variation.Instructions)
	require.Equal(t, "claude-3-5-sonnet-20241022", resources[0].Variation.Model["modelName"])
}

func TestCreateLinkedPromptRejectsExistingServerVariation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Help"), 0o644))

	_, err := create(Options{
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

func TestCreateLinkedPromptValidatesDestinationBeforeUpdatingLinkedFile(t *testing.T) {
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

func TestCreateDoesNotOverwriteLinkedFileChangedAfterRead(t *testing.T) {
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
	prompt.content = []byte("content collected by the linker")

	require.NoError(t, os.WriteFile(sourcePath, []byte("newer content from the editor"), 0o644))

	_, err = createLinkedPrompt(options, Selection{
		Project:     syncapi.Project{Key: "production"},
		Config:      syncapi.Config{Key: "support", Mode: syncdomain.VariationModeAgent},
		ModelConfig: syncapi.ModelConfig{Key: "claude", ID: "claude-3-5-sonnet-20241022"},
		Key:         "prompt",
		Name:        "Prompt",
	}, prompt)
	require.ErrorContains(t, err, "changed while syncing")

	content, readErr := os.ReadFile(sourcePath)
	require.NoError(t, readErr)
	require.Equal(t, "newer content from the editor", string(content))
}

func TestRequiredValueRejectsBlankInput(t *testing.T) {
	require.Error(t, requiredValue("variation name")("  "))
	require.NoError(t, requiredValue("variation name")("Custom name"))
}

type fakeCatalog struct {
	config syncapi.Config
	model  syncapi.ModelConfig
}

func (catalog *fakeCatalog) Config(string, string) (syncapi.Config, error) {
	return catalog.config, nil
}

func (*fakeCatalog) SearchProjects(string, int, int) (syncapi.Page[syncapi.Project], error) {
	return syncapi.Page[syncapi.Project]{}, nil
}

func (*fakeCatalog) SearchConfigs(string, string, []syncdomain.VariationMode, int, int) (syncapi.Page[syncapi.Config], error) {
	return syncapi.Page[syncapi.Config]{}, nil
}

func (catalog *fakeCatalog) ModelConfigs(string) ([]syncapi.ModelConfig, error) {
	return []syncapi.ModelConfig{catalog.model}, nil
}

func create(options Options, selection Selection) (string, error) {
	prompt, err := readLinkedPrompt(options)
	if err != nil {
		return "", err
	}
	return createLinkedPrompt(options, selection, prompt)
}
