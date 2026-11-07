package local

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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
	assert.Contains(t, paths, ".launchdarkly/project/tools/new.json", "every reported path is relative to the repository")
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

func TestRenderAndWriteDescriptorRejectAnInvalidSourcePath(t *testing.T) {
	id := syncdomain.VariationID("project", "support", "default")
	for name, file := range map[string]string{
		"managed directory": ".launchdarkly/prompt.md",
		"unclean path":      "./prompt.md",
		"parent path":       "../prompt.md",
		"empty":             "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := RenderDescriptor(id, Reference{File: file, Format: "markdown"})
			require.Error(t, err)
			_, err = NewStore(t.TempDir()).WriteDescriptor(id, Reference{File: file, Format: "markdown"})
			require.Error(t, err)
		})
	}
}

func TestWriteDescriptorRejectsASymlinkedParentOnReplace(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	writeTestFile(t, external, "default.ref.yaml", "formatVersion: 1\nfile: old.md\nformat: markdown\n")
	configs := filepath.Join(root, ".launchdarkly", "project", "configs")
	require.NoError(t, os.MkdirAll(configs, 0o755))
	require.NoError(t, os.Symlink(external, filepath.Join(configs, "support")))

	_, err := NewStore(root).WriteDescriptor(id, Reference{File: "prompt.md", Format: "markdown"})

	require.ErrorContains(t, err, "symbolic links are not supported")
	data, err := os.ReadFile(filepath.Join(external, "default.ref.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "file: old.md", "the file outside the managed tree is unchanged")
}

func TestAddKeepsALinkedToolThatTheVariationUses(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tool.json", `{"description":"Linked","schema":{"type":"object"}}`)
	writeTestFile(t, root, ".launchdarkly/project/tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	search := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default", Instructions: "Help.",
		Tools:       []syncdomain.AttachmentRef{{Key: "search"}},
		Attachments: []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &search}},
	}

	creation, err := NewStore(root).Add([]VariationFile{{ProjectKey: "project", ConfigKey: "config", Variation: variation}})

	require.NoError(t, err)
	assert.Equal(t, []string{"project/configs/config/default.prompt.md"}, creation.CreatedPaths)
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "tools", "search.json"))
	resources, err := CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "Linked", *resources[0].Variation.Attachments[0].Tool.Description)
}

func TestCompileSourceRejectsSkillsOnACompletionSource(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompts.py", "from langchain_core.prompts import ChatPromptTemplate\n"+
		"chat = ChatPromptTemplate.from_messages([(\"human\", \"Hi\")])\n")
	writeTestFile(t, root, ".launchdarkly/project/configs/support/default.ref.yaml",
		"formatVersion: 1\nfile: prompts.py\nformat: python\nselect: 'prompts[id=\"chat\"]'\n"+
			"map:\n  messages: messages\nskills:\n  - key: refunds\n")

	_, err := CompileWorkspace(root)

	require.ErrorContains(t, err, "do not fit the completion source")
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorInvalidReference, problem.Code)
}

func TestStageSourceDocumentFollowsARepositoryLink(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lib/prompt.md", "Old\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "lib"), filepath.Join(root, "linked")))

	staged, err := NewStore(root).stageSourceDocument([]string{"linked/prompt.md"}, []byte("Old\n"), []byte("New\n"))

	require.NoError(t, err)
	assert.Equal(t, "linked/prompt.md", staged.displayPath)
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, "lib", "prompt.md"))
	require.NoError(t, err)
	assert.Equal(t, resolved, staged.path, "the write targets the regular file that compile read")
}

func TestSourcePathsStayOutsideTheManagedDirectoryAfterResolution(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".launchdarkly/sync.lock", "formatVersion: 1\n")
	require.NoError(t, os.Symlink(filepath.Join(root, ".launchdarkly"), filepath.Join(root, "managed")))

	_, err := ReadSourceFile(root, "managed/sync.lock")
	require.ErrorContains(t, err, "resolves inside .launchdarkly")
	assert.Error(t, validateSourcePath(".LaunchDarkly/sync.lock"), "the check ignores case")
	assert.NoError(t, validateSourcePath("launchdarkly/notes.md"), "a similar name outside the directory is fine")
}

