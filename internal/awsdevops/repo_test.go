package awsdevops_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent/document"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func newAssetMetadata(m map[string]any) document.Interface {
	return document.NewLazyDocument(m)
}

func TestAgentFilesReturnsGuideTemplates(t *testing.T) {
	files := awsdevops.AgentFiles()

	require.Len(t, files, 3)
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
		assert.NotEmpty(t, file.Content, "%s should have content", file.Path)
		assert.NotEmpty(t, file.Message, "%s should have a commit message", file.Path)
	}
	assert.ElementsMatch(t, []string{
		".kiro/agents/flag-implementer.json",
		".github/workflows/kiro-implement.yml",
		".github/workflows/merge-pr.yml",
	}, paths)
}

func TestFlagImplementerAgentDeclaresKiroToolPermissions(t *testing.T) {
	var contents string
	for _, file := range awsdevops.AgentFiles() {
		if file.Path == ".kiro/agents/flag-implementer.json" {
			contents = string(file.Content)
		}
	}

	require.NotEmpty(t, contents)
	assert.Contains(t, contents, `"name": "flag-implementer"`)
	assert.Contains(t, contents, `"fs_read"`)
	assert.Contains(t, contents, `"fs_write"`)
	assert.Contains(t, contents, `"execute_bash"`)
}

func TestKiroImplementWorkflowUsesFlagImplementerAgent(t *testing.T) {
	var workflow string
	for _, file := range awsdevops.AgentFiles() {
		if file.Path == ".github/workflows/kiro-implement.yml" {
			workflow = string(file.Content)
		}
	}

	require.NotEmpty(t, workflow)
	assert.Contains(t, workflow, "workflow_dispatch")
	assert.Contains(t, workflow, "KIRO_API_KEY")
	assert.Contains(t, workflow, "--agent flag-implementer")
	assert.Contains(t, workflow, "set -o pipefail")
}

func TestDefaultSkillBodyMatchesTheEmbeddedGuide(t *testing.T) {
	body := awsdevops.DefaultSkillBody()

	require.True(t, strings.HasPrefix(body, "---\n"), "skill body should start with YAML front matter")
	assert.Contains(t, body, `name: "experiment-orchestration"`)
	assert.Contains(t, body, "Step 1: Goal Clarification")
	assert.Contains(t, body, "Step 8: Report")
}

func TestDefaultSkillNameMatchesTheEmbeddedSkill(t *testing.T) {
	assert.Equal(t, "experiment-orchestration", awsdevops.DefaultSkillName)
	assert.Contains(t, awsdevops.DefaultSkillBody(), awsdevops.DefaultSkillName)
}

func TestSetupCreatesTheDefaultSkillWhenNoBodyIsGiven(t *testing.T) {
	agent := &fakeAgent{}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		Skip:           []string{awsdevops.SkipOperatorApp, awsdevops.SkipMCP},
	})
	require.NoError(t, err)

	require.Len(t, agent.createdAssetInputs, 1)
	input := agent.createdAssetInputs[0]
	assert.Equal(t, "skill", aws.ToString(input.AssetType))

	var metadata struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		AgentTypes  []string `json:"agent_types"`
	}
	raw, err := input.Metadata.MarshalSmithyDocument()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &metadata))
	assert.Equal(t, awsdevops.DefaultSkillName, metadata.Name)
	assert.NotEmpty(t, metadata.Description, "AWS rejects skills without a description")

	file, ok := input.Content.(*agenttypes.AssetContentMemberFile)
	require.True(t, ok, "skill content should be file bytes")
	body, ok := file.Value.Body.(*agenttypes.AssetFileBodyMemberText)
	require.True(t, ok, "skill file body should be text")
	assert.Contains(t, body.Value, "experiment-orchestration")
}

func TestSetupReusesAnExistingSkillInsteadOfCreatingIt(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			{
				AssetId:   aws.String("skill-existing"),
				AssetType: aws.String("skill"),
				Metadata: newAssetMetadata(map[string]any{
					"name": awsdevops.DefaultSkillName,
				}),
			},
		},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		Skip:           []string{awsdevops.SkipOperatorApp, awsdevops.SkipMCP},
	})
	require.NoError(t, err)

	assert.Equal(t, "skill-existing", result.SkillAssetID)
	assert.Empty(t, agent.createdAssetInputs, "an existing skill should not be recreated")
}
