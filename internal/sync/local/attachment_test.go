package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestCompileHydratesVariationAttachments(t *testing.T) {
	fsys := fstest.MapFS{
		".launchdarkly/project/configs/config/default.prompt.md": {
			Data: []byte(`---
formatVersion: 1
mode: agent
key: default
name: Default
tools:
  - key: search
skills:
  - key: support
---

Help the customer.
`),
		},
		".launchdarkly/project/tools/search.json": {
			Data: []byte(`{
  "formatVersion": 1,
  "upsert": true,
  "key": "search",
  "description": "Search documentation",
  "schema": {"type": "object"}
}`),
		},
		".launchdarkly/project/skills/support.md": {
			Data: []byte("---\nkey: support\ndescription: Follow the standard support process.\n---\n\nFollow the support process.\n"),
		},
	}

	resources, err := Compile(fsys)

	require.NoError(t, err)
	require.Len(t, resources, 1)
	variation := resources[0].Variation
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search"}}, variation.Tools)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "support"}}, variation.Skills)
	require.Len(t, variation.Attachments, 2)
	tool, ok := variation.Attachment(syncdomain.AttachmentTool, "search")
	require.True(t, ok)
	assert.Equal(t, "Search documentation", *tool.Tool.Description)
	assert.True(t, tool.Upsert)
	skill, ok := variation.Attachment(syncdomain.AttachmentSkill, "support")
	require.True(t, ok)
	assert.Equal(t, "Follow the standard support process.", skill.Skill.Description)
	assert.Contains(t, skill.Skill.Markdown, "Follow the support process.")
}

func TestRenderSkillIncludesReadOnlyKeyAndEditableDescription(t *testing.T) {
	rendered, err := renderSkill(syncdomain.Skill{
		Key: "support", Description: "Follow the standard support process.", Markdown: "Follow the support process.\n",
	})

	require.NoError(t, err)
	assert.Equal(t, "---\nkey: support\ndescription: Follow the standard support process.\n---\n\nFollow the support process.\n", string(rendered))
}

func TestReadSkillRejectsKeyThatDoesNotMatchFilename(t *testing.T) {
	fsys := fstest.MapFS{
		".launchdarkly/project/skills/support.md": {
			Data: []byte("---\nkey: other\ndescription: Support\n---\n\nFollow the support process.\n"),
		},
	}

	_, err := readSkill(fsys, "project", "support")

	require.ErrorContains(t, err, `skill key "other" does not match filename "support"`)
}

func TestReadSkillRequiresDescriptionFrontMatter(t *testing.T) {
	fsys := fstest.MapFS{
		".launchdarkly/project/skills/support.md": {
			Data: []byte("---\nkey: support\n---\n\nFollow the support process.\n"),
		},
	}

	_, err := readSkill(fsys, "project", "support")

	require.ErrorContains(t, err, "description is required")
}

func TestCompileRejectsSkillsForCompletionVariation(t *testing.T) {
	fsys := fstest.MapFS{
		".launchdarkly/project/configs/config/default.prompt.md": {
			Data: []byte(`---
formatVersion: 1
mode: completion
key: default
name: Default
skills:
  - key: support
---

<system>
Help the customer.
</system>
`),
		},
	}

	_, err := Compile(fsys)

	require.ErrorContains(t, err, "skills can only be attached to agent-mode configs")
}

func TestReadToolRejectsMultipleJSONValues(t *testing.T) {
	fsys := fstest.MapFS{
		".launchdarkly/project/tools/search.json": {
			Data: []byte(`{"formatVersion":1,"key":"search","schema":{}} {}`),
		},
	}

	_, err := readTool(fsys, "project", "search")

	require.ErrorContains(t, err, "multiple JSON values")
}

func TestPreserveToolUpsertAcrossServerWrites(t *testing.T) {
	oldDescription, newDescription := "Old", "New"
	original, err := renderToolFile(toolFile{
		FormatVersion: 1, Upsert: true,
		Tool: syncdomain.Tool{Key: "search", Description: &oldDescription, Schema: map[string]any{}},
	})
	require.NoError(t, err)
	replacement, err := renderToolFile(toolFile{
		FormatVersion: 1,
		Tool:          syncdomain.Tool{Key: "search", Description: &newDescription, Schema: map[string]any{}},
	})
	require.NoError(t, err)

	preserved, err := preserveToolUpsert("tools/search.json", original, replacement)

	require.NoError(t, err)
	var file toolFile
	require.NoError(t, json.Unmarshal(preserved, &file))
	assert.True(t, file.Upsert)
	assert.Equal(t, "New", *file.Description)
}

func TestReplaceVariationsRemovesNewAttachmentWhenVariationUpdateFails(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	existing := localVariation("default")
	_, err := store.Add([]VariationFile{existing})
	require.NoError(t, err)

	tool := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	variation := existing.Variation
	variation.Tools = []syncdomain.AttachmentRef{{Key: tool.Key}}
	variation.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool, Tool: &tool,
	}}

	_, err = store.ReplaceVariations([]VariationReplacement{{ProjectKey: "project", ConfigKey: "../invalid", Variation: variation}})

	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(root, ".launchdarkly", "project", "tools", "search.json"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	resources, compileErr := CompileWorkspace(root)
	require.NoError(t, compileErr)
	require.Len(t, resources, 1)
	assert.Empty(t, resources[0].Variation.Attachments)
}