func TestRelationshipPullRejectsASymlinkedDescriptorParent(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help\n")
	writeTestFile(t, external, "default.ref.yaml", "formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools:\n  - key: old\n")
	configs := filepath.Join(root, ".launchdarkly", "project", "configs")
	require.NoError(t, os.MkdirAll(configs, 0o755))
	require.NoError(t, os.Symlink(external, filepath.Join(configs, "support")))
	id := syncdomain.VariationID("project", "support", "default")
	projection := syncdomain.VariationProjection{
		Tools: &[]string{"new"},
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
		},
	}
	old := []syncdomain.AttachmentRef{{Key: "old"}}

	_, _, err := NewStore(root).prepareDescriptorRelationshipReplacement(SourceReplacement{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &old}, Projection: projection,
	})

	require.ErrorContains(t, err, "symbolic links are not supported")
}

func TestPublishSharesOneSnapshotForTwoNamesOfOneFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lib/prompts.json", `{"first": "one", "second": "two"}`)
	require.NoError(t, os.Symlink(filepath.Join(root, "lib"), filepath.Join(root, "linked")))
	first := syncdomain.VariationID("project", "support", "first")
	second := syncdomain.VariationID("project", "support", "second")
	store := NewStore(root)
	_, err := store.WriteDescriptor(first, Reference{File: "lib/prompts.json", Format: "json", Select: "$", Map: map[string]string{"prompt": "first"}})
	require.NoError(t, err)
	_, err = store.WriteDescriptor(second, Reference{File: "linked/prompts.json", Format: "json", Select: "$", Map: map[string]string{"prompt": "second"}})
	require.NoError(t, err)
	resources, err := CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 2)
	replacements := make([]SourceReplacement, 0, 2)
	for _, resource := range resources {
		projection := resource.Projection.(syncdomain.VariationProjection)
		next := projection.Instructions
		*next = *next + " updated"
		replacements = append(replacements, SourceReplacement{
			ID: resource.ID(), Reference: *resource.Source, Projection: projection,
		})
	}

	paths, err := store.ReplaceSources(replacements)

	require.NoError(t, err)
	assert.Len(t, paths, 1, "one file is written once")
	data, err := os.ReadFile(filepath.Join(root, "lib", "prompts.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"first": "one updated", "second": "two updated"}`, string(data))
}

func TestStageSourceDocumentRejectsAResolvedGeneratedPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "vendor/prompt.md", "Old\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "vendor"), filepath.Join(root, "authored")))

	_, err := NewStore(root).stageSourceDocument([]string{"authored/prompt.md"}, []byte("Old\n"), []byte("New\n"))

	require.ErrorContains(t, err, "generated, vendored, or minified")
}

func TestRelationshipPullMovesATrailingCommentWithTheList(t *testing.T) {
	original := []byte("formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools:\n  - key: old\n  # keep this tool list short\nskills: []\n")

	updated, err := updateDescriptorRelationships(original, []syncdomain.AttachmentRef{{Key: "new"}}, true, nil, false)

	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(updated), "# keep this tool list short"), "the comment appears once")
	assert.Contains(t, string(updated), "key: new")
	assert.NotContains(t, string(updated), "key: old")
}

func TestRestoreDescriptorPutsThePreviousContentBackOrRemovesIt(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	id := syncdomain.VariationID("project", "support", "default")
	writeTestFile(t, root, "prompt.md", "Help\n")
	path, err := store.WriteDescriptor(id, Reference{File: "prompt.md", Format: "markdown"})
	require.NoError(t, err)
	previous, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)

	_, err = store.WriteDescriptor(id, Reference{File: "prompt.md", Format: "markdown", Select: "$"})
	require.NoError(t, err)
	written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	require.NoError(t, store.RestoreDescriptor(id, previous, written))
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	assert.Equal(t, previous, current)

	require.NoError(t, store.RestoreDescriptor(id, nil, []byte("other content\n")))
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(path)), "a descriptor with other content than the link wrote stays")

	require.NoError(t, store.RestoreDescriptor(id, nil, previous))
	require.NoFileExists(t, filepath.Join(root, filepath.FromSlash(path)))
}

func TestSourcePathsRejectWindowsAbsolutePaths(t *testing.T) {
	for _, file := range []string{`C:/prompts/prompt.md`, `c:prompt.md`, `prompts\prompt.md`, `\\server\share\prompt.md`} {
		assert.Error(t, validateSourcePath(file), file)
	}
	assert.NoError(t, validateSourcePath("prompts/prompt.md"))
}

