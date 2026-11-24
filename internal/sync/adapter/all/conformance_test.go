package all

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/adaptertest"
)

func TestRegisteredFormatsReadAndWriteRealisticSources(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		realPath   string
		format     string
		id         syncdomain.ResourceID
		selectPath string
		fieldMap   map[string]string
		want       string
		update     string
	}{
		{
			name: "Markdown section", fixture: "markdown/agents-instructions.md.golden", format: "markdown",
			id:         syncdomain.VariationID("project", "support", "default"),
			selectPath: `sections[heading="Support agent"]`, fieldMap: map[string]string{"prompt": "body"},
			want: "Ask for an order ID before discussing an order.", update: "Ask for an order ID and region.",
		},
		{
			name: "Agent Skill", fixture: "agent-skill/refunds/skill-instructions.md.golden",
			realPath: ".agents/skills/refunds/SKILL.md", format: "agent-skill",
			id:   syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"},
			want: "Explain the refund policy and escalation rules.", update: "Explain refunds and escalation rules.",
		},
		{
			name: "Python agent", fixture: "python/agents.py", format: "python",
			id:         syncdomain.VariationID("project", "support", "default"),
			selectPath: `agents[id="support"]`, fieldMap: map[string]string{"prompt": "instructions"},
			want: "Ask for an order ID before discussing an order.", update: "Ask for an order ID and region.",
		},
		{
			name: "TypeScript MCP tool", fixture: "typescript/server.ts", format: "typescript",
			id:         syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup_order"},
			selectPath: `tools[id="lookup_order"]`, fieldMap: map[string]string{"description": "description"},
			want: "Look up one order.", update: "Look up one order and status.",
		},
		{
			name: "JavaScript MCP tool", fixture: "javascript/server.js", format: "javascript",
			id:         syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "weather"},
			selectPath: `tools[id="weather"]`, fieldMap: map[string]string{"description": "description"},
			want: "Return the weather for one city.", update: "Return weather and alerts.",
		},
		{
			name: "JSON tool", fixture: "json/mcp.json", format: "json",
			id:         syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "lookup_order"},
			selectPath: `tools[name="lookup_order"]`, fieldMap: map[string]string{"description": "description"},
			want: "Look up one order.", update: "Look up one order and status.",
		},
		{
			name: "YAML agent", fixture: "yaml/agents.yaml", format: "yaml",
			id:         syncdomain.VariationID("project", "support", "default"),
			selectPath: `agents[name="support"]`, fieldMap: map[string]string{"prompt": "prompt"},
			want: "Ask for an order ID.", update: "Ask for an order ID and region.",
		},
		{
			name: "TOML agent", fixture: "toml/pyproject.toml", format: "toml",
			id:         syncdomain.VariationID("project", "support", "default"),
			selectPath: "tool.agent.support", fieldMap: map[string]string{"prompt": "prompt"},
			want: "Ask for an order ID.\nKeep the answer short.", update: "Ask for an order ID and region.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := fixtureFile(t, test.fixture)
			documentPath := test.fixture
			if test.realPath != "" {
				documentPath, source = materializeFixture(t, test.fixture, test.realPath)
			}
			reference := syncdomain.Reference{
				File: documentPath, Format: test.format, Select: test.selectPath, Map: test.fieldMap,
			}
			projection, err := adapter.ParseSource(adaptertest.Bundle(documentPath, source), test.id, reference)
			require.NoError(t, err)
			assert.Equal(t, test.want, adaptertest.PrimaryText(t, projection))

			desired := adaptertest.WithText(projection, test.update)
			result, err := adapter.UpdateSource(adaptertest.Bundle(documentPath, source), test.id, reference, desired)
			require.NoError(t, err)
			edits := adaptertest.DirectEdits(t, result)
			updated := adaptertest.ApplyChecked(t, source, edits)
			reparsed, err := adapter.ParseSource(adaptertest.Bundle(documentPath, updated), test.id, reference)
			require.NoError(t, err)
			assert.Equal(t, test.update, adaptertest.PrimaryText(t, reparsed))
		})
	}
}

