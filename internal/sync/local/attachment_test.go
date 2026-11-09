package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	skill := syncdomain.Skill{
		Key: "support", Name: "Support", Description: "Follow the standard support process.", Markdown: "Follow the support process.\n",
	}

	rendered, err := renderAttachment(syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Skill: &skill})
	require.NoError(t, err)
	assert.Equal(t, "---\nkey: support\ndescription: Follow the standard support process.\n---\n\nFollow the support process.\n", string(rendered),
		"LaunchDarkly owns the name, so the file does not get it")

	rendered, err = renderAttachment(syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Upsert: true, Skill: &skill})
	require.NoError(t, err)
	assert.Contains(t, string(rendered), "\nupsert: true\n")
}

func TestReadSkillReadsTheOptionalNameAndUpsert(t *testing.T) {
	fsys := fstest.MapFS{
		".launchdarkly/project/skills/handoff.md": {
			Data: []byte("---\nkey: handoff\ndescription: Hand off\nname: Human handoff\nupsert: true\n---\n\n# Handoff\n"),
		},
	}

	attachment, err := readSkill(fsys, "project", "handoff")

	require.NoError(t, err)
	assert.True(t, attachment.Upsert)
	assert.Equal(t, "Human handoff", attachment.Skill.Name)
	assert.Equal(t, "# Handoff\n", attachment.Skill.Markdown)
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

	preserved, err := preserveLocalFields("tools/search.json", original, replacement)

	require.NoError(t, err)
	var file toolFile
	require.NoError(t, json.Unmarshal(preserved, &file))
	assert.True(t, file.Upsert)
	assert.Equal(t, "New", *file.Description)
}

func TestPreserveSkillNameAndUpsertAcrossServerWrites(t *testing.T) {
	original := []byte("---\nkey: handoff\ndescription: Old\nname: Human handoff\nupsert: true\n---\n\n# Old\n")
	server := syncdomain.Skill{Key: "handoff", Name: "Renamed in LaunchDarkly", Description: "New", Markdown: "# New\n"}
	replacement, err := renderAttachment(syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Skill: &server})
	require.NoError(t, err)

	preserved, err := preserveLocalFields("project/skills/handoff.md", original, replacement)

	require.NoError(t, err)
	assert.Equal(t, "---\nkey: handoff\ndescription: New\nname: Human handoff\nupsert: true\n---\n\n# New\n", string(preserved))

	plain := []byte("---\nkey: handoff\ndescription: Old\n---\n\n# Old\n")
	preserved, err = preserveLocalFields("project/skills/handoff.md", plain, replacement)
	require.NoError(t, err)
	assert.Equal(t, replacement, preserved)
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

func TestDeleteResourcesDeletesTheFilesOfEveryKindByID(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	toolPath := filepath.Join(root, ".launchdarkly", "project", "tools", "search.json")
	skillPath := filepath.Join(root, ".launchdarkly", "project", "skills", "support.md")
	variationPath := filepath.Join(root, ".launchdarkly", "project", "configs", "config", "default.prompt.md")
	for _, file := range []string{toolPath, skillPath, variationPath} {
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
		require.NoError(t, os.WriteFile(file, []byte("content\n"), 0o644))
	}

	deleted, err := store.DeleteResources([]syncdomain.ResourceID{
		{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"},
		{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "support"},
		syncdomain.VariationID("project", "config", "default"),
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"project/tools/search.json", "project/skills/support.md", "project/configs/config/default.prompt.md"}, deleted)
	for _, file := range []string{toolPath, skillPath, variationPath} {
		_, statErr := os.Stat(file)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	}
}

func TestCompileAttachmentsReadsStandaloneAndReferencedFiles(t *testing.T) {
	root := t.TempDir()
	search := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default", Instructions: "Help.",
		Tools:       []syncdomain.AttachmentRef{{Key: "search"}},
		Attachments: []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &search}},
	}
	_, err := NewStore(root).Add([]VariationFile{{ProjectKey: "project", ConfigKey: "config", Variation: variation}})
	require.NoError(t, err)
	writeManagedFile(t, root, "project/tools/refund.json", `{"formatVersion": 1, "upsert": true, "key": "refund", "schema": {}}`)
	writeManagedFile(t, root, "project/skills/triage.md", "---\nkey: triage\ndescription: Triage\n---\n\nPick a queue.\n")
	writeManagedFile(t, root, "other/tools/broken.json", "not json")

	attachments, err := CompileAttachments(root, func(id syncdomain.ResourceID) bool { return id.ProjectKey == "project" })

	require.NoError(t, err)
	var ids []string
	for _, attachment := range attachments {
		ids = append(ids, attachment.ID().Label())
	}
	assert.Equal(t, []string{"skill project/triage", "tool project/refund", "tool project/search"}, ids)
	assert.True(t, attachments[1].Attachment.Upsert)
}