func TestReplaceVariationsKeepsALinkedToolDefinition(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tool.json", `{"description":"Linked","schema":{"type":"object"}}`)
	writeTestFile(t, root, ".launchdarkly/project/tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n  schema: schema\n")
	search := syncdomain.Tool{Key: "search", Description: new(string), Schema: map[string]any{"type": "object"}}
	*search.Description = "From LaunchDarkly"
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default", Instructions: "Help.",
		Tools:       []syncdomain.AttachmentRef{{Key: "search"}},
		Attachments: []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &search}},
	}

	_, err := NewStore(root).ReplaceVariations([]VariationReplacement{{
		ProjectKey: "project", ConfigKey: "config", CreateIfMissing: true, Variation: variation,
	}})

	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "tools", "search.json"), "the descriptor stays the one definition")
	resources, err := CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
}

func TestReplaceVariationsRejectsAManagedFileBesideADescriptor(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help\n")
	writeTestFile(t, root, ".launchdarkly/project/configs/config/default.ref.yaml", "formatVersion: 1\nfile: prompt.md\nformat: markdown\n")
	variation := syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default", Instructions: "Help."}

	_, err := NewStore(root).ReplaceVariations([]VariationReplacement{{
		ProjectKey: "project", ConfigKey: "config", CreateIfMissing: true, Variation: variation,
	}})

	require.ErrorContains(t, err, "already has a descriptor")
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "configs", "config", "default.prompt.md"))
}

func TestReplaceAttachmentsRejectsAManagedFileBesideADescriptor(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}
	writeTestFile(t, root, "tool.json", `{"description":"Linked","schema":{"type":"object"}}`)
	writeDescriptorForTest(t, root, id, Reference{
		File: "tool.json", Format: "json", Select: "$", Map: map[string]string{"description": "description", "schema": "schema"},
	})
	writeTestFile(t, root, ".launchdarkly/project/tools/search.json", `{"formatVersion": 1, "key": "search", "schema": {}}`)
	description := "Server"
	search := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: 2, Tool: &syncdomain.Tool{
		Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
	}}

	_, err := NewStore(root).ReplaceAttachments([]syncdomain.SyncedAttachment{{ProjectKey: "project", Attachment: search}})

	require.ErrorContains(t, err, "has a managed file and a descriptor")
	data, readErr := os.ReadFile(filepath.Join(root, ".launchdarkly", "project", "tools", "search.json"))
	require.NoError(t, readErr)
	assert.NotContains(t, string(data), "Server", "the managed file is not written")
}

func TestAddRejectsAToolManagedFileBesideADescriptor(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}
	writeTestFile(t, root, "tool.json", `{"description":"Linked","schema":{"type":"object"}}`)
	writeDescriptorForTest(t, root, id, Reference{
		File: "tool.json", Format: "json", Select: "$", Map: map[string]string{"description": "description", "schema": "schema"},
	})
	writeTestFile(t, root, ".launchdarkly/project/tools/search.json", `{"formatVersion": 1, "key": "search", "schema": {}}`)
	variation := syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default"}
	variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	variation.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Schema: map[string]any{"type": "object"},
	}}}

	_, err := NewStore(root).Add([]VariationFile{{ProjectKey: "project", ConfigKey: "config", Variation: variation}})

	require.ErrorContains(t, err, "has a managed file and a descriptor")
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "configs", "config", "default.prompt.md"), "nothing is created")
}

func TestReplaceAttachmentsSkipsALeftoverLinkBesideADescriptor(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}
	writeTestFile(t, root, "tool.json", `{"description":"Linked","schema":{"type":"object"}}`)
	writeDescriptorForTest(t, root, id, Reference{
		File: "tool.json", Format: "json", Select: "$", Map: map[string]string{"description": "description", "schema": "schema"},
	})
	writeTestFile(t, root, "external.json", `{"formatVersion": 1, "key": "search", "schema": {}}`)
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	target := filepath.Join(root, filepath.FromSlash(managedPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "external.json"), target))
	description := "Server"
	search := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: 2, Tool: &syncdomain.Tool{
		Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
	}}

	written, err := NewStore(root).ReplaceAttachments([]syncdomain.SyncedAttachment{{ProjectKey: "project", Attachment: search}})

	require.NoError(t, err, "a leftover link is not a definition, so the descriptor keeps the one definition")
	assert.Empty(t, written)
	info, err := os.Lstat(target)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link stays")
}

func TestReplaceVariationsTreatsALeftoverLinkAsAbsent(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	writeTestFile(t, root, "prompt.md", "Help.\n")
	writeDescriptorForTest(t, root, id, Reference{File: "prompt.md", Format: "markdown"})
	writeTestFile(t, root, "external.md", "---\nformatVersion: 1\nmode: agent\nkey: default\n---\n\nOther.\n")
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	target := filepath.Join(root, filepath.FromSlash(managedPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "external.md"), target))

	_, err = NewStore(root).ReplaceVariations([]VariationReplacement{{
		ProjectKey: "project", ConfigKey: "support", CreateIfMissing: true,
		Variation: syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default"},
	}})

	require.ErrorContains(t, err, "already has a descriptor", "the link is absent, so the descriptor is the one definition")
}

