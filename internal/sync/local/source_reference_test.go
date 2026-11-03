package local

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestCompileWorkspaceReadsVariationDescriptor(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "agents.py", `from agents import Agent
support = Agent(instructions="Help with orders.")
`)
	writeTestFile(t, root, ".launchdarkly/project/configs/support/default.ref.yaml", `formatVersion: 1
file: agents.py
format: python
select: 'agents[id="support"]'
map:
  prompt: instructions
`)

	resources, err := CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, syncdomain.VariationID("project", "support", "default"), resources[0].ID())
	assert.Equal(t, "Help with orders.", resources[0].Variation.Instructions)
	require.NotNil(t, resources[0].Source)
	assert.Equal(t, "agents.py", resources[0].Source.File)
}

func TestCompileSourceWithoutWritingDescriptor(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help with orders.\n")
	id := syncdomain.VariationID("project", "support", "default")
	reference := Reference{File: "prompt.md", Format: "markdown"}

	definition, err := CompileSource(root, id, reference)

	require.NoError(t, err)
	assert.Equal(t, id, definition.ID())
	require.NotNil(t, definition.Variation)
	assert.Equal(t, "Help with orders.", definition.Variation.Variation.Instructions)
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "configs", "support", "default.ref.yaml"))
}

func TestImportedSourceEditsDependencyAtomically(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "app/agent.py", `from agents import Agent
from .prompts import SUPPORT_PROMPT
support = Agent(instructions=SUPPORT_PROMPT, model="gpt-5")
`)
	writeTestFile(t, root, "app/prompts.py", `SUPPORT_PROMPT = "Original prompt"`+"\n")
	id := syncdomain.VariationID("project", "support", "default")
	reference := Reference{
		File: "app/agent.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	writeDescriptorForTest(t, root, id, reference)
	prompt := "Updated imported prompt"

	paths, err := NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: reference,
		Projection: syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Instructions: &prompt,
		},
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"app/prompts.py"}, paths)
	primary, err := os.ReadFile(filepath.Join(root, "app", "agent.py"))
	require.NoError(t, err)
	assert.Contains(t, string(primary), "instructions=SUPPORT_PROMPT")
	dependency, err := os.ReadFile(filepath.Join(root, "app", "prompts.py"))
	require.NoError(t, err)
	assert.Contains(t, string(dependency), `"Updated imported prompt"`)
	files, err := SourceFiles(root)
	require.NoError(t, err)
	assert.Contains(t, files, "app/prompts.py")
}

func TestReplaceSourcesEditsOneSharedDependencyOnce(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "app/a.py", "from agents import Agent\nfrom .prompts import A_PROMPT\na = Agent(instructions=A_PROMPT, model=\"gpt-5\")\n")
	writeTestFile(t, root, "app/b.py", "from agents import Agent\nfrom .prompts import B_PROMPT\nb = Agent(instructions=B_PROMPT, model=\"gpt-5\")\n")
	writeTestFile(t, root, "app/prompts.py", "A_PROMPT = \"Original A\"\nB_PROMPT = \"Original B\"\n")
	idA := syncdomain.VariationID("project", "support", "a")
	idB := syncdomain.VariationID("project", "support", "b")
	referenceA := Reference{File: "app/a.py", Format: "python", Select: `agents[id="a"]`, Map: map[string]string{"prompt": "instructions"}}
	referenceB := Reference{File: "app/b.py", Format: "python", Select: `agents[id="b"]`, Map: map[string]string{"prompt": "instructions"}}
	writeDescriptorForTest(t, root, idA, referenceA)
	writeDescriptorForTest(t, root, idB, referenceB)

	paths, err := NewStore(root).ReplaceSources([]SourceReplacement{
		{ID: idA, Reference: referenceA,
			Projection: syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("Updated A")}},
		{ID: idB, Reference: referenceB,
			Projection: syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("Updated B")}},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"app/prompts.py"}, paths)
	shared, err := os.ReadFile(filepath.Join(root, "app", "prompts.py"))
	require.NoError(t, err)
	assert.Equal(t, "A_PROMPT = \"Updated A\"\nB_PROMPT = \"Updated B\"\n", string(shared))
}

