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
	})
	require.NoError(t, err)

	require.Len(t, agent.createAgents, 1)
	in := agent.createAgents[0]
	assert.Equal(t, "space-1", aws.ToString(in.AgentSpaceId))
	metadata := metadataOf(t, in.Metadata)
	assert.Equal(t, awsdevops.ExperimentAgentName, metadata["name"])
	assert.Equal(t, []any{"ki-created"}, metadata["skills"])
	assert.Contains(t, metadata["tools"], awsdevops.MCPOAuthServerName+"_get-flag")
	assert.NotContains(t, metadata, "memory_stores")
	file, ok := in.Content.(*agenttypes.AssetContentMemberFile)
	require.True(t, ok)
	assert.Equal(t, "experiment-orchestrator.md", aws.ToString(file.Value.Path))
	assert.Equal(t, &agenttypes.AssetFileBodyMemberText{Value: testAgentPrompt}, file.Value.Body)
	assert.Equal(t, "ki-agent-created", result.AgentAssetID)
	assert.Equal(t, awsdevops.AgentCreated, result.AgentAction)
	assert.Empty(t, result.AgentError)
}

func TestPlanSetupNamesAgentToolsAfterTheExistingServer(t *testing.T) {
	server := oauthMCPService("mcp-1")
	server.Name = aws.String("Prod-LD")
	server.AdditionalServiceDetails.(*agenttypes.AdditionalServiceDetailsMemberMcpserver).Value.Name = aws.String("Prod-LD")
	agent := &fakeAgent{services: []agenttypes.RegisteredService{server}}
	clients := newTestClients(agent, newFakeIAM())
	opts := reuseOptions()

	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)

	require.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
	assert.Equal(t, "Prod-LD", plan.Agent.ServerName)
	assert.Equal(t, "Create custom agent experiment-orchestrator with skill experiment-orchestration "+
		"and the MCP tools of server Prod-LD", plan.Text().Agent)

	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)
	require.Len(t, agent.createAgents, 1, result.AgentError)
	assert.Contains(t, metadataOf(t, agent.createAgents[0].Metadata)["tools"], "Prod-LD_get-flag")
}

func TestSetupKeepsCurrentAgent(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", awsdevops.AgentTools(awsdevops.MCPOAuthServerName), []string{"ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": testSkill},
		agentFiles: map[string]string{"ki-agent": testAgentPrompt},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	assert.Empty(t, agent.createAgents)
	assert.Empty(t, agent.updateAgents)
	assert.Equal(t, "ki-agent", result.AgentAssetID)
	assert.Equal(t, awsdevops.AgentKept, result.AgentAction)
}

func TestSetupUpdatesAgentWithOldTools(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", []string{awsdevops.MCPOAuthServerName + "_get-flag"}, []string{"ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": testSkill},
		agentFiles: map[string]string{"ki-agent": testAgentPrompt},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	require.Len(t, agent.updateAgents, 1)
	metadata := metadataOf(t, agent.updateAgents[0].Metadata)
	assert.ElementsMatch(t, awsdevops.AgentTools(awsdevops.MCPOAuthServerName), metadata["tools"])
	assert.Equal(t, []any{"ki-ours"}, metadata["skills"])
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
	assert.Contains(t, plan.Text().Agent, "delete the agent in the AWS console")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseOptions(), plan)
	require.NoError(t, err)
	assert.Empty(t, agent.updateAgents)
	assert.Equal(t, awsdevops.AgentLeft, result.AgentAction)
	assert.Empty(t, result.AgentError)
}

func TestSetupSkipsAgentWhenSkillFails(t *testing.T) {
	agent := &fakeAgent{createAssetErr: errors.New("AccessDeniedException: not allowed")}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	assert.Empty(t, agent.createAgents)
	assert.Equal(t, awsdevops.SkillFailed, result.SkillAction)
	assert.Equal(t, awsdevops.AgentSkipped, result.AgentAction)
	assert.Contains(t, result.AgentError, "skill experiment-orchestration is not ready")
}

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

func TestPlanSetupKeepsAgentWithAddedTools(t *testing.T) {
	tools := append(awsdevops.AgentTools(awsdevops.MCPOAuthServerName), "Datadog_query-logs")
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", tools, []string{"ki-other", "ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": testSkill},
		agentFiles: map[string]string{"ki-agent": testAgentPrompt},
	}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.AgentKeep, plan.Agent.Action)
}

func TestSetupUpdateKeepsAddedToolsAndSkills(t *testing.T) {
	agent := &fakeAgent{
		assets: []agenttypes.Asset{
			agentAsset("ki-agent", "experiment-orchestrator", []string{"Datadog_query-logs"}, []string{"ki-other"}),
		},
		agentFiles: map[string]string{"ki-agent": testAgentPrompt},
	}

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions())
	require.NoError(t, err)

	require.Len(t, agent.updateAgents, 1)
	metadata := metadataOf(t, agent.updateAgents[0].Metadata)
	assert.Contains(t, metadata["tools"], "Datadog_query-logs")
	assert.Contains(t, metadata["tools"], awsdevops.MCPOAuthServerName+"_get-flag")
	assert.Equal(t, []any{"ki-other", "ki-created"}, metadata["skills"])
}

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

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), opts)
	require.NoError(t, err)

	require.Len(t, agent.createAgents, 1, result.AgentError)
	assert.Equal(t, []any{awsdevops.MCPServerName + "_get-flag"}, metadataOf(t, agent.createAgents[0].Metadata)["tools"])
}

func TestPlanSetupLeavesAgentWithoutPromptFile(t *testing.T) {
	agent := &fakeAgent{assets: []agenttypes.Asset{agentAsset("ki-agent", "experiment-orchestrator", nil, nil)}}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), reuseOptions(), true)
	require.NoError(t, err)

	assert.Equal(t, awsdevops.AgentLeave, plan.Agent.Action)
	assert.Contains(t, plan.Text().Agent, "because it has no experiment-orchestrator.md")
}

func TestPlanSetupNamesAgentToolsAfterANewServer(t *testing.T) {
	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(&fakeAgent{}, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
	}, true)
	require.NoError(t, err)

	assert.Contains(t, plan.Text().Agent, "the MCP tools of server "+awsdevops.MCPOAuthServerName)
}

func TestSetupGivesAgentTheToolsThatSetupAddsToTheAllowlist(t *testing.T) {
	old := awsdevops.AgentTools(awsdevops.MCPServerName)[:1]
	agent := &fakeAgent{
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: []agenttypes.Association{mcpAssociation("list-projects")},
		assets: []agenttypes.Asset{
			skillAsset("ki-ours", "experiment-orchestration"),
			agentAsset("ki-agent", "experiment-orchestrator", old, []string{"ki-ours"}),
		},
		skillFiles: map[string]string{"ki-ours": testSkill},
		agentFiles: map[string]string{"ki-agent": testAgentPrompt},
	}
	clients := newTestClients(agent, newFakeIAM())
	opts := reuseOptions()

	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	require.Equal(t, awsdevops.AgentUpdate, plan.Agent.Action)

	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)
	require.Len(t, agent.updateAgents, 1, result.AgentError)
	assert.ElementsMatch(t, awsdevops.AgentTools(awsdevops.MCPServerName), metadataOf(t, agent.updateAgents[0].Metadata)["tools"])
}
