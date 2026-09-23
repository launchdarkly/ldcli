package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

func TestNewReferenceStoresRepositoryRelativePath(t *testing.T) {
	root := t.TempDir()
	workingDirectory := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(filepath.Join(workingDirectory, "prompts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workingDirectory, "prompts", "support.md"), []byte("Help"), 0o644))

	reference, err := NewReference(root, workingDirectory, "prompts/support.md", syncreference.PlainMarkdown)

	require.NoError(t, err)
	require.Equal(t, Reference{File: "app/prompts/support.md", Format: syncreference.PlainMarkdown}, reference)
}

func TestNewReferenceRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "prompt.md")
	require.NoError(t, os.WriteFile(outside, []byte("Help"), 0o644))

	_, err := NewReference(root, root, outside, syncreference.PlainMarkdown)
	require.ErrorContains(t, err, "inside the Git repository")

	managed := filepath.Join(root, syncdomain.RootDir, "prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(managed), 0o755))
	require.NoError(t, os.WriteFile(managed, []byte("Help"), 0o644))
	_, err = NewReference(root, root, managed, syncreference.PlainMarkdown)
	require.ErrorContains(t, err, "must be outside")
}

func TestNewReferenceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "prompt.md")
	require.NoError(t, os.WriteFile(outside, []byte("Help"), 0o644))
	link := filepath.Join(root, "prompt.md")
	require.NoError(t, os.Symlink(outside, link))

	_, err := NewReference(root, root, link, syncreference.PlainMarkdown)

	require.ErrorContains(t, err, "inside the Git repository")
}

func TestCompileWorkspaceReadsLinkedPrompt(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "prompts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompts", "support.md"), []byte("Be helpful.\n"), 0o644))
	_, err := NewStore(root).Add([]VariationFile{{
		ProjectKey: "project",
		ConfigKey:  "support",
		Upsert:     true,
		Ref:        &Reference{File: "prompts/support.md", Format: syncreference.PlainMarkdown},
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent,
			Key:  "default",
			Name: "Default",
		},
	}})
	require.NoError(t, err)

	resources, err := CompileWorkspace(root)

	require.NoError(t, err)
	require.Len(t, resources, 1)
	var variation syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[0].Payload, &variation))
	require.Equal(t, "Be helpful.", variation.Instructions)
}

func TestReplaceVariationsUpdatesLinkedWrapperAndPrompt(t *testing.T) {
	root := t.TempDir()
	referencePath := filepath.Join(root, "prompts", "support.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(referencePath), 0o755))
	require.NoError(t, os.WriteFile(referencePath, []byte("Old prompt\n"), 0o640))
	store := NewStore(root)
	reference := &Reference{File: "prompts/support.md", Format: syncreference.PlainMarkdown}
	_, err := store.Add([]VariationFile{{
		ProjectKey: "project", ConfigKey: "support", Upsert: true, Ref: reference,
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Old", Instructions: "Old prompt",
		},
	}})
	require.NoError(t, err)

	paths, err := store.ReplaceVariations([]VariationReplacement{{
		ProjectKey: "project",
		ConfigKey:  "support",
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "default", Name: "New", Instructions: "New prompt",
		},
	}})

	require.NoError(t, err)
	require.Equal(t, []string{"project/configs/support/default.prompt.md"}, paths)
	content, err := os.ReadFile(referencePath)
	require.NoError(t, err)
	require.Equal(t, "New prompt\n", string(content))
	info, err := os.Stat(referencePath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	wrapper, err := os.ReadFile(filepath.Join(root, syncdomain.RootDir, paths[0]))
	require.NoError(t, err)
	require.Contains(t, string(wrapper), "name: New")
	require.Contains(t, string(wrapper), "file: prompts/support.md")
}

func TestReplaceVariationsDoesNotChangeLinkedFilesWhenServerPromptIsNotRepresentable(t *testing.T) {
	root := t.TempDir()
	referencePath := filepath.Join(root, "prompt.md")
	require.NoError(t, os.WriteFile(referencePath, []byte("Old prompt\n"), 0o644))
	store := NewStore(root)
	_, err := store.Add([]VariationFile{{
		ProjectKey: "project", ConfigKey: "support",
		Ref: &Reference{File: "prompt.md", Format: syncreference.PlainMarkdown},
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeCompletion, Key: "default", Name: "Old",
			Messages: []syncdomain.Message{{Role: "system", Content: "Old prompt"}},
		},
	}})
	require.NoError(t, err)
	wrapperPath := filepath.Join(root, syncdomain.RootDir, "project", configsDir, "support", "default"+variationFileSuffix)
	originalWrapper, err := os.ReadFile(wrapperPath)
	require.NoError(t, err)

	_, err = store.ReplaceVariations([]VariationReplacement{{
		ProjectKey: "project", ConfigKey: "support",
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeCompletion, Key: "default", Name: "New",
			Messages: []syncdomain.Message{
				{Role: "system", Content: "System"},
				{Role: "user", Content: "User"},
			},
		},
	}})

	require.ErrorContains(t, err, "at most one system message")
	content, readErr := os.ReadFile(referencePath)
	require.NoError(t, readErr)
	require.Equal(t, "Old prompt\n", string(content))
	wrapper, readErr := os.ReadFile(wrapperPath)
	require.NoError(t, readErr)
	require.Equal(t, originalWrapper, wrapper)
}

func TestSourceFilesIncludesWrappersAndReferences(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "prompts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompts", "support.md"), []byte("Help"), 0o644))
	_, err := NewStore(root).Add([]VariationFile{{
		ProjectKey: "project", ConfigKey: "support",
		Ref: &Reference{File: "prompts/support.md", Format: syncreference.PlainMarkdown},
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default",
		},
	}})
	require.NoError(t, err)

	files, err := SourceFiles(root)

	require.NoError(t, err)
	require.Equal(t, []string{
		".launchdarkly/project/configs/support/default.prompt.md",
		"prompts/support.md",
	}, files)
}