func TestReplaceSourcesKeepsBindingsOfAnEditedDependency(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "app/agent.py", "from agents import Agent\nfrom .prompts import PROMPT\nagent = Agent(instructions=PROMPT, model=\"gpt-5\")\n")
	writeTestFile(t, root, "app/prompts.py", "PROMPT = \"Original\"\n")
	variationID := syncdomain.VariationID("project", "support", "default")
	variationReference := Reference{File: "app/agent.py", Format: "python", Select: `agents[id="agent"]`, Map: map[string]string{"prompt": "instructions"}}
	writeDescriptorForTest(t, root, variationID, variationReference)
	// A second descriptor binds the same constant through the dependency file.
	otherID := syncdomain.VariationID("project", "support", "other")
	writeDescriptorForTest(t, root, otherID, Reference{
		File: "app/prompts.py", Format: "python", Select: `constants[id="PROMPT"]`, Map: map[string]string{"prompt": "$"},
	})

	_, err := NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: variationID, Reference: variationReference,
		Projection: syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("Updated")},
	}})

	require.ErrorContains(t, err, "non-target projection")
	shared, err := os.ReadFile(filepath.Join(root, "app", "prompts.py"))
	require.NoError(t, err)
	assert.Equal(t, "PROMPT = \"Original\"\n", string(shared))
}

func TestWriteDescriptorCreatesUpdatesAndKeepsIdenticalContent(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "first.md", "First.\n")
	writeTestFile(t, root, "second.md", "Second.\n")
	id := syncdomain.VariationID("project", "support", "default")
	store := NewStore(root)

	path, err := store.WriteDescriptor(id, Reference{File: "first.md", Format: "markdown"})
	require.NoError(t, err)
	first, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	_, err = store.WriteDescriptor(id, Reference{File: "first.md", Format: "markdown"})
	require.NoError(t, err)
	same, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	assert.Equal(t, first, same)

	_, err = store.WriteDescriptor(id, Reference{File: "second.md", Format: "markdown"})
	require.NoError(t, err)
	updated, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "file: second.md")
}

func TestCompileSourceReportsMissingFile(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")

	_, err := CompileSource(root, id, Reference{File: "missing.md", Format: "markdown"})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorLinkedSourceMissing, problem.Code)
	assert.Equal(t, "missing.md", problem.File)
}

func TestCompileWorkspaceRejectsManagedAndDescriptorDefinitions(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	writeTestFile(t, root, ".launchdarkly/project/configs/support/default.ref.yaml", `formatVersion: 1
file: prompt.md
format: markdown
`)
	writeTestFile(t, root, ".launchdarkly/project/configs/support/default.prompt.md", `---
formatVersion: 1
upsert: false
mode: agent
key: default
name: Default
---

Help.
`)

	_, err := CompileWorkspace(root)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorDefinitionConflict, problem.Code)
}

func TestCompileAttachmentsReadsToolAndSkillDescriptors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tools.ts", `import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
const server = new McpServer({ name: "orders", version: "1" });
server.registerTool("lookup", {
  description: "Look up an order.",
  inputSchema: { type: "object" },
}, handler);
`)
	writeTestFile(t, root, ".agents/skills/refunds/SKILL.md", `---
name: refunds
description: Explain refunds.
---

# Refunds

Ask for the order ID.
`)
	toolID := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"}
	skillID := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}
	writeDescriptorForTest(t, root, toolID, Reference{
		File: "tools.ts", Format: "typescript", Select: `tools[id="lookup"]`,
		Map: map[string]string{"description": "description", "schema": "inputSchema"},
	})
	writeDescriptorForTest(t, root, skillID, Reference{
		File: ".agents/skills/refunds/SKILL.md", Format: "agent-skill",
	})

	attachments, err := CompileAttachments(root, func(syncdomain.ResourceID) bool { return true })

	require.NoError(t, err)
	require.Len(t, attachments, 2)
	byID := map[syncdomain.ResourceID]syncdomain.SyncedAttachment{}
	for _, attachment := range attachments {
		byID[attachment.ID()] = attachment
	}
	assert.Equal(t, "Look up an order.", *byID[toolID].Attachment.Tool.Description)
	assert.Equal(t, "Explain refunds.", byID[skillID].Attachment.Skill.Description)
}

func TestReplaceSourcesGroupsEditsAndPreservesOtherBytes(t *testing.T) {
	root := t.TempDir()
	original := `from agents import Agent

