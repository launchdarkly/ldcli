package awsdevops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// A change to the prompt must add its hash, so the next release updates the
// agents that this release makes.
func TestShippedAgentHashesHoldTheCurrentPrompt(t *testing.T) {
	assert.Contains(t, awsdevops.ShippedAgentHashes(), awsdevops.SkillHash(awsdevops.ExperimentAgentPrompt),
		"add the hash of the new prompt to shippedAgentHashes")
}

// AWS names a tool of an MCP server "<server name>_<tool name>".
func TestAgentToolsStartWithTheServerName(t *testing.T) {
	tools := awsdevops.AgentTools("LaunchDarkly")

	assert.Contains(t, tools, "LaunchDarkly_get-flag")
	assert.Contains(t, tools, "LaunchDarkly_toggle-flag")
	for _, tool := range tools {
		assert.Regexp(t, `^LaunchDarkly_[a-z-]+$`, tool)
	}
}

func TestSetupCreatesAgentInNewSpace(t *testing.T) {
	agent := &fakeAgent{}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
		LDAccessToken:  "api-token",
	})
	require.NoError(t, err)

	require.Len(t, agent.createAgents, 1)
	in := agent.createAgents[0]
	assert.Equal(t, "space-1", aws.ToString(in.AgentSpaceId))
	metadata := metadataOf(t, in.Metadata)
	assert.Equal(t, awsdevops.ExperimentAgentName, metadata["name"])
	assert.Equal(t, []any{"ki-created"}, metadata["skills"])
	assert.Contains(t, metadata["tools"], awsdevops.MCPServerName+"_get-flag")
	assert.NotContains(t, metadata, "memory_stores")
	file, ok := in.Content.(*agenttypes.AssetContentMemberFile)
	require.True(t, ok)
	assert.Equal(t, "experiment-orchestrator.md", aws.ToString(file.Value.Path))
	assert.Equal(t, &agenttypes.AssetFileBodyMemberText{Value: awsdevops.ExperimentAgentPrompt}, file.Value.Body)
	assert.Equal(t, "ki-agent-created", result.AgentAssetID)
	assert.Equal(t, awsdevops.AgentCreated, result.AgentAction)
	assert.Empty(t, result.AgentError)
}

// The tool IDs use the name of the server in AWS, which the customer can
// change.
func TestPlanSetupNamesAgentToolsAfterTheExistingServer(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{
		mcpService("mcp-1", "Prod-LD", awsdevops.MCPServerEndpoint),
	}}
	opts := reuseOptions()
	opts.LDAccessToken = ""

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)
	require.NoError(t, err)

	require.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
	assert.Equal(t, "Prod-LD", plan.Agent.ServerName)
	assert.Equal(t, "Create custom agent experiment-orchestrator with skill experiment-orchestration "+
		"and the MCP tools of server Prod-LD", plan.Text().Agent)
}

func TestSetupKeepsCurrentAgent(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", awsdevops.AgentTools(awsdevops.MCPServerName), []string{"ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": awsdevops.ExperimentSkill},
		agentFiles: map[string]string{"ki-agent": awsdevops.ExperimentAgentPrompt},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	assert.Empty(t, agent.createAgents)
	assert.Empty(t, agent.updateAgents)
	assert.Equal(t, "ki-agent", result.AgentAssetID)
	assert.Equal(t, awsdevops.AgentKept, result.AgentAction)
}

// A newer ldcli allows more tools, so an agent with the ldcli prompt and
// fewer tools gets the new list.
func TestSetupUpdatesAgentWithOldTools(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", []string{"LaunchDarkly_get-flag"}, []string{"ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": awsdevops.ExperimentSkill},
		agentFiles: map[string]string{"ki-agent": awsdevops.ExperimentAgentPrompt},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	require.Len(t, agent.updateAgents, 1)
	metadata := metadataOf(t, agent.updateAgents[0].Metadata)
	assert.ElementsMatch(t, awsdevops.AgentTools(awsdevops.MCPServerName), metadata["tools"])
	assert.Equal(t, []any{"ki-ours"}, metadata["skills"])
	// AWS keeps the keys that the request leaves out, such as memory stores.
	assert.NotContains(t, metadata, "memory_stores")
	assert.Equal(t, awsdevops.AgentUpdated, result.AgentAction)
}

func TestSetupLeavesEditedAgent(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{agentAsset("ki-agent", "experiment-orchestrator", nil, nil)},
		agentFiles: map[string]string{"ki-agent": "# Edited by the customer"},
	}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseOptions(), true)
	require.NoError(t, err)
	assert.Equal(t, awsdevops.AgentLeave, plan.Agent.Action)
	assert.Contains(t, plan.Text().Agent, "--replace-agent")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Empty(t, agent.updateAgents)
	assert.Equal(t, awsdevops.AgentLeft, result.AgentAction)
	assert.Empty(t, result.AgentError)
}