func TestCompileAttachmentsReportsTheInvalidFile(t *testing.T) {
	root := t.TempDir()
	writeManagedFile(t, root, "project/tools/broken.json", "not json")

	_, err := CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })

	var parseErr ParseError
	require.ErrorAs(t, err, &parseErr)
	assert.Equal(t, ".launchdarkly/project/tools/broken.json", parseErr.Path)
	assert.ErrorContains(t, err, `parse tool "broken"`)
}

func TestCompileAttachmentsRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(root, "external.json")
	require.NoError(t, os.WriteFile(external, []byte(`{"formatVersion":1,"key":"search","schema":{}}`), 0o644))
	toolPath := filepath.Join(root, ".launchdarkly", "project", "tools", "search.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0o755))
	require.NoError(t, os.Symlink(external, toolPath))

	_, err := CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })

	require.ErrorContains(t, err, "symbolic links are not supported")
}

func TestReplaceAttachmentsWritesStandaloneFilesAndKeepsLocalFields(t *testing.T) {
	root := t.TempDir()
	writeManagedFile(t, root, "project/tools/search.json", `{"formatVersion": 1, "upsert": true, "key": "search", "schema": {}}`)
	description := "Search the docs"
	search := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: 4, Tool: &syncdomain.Tool{
		Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
	}}
	triage := syncdomain.Attachment{Kind: syncdomain.AttachmentSkill, Skill: &syncdomain.Skill{
		Key: "triage", Name: "Triage", Description: "Triage", Markdown: "Pick a queue.\n",
	}}

	written, err := NewStore(root).ReplaceAttachments([]syncdomain.SyncedAttachment{
		{ProjectKey: "project", Attachment: search}, {ProjectKey: "project", Attachment: triage},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"project/tools/search.json", "project/skills/triage.md"}, written)
	attachments, err := CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })
	require.NoError(t, err)
	require.Len(t, attachments, 2)
	assert.Equal(t, "Pick a queue.\n", attachments[0].Attachment.Skill.Markdown)
	assert.Empty(t, attachments[0].Attachment.Skill.Name, "the file does not get the LaunchDarkly name")
	assert.Equal(t, "Search the docs", *attachments[1].Attachment.Tool.Description)
	assert.True(t, attachments[1].Attachment.Upsert, "the file keeps its upsert flag")
}

func writeManagedFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, syncdomain.RootDir, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
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

func TestCompileWorkspaceIgnoresASymlinkAtAManagedPathBesideADescriptor(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "config", "default")
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Help the customer.\n"), 0o644))
	_, err := NewStore(root).WriteDescriptor(id, Reference{File: "prompt.md", Format: "markdown"})
	require.NoError(t, err)
	external := filepath.Join(root, "external.md")
	require.NoError(t, os.WriteFile(external, []byte("---\nformatVersion: 1\nmode: agent\nkey: default\n---\n\nOther.\n"), 0o644))
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(external, filepath.Join(root, filepath.FromSlash(managedPath))))

	variations, err := CompileWorkspace(root)

	require.NoError(t, err, "a link is not a definition, so the descriptor defines the variation")
	require.Len(t, variations, 1)
	assert.Contains(t, variations[0].Variation.Instructions, "Help the customer.")
	assert.NotNil(t, variations[0].Source)
}

func TestCompileWorkspaceRejectsASymlinkOnlyVariation(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "config", "default")
	external := filepath.Join(root, "external.md")
	require.NoError(t, os.WriteFile(external, []byte("---\nformatVersion: 1\nmode: agent\nkey: default\n---\n\nOther.\n"), 0o644))
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	target := filepath.Join(root, filepath.FromSlash(managedPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.Symlink(external, target))

	_, err = CompileWorkspace(root)

	require.ErrorContains(t, err, "symbolic links are not supported", "a link without a descriptor is an error, not an absent variation")
}

