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
	var variation syncdomain.Variation
	require.NoError(t, json.Unmarshal(resources[0].Payload, &variation))
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search"}}, variation.Tools)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "support"}}, variation.Skills)
	require.Len(t, resources[0].Attachments, 2)
	assert.Equal(t, "Search documentation", *resources[0].Attachments[0].Tool.Description)
	assert.True(t, resources[0].Attachments[0].Upsert)
	assert.Equal(t, "Follow the standard support process.", resources[0].Attachments[1].Skill.Description)
	assert.Contains(t, resources[0].Attachments[1].Skill.Markdown, "Follow the support process.")
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
	err = NewStore(root).AttachVariation("project", "config", variation)
	require.ErrorContains(t, err, "symbolic links are not supported")
}