func TestReplaceVariationsWritesOverALeftoverLink(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	writeTestFile(t, root, "external.md", "---\nformatVersion: 1\nmode: agent\nkey: default\n---\n\nOther.\n")
	managedPath, err := ResourcePath(id)
	require.NoError(t, err)
	target := filepath.Join(root, filepath.FromSlash(managedPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "external.md"), target))

	written, err := NewStore(root).ReplaceVariations([]VariationReplacement{{
		ProjectKey: "project", ConfigKey: "support", CreateIfMissing: true,
		Variation: syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default", Instructions: "Written."},
	}})

	require.NoError(t, err, "a leftover link without a descriptor is absent, so the create writes the file")
	require.Len(t, written, 1)
	info, err := os.Lstat(target)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "the managed file replaces the link")
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Written.")
	external, err := os.ReadFile(filepath.Join(root, "external.md"))
	require.NoError(t, err)
	assert.Contains(t, string(external), "Other.", "the target of the link is not written")
}

func TestReplaceFilesStopsWhenASourceLinkWasRetargeted(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lib/old.md", "old")
	writeTestFile(t, root, "lib/new.md", "new")
	// The snapshot resolved the link to the old file. The link now names
	// the new file.
	require.NoError(t, os.Symlink(filepath.Join(root, "lib", "new.md"), filepath.Join(root, "prompt.md")))

	err := replaceFiles([]stagedFile{{
		displayPath: "prompt.md", path: filepath.Join(root, "lib", "old.md"), original: []byte("old"), originalExists: true,
		replacement: []byte("edited"), mode: 0o644, sources: []string{"prompt.md"}, root: root,
	}})

	require.ErrorContains(t, err, "resolves to another file")
	data, readErr := os.ReadFile(filepath.Join(root, "lib", "old.md"))
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(data), "the file that the name no longer names is not written")
}

func TestReplaceFilesVerifiesAFileThatItDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	written := filepath.Join(root, "written.txt")
	guarded := filepath.Join(root, "guarded.txt")
	require.NoError(t, os.WriteFile(written, []byte("old"), 0o644))
	require.NoError(t, os.WriteFile(guarded, []byte("current"), 0o644))

	err := replaceFiles([]stagedFile{
		{displayPath: "written.txt", path: written, original: []byte("old"), originalExists: true, replacement: []byte("new"), mode: 0o644},
		{displayPath: "guarded.txt", path: guarded, original: []byte("checked"), originalExists: true, verifyOnly: true},
	})

	require.ErrorContains(t, err, "guarded.txt changed while syncing")
	data, readErr := os.ReadFile(written)
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(data), "nothing is written when a guarded file changed")

	err = replaceFiles([]stagedFile{
		{displayPath: "written.txt", path: written, original: []byte("old"), originalExists: true, replacement: []byte("new"), mode: 0o644},
		{displayPath: "guarded.txt", path: guarded, original: []byte("current"), originalExists: true, verifyOnly: true},
	})

	require.NoError(t, err)
	data, readErr = os.ReadFile(written)
	require.NoError(t, readErr)
	assert.Equal(t, "new", string(data))
	data, readErr = os.ReadFile(guarded)
	require.NoError(t, readErr)
	assert.Equal(t, "current", string(data), "a guarded file is not written")
}

func TestWriteDescriptorFromStopsWhenTheDescriptorChanged(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	store := NewStore(root)
	id := syncdomain.VariationID("project", "support", "default")
	first := Reference{File: "prompt.md", Format: "markdown"}
	second := Reference{File: "prompt.md", Format: "markdown", Select: "$"}

	_, err := store.WriteDescriptorFrom(id, first, nil)
	require.NoError(t, err, "a create with no previous descriptor")
	path, err := ReferencePath(id)
	require.NoError(t, err)
	written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)

	_, err = store.WriteDescriptorFrom(id, second, nil)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok, "a create over a descriptor that appeared is a problem")
	assert.Equal(t, syncdomain.ErrorStateChanged, problem.Code)

	_, err = store.WriteDescriptorFrom(id, second, []byte("other content\n"))
	problem, ok = syncdomain.ProblemFrom(err)
	require.True(t, ok, "a replace of a descriptor with other content is a problem")
	assert.Equal(t, syncdomain.ErrorStateChanged, problem.Code)
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	assert.Equal(t, written, current, "the descriptor is not written")

	_, err = store.WriteDescriptorFrom(id, second, written)
	require.NoError(t, err, "a replace of the expected content proceeds")
	current, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	assert.Contains(t, string(current), "select: $")
}

