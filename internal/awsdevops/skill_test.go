package awsdevops_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent/document"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func reuseOptions() awsdevops.SetupOptions {
	return awsdevops.SetupOptions{AgentSpaceID: "space-2", AuthFlow: "iam", LDAccessToken: "api-token"}
}

func metadataOf(t *testing.T, doc document.Interface) map[string]any {
	t.Helper()
	encoded, err := doc.MarshalSmithyDocument()
	require.NoError(t, err)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(encoded, &metadata))

	return metadata
}

func TestExperimentSkillFrontMatter(t *testing.T) {
	assert.Equal(t, "experiment-orchestration", awsdevops.ExperimentSkillName)
	assert.Contains(t, awsdevops.ExperimentSkill, "# Automated Experimentation")
}

func TestSetupCreatesSkillInNewSpace(t *testing.T) {
	agent := &fakeAgent{}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
		LDAccessToken:  "api-token",
	})
	require.NoError(t, err)

	require.Len(t, agent.createAssets, 1)
	in := agent.createAssets[0]
	assert.Equal(t, "space-1", aws.ToString(in.AgentSpaceId))
	assert.Equal(t, "skill", aws.ToString(in.AssetType))
	metadata := metadataOf(t, in.Metadata)
	assert.Equal(t, awsdevops.ExperimentSkillName, metadata["name"])
	assert.Contains(t, metadata["description"], "Orchestrates automated experimentation lifecycle")
	assert.Equal(t, []any{"GENERIC"}, metadata["agent_types"])
	assert.NotContains(t, metadata, "status")
	file, ok := in.Content.(*agenttypes.AssetContentMemberFile)
	require.True(t, ok)
	assert.Equal(t, "SKILL.md", aws.ToString(file.Value.Path))
	assert.Equal(t, &agenttypes.AssetFileBodyMemberText{Value: awsdevops.ExperimentSkill}, file.Value.Body)
	assert.Equal(t, "ki-created", result.SkillAssetID)
}

// AWS adds sample skills to a space. Only the name of our skill counts.
func TestPlanSetupFindsSkillOnLaterPage(t *testing.T) {
	agent := &fakeAgent{
		paginate:   true,
		assets:     []agenttypes.Asset{skillAsset("ki-sample", "sample-skip-scheduled-maintenance"), skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": awsdevops.ExperimentSkill},
	}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillStep{Action: awsdevops.SkillKeep, AssetID: "ki-ours"}, plan.Skill)
	assert.Equal(t, "Keep skill experiment-orchestration (ki-ours)", plan.Text().Skill)
}

func TestPlanSetupCreatesMissingSkill(t *testing.T) {
	agent := &fakeAgent{assets: []agenttypes.Asset{skillAsset("ki-sample", "sample-skip-scheduled-maintenance")}}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillCreate, plan.Skill.Action)
	assert.Equal(t, "Create skill experiment-orchestration", plan.Text().Skill)
}

func TestSetupKeepsSameSkill(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": awsdevops.ExperimentSkill},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	assert.Empty(t, agent.createAssets)
	assert.Empty(t, agent.updateAssets)
	assert.Equal(t, "ki-ours", result.SkillAssetID)
}

func TestSetupLeavesChangedSkill(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Edited by the customer"},
	}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillLeave, plan.Skill.Action)
	assert.Contains(t, plan.Text().Skill, "--replace-skill")

	_, err = awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Empty(t, agent.createAssets)
	assert.Empty(t, agent.updateAssets)
}

func TestSetupReplacesChangedSkillWithFlag(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Older instructions"},
	}
	opts := reuseOptions()
	opts.ReplaceSkill = true

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), opts)
	require.NoError(t, err)

	assert.Empty(t, agent.createAssets)
	require.Len(t, agent.updateAssets, 1)
	in := agent.updateAssets[0]
	assert.Equal(t, "ki-ours", aws.ToString(in.AssetId))
	// Only the description, so the status and agent types that the
	// customer set stay.
	assert.Equal(t, []string{"description"}, keysOf(metadataOf(t, in.Metadata)))
	file, ok := in.Content.(*agenttypes.AssetContentMemberFile)
	require.True(t, ok)
	assert.Equal(t, &agenttypes.AssetFileBodyMemberText{Value: awsdevops.ExperimentSkill}, file.Value.Body)
}

// The flag does nothing to a skill that is already the same.
func TestPlanSetupKeepsSameSkillWithFlag(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": awsdevops.ExperimentSkill},
	}
	opts := reuseOptions()
	opts.ReplaceSkill = true

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillKeep, plan.Skill.Action)
}

func TestConnectCreatesSkill(t *testing.T) {
	agent := &fakeAgent{}

	result, err := awsdevops.ConnectExisting(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	require.Len(t, agent.createAssets, 1)
	assert.Equal(t, "space-2", aws.ToString(agent.createAssets[0].AgentSpaceId))
	assert.Equal(t, "ki-created", result.SkillAssetID)
}

func TestRunPlanStopsWhenSkillAppears(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	agent.assets = []agenttypes.Asset{skillAsset("ki-other", "experiment-orchestration")}

	_, err = awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, agent.createAssets)
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
}

func TestRunPlanStopsWhenSkillIsGone(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Older instructions"},
	}
	clients := newTestClients(agent, newFakeIAM())
	opts := reuseOptions()
	opts.ReplaceSkill = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	agent.assets = nil

	_, err = awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.ErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, agent.updateAssets)
}

func TestPlanSetupFailsWhenAssetsCannotBeListed(t *testing.T) {
	agent := &fakeAgent{listAssetsErr: errors.New("AccessDeniedException: no aidevops:ListAssets")}

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)

	require.ErrorContains(t, err, "aidevops:ListAssets")
	assert.ErrorContains(t, err, "space-2")
}

func TestSetupReportsSkillCreateFailure(t *testing.T) {
	agent := &fakeAgent{createAssetErr: errors.New("ServiceQuotaExceededException: 200 skills")}

	_, err := awsdevops.ConnectExisting(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())

	require.ErrorContains(t, err, "unable to create skill experiment-orchestration")
	assert.NotContains(t, agent.calls, "RegisterService")
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	return keys
}
