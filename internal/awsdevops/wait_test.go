package awsdevops_test

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func TestWaitForGitHubServiceReturnsTheRegisteredService(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			{ServiceId: aws.String("gh-1"), ServiceType: agenttypes.ServiceGithub},
		},
	}

	serviceID, err := awsdevops.WaitForGitHubService(context.Background(), newTestClients(agent, newFakeIAM()), time.Millisecond)
	require.NoError(t, err)

	assert.Equal(t, "gh-1", serviceID)
}

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

func TestSetupSkipsTheGitHubStepWhenItIsAlreadyRegistered(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			{ServiceId: aws.String("gh-1"), ServiceType: agenttypes.ServiceGithub},
		},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AuthFlow:       "iam",
		SkipMCPServer:  true,
	})
	require.NoError(t, err)

	assert.Equal(t, "gh-1", result.GitHubServiceID)
	for _, step := range result.RemainingManualSteps {
		assert.NotEqual(t, awsdevops.ManualStepGitHubApp, step.Kind)
	}
}

func TestTeardownDeregistersGitHub(t *testing.T) {
	agent := &fakeAgent{
		services: []agenttypes.RegisteredService{
			{ServiceId: aws.String("gh-1"), ServiceType: agenttypes.ServiceGithub},
		},
	}

	require.NoError(t, awsdevops.Teardown(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.TeardownOptions{
		DeregisterGitHub: true,
	}))

	assert.Equal(t, []string{"gh-1"}, agent.deregisteredIDs)
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

func TestWaitForGitHubServicePollsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	agent := &fakeAgent{}
	_, err := awsdevops.WaitForGitHubService(ctx, newTestClients(agent, newFakeIAM()), time.Millisecond)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, agent.calls, "ListServices")
}