func TestSourceBindingsSkipAnUnreadableDescriptor(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	writeDescriptorForTest(t, root, id, Reference{File: "prompt.md", Format: "markdown"})
	toolPath, err := ReferencePath(syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "broken"})
	require.NoError(t, err)
	writeTestFile(t, root, toolPath, "invalid: [\n")

	bindings, err := NewStore(root).sourceBindings()

	require.NoError(t, err, "an unreadable descriptor of another resource does not block a publish")
	require.Len(t, bindings, 1)
	assert.Equal(t, id, bindings[0].id)
}

func TestReplaceSourcesRejectsADescriptorChangedAfterReview(t *testing.T) {
	root := t.TempDir()
	original := `{"first": "one", "second": "two"}`
	writeTestFile(t, root, "prompts.json", original)
	id := syncdomain.VariationID("project", "support", "default")
	reviewed := Reference{File: "prompts.json", Format: "json", Select: "$", Map: map[string]string{"prompt": "first"}}
	writeDescriptorForTest(t, root, id, reviewed)
	prompt := "changed"
	replacement := SourceReplacement{
		ID: id, Reference: reviewed, Projection: syncdomain.VariationProjection{Instructions: &prompt},
	}

	retargeted := reviewed
	retargeted.Map = map[string]string{"prompt": "second"}
	writeDescriptorForTest(t, root, id, retargeted)
	_, err := NewStore(root).ReplaceSources([]SourceReplacement{replacement})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorStateChanged, problem.Code)
	assert.Equal(t, "the source descriptor changed after review", problem.Message)
	content, err := os.ReadFile(filepath.Join(root, "prompts.json"))
	require.NoError(t, err)
	assert.Equal(t, original, string(content), "a retargeted descriptor writes nothing")

	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(root, descriptorPath)))
	_, err = NewStore(root).ReplaceSources([]SourceReplacement{replacement})
	problem, ok = syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorStateChanged, problem.Code, "a removed descriptor writes nothing")
}

func TestMissingRelationshipAttachmentsReportAnInvalidKey(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	tools := []string{"../escape"}

	_, err := NewStore(root).prepareMissingRelationshipAttachments([]SourceReplacement{{
		ID: id,
		Projection: syncdomain.VariationProjection{
			Tools: &tools,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	}})

	require.Error(t, err, "an invalid key is reported instead of treated as a present definition")
}

func TestSourceFilesIncludeTheResolvedTargetOfALink(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lib/prompts.json", `{"prompt": "one"}`)
	require.NoError(t, os.Symlink(filepath.Join(root, "lib"), filepath.Join(root, "linked")))
	id := syncdomain.VariationID("project", "support", "default")
	writeDescriptorForTest(t, root, id, Reference{
		File: "linked/prompts.json", Format: "json", Select: "$", Map: map[string]string{"prompt": "prompt"},
	})

	files, err := SourceFiles(root)

	require.NoError(t, err)
	assert.Contains(t, files, "linked/prompts.json")
	assert.Contains(t, files, "lib/prompts.json", "an edit to the file behind the link changes the plan")
}

func TestRestoreDescriptorAcceptsAMissingNewDescriptor(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")

	err := NewStore(root).RestoreDescriptor(id, nil, nil)

	require.NoError(t, err, "a link that wrote nothing has nothing to remove")
}

func TestReferencePathRejectsAnUnsafeVariationKey(t *testing.T) {
	_, err := ReferencePath(syncdomain.VariationID("project", "..", "default"))
	require.ErrorContains(t, err, "invalid config key")
	_, err = ReferencePath(syncdomain.VariationID("project", "support", ".."))
	require.ErrorContains(t, err, "invalid variation key")
}

func TestDescriptorRelationshipsOnOneLineAreRenderedAgain(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	writeTestFile(t, root, descriptorPath,
		"{formatVersion: 1, file: prompt.md, format: markdown, tools: [{key: old}], skills: [{key: triage}]}\n")
	oldTools := []syncdomain.AttachmentRef{{Key: "old"}}
	oldSkills := []syncdomain.AttachmentRef{{Key: "triage"}}
	tools, skills := []string{"new"}, []string{"summarize"}
	direct := syncdomain.FieldState{Capability: syncdomain.CapabilityDirect, Known: true}

	staged, changed, err := NewStore(root).prepareDescriptorRelationshipReplacement(SourceReplacement{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &oldTools, Skills: &oldSkills},
		Projection: syncdomain.VariationProjection{
			Tools: &tools, Skills: &skills,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldTools: direct, syncdomain.FieldSkills: direct},
		},
	})

	require.NoError(t, err)
	require.True(t, changed)
	descriptor, err := ReadDescriptor(fstest.MapFS{"d.ref.yaml": {Data: staged.replacement}}, "d.ref.yaml", id)
	require.NoError(t, err)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "new"}}, *descriptor.Tools)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "summarize"}}, *descriptor.Skills)
	assert.Equal(t, "prompt.md", descriptor.File)
}

