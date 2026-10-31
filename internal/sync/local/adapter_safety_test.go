package local

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncreference "github.com/launchdarkly/ldcli/internal/sync/reference"
)

func TestReferenceDescriptorValidationFailures(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	for name, content := range map[string]string{
		"invalid YAML":       "formatVersion: [",
		"unknown field":      "formatVersion: 1\nfile: prompt.md\nformat: markdown\nunknown: true\n",
		"multiple docs":      "formatVersion: 1\nfile: prompt.md\nformat: markdown\n---\nextra: true\n",
		"unsupported format": "formatVersion: 99\nfile: prompt.md\nformat: markdown\n",
		"missing file":       "formatVersion: 1\nformat: markdown\n",
	} {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"value.ref.yaml": {Data: []byte(content)}}
			_, err := ReadDescriptor(fsys, "value.ref.yaml", id)
			require.Error(t, err)
		})
	}
	_, err := ReadDescriptor(fstest.MapFS{}, "missing.ref.yaml", id)
	require.Error(t, err)
}

func TestReferencePathValidationFailures(t *testing.T) {
	for name, reference := range map[string]Reference{
		"missing file":   {Format: "markdown"},
		"missing format": {File: "prompt.md"},
		"absolute":       {File: "/prompt.md", Format: "markdown"},
		"managed":        {File: ".launchdarkly/prompt.md", Format: "markdown"},
		"unclean":        {File: "dir/../prompt.md", Format: "markdown"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, validateReference(reference))
		})
	}

	root := t.TempDir()
	writeTestFile(t, root, "directory/file.md", "body")
	_, err := resolveReferencePath(root, Reference{File: "missing.md", Format: "markdown"})
	require.Error(t, err)
	_, err = resolveReferencePath(root, Reference{File: "directory", Format: "markdown"})
	require.Error(t, err)

	outside := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
	link := filepath.Join(root, "link.md")
	if err := os.Symlink(outside, link); err == nil {
		_, err = resolveReferencePath(root, Reference{File: "link.md", Format: "markdown"})
		require.Error(t, err)
	}

	_, err = readReferenceFromFS(fstest.MapFS{}, Reference{})
	require.Error(t, err)
	_, err = readReferenceFromFS(fstest.MapFS{}, Reference{File: "prompt.md", Format: "markdown"})
	require.Error(t, err)
	data, err := readReferenceFromFS(
		fstest.MapFS{"prompt.md": {Data: []byte("Help")}},
		Reference{File: "prompt.md", Format: "markdown"},
	)
	require.NoError(t, err)
	assert.Equal(t, "Help", string(data))
}

func TestNewSourceReferenceValidatesRepositoryBoundaries(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "work"), 0o755))
	writeTestFile(t, root, "work/prompt.md", "Help")

	_, err := NewSourceReference(root, filepath.Join(root, "work"), "", "markdown", "", nil)
	require.Error(t, err)
	_, err = NewSourceReference(root, filepath.Join(root, "work"), "missing.md", "markdown", "", nil)
	require.Error(t, err)

	outside := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
	_, err = NewSourceReference(root, root, outside, "markdown", "", nil)
	require.Error(t, err)

	reference, err := NewSourceReference(root, filepath.Join(root, "work"), "prompt.md", "markdown", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "work/prompt.md", reference.File)
}

