package awsdevops_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent/document"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func reuseOptions() awsdevops.SetupOptions {
	return awsdevops.SetupOptions{AgentSpaceID: "space-2", AuthFlow: "iam"}
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

// A change to SKILL.md must add its hash, so the next release updates the
// copies that this release sends.
func TestShippedSkillHashesHoldTheCurrentSkill(t *testing.T) {
	assert.Contains(t, awsdevops.ShippedSkillHashes(), awsdevops.SkillHash(awsdevops.ExperimentSkill),
		"add the hash of the new SKILL.md to shippedSkillHashes")
}

// skillToolPattern matches an MCP tool name in backticks, such as `get-flag`.
var skillToolPattern = regexp.MustCompile("`((?:list|get|create|update|start|stop|toggle|archive|apply|check|query|match|delete|copy|save|search|find|manage|run|clone|setup|preview|link|invite|restrict|unrestrict)-[a-z-]+)`")

// The agent can call only the allowlisted tools, so every tool that SKILL.md
// names must be in the allowlist.
func TestMCPAllowlistHoldsTheSkillTools(t *testing.T) {
	matches := skillToolPattern.FindAllStringSubmatch(awsdevops.ExperimentSkill, -1)
	require.NotEmpty(t, matches)
	for _, match := range matches {
		assert.Contains(t, awsdevops.MCPTools(), match[1], "add the tool that SKILL.md names to the MCP allowlist")
	}
}

func TestSetupCreatesSkillInNewSpace(t *testing.T) {
	agent := &fakeAgent{}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
	})
	require.NoError(t, err)

	require.Len(t, agent.createAssets, 1)
	in := agent.createAssets[0]
	assert.Equal(t, "space-1", aws.ToString(in.AgentSpaceId))
	assert.Equal(t, "skill", aws.ToString(in.AssetType))
	metadata := metadataOf(t, in.Metadata)
	assert.Equal(t, awsdevops.ExperimentSkillName, metadata["name"])
	assert.Equal(t, awsdevops.ExperimentSkillDescription(), metadata["description"])
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
	assert.Equal(t, "Create skill experiment-orchestration for every agent type in the space", plan.Text().Skill)
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
	assert.Equal(t, awsdevops.SkillKept, result.SkillAction)
	assert.Empty(t, result.SkillError)
}

// AWS can send the file as bytes. The text is the same, so setup keeps it.
func TestPlanSetupKeepsSameSkillSentAsBytes(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillBytes: map[string][]byte{"ki-ours": []byte(awsdevops.ExperimentSkill)},
	}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillKeep, plan.Skill.Action)
}

// An empty SKILL.md has no edits to lose, so setup updates it without the flag.
func TestPlanSetupUpdatesEmptySkill(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": ""},
	}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillUpdate, plan.Skill.Action)
}

// A skill without SKILL.md, for example one made from a PDF, is not ours.
func TestPlanSetupLeavesSkillWithoutFile(t *testing.T) {
	agent := &fakeAgent{assets: []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")}}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillLeave, plan.Skill.Action)
	assert.Equal(t,
		"Leave skill experiment-orchestration (ki-ours) unchanged because it has no SKILL.md. Pass --replace-skill to overwrite it",
		plan.Text().Skill)
}

// A copy from an older ldcli has no edits, so setup updates it without the flag.
func TestSetupUpdatesOlderShippedSkill(t *testing.T) {
	t.Cleanup(awsdevops.ShipSkill("# Instructions of an older ldcli"))
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Instructions of an older ldcli"},
	}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillUpdate, plan.Skill.Action)
	assert.Contains(t, plan.Text().Skill, "Update skill experiment-orchestration (ki-ours)")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	require.Len(t, agent.updateAssets, 1)
	assert.Equal(t, "ki-ours", aws.ToString(agent.updateAssets[0].AssetId))
	assert.Equal(t, awsdevops.SkillUpdated, result.SkillAction)
}

func TestSetupLeavesEditedSkill(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Edited by the customer"},
	}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillLeave, plan.Skill.Action)
	assert.Contains(t, plan.Text().Skill, "--replace-skill")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Empty(t, agent.createAssets)
	assert.Empty(t, agent.updateAssets)
	// JSON output shows that the skill is not the ldcli version.
	assert.Equal(t, awsdevops.SkillLeft, result.SkillAction)
	assert.Empty(t, result.SkillError)
}

func TestSetupOverwritesEditedSkillWithFlag(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Edited by the customer"},
	}
	opts := reuseOptions()
	opts.ReplaceSkill = true

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), opts)
	require.NoError(t, err)

	assert.Empty(t, agent.createAssets)
	assert.Equal(t, awsdevops.SkillOverwritten, result.SkillAction)
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
	assert.Equal(t, awsdevops.SkillCreated, result.SkillAction)
}

// The skill step goes after the MCP server, and a plan change in it only
// skips the skill.
func TestRunPlanSkipsSkillThatAppears(t *testing.T) {
	agent := &fakeAgent{}
	clients := newTestClients(agent, newFakeIAM())
	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	agent.assets = []agenttypes.Asset{skillAsset("ki-other", "experiment-orchestration")}

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)

	require.NoError(t, err)
	assert.Empty(t, agent.createAssets)
	assert.Contains(t, agent.calls, "RegisterService")
	assert.Contains(t, result.SkillError, "appeared after the plan")
	assert.Equal(t, awsdevops.SkillSkipped, result.SkillAction)
}