func TestDescriptorRelationshipEditThatDoesNotReadBackIsRenderedAgain(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	// The skills list is an alias of the tools list, so an in-place edit of
	// the tools lines loses the anchor and the file does not decode.
	writeTestFile(t, root, descriptorPath,
		"formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools: &shared\n  - key: old\nskills: *shared\n")
	oldRefs := []syncdomain.AttachmentRef{{Key: "old"}}
	tools := []string{"new"}
	direct := syncdomain.FieldState{Capability: syncdomain.CapabilityDirect, Known: true}

	staged, changed, err := NewStore(root).prepareDescriptorRelationshipReplacement(SourceReplacement{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &oldRefs, Skills: &oldRefs},
		Projection: syncdomain.VariationProjection{
			Tools:       &tools,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldTools: direct},
		},
	})

	require.NoError(t, err)
	require.True(t, changed)
	descriptor, err := ReadDescriptor(fstest.MapFS{"d.ref.yaml": {Data: staged.replacement}}, "d.ref.yaml", id)
	require.NoError(t, err, "the staged descriptor reads back")
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "new"}}, *descriptor.Tools)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "old"}}, *descriptor.Skills, "the skills keep the value that the alias had")
}

func TestReplaceSourcesAcceptsRelationshipsInServerOrder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	writeDescriptorForTest(t, root, id, Reference{File: "prompt.md", Format: "markdown"})
	next := []string{"zeta", "alpha"}
	attachments := []syncdomain.Attachment{
		{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "zeta", Schema: map[string]any{"type": "object"}}},
		{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "alpha", Schema: map[string]any{"type": "object"}}},
	}

	paths, err := NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown"},
		Projection: syncdomain.VariationProjection{
			Mode:  syncdomain.VariationModeAgent,
			Tools: &next,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
		RelationshipAttachments: attachments,
	}})

	require.NoError(t, err, "the order of the tools in LaunchDarkly does not fail the publish")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	assert.Contains(t, paths, descriptorPath)
	descriptor, err := ReadDescriptor(os.DirFS(root), descriptorPath, id)
	require.NoError(t, err)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "alpha"}, {Key: "zeta"}}, *descriptor.Tools)
}

func TestMissingRelationshipAttachmentDoesNotTreatASymlinkAsADefinition(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	writeDescriptorForTest(t, root, id, Reference{File: "prompt.md", Format: "markdown"})
	writeTestFile(t, root, "elsewhere.json", `{"formatVersion": 1, "key": "new", "schema": {"type": "object"}}`)
	toolPath, err := ResourcePath(syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "new"})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, toolPath)), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "elsewhere.json"), filepath.Join(root, toolPath)))
	next := []string{"new"}

	_, err = NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown"},
		Projection: syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Tools: &next,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
		RelationshipAttachments: []syncdomain.Attachment{{
			Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "new", Schema: map[string]any{"type": "object"}},
		}},
	}})

	require.ErrorContains(t, err, "symbolic link", "a link at the managed path is not a definition")
}

func TestDescriptorRelationshipEditChecksTheMappingOfTheDescriptorItReads(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	writeTestFile(t, root, descriptorPath,
		"formatVersion: 1\nfile: other.md\nformat: markdown\ntools:\n  - key: old\n")
	old := []syncdomain.AttachmentRef{{Key: "old"}}
	next := []string{"new"}

	_, _, err = NewStore(root).prepareDescriptorRelationshipReplacement(SourceReplacement{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &old},
		Projection: syncdomain.VariationProjection{
			Tools: &next,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorStateChanged, problem.Code, "a descriptor that names another file is not edited")
}

func TestCheckSourcesAcceptsRelationshipsBeforeTheDescriptorExists(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	next := []string{"new"}

	err := NewStore(root).CheckSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown"},
		Projection: syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Tools: &next,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
		RelationshipAttachments: []syncdomain.Attachment{{
			Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "new", Schema: map[string]any{"type": "object"}},
		}},
	}})

	require.NoError(t, err, "a link checks its plan before it writes the descriptor")
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "tools", "new.json"), "a check writes nothing")
}

