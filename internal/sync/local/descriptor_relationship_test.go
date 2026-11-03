package local

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestDescriptorRelationshipsPreserveAbsentAndEmpty(t *testing.T) {
	id := syncdomain.VariationID("project", "config", "default")
	base := Reference{File: "prompt.md", Format: "markdown"}

	absent, err := RenderDescriptor(id, base)
	require.NoError(t, err)
	assert.NotContains(t, string(absent), "tools:")

	empty := []syncdomain.AttachmentRef{}
	rendered, err := RenderDescriptor(id, Reference{
		File: "prompt.md", Format: "markdown", Tools: &empty,
	})
	require.NoError(t, err)
	assert.Contains(t, string(rendered), "tools: []")

	reference, err := ReadDescriptor(fstest.MapFS{
		"value.ref.yaml": {Data: rendered},
	}, "value.ref.yaml", id)
	require.NoError(t, err)
	require.NotNil(t, reference.Tools)
	assert.Empty(t, *reference.Tools)
	assert.Nil(t, reference.Skills)
}

func TestDescriptorRelationshipsUseStrictKeyOnlyEntries(t *testing.T) {
	variationID := syncdomain.VariationID("project", "config", "default")
	toolID := syncdomain.ResourceID{
		Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search",
	}
	skillID := syncdomain.ResourceID{
		Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "review",
	}
	tests := []struct {
		name    string
		id      syncdomain.ResourceID
		content string
		want    string
	}{
		{
			name: "version",
			id:   variationID,
			content: "formatVersion: 1\nfile: prompt.md\nformat: markdown\n" +
				"tools:\n  - key: search\n    version: 2\n",
			want: "field version not found",
		},
		{
			name: "custom parameters",
			id:   variationID,
			content: "formatVersion: 1\nfile: prompt.md\nformat: markdown\n" +
				"tools:\n  - key: search\n    customParameters: {}\n",
			want: "field customParameters not found",
		},
		{
			name: "tool descriptor relationships",
			id:   toolID,
			content: "formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\n" +
				"map:\n  description: description\ntools: []\n",
			want: `top-level "tools" is not valid for tool descriptors`,
		},
		{
			name: "skill descriptor relationships",
			id:   skillID,
			content: "formatVersion: 1\nfile: skill.md\nformat: markdown\n" +
				"skills: []\n",
			want: `top-level "skills" is not valid for skill descriptors`,
		},
		{
			name: "mapped tools",
			id:   variationID,
			content: "formatVersion: 1\nfile: prompt.json\nformat: json\nselect: $\n" +
				"map:\n  prompt: prompt\n  tools: tools\n",
			want: `map key "tools" is not allowed`,
		},
		{
			name: "mapped skills",
			id:   variationID,
			content: "formatVersion: 1\nfile: prompt.json\nformat: json\nselect: $\n" +
				"map:\n  prompt: prompt\n  skills: skills\n",
			want: `map key "skills" is not allowed`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadDescriptor(fstest.MapFS{
				"value.ref.yaml": {Data: []byte(test.content)},
			}, "value.ref.yaml", test.id)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestCompileDescriptorHydratesOnlyLocalProjectDefinitions(t *testing.T) {
	descriptor := "formatVersion: 1\nfile: prompt.md\nformat: markdown\n" +
		"tools:\n  - key: Search.Case\n"
	tool := `{"formatVersion":1,"key":"Search.Case","schema":{"type":"object"}}`
	fsys := fstest.MapFS{
		".launchdarkly/project/configs/config/default.ref.yaml": {Data: []byte(descriptor)},
		".launchdarkly/project/tools/Search.Case.json":          {Data: []byte(tool)},
		"prompt.md": {Data: []byte("Help")},
	}

	resources, err := Compile(fsys)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "Search.Case"}}, resources[0].Variation.Tools)
	require.Len(t, resources[0].Variation.Attachments, 1)
	assert.Equal(t, "Search.Case", resources[0].Variation.Attachments[0].Key())

	delete(fsys, ".launchdarkly/project/tools/Search.Case.json")
	fsys[".launchdarkly/project/tools/Search.Case.ref.yaml"] = &fstest.MapFile{Data: []byte(
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\n" +
			"map:\n  description: description\n  schema: schema\n",
	)}
	fsys["tool.json"] = &fstest.MapFile{Data: []byte(
		`{"description":"Linked","schema":{"type":"object"}}`,
	)}
	resources, err = Compile(fsys)
	require.NoError(t, err)
	require.Len(t, resources[0].Variation.Attachments, 1)
	require.NotNil(t, resources[0].Variation.Attachments[0].Projection)

	delete(fsys, ".launchdarkly/project/tools/Search.Case.ref.yaml")
	delete(fsys, "tool.json")
	fsys[".launchdarkly/other/tools/Search.Case.json"] = &fstest.MapFile{Data: []byte(tool)}
	_, err = Compile(fsys)
	require.ErrorContains(t, err, `tool "Search.Case" has no local definition`)
	require.ErrorContains(t, err, "ldcli sync tools add --project project --key Search.Case")
	require.ErrorContains(t, err, "ldcli sync tools link --project project --key Search.Case")
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorAttachmentDefinitionMissing, problem.Code)
}

