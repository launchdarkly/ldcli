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

// mcpAssociation is the association of server mcp-1 with space-2 that allows
// these tools.
func mcpAssociation(tools ...string) agenttypes.Association {
	return agenttypes.Association{
		AgentSpaceId:  aws.String("space-2"),
		AssociationId: aws.String("assoc-mcp-1"),
		ServiceId:     aws.String("mcp-1"),
		Configuration: &agenttypes.ServiceConfigurationMemberMcpserver{
			Value: agenttypes.MCPServerConfiguration{Tools: tools},
		},
		Capabilities: map[string]agenttypes.CapabilityConfiguration{"kept": {}},
	}
}

func reuseServerOptions() awsdevops.SetupOptions {
	opts := reuseOptions()
	opts.LDAccessToken = ""
	opts.AddMCPTools = true

	return opts
}

// An older ldcli allowed fewer tools. A re-run adds the new ones, so the skill
// can use them.
func TestSetupAddsMissingToolsToAnExistingAllowlist(t *testing.T) {
	agent := &fakeAgent{
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: []agenttypes.Association{mcpAssociation("list-projects", "get-flag", "toggle-flag")},
	}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseServerOptions(), true)
	require.NoError(t, err)
	require.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
	assert.Contains(t, plan.Text().MCPServer, "and add get-project, get-environment, list-flags,")
	assert.Contains(t, plan.Text().MCPServer, "update-flag-settings")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseServerOptions(), plan)
	require.NoError(t, err)

	require.Len(t, agent.updateAssociations, 1)
	in := agent.updateAssociations[0]
	assert.Equal(t, "assoc-mcp-1", aws.ToString(in.AssociationId))
	config, ok := in.Configuration.(*agenttypes.ServiceConfigurationMemberMcpserver)
	require.True(t, ok)
	assert.Contains(t, config.Value.Tools, "update-flag-settings")
	assert.Contains(t, config.Value.ToolDetails, agenttypes.MCPToolDetail{
		Name:               aws.String("update-flag-settings"),
		ToolClassification: agenttypes.ToolClassificationMutative,
	})
	// AWS replaces the whole association, so setup sends the capabilities back.
	assert.Contains(t, in.Capabilities, "kept")
	assert.Contains(t, result.MCPToolsAdded, "create-metric")
	assert.NotContains(t, result.MCPToolsAdded, "get-flag")
	assert.Empty(t, result.MCPToolsError)
}

func TestSetupKeepsACompleteAllowlist(t *testing.T) {
	agent := &fakeAgent{
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: []agenttypes.Association{mcpAssociation(awsdevops.MCPTools()...)},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseServerOptions())
	require.NoError(t, err)

	assert.Empty(t, agent.updateAssociations)
	assert.Empty(t, result.MCPToolsAdded)
}

// AWS does not send the classification of each tool back. Rewriting an
// allowlist with a tool that ldcli does not know would make that tool
// read-only, so setup warns instead.
func TestSetupLeavesAnAllowlistWithForeignTools(t *testing.T) {
	agent := &fakeAgent{
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: []agenttypes.Association{mcpAssociation("get-flag", "delete-everything")},
	}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseServerOptions(), true)
	require.NoError(t, err)
	assert.Contains(t, plan.Text().MCPServer, "which setup cannot add")

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseServerOptions(), plan)
	require.NoError(t, err)

	assert.Empty(t, agent.updateAssociations)
	assert.Contains(t, result.MCPToolsError, "Add the missing tools in the AWS console")
}

// Setup cannot tell an older ldcli allowlist from one that someone trimmed,
// so without --add-mcp-tools it only warns.
func TestSetupWarnsAboutMissingToolsWithoutTheFlag(t *testing.T) {
	agent := &fakeAgent{
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: []agenttypes.Association{mcpAssociation("get-flag")},
	}
	clients := newTestClients(agent, newFakeIAM())
	opts := reuseServerOptions()
	opts.AddMCPTools = false

	plan, err := awsdevops.PlanSetup(context.Background(), clients, opts, true)
	require.NoError(t, err)
	assert.Contains(t, plan.Text().MCPServer, "Pass --add-mcp-tools to add them")

	result, err := awsdevops.RunPlan(context.Background(), clients, opts, plan)
	require.NoError(t, err)

	assert.Empty(t, agent.updateAssociations)
	assert.Contains(t, result.MCPToolsError, "--add-mcp-tools")
}

// The run makes only the change that the plan showed.
func TestSetupSkipsAnAllowlistThatChangedAfterThePlan(t *testing.T) {
	agent := &fakeAgent{
		services:     []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations: []agenttypes.Association{mcpAssociation("get-flag", "delete-everything")},
	}
	clients := newTestClients(agent, newFakeIAM())

	plan, err := awsdevops.PlanSetup(context.Background(), clients, reuseServerOptions(), true)
	require.NoError(t, err)
	agent.associations = []agenttypes.Association{mcpAssociation("get-flag")}

	result, err := awsdevops.RunPlan(context.Background(), clients, reuseServerOptions(), plan)
	require.NoError(t, err)

	assert.Empty(t, agent.updateAssociations)
	assert.Contains(t, result.MCPToolsError, "changed after the plan")
}

// The server works with the tools that it has, so a failed update does not
// fail setup.
func TestSetupWarnsWhenTheAllowlistUpdateFails(t *testing.T) {
	agent := &fakeAgent{
		services:             []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		associations:         []agenttypes.Association{mcpAssociation("get-flag")},
		updateAssociationErr: errors.New("AccessDeniedException: not allowed"),
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), reuseServerOptions())
	require.NoError(t, err)

	assert.Contains(t, result.MCPToolsError, "unable to add")
	assert.Empty(t, result.MCPToolsAdded)
}