support = Agent(instructions="Help with orders.")
refund = Agent(instructions='Explain refunds.')
`
	writeTestFile(t, root, "agents.py", original)
	supportID := syncdomain.VariationID("project", "support", "default")
	refundID := syncdomain.VariationID("project", "refund", "default")
	supportReference := Reference{
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	refundReference := Reference{
		File: "agents.py", Format: "python", Select: `agents[id="refund"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	writeDescriptorForTest(t, root, supportID, supportReference)
	writeDescriptorForTest(t, root, refundID, refundReference)

	supportPrompt := "Help with orders and shipping."
	refundPrompt := "Explain refunds and escalation."
	paths, err := NewStore(root).ReplaceSources([]SourceReplacement{
		{
			ID: supportID, Reference: supportReference,
			Projection: syncdomain.VariationProjection{
				Mode: syncdomain.VariationModeAgent, Instructions: &supportPrompt,
			},
		},
		{
			ID: refundID, Reference: refundReference,
			Projection: syncdomain.VariationProjection{
				Mode: syncdomain.VariationModeAgent, Instructions: &refundPrompt,
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"agents.py"}, paths)
	content, err := os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, err)
	assert.Equal(t, `from agents import Agent

support = Agent(instructions="Help with orders and shipping.")
refund = Agent(instructions='Explain refunds and escalation.')
`, string(content))
}

func TestReplaceSourcesGroupsStructuredFormatEdits(t *testing.T) {
	tests := []struct {
		name         string
		file         string
		format       string
		source       string
		firstID      syncdomain.ResourceID
		secondID     syncdomain.ResourceID
		firstSelect  string
		secondSelect string
		fieldMap     map[string]string
		first        syncdomain.Projection
		second       syncdomain.Projection
		want         []string
	}{
		{
			name: "JSON tools", file: "tools.json", format: "json",
			source:      `{"tools":[{"name":"lookup","description":"Old lookup"},{"name":"refund","description":"Old refund"}]}` + "\n",
			firstID:     syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup"},
			secondID:    syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "refund"},
			firstSelect: `tools[name="lookup"]`, secondSelect: `tools[name="refund"]`,
			fieldMap: map[string]string{"description": "description"},
			first:    syncdomain.ToolProjection{Description: stringPointer("New lookup")},
			second:   syncdomain.ToolProjection{Description: stringPointer("New refund")},
			want:     []string{`"description":"New lookup"`, `"description":"New refund"`},
		},
		{
			name: "YAML agents", file: "agents.yaml", format: "yaml",
			source:      "agents:\n  - name: support\n    prompt: Old support\n  - name: refunds\n    prompt: Old refunds\n",
			firstID:     syncdomain.VariationID("project", "support", "default"),
			secondID:    syncdomain.VariationID("project", "refunds", "default"),
			firstSelect: `agents[name="support"]`, secondSelect: `agents[name="refunds"]`,
			fieldMap: map[string]string{"prompt": "prompt"},
			first:    syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("New support")},
			second:   syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("New refunds")},
			want:     []string{"prompt: New support", "prompt: New refunds"},
		},
		{
			name: "TOML agents", file: "agents.toml", format: "toml",
			source:      "[agents.support]\nprompt = \"Old support\"\n[agents.refunds]\nprompt = \"Old refunds\"\n",
			firstID:     syncdomain.VariationID("project", "support", "default"),
			secondID:    syncdomain.VariationID("project", "refunds", "default"),
			firstSelect: "agents.support", secondSelect: "agents.refunds",
			fieldMap: map[string]string{"prompt": "prompt"},
			first:    syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("New support")},
			second:   syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("New refunds")},
			want:     []string{`prompt = "New support"`, `prompt = "New refunds"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, test.file, test.source)
			firstReference := Reference{
				File: test.file, Format: test.format, Select: test.firstSelect, Map: test.fieldMap,
			}
			secondReference := Reference{
				File: test.file, Format: test.format, Select: test.secondSelect, Map: test.fieldMap,
			}
			writeDescriptorForTest(t, root, test.firstID, firstReference)
			writeDescriptorForTest(t, root, test.secondID, secondReference)

			_, err := NewStore(root).ReplaceSources([]SourceReplacement{
				{ID: test.firstID, Reference: firstReference, Projection: test.first},
				{ID: test.secondID, Reference: secondReference, Projection: test.second},
			})

			require.NoError(t, err)
			content, err := os.ReadFile(filepath.Join(root, test.file))
			require.NoError(t, err)
			for _, expected := range test.want {
				assert.Contains(t, string(content), expected)
			}
		})
	}
}

func TestReplaceSourcesRejectsOverlappingEdits(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tool.json", `{"description":"Old"}`+"\n")
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"}
	reference := Reference{
		File: "tool.json", Format: "json", Select: "$",
		Map: map[string]string{"description": "description"},
	}
	writeDescriptorForTest(t, root, id, reference)
	description := "New"
	replacement := SourceReplacement{
		ID: id, Reference: reference, Projection: syncdomain.ToolProjection{Description: &description},
	}

	_, err := NewStore(root).ReplaceSources([]SourceReplacement{replacement, replacement})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorOverlappingEdits, problem.Code)
	content, readErr := os.ReadFile(filepath.Join(root, "tool.json"))
	require.NoError(t, readErr)
	assert.Equal(t, `{"description":"Old"}`+"\n", string(content))
}

func TestReplaceSourcesWritesNothingWhenOneFieldIsReadOnly(t *testing.T) {
	root := t.TempDir()
	original := `from agents import Agent
support = Agent(instructions="Help.")
dynamic = Agent(instructions=load_prompt())
`
	writeTestFile(t, root, "agents.py", original)
	supportID := syncdomain.VariationID("project", "support", "default")
	dynamicID := syncdomain.VariationID("project", "dynamic", "default")
	supportReference := Reference{
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	dynamicReference := Reference{
		File: "agents.py", Format: "python", Select: `agents[id="dynamic"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	writeDescriptorForTest(t, root, supportID, supportReference)
	writeDescriptorForTest(t, root, dynamicID, dynamicReference)
	supportPrompt, dynamicPrompt := "Changed.", "Cannot write."

	_, err := NewStore(root).ReplaceSources([]SourceReplacement{
		{
			ID: supportID, Reference: supportReference,
			Projection: syncdomain.VariationProjection{
				Mode: syncdomain.VariationModeAgent, Instructions: &supportPrompt,
			},
		},
		{
			ID: dynamicID, Reference: dynamicReference,
			Projection: syncdomain.VariationProjection{
				Mode: syncdomain.VariationModeAgent, Instructions: &dynamicPrompt,
			},
		},
	})
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorFieldReadOnly, problem.Code)
	content, readErr := os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, readErr)
	assert.Equal(t, original, string(content))
}