func TestProjectionValidationCoversEveryResourceKind(t *testing.T) {
	_, err := adapter.NormalizeReference(
		syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"},
		syncdomain.Reference{File: "tool.json", Format: "json", Map: map[string]string{"prompt": "value"}},
	)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorInvalidReference, problem.Code)

	_, err = adapter.NormalizeReference(
		syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "skill"},
		syncdomain.Reference{File: "skill.toml", Format: "toml", Map: map[string]string{"schema": "value"}},
	)
	problem, ok = syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorInvalidReference, problem.Code)
}

func TestUpdateSourceRejectsWrongProjectionAndMissingMap(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	reference := syncdomain.Reference{
		File: "prompt.json", Format: "json", Map: map[string]string{"prompt": "missing"},
	}
	_, err := adapter.UpdateSource(
		adaptertest.Bundle(reference.File, []byte(`{"prompt":"Help"}`)),
		id, reference,
		syncdomain.ToolProjection{},
	)
	require.ErrorContains(t, err, "projection kind")

	_, err = adapter.UpdateSource(
		adaptertest.Bundle(reference.File, []byte(`{"prompt":"Help"}`)),
		id, reference,
		syncdomain.VariationProjection{Mode: syncdomain.VariationModeAgent, Instructions: adaptertest.StringPointer("Next")},
	)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorPathNotFound, problem.Code)
}

func TestSourceEntryPointsPreserveStructuredFailures(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	_, err := adapter.NormalizeReference(id, syncdomain.Reference{
		File: "value.txt", Format: "missing", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "$"},
	})
	require.Error(t, err)
	_, err = adapter.NormalizeReference(id, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "$", Map: map[string]string{},
	})
	require.Error(t, err)
	_, err = adapter.NormalizeReference(id, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "invalid["},
	})
	require.Error(t, err)
	_, err = adapter.NormalizeReference(id, syncdomain.Reference{
		File: "main.py", Format: "python",
		Map: map[string]string{string(syncdomain.FieldPrompt): "template"},
	})
	require.Error(t, err)
	_, err = adapter.ParseSource(adapter.SourceBundle{}, id, syncdomain.Reference{
		File: "value.json", Format: "missing", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "$"},
	})
	require.Error(t, err)
	_, err = adapter.ParseSource(adaptertest.Bundle("value.json", []byte(`{"prompt":"Help"}`)), id, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "missing",
		Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
	})
	require.Error(t, err)
	_, err = adapter.ParseSource(adaptertest.Bundle("value.json", []byte(`{"prompt":`)), id, syncdomain.Reference{
		File: "value.json", Format: "json", Select: "$",
		Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
	})
	require.Error(t, err)
	_, err = adapter.UpdateSource(
		adapter.SourceBundle{}, id, syncdomain.Reference{
			File: "value.json", Format: "missing", Select: "$",
			Map: map[string]string{string(syncdomain.FieldPrompt): "$"},
		},
		syncdomain.VariationProjection{},
	)
	require.Error(t, err)
	_, err = adapter.UpdateSource(
		adaptertest.Bundle("value.json", []byte(`{"prompt":`)), id, syncdomain.Reference{
			File: "value.json", Format: "json", Select: "$",
			Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
		},
		syncdomain.VariationProjection{},
	)
	require.Error(t, err)
	_, err = adapter.UpdateSource(
		adaptertest.Bundle("value.json", []byte(`{"prompt":"Help"}`)), id, syncdomain.Reference{
			File: "value.json", Format: "json", Select: "missing",
			Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
		},
		syncdomain.VariationProjection{},
	)
	require.Error(t, err)

	messages := []syncdomain.Message{
		{Role: syncdomain.RoleSystem, Content: "one"},
		{Role: syncdomain.RoleUser, Content: "two"},
	}
	result, err := adapter.UpdateSource(
		adaptertest.Bundle("value.json", []byte(`{"prompt":"Help"}`)),
		id, syncdomain.Reference{
			File: "value.json", Format: "json", Select: "$",
			Map: map[string]string{string(syncdomain.FieldPrompt): "prompt"},
		},
		syncdomain.VariationProjection{Messages: &messages},
	)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, result.Fields[syncdomain.FieldPrompt].Capability)

}

