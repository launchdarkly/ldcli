package awsdevops_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func TestSetupReusesAnAlreadyRegisteredMCPServer(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			{
				ServiceId:   aws.String("mcp-1"),
				ServiceType: agenttypes.ServiceMcpServer,
				Name:        aws.String(awsdevops.MCPServerName),
			},
		},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
		LDAccessToken:  "api-token",
	})
	require.NoError(t, err)

	assert.NotContains(t, agent.calls, "RegisterService")
	assert.Equal(t, "mcp-1", result.MCPServiceID)
	assert.Equal(t, "assoc-mcp-1", result.MCPAssociationID)
}

func TestSetupReplacesTheMCPServerTokenWhenAsked(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			{
				ServiceId:   aws.String("mcp-1"),
				ServiceType: agenttypes.ServiceMcpServer,
				Name:        aws.String(awsdevops.MCPServerName),
			},
		},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName:  "launchdarkly",
		AuthFlow:        "iam",
		SkipOperatorApp: true,
		LDAccessToken:   "api-token",
		ReplaceMCPToken: true,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"mcp-1"}, agent.deregisteredIDs)
	assert.Contains(t, agent.calls, "RegisterService")
	assert.Equal(t, "mcp-1", result.MCPServiceID)
}

func TestFindMCPServerMatchesOnTheEndpointInTheServiceDetails(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			{
				ServiceId:   aws.String("mcp-2"),
				ServiceType: agenttypes.ServiceMcpServer,
				AdditionalServiceDetails: &agenttypes.AdditionalServiceDetailsMemberMcpserver{
					Value: agenttypes.RegisteredMCPServerDetails{
						Endpoint: aws.String(awsdevops.MCPServerEndpoint),
					},
				},
			},
		},
	}

	serviceID, err := awsdevops.FindMCPServer(context.Background(), newTestClients(agent, newFakeIAM()))
	require.NoError(t, err)

	assert.Equal(t, "mcp-2", serviceID)
}