func TestSetupOverwritesEditedAgentWithFlag(t *testing.T) {
	agent := &fakeAgent{
		assets:     []agenttypes.Asset{agentAsset("ki-agent", "experiment-orchestrator", nil, nil)},
		agentFiles: map[string]string{"ki-agent": "# Edited by the customer"},
	}
	opts := reuseOptions()
	opts.ReplaceAgent = true

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), opts)
	require.NoError(t, err)

	require.Len(t, agent.updateAgents, 1)
	assert.Equal(t, "ki-agent", aws.ToString(agent.updateAgents[0].AssetId))
	assert.Equal(t, awsdevops.AgentOverwritten, result.AgentAction)
}

// Without the skill, the agent could not run, so setup does not make it.
func TestSetupSkipsAgentWhenSkillFails(t *testing.T) {
	agent := &fakeAgent{createAssetErr: errors.New("AccessDeniedException: not allowed")}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	assert.Empty(t, agent.createAgents)
	assert.Equal(t, awsdevops.SkillFailed, result.SkillAction)
	assert.Equal(t, awsdevops.AgentSkipped, result.AgentAction)
	assert.Contains(t, result.AgentError, "skill experiment-orchestration is not ready")
}

// The agent is optional, so its failure does not fail setup.
func TestSetupWarnsWhenAgentCreateFails(t *testing.T) {
	agent := &fakeAgent{createAgentErr: accessDenied("aidevops:CreateAsset")}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	assert.Equal(t, awsdevops.SkillCreated, result.SkillAction)
	assert.Equal(t, awsdevops.AgentFailed, result.AgentAction)
	assert.Contains(t, result.AgentError, "The custom agent step needs aidevops:ListAssets")
}

func TestPlanSetupSkipsAgentWhenAssetsCannotBeListed(t *testing.T) {
	agent := &fakeAgent{listAssetsErr: accessDenied("aidevops:ListAssets")}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.AgentSkip, plan.Agent.Action)
	assert.Contains(t, plan.Text().Agent, "The custom agent step needs aidevops:ListAssets")
}

// A customer can give the agent more tools or skills. The ldcli ones are
// there, so setup keeps the agent.
func TestPlanSetupKeepsAgentWithAddedTools(t *testing.T) {
	tools := append(awsdevops.AgentTools(awsdevops.MCPServerName), "Datadog_query-logs")
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", tools, []string{"ki-other", "ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": awsdevops.ExperimentSkill},
		agentFiles: map[string]string{"ki-agent": awsdevops.ExperimentAgentPrompt},
	}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.AgentKeep, plan.Agent.Action)
}

// An update adds the ldcli tools and skill, and keeps the ones that the
// customer added.
func TestSetupUpdateKeepsAddedToolsAndSkills(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			agentAsset("ki-agent", "experiment-orchestrator", []string{"Datadog_query-logs"}, []string{"ki-other"}),
		},
		agentFiles: map[string]string{"ki-agent": awsdevops.ExperimentAgentPrompt},
	}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	require.Len(t, agent.updateAgents, 1)
	metadata := metadataOf(t, agent.updateAgents[0].Metadata)
	assert.Contains(t, metadata["tools"], "Datadog_query-logs")
	assert.Contains(t, metadata["tools"], awsdevops.MCPServerName+"_get-flag")
	assert.Equal(t, []any{"ki-other", "ki-created"}, metadata["skills"])
}

// Setup keeps the allowlist of an existing association, and an agent can use
// only the tools that it allows.
func TestSetupGivesAgentOnlyTheToolsTheSpaceAllows(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: []agenttypes.Association{{
			AgentSpaceId:  aws.String("space-2"),
			AssociationId: aws.String("assoc-mcp-1"),
			ServiceId:     aws.String("mcp-1"),
			Configuration: &agenttypes.ServiceConfigurationMemberMcpserver{
				Value: agenttypes.MCPServerConfiguration{Tools: []string{"get-flag", "not-an-ldcli-tool"}},
			},
		}},
	}
	opts := reuseOptions()
	opts.LDAccessToken = ""

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), opts)
	require.NoError(t, err)

	require.Len(t, agent.createAgents, 1, result.AgentError)
	assert.Equal(t, []any{awsdevops.MCPServerName + "_get-flag"}, metadataOf(t, agent.createAgents[0].Metadata)["tools"])
}

func TestPlanTextSkipsAgentWithoutMCPServer(t *testing.T) {
	plan := awsdevops.SetupPlan{
		MCP:   awsdevops.MCPStep{Action: awsdevops.MCPSkip},
		Agent: awsdevops.AgentStep{Action: awsdevops.AgentCreate},
	}

	assert.Equal(t, "Skip custom agent experiment-orchestrator, because no MCP server is connected", plan.Text().Agent)
}

func TestPlanSetupLeavesAgentWithoutPromptFile(t *testing.T) {
	agent := &fakeAgent{assets: []agenttypes.Asset{agentAsset("ki-agent", "experiment-orchestrator", nil, nil)}}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.AgentLeave, plan.Agent.Action)
	assert.Contains(t, plan.Text().Agent, "because it has no experiment-orchestrator.md")
}