func TestDescriptorRelationshipEditPreservesOtherBytesAndSorts(t *testing.T) {
	original := []byte("# file comment\nformatVersion: 1\nfile: prompt.md # keep\n" +
		"format: markdown\n# relationship comment\ntools: # replace\n" +
		"  # keep list comment\n  - key: old # keep item comment\n" +
		"skills:\n  - key: keep\n# tail comment\n")
	tools := []syncdomain.AttachmentRef{{Key: "z"}, {Key: "A.Case"}}
	skills := []syncdomain.AttachmentRef{}

	updated, err := updateDescriptorRelationships(original, tools, true, skills, true)
	require.NoError(t, err)
	text := string(updated)
	assert.Contains(t, text, "# file comment\nformatVersion: 1\nfile: prompt.md # keep\nformat: markdown\n")
	assert.Contains(t, text, "# relationship comment\n")
	assert.Contains(t, text, "tools: # replace\n  # keep list comment\n  # keep item comment\n"+
		"  - key: A.Case\n  - key: z\n")
	assert.Contains(t, text, "skills: []\n# tail comment\n")
	assert.False(t, strings.Contains(text, "key: old"))
}

func TestDescriptorRelationshipHelpersHandleOwnedAndUnownedFields(t *testing.T) {
	base := Reference{File: "prompt.md", Format: "markdown"}
	unchanged, err := referenceWithProjectedRelationships(
		base,
		syncdomain.VariationProjection{FieldStates: map[syncdomain.Field]syncdomain.FieldState{}},
	)
	require.NoError(t, err)
	assert.Nil(t, unchanged.Tools)
	assert.Nil(t, unchanged.Skills)

	tools := []string{"search"}
	skills := []string{"review"}
	owned, err := referenceWithProjectedRelationships(
		base,
		syncdomain.VariationProjection{
			Tools: &tools, Skills: &skills,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools:  {Capability: syncdomain.CapabilityDirect, Known: true},
				syncdomain.FieldSkills: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "search"}}, *owned.Tools)
	assert.Equal(t, []syncdomain.AttachmentRef{{Key: "review"}}, *owned.Skills)

	_, _, err = projectionRelationship(
		syncdomain.ToolProjection{FieldStates: map[syncdomain.Field]syncdomain.FieldState{
			syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
		}},
		syncdomain.FieldTools,
	)
	require.ErrorContains(t, err, `relationship field "tools" has no value`)
}

func TestDescriptorRelationshipEditHandlesNoChangeAndInvalidDocuments(t *testing.T) {
	original := []byte("formatVersion: 1\nfile: prompt.md\nformat: markdown\n")
	updated, err := updateDescriptorRelationships(original, nil, false, nil, false)
	require.NoError(t, err)
	assert.Equal(t, original, updated)

	_, err = updateDescriptorRelationships([]byte("- not-a-mapping\n"), nil, true, nil, false)
	require.EqualError(t, err, "descriptor must be a YAML mapping")

	_, err = updateDescriptorRelationships([]byte("invalid: [\n"), nil, true, nil, false)
	require.Error(t, err)
}

func TestDescriptorRelationshipReplacementSkipsIdenticalContent(t *testing.T) {
	root := t.TempDir()
	id := syncdomain.VariationID("project", "support", "default")
	descriptorPath, err := ReferencePath(id)
	require.NoError(t, err)
	writeTestFile(t, root, descriptorPath,
		"formatVersion: 1\nfile: prompt.md\nformat: markdown\ntools:\n  - key: search\n")
	tools := []syncdomain.AttachmentRef{{Key: "search"}}
	projected := []string{"search"}
	replacement := SourceReplacement{
		ID: id,
		Reference: Reference{
			File: "prompt.md", Format: "markdown", Tools: &tools,
		},
		Projection: syncdomain.VariationProjection{
			Tools: &projected,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldTools: {Capability: syncdomain.CapabilityDirect, Known: true},
			},
		},
	}

	_, changed, err := NewStore(root).prepareDescriptorRelationshipReplacement(replacement)

	require.NoError(t, err)
	assert.False(t, changed)

	replacement.Projection = syncdomain.VariationProjection{
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{},
	}
	_, changed, err = NewStore(root).prepareDescriptorRelationshipReplacement(replacement)
	require.NoError(t, err)
	assert.False(t, changed)
}