func TestReplaceSourcesRejectsMappedChangeAfterReview(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "agents.py", `from agents import Agent
support = Agent(instructions="Changed after review.")
`)
	id := syncdomain.VariationID("project", "support", "default")
	reference := Reference{
		File: "agents.py", Format: "python", Select: `agents[id="support"]`,
		Map: map[string]string{"prompt": "instructions"},
	}
	writeDescriptorForTest(t, root, id, reference)
	desired := "Server value."

	_, err := NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: reference,
		Projection: syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Instructions: &desired,
		},
		ExpectedFingerprint: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorStateChanged, problem.Code)
	content, readErr := os.ReadFile(filepath.Join(root, "agents.py"))
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "Changed after review.")
}

func TestCommitFilesRollsBackEarlierReplacement(t *testing.T) {
	root := t.TempDir()
	firstPath, secondPath := filepath.Join(root, "first.txt"), filepath.Join(root, "second.txt")
	require.NoError(t, os.WriteFile(firstPath, []byte("first original"), 0o644))
	require.NoError(t, os.WriteFile(secondPath, []byte("second original"), 0o644))
	firstStaged, err := writeTempFile(firstPath, []byte("first updated"), 0o644)
	require.NoError(t, err)
	secondStaged, err := writeTempFile(secondPath, []byte("second updated"), 0o644)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(firstStaged)
		_ = os.Remove(secondStaged)
	})
	files := []stagedFile{
		{
			displayPath: "first.txt", path: firstPath, original: []byte("first original"),
			originalExists: true, replacement: []byte("first updated"), mode: 0o644, stagedPath: firstStaged,
		},
		{
			displayPath: "second.txt", path: secondPath, original: []byte("second original"),
			originalExists: true, replacement: []byte("second updated"), mode: 0o644, stagedPath: secondStaged,
		},
	}

	err = commitFilesWithRename(files, func(source, destination string) error {
		if source == secondStaged {
			return errors.New("injected rename failure")
		}
		return os.Rename(source, destination)
	})

	require.ErrorContains(t, err, "injected rename failure")
	first, readErr := os.ReadFile(firstPath)
	require.NoError(t, readErr)
	second, readErr := os.ReadFile(secondPath)
	require.NoError(t, readErr)
	assert.Equal(t, "first original", string(first))
	assert.Equal(t, "second original", string(second))
}