func TestCompileAttachmentsIgnoresASymlinkAtAManagedPathBesideADescriptor(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}
	require.NoError(t, os.WriteFile(filepath.Join(root, "tool.json"), []byte(`{"description":"Search","schema":{"type":"object"}}`), 0o644))
	_, err := NewStore(root).WriteDescriptor(id, Reference{
		File: "tool.json", Format: "json", Select: "$", Map: map[string]string{"description": "description", "schema": "schema"},
	})
	require.NoError(t, err)
	external := filepath.Join(root, "external.json")
	require.NoError(t, os.WriteFile(external, []byte(`{"formatVersion":1,"key":"search","schema":{}}`), 0o644))
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(external, filepath.Join(root, filepath.FromSlash(managedPath))))

	attachments, err := CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })

	require.NoError(t, err, "a link is not a definition, so the descriptor defines the tool")
	require.Len(t, attachments, 1)
	assert.Equal(t, "Search", *attachments[0].Attachment.Tool.Description)
	assert.NotNil(t, attachments[0].Source)
}

func TestExistsAndDeleteTreatALeftoverLinkBesideADescriptorAsAbsent(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "config", "default")
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Help the customer.\n"), 0o644))
	store := NewStore(root)
	descriptorPath, err := store.WriteDescriptor(id, Reference{File: "prompt.md", Format: "markdown"})
	require.NoError(t, err)
	external := filepath.Join(root, "external.md")
	require.NoError(t, os.WriteFile(external, []byte("Other.\n"), 0o644))
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(external, filepath.Join(root, filepath.FromSlash(managedPath))))

	exists, err := store.ResourceExists(id)
	require.NoError(t, err, "a link is not a second definition")
	assert.True(t, exists)

	deleted, err := store.DeleteDefinitions([]syncdomain.ResourceID{id})
	require.NoError(t, err)
	assert.Equal(t, []string{
		strings.TrimPrefix(descriptorPath, syncdomain.RootDir+"/"),
		strings.TrimPrefix(managedPath, syncdomain.RootDir+"/"),
	}, deleted, "detach deletes the descriptor and the leftover link beside it")
	assert.NoFileExists(t, filepath.Join(root, filepath.FromSlash(descriptorPath)))
	_, err = os.Lstat(filepath.Join(root, filepath.FromSlash(managedPath)))
	require.ErrorIs(t, err, os.ErrNotExist, "the link does not stand alone, where compile would reject it")
	assert.FileExists(t, external, "the target of the link is kept")
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

func TestCompileAttachmentsFollowsARepositoryLinkToALinkedSource(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib", "tools"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "lib", "tools", "search.json"),
		[]byte(`{"description":"Linked","schema":{"type":"object"}}`), 0o644,
	))
	require.NoError(t, os.Symlink(filepath.Join(root, "lib"), filepath.Join(root, "linked")))
	writeManagedFile(t, root, "project/tools/search.ref.yaml",
		"formatVersion: 1\nfile: linked/tools/search.json\nformat: json\nselect: $\n"+
			"map:\n  description: description\n  schema: schema\n")

	attachments, err := CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })

	require.NoError(t, err)
	require.Len(t, attachments, 1)
	assert.Equal(t, "Linked", *attachments[0].Attachment.Tool.Description)
	variations, err := CompileWorkspace(root)
	require.NoError(t, err)
	assert.Empty(t, variations, "the same read rule applies to both compile paths")
}

func TestDeleteDefinitionsRemovesALoneLeftoverLink(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(root, "external.md")
	require.NoError(t, os.WriteFile(external, []byte("Other.\n"), 0o644))
	id := syncdomain.VariationID("project", "config", "linked")
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	target := filepath.Join(root, filepath.FromSlash(managedPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.Symlink(external, target))

	deleted, err := NewStore(root).DeleteDefinitions([]syncdomain.ResourceID{id})

	require.NoError(t, err)
	assert.Len(t, deleted, 1)
	_, err = os.Lstat(target)
	assert.ErrorIs(t, err, os.ErrNotExist, "the link itself is removed")
	assert.FileExists(t, external, "the target of the link stays")
}