func TestRunPlanSkipsSkillThatIsGone(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		skillFiles: map[string]string{"ki-ours": "# Edited by the customer"},
	}
	clients := newTestClients(agent, newFakeIAM())
	opts := reuseOptions()
	opts.ReplaceSkill = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	agent.assets = nil

	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
	assert.Empty(t, agent.updateAssets)
	assert.Contains(t, result.SkillError, "disappeared after the plan")
}

// An edit after the plan changes the version. The overwrite stops, so it
// cannot remove an edit that the plan did not show.
func TestRunPlanSkipsSkillEditedAfterPlan(t *testing.T) {
	asset := skillAsset("ki-ours", "experiment-orchestration")
	asset.Version = aws.Int32(3)
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{asset},
		skillFiles: map[string]string{"ki-ours": "# Edited by the customer"},
	}
	clients := newTestClients(agent, newFakeIAM())
	opts := reuseOptions()
	opts.ReplaceSkill = true
	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	require.Equal(t, int32(3), plan.Skill.Version)
	agent.assets[0].Version = aws.Int32(4)

	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)

	require.NoError(t, err)
	assert.Empty(t, agent.updateAssets)
	assert.Contains(t, result.SkillError, "changed after the plan")
	// The step stopped before the write.
	assert.Equal(t, awsdevops.SkillSkipped, result.SkillAction)
}

func accessDenied(action string) error {
	return &agenttypes.AccessDeniedException{
		Message: aws.String("User: arn:aws:sts::123456789012:assumed-role/Dev/me is not authorized to perform: " + action),
	}
}

// Without aidevops:ListAssets, the plan skips the skill and setup does the
// other steps. The AWS error has a code, and the text keeps its message,
// which names the denied action.
func TestSetupSkipsSkillWhenAssetsCannotBeListed(t *testing.T) {
	agent := &fakeAgent{listAssetsErr: accessDenied("aidevops:ListAssets")}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillSkip, plan.Skill.Action)
	assert.True(t, strings.HasPrefix(plan.Text().Skill, "Skip skill experiment-orchestration: "), plan.Text().Skill)
	assert.Contains(t, plan.Text().Skill, "not authorized to perform: aidevops:ListAssets")
	assert.Contains(t, plan.Text().Skill, "The skill step needs")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Contains(t, agent.calls, "RegisterService")
	assert.Equal(t, awsdevops.SkillSkipped, result.SkillAction)
	assert.Contains(t, result.SkillError, "not authorized to perform: aidevops:ListAssets")
	assert.Equal(t, 1, strings.Count(result.SkillError, awsdevops.ExperimentSkillName))
}

func TestSetupSkipsSkillWhenFileCannotBeRead(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")},
		getFileErr: accessDenied("aidevops:GetAssetFile"),
	}

	result, err := awsdevops.ConnectExisting(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())

	require.NoError(t, err)
	assert.Equal(t, awsdevops.SkillSkipped, result.SkillAction)
	assert.Contains(t, result.SkillError, "not authorized to perform: aidevops:GetAssetFile")
	assert.Contains(t, result.SkillError, "ki-ours")
	assert.Equal(t, 1, strings.Count(result.SkillError, awsdevops.ExperimentSkillName))
}

// AWS sends ResourceNotFoundException also for a skill that someone deleted
// after the list. That skill does not exist, so the plan creates one.
func TestPlanSetupCreatesSkillDeletedDuringPlan(t *testing.T) {
	agent := &fakeAgent{assets: []agenttypes.Asset{skillAsset("ki-ours", "experiment-orchestration")}}
	agent.onGetFile = func() { agent.assets = nil }

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillStep{Action: awsdevops.SkillCreate}, plan.Skill)
}

func TestSetupWarnsOnSkillCreateFailure(t *testing.T) {
	agent := &fakeAgent{createAssetErr: accessDenied("aidevops:CreateAsset")}

	result, err := awsdevops.ConnectExisting(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())

	require.NoError(t, err)
	assert.Contains(t, agent.calls, "RegisterService")
	assert.Equal(t, "mcp-1", result.MCPServiceID)
	assert.Equal(t, awsdevops.SkillFailed, result.SkillAction)
	assert.Contains(t, result.SkillError, "unable to create skill experiment-orchestration")
	assert.Contains(t, result.SkillError, "not authorized to perform: aidevops:CreateAsset")
	assert.Empty(t, result.SkillAssetID)
}

// Other failures do not get the permission text.
func TestSkillFailureNamesPermissionsOnlyWhenDenied(t *testing.T) {
	agent := &fakeAgent{createAssetErr: &agenttypes.ServiceQuotaExceededException{Message: aws.String("200 skills")}}

	result, err := awsdevops.ConnectExisting(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())

	require.NoError(t, err)
	assert.Contains(t, result.SkillError, "200 skills")
	assert.NotContains(t, result.SkillError, "The skill step needs")
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	return keys
}