func TestSourceFilesLeaveOutALinkTargetInTheManagedDirectory(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".launchdarkly/sync.lock", "formatVersion: 1\n")
	require.NoError(t, os.Symlink(filepath.Join(root, ".launchdarkly", "sync.lock"), filepath.Join(root, "linked.md")))
	id := syncdomain.VariationID("project", "support", "default")
	writeDescriptorForTest(t, root, id, Reference{File: "linked.md", Format: "markdown"})

	files, err := SourceFiles(root)

	require.NoError(t, err)
	assert.Contains(t, files, "linked.md")
	assert.NotContains(t, files, ".launchdarkly/sync.lock", "a write of the lock must not start a sync again")
}

func TestReplaceSourcesRejectsTwoBodiesForOneNewAttachment(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "first.md", "Help.\n")
	writeTestFile(t, root, "second.md", "Assist.\n")
	first := syncdomain.VariationID("project", "support", "first")
	second := syncdomain.VariationID("project", "support", "second")
	writeDescriptorForTest(t, root, first, Reference{File: "first.md", Format: "markdown"})
	writeDescriptorForTest(t, root, second, Reference{File: "second.md", Format: "markdown"})
	next := []string{"new"}
	replacement := func(id syncdomain.ResourceID, file, description string) SourceReplacement {
		description = strings.Clone(description)
		return SourceReplacement{
			ID: id, Reference: Reference{File: file, Format: "markdown"},
			Projection: syncdomain.VariationProjection{
				Mode: syncdomain.VariationModeAgent, Tools: &next,
				FieldStates: map[syncdomain.Field]syncdomain.FieldState{
					syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
				},
			},
			RelationshipAttachments: []syncdomain.Attachment{{
				Kind: syncdomain.AttachmentTool,
				Tool: &syncdomain.Tool{Key: "new", Description: &description, Schema: map[string]any{"type": "object"}},
			}},
		}
	}

	_, err := NewStore(root).ReplaceSources([]SourceReplacement{
		replacement(first, "first.md", "One body"), replacement(second, "second.md", "Another body"),
	})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorDefinitionConflict, problem.Code, "one publish writes one definition")
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "tools", "new.json"))
}

func TestFlowDescriptorKeepsItsMappingWhenOneListChanges(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	writeTestFile(t, root, descriptorPath, "{formatVersion: 1, file: prompt.md, format: markdown, tools: [{key: old}]}\n")
	oldTools := []syncdomain.AttachmentRef{{Key: "old"}}
	tools := []string{"new"}

	staged, changed, err := NewStore(root).prepareDescriptorRelationshipReplacement(SourceReplacement{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown", Tools: &oldTools},
		Projection: syncdomain.VariationProjection{
			Tools: &tools,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	})

	require.NoError(t, err)
	require.True(t, changed)
	descriptor, err := ReadDescriptor(fstest.MapFS{"d.ref.yaml": {Data: staged.replacement}}, "d.ref.yaml", id)
	require.NoError(t, err, "the descriptor is rendered again instead of edited in place: %s", staged.replacement)
	assert.Equal(t, "prompt.md", descriptor.File, "the mapping survives a change to one list")
	assert.Equal(t, "markdown", descriptor.Format)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "new"}}, *descriptor.Tools)
}

func TestMissingRelationshipAttachmentKeepsADescriptorBesideALeftoverLink(t *testing.T) {
	root := t.TempDir()
	tool := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}
	writeTestFile(t, root, "tool.json", `{"description":"Linked","schema":{"type":"object"}}`)
	writeDescriptorForTest(t, root, tool, Reference{
		File: "tool.json", Format: "json", Select: "$", Map: map[string]string{"description": "description", "schema": "schema"},
	})
	writeTestFile(t, root, "external.json", `{"formatVersion": 1, "key": "search", "schema": {}}`)
	managedPath, err := ResourcePath(tool)
	require.NoError(t, err)
	target := filepath.Join(root, filepath.FromSlash(managedPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "external.json"), target))
	id := syncdomain.VariationID("project", "support", "default")
	tools := []string{"search"}
	direct := syncdomain.FieldState{Capability: syncdomain.CapabilityDirect, Known: true}

	staged, err := NewStore(root).prepareMissingRelationshipAttachments([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown"},
		Projection: syncdomain.VariationProjection{
			Tools:       &tools,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldTools: direct},
		},
	}})

	require.NoError(t, err, "the descriptor defines the tool, and the leftover link does not stop the pull")
	assert.Empty(t, staged)
}