func TestReplaceSourcesPullsRelationshipsAndMissingDefinitionAtomically(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	writeTestFile(t, root, descriptorPath,
		"# keep\nformatVersion: 1\nfile: prompt.md\nformat: markdown\n"+
			"tools:\n  - key: old\n# tail\n")
	old := []syncdomain.AttachmentRef{{Key: "old"}}
	next := []string{"new"}
	projection := syncdomain.VariationProjection{
		Mode:  syncdomain.VariationModeAgent,
		Tools: &next,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
		},
	}
	tool := syncdomain.Attachment{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "new", Schema: map[string]any{"type": "object"}},
	}

	paths, err := NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &old},
		Projection: projection, RelationshipAttachments: []syncdomain.Attachment{tool},
	}})

	require.NoError(t, err)
	assert.Contains(t, paths, descriptorPath)
	descriptor, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(descriptorPath)))
	require.NoError(t, err)
	assert.Contains(t, string(descriptor), "# keep\n")
	assert.Contains(t, string(descriptor), "tools:\n  - key: new\n# tail\n")
	require.FileExists(t, filepath.Join(root, ".launchdarkly/project/tools/new.json"))
}

func TestReplaceSourcesWritesNothingWhenRelationshipDependencyIsInvalid(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	original := "formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools: []\n"
	writeTestFile(t, root, descriptorPath, original)
	empty := []syncdomain.AttachmentRef{}
	next := []string{"new"}
	projection := syncdomain.VariationProjection{
		Mode: syncdomain.VariationModeAgent, Tools: &next,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
		},
	}

	_, err = NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &empty},
		Projection: projection,
		RelationshipAttachments: []syncdomain.Attachment{{
			Kind: syncdomain.AttachmentTool,
			Tool: &syncdomain.Tool{Key: "new"},
		}},
	}})

	require.ErrorContains(t, err, `tool "new" schema is required`)
	descriptor, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(descriptorPath)))
	require.NoError(t, readErr)
	assert.Equal(t, original, string(descriptor))
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly/project/tools/new.json"))
}

func TestReplaceSourcesAddsRelationshipNodesWithoutChangingOtherBytes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	original := "# keep\nformatVersion: 1\nfile: prompt.md\nformat: markdown"
	writeTestFile(t, root, descriptorPath, original)
	writeTestFile(t, root, ".launchdarkly/project/tools/search.json",
		`{"formatVersion":1,"key":"search","schema":{"type":"object"}}`)
	tools := []string{"search"}
	skills := []string{}
	projection := syncdomain.VariationProjection{
		Mode: syncdomain.VariationModeAgent, Tools: &tools, Skills: &skills,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldTools:  {Capability: syncdomain.CapabilityDirect, Known: true},
			syncdomain.FieldSkills: {Capability: syncdomain.CapabilityDirect, Known: true},
		},
	}

	_, err = NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown"},
		Projection: projection,
	}})

	require.NoError(t, err)
	descriptor, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(descriptorPath)))
	require.NoError(t, err)
	assert.Equal(t, original+"\ntools:\n  - key: search\nskills: []\n", string(descriptor))
}

func TestReplaceSourcesRejectsChangedDescriptorRelationships(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	original := "formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools:\n  - key: changed\n"
	writeTestFile(t, root, descriptorPath, original)
	old := []syncdomain.AttachmentRef{{Key: "old"}}
	next := []string{}
	projection := syncdomain.VariationProjection{
		Mode: syncdomain.VariationModeAgent, Tools: &next,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
		},
	}

	_, err = NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &old},
		Projection: projection,
	}})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorStateChanged, problem.Code)
	descriptor, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(descriptorPath)))
	require.NoError(t, readErr)
	assert.Equal(t, original, string(descriptor))
}

func TestReplaceSourcesNeedsContentForNewRelationshipDefinition(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	writeTestFile(t, root, descriptorPath,
		"formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools: []\n")
	empty := []syncdomain.AttachmentRef{}
	next := []string{"missing"}
	projection := syncdomain.VariationProjection{
		Mode: syncdomain.VariationModeAgent, Tools: &next,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
		},
	}

	_, err = NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &empty},
		Projection: projection,
	}})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorAttachmentDefinitionMissing, problem.Code)
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly/project/tools/missing.json"))
}

func writeDescriptorForTest(t *testing.T, root string, id syncdomain.ResourceID, reference Reference) {
	t.Helper()
	path, err := ReferencePath(id)
	require.NoError(t, err)
	content, err := RenderDescriptor(id, reference)
	require.NoError(t, err)
	writeTestFile(t, root, path, string(content))
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func stringPointer(value string) *string {
	return &value
}