func TestNormalizeReferenceRejectsOverlappingOrInvalidParameterTargets(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	tests := []struct {
		name    string
		targets map[string]string
		code    syncdomain.ErrorCode
		message string
	}{
		{
			name: "whole and child overlap",
			targets: map[string]string{
				"prompt": "prompt", "model.parameters": "parameters",
				`model.parameters["temperature"]`: "temperature",
			},
			code: syncdomain.ErrorInvalidReference, message: "overlaps",
		},
		{
			name: "same exact key through JSON escape",
			targets: map[string]string{
				"prompt": "prompt", `model.parameters["top_k"]`: "top_k",
				`model.parameters["top\u005fk"]`: "other",
			},
			code: syncdomain.ErrorInvalidReference, message: "same model parameter",
		},
		{
			name: "expanded tools child",
			targets: map[string]string{
				"prompt": "prompt", `model.parameters["tools"]`: "tools",
			},
			code: syncdomain.ErrorInvalidReference, message: "duplicates the tools field",
		},
		{
			name: "child needs exact bracket key",
			targets: map[string]string{
				"prompt": "prompt", "model.parameters.temperature": "temperature",
			},
			code: syncdomain.ErrorInvalidReference, message: "unsupported mapped field",
		},
		{
			name: "target uses shared path syntax",
			targets: map[string]string{
				"prompt": "prompt", `model.parameters["temperature"`: "temperature",
			},
			code: syncdomain.ErrorInvalidPathSyntax, message: "invalid path",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.NormalizeReference(id, syncdomain.Reference{
				File: "agent.json", Format: "json", Map: test.targets,
			})
			require.Error(t, err)
			problem, ok := syncdomain.ProblemFrom(err)
			require.True(t, ok)
			assert.Equal(t, test.code, problem.Code)
			assert.ErrorContains(t, err, test.message)
		})
	}
}

func TestExternalAdapterNeedsOnlyRegistration(t *testing.T) {
	require.NoError(t, adapter.Register(externalFakeAdapter{}))
	id := syncdomain.VariationID("project", "config", "default")

	projection, err := adapter.ParseSource(
		adapter.SourceBundle{
			Primary: adapter.Document{Path: "prompt.fake", Data: []byte("External adapter")},
		},
		id, syncdomain.Reference{File: "prompt.fake", Format: "external-fake"},
	)

	require.NoError(t, err)
	require.Equal(t, "External adapter", *projection.(syncdomain.VariationProjection).Instructions)
}

func TestAdapterFixturesHaveNoToolLoadedInstructionPaths(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "internal", "sync")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		relative, err := filepath.Rel(root, path)
		require.NoError(t, err)
		relative = filepath.ToSlash(relative)
		if !insideTestdata(relative) {
			return nil
		}
		require.Falsef(t, forbiddenInstructionPath(relative), "forbidden adapter fixture path: %s", relative)
		return nil
	})
	require.NoError(t, err)
}

func FuzzPathIndexNeverPanics(f *testing.F) {
	for _, path := range []string{
		"items[0]", "items[-1]", "items[1.5]", "items[]", "items[2147483648]",
		"items[0][1]", `items[id="support"].messages[0]`,
	} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		err := adapter.ParsePath(path)
		if err == nil {
			return
		}
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok)
		require.Equal(t, syncdomain.ErrorInvalidPathSyntax, problem.Code)
	})
}

func TestReferenceValidationUsesStableProblem(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	tests := []syncdomain.Reference{
		{Format: "python", Select: `agents[id="support"]`, Map: map[string]string{"prompt": "instructions"}},
		{File: "agents.py", Format: "python", Map: map[string]string{"prompt": "instructions"}},
		{File: "agents.py", Format: "python", Select: `agents[id="support"]`},
		{File: "agents.py", Format: "python", Select: `agents[id="support"]`, Map: map[string]string{"description": "instructions"}},
	}
	for _, reference := range tests {
		_, err := adapter.NormalizeReference(id, reference)
		problem, ok := syncdomain.ProblemFrom(err)
		require.True(t, ok)
		assert.Equal(t, syncdomain.ErrorInvalidReference, problem.Code)
		assert.Equal(t, id.Label(), problem.Resource)
	}
}

func FuzzPathParserNeverPanics(f *testing.F) {
	for _, value := range []string{
		"$", `agents[id="support"]`, `items[role="system",name="intro"]`,
		`metadata["display.name"]`, "items[",
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		_ = adapter.ParsePath(value)
	})
}