func TestMissingRelationshipAttachmentRejectsASymlinkAtTheDescriptorPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Help.\n")
	id := syncdomain.VariationID("project", "support", "default")
	writeDescriptorForTest(t, root, id, Reference{File: "prompt.md", Format: "markdown"})
	writeTestFile(t, root, "elsewhere.ref.yaml", "formatVersion: 1\nfile: tool.json\nformat: json\n")
	toolDescriptor, err := ReferencePath(syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "new"})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, toolDescriptor)), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "elsewhere.ref.yaml"), filepath.Join(root, toolDescriptor)))
	next := []string{"new"}

	_, err = NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Reference: Reference{File: "prompt.md", Format: "markdown"},
		Projection: syncdomain.VariationProjection{
			Mode: syncdomain.VariationModeAgent, Tools: &next,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
		RelationshipAttachments: []syncdomain.Attachment{{
			Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "new", Schema: map[string]any{"type": "object"}},
		}},
	}})

	require.ErrorContains(t, err, "symbolic link", "a link at the descriptor path stops the pull")
	require.NoFileExists(t, filepath.Join(root, ".launchdarkly", "project", "tools", "new.json"))
}

func TestCompileSourceRejectsALinkIntoAVendoredTree(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "vendor/lib/prompt.md", "Vendored.\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "vendor", "lib", "prompt.md"), filepath.Join(root, "prompt.md")))
	id := syncdomain.VariationID("project", "support", "default")

	_, err := CompileSource(root, id, Reference{File: "prompt.md", Format: "markdown"})

	require.ErrorContains(t, err, "generated, vendored, or minified", "the resolved path counts at compile time too")
}

func TestReplaceFilesChecksEveryNameOfASharedFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lib/prompts.md", "old")
	writeTestFile(t, root, "other/prompts.md", "other")
	// Two descriptors named one file, once directly and once through a
	// link. The link now names another file.
	require.NoError(t, os.Symlink(filepath.Join(root, "other"), filepath.Join(root, "linked")))
	path, err := resolveSourcePath(root, "lib/prompts.md")
	require.NoError(t, err)

	err = replaceFiles([]stagedFile{{
		displayPath: "lib/prompts.md", path: path,
		original: []byte("old"), originalExists: true, replacement: []byte("edited"), mode: 0o644,
		sources: []string{"lib/prompts.md", "linked/prompts.md"}, root: root,
	}})

	require.ErrorContains(t, err, "source linked/prompts.md changed while syncing")
	data, readErr := os.ReadFile(filepath.Join(root, "lib", "prompts.md"))
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(data), "the file that the second name no longer names is not written")
}

func TestPrepareSourceReplacementsKeepsEveryNameOfASharedFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lib/prompts.py", "A = \"Original A\"\nB = \"Original B\"\n")
	require.NoError(t, os.Symlink(filepath.Join(root, "lib"), filepath.Join(root, "linked")))
	idA := syncdomain.VariationID("project", "support", "a")
	idB := syncdomain.VariationID("project", "support", "b")
	referenceA := Reference{File: "lib/prompts.py", Format: "python", Select: `constants[id="A"]`, Map: map[string]string{"prompt": "$"}}
	referenceB := Reference{File: "linked/prompts.py", Format: "python", Select: `constants[id="B"]`, Map: map[string]string{"prompt": "$"}}
	writeDescriptorForTest(t, root, idA, referenceA)
	writeDescriptorForTest(t, root, idB, referenceB)

	staged, err := NewStore(root).prepareSourceReplacements([]SourceReplacement{
		{ID: idA, Reference: referenceA,
			Projection: syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("Updated A")}},
		{ID: idB, Reference: referenceB,
			Projection: syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: stringPointer("Updated B")}},
	})

	require.NoError(t, err)
	require.Len(t, staged, 1, "two names of one file stage one write")
	assert.ElementsMatch(t, []string{"lib/prompts.py", "linked/prompts.py"}, staged[0].sources, "the commit checks both names")
	assert.Equal(t, "A = \"Updated A\"\nB = \"Updated B\"\n", string(staged[0].replacement))
}