func TestSourceDefinitionAndCompileSafety(t *testing.T) {
	assert.Empty(t, (SourceDefinition{}).ID())
	variationID := syncdomain.VariationID("project", "config", "default")
	variation := syncdomain.SyncedResource{Kind: variationID.Kind, ProjectKey: variationID.ProjectKey, LookupKey: variationID.LookupKey}
	assert.Equal(t, variationID, (SourceDefinition{Variation: &variation}).ID())
	toolID := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"}
	attachment := syncdomain.SyncedAttachment{
		ProjectKey: "project",
		Attachment: syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "tool"}},
	}
	assert.Equal(t, toolID, (SourceDefinition{Attachment: &attachment}).ID())

	root := t.TempDir()
	writeTestFile(t, root, "value.json", `{"description":"Tool"}`)
	definition, err := CompileSource(root, toolID, Reference{
		File: "value.json", Format: "json", Select: "$", Map: map[string]string{"description": "description"},
	})
	require.NoError(t, err)
	require.NotNil(t, definition.Attachment)

	_, err = CompileSource(root, syncdomain.ResourceID{Kind: "unknown"}, Reference{
		File: "value.json", Format: "json", Select: "$", Map: map[string]string{"description": "description"},
	})
	require.Error(t, err)

	generated := syncreference.SourceBundle{Primary: syncreference.Document{
		Path: "generated.py", Data: []byte("# Code generated. DO NOT EDIT.\n"),
	}}
	_, err = compileVariationSource(variationID, Reference{File: "generated.py", Format: "python"}, generated)
	require.Error(t, err)
	_, err = compileAttachmentSource(toolID, Reference{File: "generated.py", Format: "python"}, generated)
	require.Error(t, err)
	_, err = compileAttachmentSource(
		syncdomain.ResourceID{Kind: "unknown", ProjectKey: "project", LookupKey: "value"},
		Reference{File: "value.json", Format: "json", Select: "$", Map: map[string]string{"description": "description"}},
		syncreference.SourceBundle{Primary: syncreference.Document{Path: "value.json", Data: []byte(`{"description":"value"}`)}},
	)
	require.Error(t, err)
}

func TestSourceBundleHandlesMissingAndFailedDependencies(t *testing.T) {
	reference := Reference{File: "app/main.py", Format: "python"}
	content := []byte("from .values import PROMPT\n")
	bundle, err := sourceBundle(reference, content, func(Reference) ([]byte, error) {
		return nil, fs.ErrNotExist
	})
	require.NoError(t, err)
	assert.Empty(t, bundle.Dependencies)

	expected := errors.New("read failed")
	_, err = sourceBundle(reference, content, func(Reference) ([]byte, error) {
		return nil, expected
	})
	require.ErrorIs(t, err, expected)

	_, err = sourceBundle(Reference{File: "value", Format: "missing"}, nil, func(Reference) ([]byte, error) {
		return nil, nil
	})
	require.Error(t, err)

	_, err = sourceBundle(reference, content, func(dependency Reference) ([]byte, error) {
		if strings.HasSuffix(dependency.File, "values.py") {
			return make([]byte, 8<<20), nil
		}
		return nil, fs.ErrNotExist
	})
	require.ErrorContains(t, err, "8 MiB")
}

func TestSourceEditHelpersEnforceAtomicSafety(t *testing.T) {
	problem := publicationProblem("value.py", "unsafe", "fix it", assert.AnError)
	typed, ok := syncdomain.ProblemFrom(problem)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorPublicationUnsupported, typed.Code)

	original := []byte("abcdef")
	updated, err := applySourceEdits("value.txt", original, []syncreference.TextEdit{
		{File: "value.txt", Start: 4, End: 6, Expected: []byte("ef"), Replacement: []byte("EF")},
		{File: "value.txt", Start: 0, End: 2, Expected: []byte("ab"), Replacement: []byte("AB")},
	})
	require.NoError(t, err)
	assert.Equal(t, "ABcdEF", string(updated))

	for name, edits := range map[string][]syncreference.TextEdit{
		"invalid range": {{File: "value.txt", Start: -1}},
		"wrong file":    {{File: "other.txt", Start: 0, End: 1, Expected: []byte("a")}},
		"stale bytes":   {{File: "value.txt", Start: 0, End: 1, Expected: []byte("x")}},
		"overlap": {
			{File: "value.txt", Start: 0, End: 3, Expected: []byte("abc")},
			{File: "value.txt", Start: 2, End: 4, Expected: []byte("cd")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := applySourceEdits("value.txt", original, edits)
			require.Error(t, err)
		})
	}

	id := syncdomain.VariationID("project", "config", "default")
	prompt := "Help"
	projection := syncdomain.VariationProjection{Instructions: &prompt}
	assert.True(t, sameProjection(id, projection, projection))
	other := "Other"
	assert.False(t, sameProjection(id, projection, syncdomain.VariationProjection{Instructions: &other}))

	assert.True(t, generatedSource("vendor/value.go", nil))
	assert.True(t, generatedSource("value.min.js", nil))
	assert.True(t, generatedSource("value.go", []byte("// Code generated by tool. DO NOT EDIT.")))
	assert.False(t, generatedSource("value.go", []byte("package value")))

	bundle := syncreference.SourceBundle{
		Primary:      syncreference.Document{Path: "main.py"},
		Dependencies: map[string]syncreference.Document{"dependency.py": {Path: "dependency.py"}},
	}
	_, ok = sourceBundleDocument(bundle, "main.py")
	assert.True(t, ok)
	_, ok = sourceBundleDocument(bundle, "dependency.py")
	assert.True(t, ok)
	_, ok = sourceBundleDocument(bundle, "missing.py")
	assert.False(t, ok)
}

func TestCheckSourcesDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "prompt.md", "Old prompt\n")
	id := syncdomain.VariationID("project", "config", "default")
	reference := Reference{File: "prompt.md", Format: "markdown"}
	writeDescriptorForTest(t, root, id, reference)
	prompt := "New prompt"

	err := NewStore(root).CheckSources([]SourceReplacement{{
		ID: id, Mode: syncdomain.VariationModeAgent, Reference: reference,
		Projection: syncdomain.VariationProjection{Instructions: &prompt},
	}})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, "prompt.md"))
	require.NoError(t, err)
	assert.Equal(t, "Old prompt\n", string(content))
}

func TestIndexedSourceErrorsDoNotWrite(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	prompt := "New prompt"
	for name, selectPath := range map[string]string{
		"malformed":    "items[-1]",
		"out of range": "items[2]",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			original := []byte("{\n  \"items\": [{\"prompt\": \"Old prompt\"}]\n}\n")
			writeTestFile(t, root, "values.json", string(original))
			reference := Reference{
				File: "values.json", Format: "json", Select: selectPath,
				Map: map[string]string{"prompt": "prompt"},
			}

			_, err := NewStore(root).ReplaceSources([]SourceReplacement{{
				ID: id, Mode: syncdomain.VariationModeAgent, Reference: reference,
				Projection: syncdomain.VariationProjection{Instructions: &prompt},
			}})

			require.Error(t, err)
			actual, readErr := os.ReadFile(filepath.Join(root, "values.json"))
			require.NoError(t, readErr)
			assert.Equal(t, original, actual)
		})
	}
}

func TestIndexedSourceChangeAfterReviewDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	original := []byte(`{"items":[{"name":"first","prompt":"Keep"},{"name":"second","prompt":"Old"}]}` + "\n")
	writeTestFile(t, root, "values.json", string(original))
	id := syncdomain.VariationID("project", "config", "default")
	reference := Reference{
		File: "values.json", Format: "json", Select: "items[1]",
		Map: map[string]string{"prompt": "prompt"},
	}
	prompt := "New"

	_, err := NewStore(root).ReplaceSources([]SourceReplacement{{
		ID: id, Mode: syncdomain.VariationModeAgent, Reference: reference,
		Projection: syncdomain.VariationProjection{Instructions: &prompt},
		ExpectedIndexedSelections: []syncdomain.IndexedSelection{{
			Path: "select#0", CollectionLength: 2,
			Fingerprint: "sha256:" + strings.Repeat("0", 64),
		}},
	}})

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorIndexedSelectionChanged, problem.Code)
	actual, readErr := os.ReadFile(filepath.Join(root, "values.json"))
	require.NoError(t, readErr)
	assert.Equal(t, original, actual)
}