func TestReplaceVariationsLeavesAttachmentUnchangedWhenPreflightFails(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	oldDescription, newDescription := "Old", "New"
	tool := syncdomain.Tool{Key: "search", Description: &oldDescription, Schema: map[string]any{"type": "object"}}
	existing := localVariation("default")
	existing.Variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	existing.Variation.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &tool}}
	_, err := store.Add([]VariationFile{existing})
	require.NoError(t, err)

	updated := existing.Variation
	updated.Attachments[0].Tool.Description = &newDescription
	_, err = store.ReplaceVariations([]VariationReplacement{
		{ProjectKey: "project", ConfigKey: "config", Variation: updated},
		{ProjectKey: "project", ConfigKey: "config", Variation: localVariation("missing").Variation},
	})

	require.Error(t, err)
	attachment, err := readAttachment(os.DirFS(root), "project", syncdomain.AttachmentTool, "search")
	require.NoError(t, err)
	assert.Equal(t, oldDescription, *attachment.Tool.Description)
}

func TestOrphanedAttachmentsRequireEveryLocalReferenceToBeRemoved(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	tool := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	first, second := localVariation("first"), localVariation("second")
	for _, variation := range []*VariationFile{&first, &second} {
		variation.Variation.Tools = []syncdomain.AttachmentRef{{Key: tool.Key}}
		variation.Variation.Attachments = []syncdomain.Attachment{{
			Kind: syncdomain.AttachmentTool, Tool: &tool,
		}}
	}
	_, err := store.Add([]VariationFile{first, second})
	require.NoError(t, err)

	first.Variation.Tools = nil
	first.Variation.Attachments = nil
	_, err = store.ReplaceVariations([]VariationReplacement{{
		ProjectKey: first.ProjectKey, ConfigKey: first.ConfigKey, Variation: first.Variation,
	}})
	require.NoError(t, err)
	orphaned, err := store.OrphanedAttachments()
	require.NoError(t, err)
	assert.Empty(t, orphaned)

	second.Variation.Tools = nil
	second.Variation.Attachments = nil
	_, err = store.ReplaceVariations([]VariationReplacement{{
		ProjectKey: second.ProjectKey, ConfigKey: second.ConfigKey, Variation: second.Variation,
	}})
	require.NoError(t, err)
	orphaned, err = store.OrphanedAttachments()
	require.NoError(t, err)
	require.Equal(t, []OrphanedAttachment{{
		ProjectKey: "project", Kind: syncdomain.AttachmentTool, Key: "search", Path: "project/tools/search.json",
	}}, orphaned)

	deleted, err := store.DeleteAttachments(orphaned)
	require.NoError(t, err)
	assert.Equal(t, []string{"project/tools/search.json"}, deleted)
	_, statErr := os.Stat(filepath.Join(root, ".launchdarkly", "project", "tools", "search.json"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestOrphanedAttachmentsFindsFlatSkillFile(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, ".launchdarkly", "project", "skills", "support.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	require.NoError(t, os.WriteFile(skillPath, []byte("# Support\n"), 0o644))

	store := NewStore(root)
	orphaned, err := store.OrphanedAttachments()

	require.NoError(t, err)
	require.Equal(t, []OrphanedAttachment{{
		ProjectKey: "project", Kind: syncdomain.AttachmentSkill, Key: "support", Path: "project/skills/support.md",
	}}, orphaned)

	deleted, err := store.DeleteAttachments(orphaned)
	require.NoError(t, err)
	assert.Equal(t, []string{"project/skills/support.md"}, deleted)
	_, statErr := os.Stat(skillPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestCompileWorkspaceRejectsAttachmentSymlink(t *testing.T) {
	root := t.TempDir()
	wrapperPath := filepath.Join(root, ".launchdarkly", "project", "configs", "config", "default.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapperPath), 0o755))
	require.NoError(t, os.WriteFile(wrapperPath, []byte(`---
formatVersion: 1
mode: agent
key: default
name: Default
tools:
  - key: search
---

Help the customer.
`), 0o644))

	external := filepath.Join(root, "external.json")
	require.NoError(t, os.WriteFile(external, []byte(`{"formatVersion":1,"key":"search","schema":{}}`), 0o644))
	toolPath := filepath.Join(root, ".launchdarkly", "project", "tools", "search.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0o755))
	require.NoError(t, os.Symlink(external, toolPath))

	_, err := CompileWorkspace(root)

	require.ErrorContains(t, err, "symbolic links are not supported")
}

func TestCompileWorkspaceRejectsAttachmentParentSymlink(t *testing.T) {
	root := t.TempDir()
	wrapperPath := filepath.Join(root, ".launchdarkly", "project", "configs", "config", "default.prompt.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapperPath), 0o755))
	require.NoError(t, os.WriteFile(wrapperPath, []byte(`---
formatVersion: 1
mode: agent
key: default
name: Default
skills:
  - key: support
---

Help the customer.
`), 0o644))

	externalDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(externalDir, "support.md"), []byte("# Support\n"), 0o644))
	skillsPath := filepath.Join(root, ".launchdarkly", "project", "skills")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillsPath), 0o755))
	require.NoError(t, os.Symlink(externalDir, skillsPath))

	_, err := CompileWorkspace(root)

	require.ErrorContains(t, err, "symbolic links are not supported")

	skill := syncdomain.Skill{Key: "support", Markdown: "# Updated\n"}
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default", Instructions: "Help the customer.",
		Skills:      []syncdomain.AttachmentRef{{Key: "support"}},
		Attachments: []syncdomain.Attachment{{Kind: syncdomain.AttachmentSkill, Skill: &skill}},
	}
	_, err = NewStore(root).ReplaceVariations([]VariationReplacement{{ProjectKey: "project", ConfigKey: "config", Variation: variation}})
	require.ErrorContains(t, err, "symbolic links are not supported")
}