func TestDescriptorStoreRejectsConflictsAndInvalidTargets(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	invalidID := syncdomain.ResourceID{Kind: "unknown", ProjectKey: "project", LookupKey: "value"}
	require.Error(t, store.ValidateDescriptorTarget(invalidID))
	_, err := store.WriteDescriptor(invalidID, Reference{File: "prompt.md", Format: "markdown"})
	require.Error(t, err)

	id := syncdomain.VariationID("project", "config", "default")
	writeTestFile(t, root, ".launchdarkly/project/configs/config/default.prompt.md", "managed")
	require.Error(t, store.ValidateDescriptorTarget(id))

	_, err = RenderDescriptor(id, Reference{File: "prompt.md", Format: "missing"})
	require.Error(t, err)

	require.NoError(t, os.Remove(filepath.Join(root, ".launchdarkly/project/configs/config/default.prompt.md")))
	writeTestFile(t, root, "prompt.md", "Help")
	_, err = store.WriteDescriptor(id, Reference{File: "prompt.md", Format: "missing"})
	require.Error(t, err)

	_, err = NewSourceReference(filepath.Join(root, "missing-root"), root, filepath.Join(root, "prompt.md"), "markdown", "", nil)
	require.Error(t, err)
	_, err = resolveReferencePath(filepath.Join(root, "missing-root"), Reference{File: "prompt.md", Format: "markdown"})
	require.Error(t, err)
}

func TestSourceEditPreparationFailureBranches(t *testing.T) {
	missingStore := NewStore(filepath.Join(t.TempDir(), "missing"))
	_, err := missingStore.ReplaceSources(nil)
	require.Error(t, err)

	root := t.TempDir()
	store := NewStore(root)
	paths, err := store.ReplaceSources(nil)
	require.NoError(t, err)
	assert.Empty(t, paths)

	writeTestFile(t, root, "bad.py", "def broken(")
	id := syncdomain.VariationID("project", "config", "default")
	reference := Reference{
		File: "bad.py", Format: "python", Select: "$",
		Map: map[string]string{"prompt": "$"},
	}
	prompt := "Help"
	_, err = store.ReplaceSources([]SourceReplacement{{
		ID: id, Mode: syncdomain.VariationModeAgent, Reference: reference,
		Projection: syncdomain.VariationProjection{Instructions: &prompt},
	}})
	require.Error(t, err)

	writeTestFile(t, root, "agents.py", `from agents import Agent
agent = Agent(tools=["search"])
`)
	toolReference := Reference{
		File: "agents.py", Format: "python", Select: `agents[id="agent"]`,
		Map: map[string]string{"prompt": "instructions", "tools": "tools"},
	}
	_, err = store.ReplaceSources([]SourceReplacement{{
		ID: id, Mode: syncdomain.VariationModeAgent, Reference: toolReference,
		Projection: syncdomain.VariationProjection{Instructions: &prompt},
	}})
	require.Error(t, err)

	longGenerated := append(make([]byte, 600), []byte("code generated do not edit")...)
	assert.False(t, generatedSource("source.go", longGenerated))

	badRoot := t.TempDir()
	writeTestFile(t, badRoot, ".launchdarkly/project/configs/config/default.ref.yaml", "invalid: [")
	err = NewStore(badRoot).CheckSources(nil)
	require.Error(t, err)

	writeTestFile(t, root, "agent.py", `from agents import Agent
agent = Agent(instructions="Help", tools=["search"])
`)
	agentReference := Reference{
		File: "agent.py", Format: "python", Select: `agents[id="agent"]`,
		Map: map[string]string{"prompt": "instructions", "tools": "tools"},
	}
	_, err = store.ReplaceSources([]SourceReplacement{{
		ID: id, Mode: syncdomain.VariationModeAgent, Reference: agentReference,
		Projection: syncdomain.ToolProjection{},
	}})
	require.Error(t, err)

	changedTools := []string{"search", "read"}
	_, err = store.ReplaceSources([]SourceReplacement{{
		ID: id, Mode: syncdomain.VariationModeAgent, Reference: agentReference,
		Projection: syncdomain.VariationProjection{Instructions: &prompt, Tools: &changedTools},
	}})
	require.Error(t, err)
}

func TestStageSourceDocumentRejectsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	writeTestFile(t, root, "generated.go", "// Code generated by tool. DO NOT EDIT.\n")
	_, err := store.stageSourceDocument(
		"generated.go",
		[]byte("// Code generated by tool. DO NOT EDIT.\n"),
		[]byte("updated"),
	)
	require.Error(t, err)

	_, err = store.stageSourceDocument("missing.go", nil, []byte("updated"))
	require.Error(t, err)

	writeTestFile(t, root, "original.go", "package original\n")
	hardlink := filepath.Join(root, "hardlink.go")
	if err := os.Link(filepath.Join(root, "original.go"), hardlink); err == nil {
		_, err = store.stageSourceDocument("original.go", []byte("package original\n"), []byte("package changed\n"))
		require.Error(t, err)
	}

	outside := filepath.Join(t.TempDir(), "outside.go")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
	if err := os.Symlink(outside, filepath.Join(root, "link.go")); err == nil {
		_, err = store.stageSourceDocument("link.go", []byte("outside"), []byte("changed"))
		require.Error(t, err)
	}
}

func TestSourceBundleDependencyLimit(t *testing.T) {
	var source strings.Builder
	for index := 0; index < 33; index++ {
		source.WriteString("import value")
		source.WriteString(string(rune('A' + index%26)))
		source.WriteString(" from './dependency")
		source.WriteString(strings.Repeat("x", index+1))
		source.WriteString("';\n")
	}
	_, err := sourceBundle(
		Reference{File: "main.ts", Format: "typescript"},
		[]byte(source.String()),
		func(Reference) ([]byte, error) { return nil, fs.ErrNotExist },
	)
	require.ErrorContains(t, err, "more than 32 dependencies")
}

func TestCompileHelpersReturnReadAndParseErrors(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	fsys := fstest.MapFS{"value.ref.yaml": {Data: []byte("invalid: [")}}
	_, err := compileReferencedVariation(fsys, "value.ref.yaml", id, func(Reference) ([]byte, error) {
		return nil, nil
	})
	require.Error(t, err)

	fsys = fstest.MapFS{"value.ref.yaml": {Data: []byte(`formatVersion: 1
file: prompt.md
format: markdown
`)}}
	expectedRead := errors.New("primary read failed")
	_, err = compileReferencedVariation(fsys, "value.ref.yaml", id, func(Reference) ([]byte, error) {
		return nil, expectedRead
	})
	require.ErrorIs(t, err, expectedRead)

	fsys = fstest.MapFS{"value.ref.yaml": {Data: []byte(`formatVersion: 1
file: app/main.py
format: python
select: 'agents[id="agent"]'
map:
  prompt: instructions
`)}}
	calls := 0
	dependencyError := errors.New("dependency read failed")
	_, err = compileReferencedVariation(fsys, "value.ref.yaml", id, func(Reference) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("from .prompt import TEXT\n"), nil
		}
		return nil, dependencyError
	})
	require.ErrorIs(t, err, dependencyError)

	toolID := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "tool"}
	_, err = compileReferencedAttachment(
		fstest.MapFS{"bad.ref.yaml": {Data: []byte("invalid: [")}},
		"bad.ref.yaml",
		toolID,
		func(Reference) ([]byte, error) { return nil, nil },
	)
	require.Error(t, err)

	fsys = fstest.MapFS{"value.ref.yaml": {Data: []byte(`formatVersion: 1
file: tool.json
format: json
select: $
map:
  description: description
`)}}
	expected := errors.New("read failed")
	_, err = compileReferencedAttachment(fsys, "value.ref.yaml", toolID, func(Reference) ([]byte, error) {
		return nil, expected
	})
	require.ErrorIs(t, err, expected)

	_, err = compileVariationSource(
		id,
		Reference{File: "value.json", Format: "json", Select: "$", Map: map[string]string{"prompt": "prompt"}},
		syncreference.SourceBundle{Primary: syncreference.Document{Path: "value.json", Data: []byte(`{"prompt":`)}},
	)
	require.Error(t, err)
	_, err = compileAttachmentSource(
		toolID,
		Reference{File: "value.json", Format: "json", Select: "$", Map: map[string]string{"description": "description"}},
		syncreference.SourceBundle{Primary: syncreference.Document{Path: "value.json", Data: []byte(`{"description":`)}},
	)
	require.Error(t, err)
}
