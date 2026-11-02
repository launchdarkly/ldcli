package awsdevops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func TestSetupReusesTheMCPServerOfTheAgentSpace(t *testing.T) {
	agent := &fakeAgent{
		services:   []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")},
		spaceLinks: map[string][]agenttypes.Association{"space-1": mcpLink("mcp-1")},
	}

	result, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID: "space-1",
		AuthFlow:     "iam",
	})
	require.NoError(t, err)

	assert.NotContains(t, agent.calls, "RegisterService")
	assert.Equal(t, "mcp-1", result.MCPServiceID)
	assert.Equal(t, "assoc-mcp-1", result.MCPAssociationID)
}

const space1ARN = "arn:aws:aidevops:us-east-1:" + testAccountID + ":agentspace/space-1"

var spaceTags = map[string]string{awsdevops.ManagedTagKey: awsdevops.ManagedTagValue}

func TestAgentSpacesListsEveryPage(t *testing.T) {
	agent := &fakeAgent{
		paginate: true,
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1"), Name: aws.String("launchdarkly"), Description: aws.String("Managed by the LaunchDarkly CLI")},
			{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform"), Description: aws.String("Team space")},
		},
		tags: map[string]map[string]string{space1ARN: spaceTags},
	}

	spaces, err := awsdevops.AgentSpaces(context.Background(), newTestClients(agent, newFakeIAM()))
	require.NoError(t, err)

	assert.Equal(t, []awsdevops.AgentSpaceSummary{
		{AgentSpaceID: "space-1", Name: "launchdarkly", Description: "Managed by the LaunchDarkly CLI", ManagedByLDCLI: true},
		{AgentSpaceID: "space-2", Name: "platform", Description: "Team space"},
	}, spaces)
}

func TestConnectExistingOnlyConnectsTheMCPServer(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")},
		},
		operatorApps: map[string]*devopsagent.GetOperatorAppOutput{
			"space-2": {Iam: &agenttypes.IamAuthConfiguration{}},
		},
	}
	iamClient := newFakeIAM()

	result, err := awsdevops.ConnectExisting(context.Background(), newTestClients(agent, iamClient), awsdevops.SetupOptions{
		AgentSpaceID: "space-2",
		AuthFlow:     "iam",
	})
	require.NoError(t, err)

	assert.Equal(t, "space-2", result.AgentSpaceID)
	assert.Equal(t, awsdevops.OperatorAppURL("space-2"), result.OperatorAppURL)
	assert.Equal(t, "mcp-1", result.MCPServiceID)
	assert.Equal(t, "assoc-mcp-1", result.MCPAssociationID)
	assert.Empty(t, iamClient.createdRoles)
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
	require.Len(t, agent.associationInputs, 1)
	assert.Equal(t, "space-2", aws.ToString(agent.associationInputs[0].AgentSpaceId))
}

// A user can change the description, so only the tag counts.
func TestAgentSpacesCountsOnlyTheTag(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{
		{AgentSpaceId: aws.String("space-1"), Name: aws.String("copy"), Description: aws.String("Managed by the LaunchDarkly CLI")},
		{AgentSpaceId: aws.String("space-2"), Name: aws.String("tagged"), Description: aws.String("Renamed by a user")},
	}}
	agent.tags = map[string]map[string]string{
		"arn:aws:aidevops:us-east-1:" + testAccountID + ":agentspace/space-2": spaceTags,
	}

	spaces, err := awsdevops.AgentSpaces(context.Background(), newTestClients(agent, newFakeIAM()))
	require.NoError(t, err)

	assert.False(t, spaces[0].ManagedByLDCLI)
	assert.True(t, spaces[1].ManagedByLDCLI)
	assert.Equal(t, 2, countCalls(agent.calls, "ListTagsForResource"))
}

// If every lookup fails, AgentSpaces fails.
func TestAgentSpacesReportsATagError(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1"), Name: aws.String("launchdarkly"), Description: aws.String("Managed by the LaunchDarkly CLI")},
		},
		tagsErr: apiError{code: "AccessDeniedException"},
	}

	_, err := awsdevops.AgentSpaces(context.Background(), newTestClients(agent, newFakeIAM()))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tags of agent space space-1")
	assert.Contains(t, err.Error(), "aidevops:ListTagsForResource")
}

// One failed lookup does not hide the other spaces.
func TestAgentSpacesKeepsTheSpacesWithATag(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{namedSpace("space-1", "launchdarkly"), namedSpace("space-2", "sandbox")},
		tags:        map[string]map[string]string{space1ARN: spaceTags},
		tagsErrFor: map[string]error{
			"arn:aws:aidevops:us-east-1:" + testAccountID + ":agentspace/space-2": apiError{code: "InternalServerException"},
		},
	}

	spaces, err := awsdevops.AgentSpaces(context.Background(), newTestClients(agent, newFakeIAM()))

	require.NoError(t, err)
	require.Len(t, spaces, 2)
	assert.True(t, spaces[0].ManagedByLDCLI)
	assert.NoError(t, spaces[0].TagErr)
	assert.Equal(t, "sandbox", spaces[1].Name)
	assert.False(t, spaces[1].ManagedByLDCLI)
	assert.ErrorContains(t, spaces[1].TagErr, "tags of agent space space-2")
}

// A caller with a space ID gets the space even if its tags cannot be read.
func TestAgentSpaceReportsATagErrorOnTheSummary(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{namedSpace("space-1", "launchdarkly")},
		tagsErr:     apiError{code: "AccessDeniedException"},
	}

	space, err := awsdevops.AgentSpace(context.Background(), newTestClients(agent, newFakeIAM()), "space-1")

	require.NoError(t, err)
	assert.Equal(t, "launchdarkly", space.Name)
	assert.False(t, space.ManagedByLDCLI)
	assert.ErrorContains(t, space.TagErr, "aidevops:ListTagsForResource")
}

// Another error does not blame a missing permission.
func TestAgentSpacesReportsAnotherTagErrorWithoutThePermission(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{namedSpace("space-1", "launchdarkly")},
		tagsErr:     apiError{code: "InternalServerException"},
	}

	_, err := awsdevops.AgentSpaces(context.Background(), newTestClients(agent, newFakeIAM()))

	require.Error(t, err)
	assert.Equal(t, "unable to read the tags of agent space space-1: api error InternalServerException", err.Error())
}

func countCalls(calls []string, name string) int {
	n := 0
	for _, call := range calls {
		if call == name {
			n++
		}
	}

	return n
}

func TestStatusReadsTheOperatorApp(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{namedSpace("space-1", "on"), namedSpace("space-2", "off")},
		operatorApps: map[string]*devopsagent.GetOperatorAppOutput{
			"space-1": {Iam: &agenttypes.IamAuthConfiguration{}},
		},
	}

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, newFakeIAM()), "")
	require.NoError(t, err)

	require.Len(t, status.AgentSpaces, 2)
	assert.True(t, status.AgentSpaces[0].OperatorAppEnabled)
	assert.Equal(t, awsdevops.OperatorAppURL("space-1"), status.AgentSpaces[0].OperatorAppURL)
	assert.False(t, status.AgentSpaces[1].OperatorAppEnabled)
	assert.Empty(t, status.AgentSpaces[1].OperatorAppURL)
}

// apiError acts like an AWS API error with a code.
type apiError struct{ code string }

func (e apiError) Error() string     { return "api error " + e.code }
func (e apiError) ErrorCode() string { return e.code }

// A policy without aidevops:GetOperatorApp must not hide the rest of status.
func TestStatusReportsAnUnreadableAppAsUnknown(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:    []agenttypes.AgentSpace{namedSpace("space-1", "on")},
		operatorAppErr: apiError{code: "AccessDeniedException"},
		associations:   []agenttypes.Association{{AssociationId: aws.String("assoc-1"), ServiceId: aws.String("mcp-1")}},
	}

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, newFakeIAM()), "")
	require.NoError(t, err)

	require.Len(t, status.AgentSpaces, 1)
	space := status.AgentSpaces[0]
	assert.Equal(t, "AccessDeniedException", space.OperatorAppError)
	assert.False(t, space.OperatorAppEnabled)
	assert.Empty(t, space.OperatorAppURL)
	assert.Len(t, space.Associations, 1)
	assert.Len(t, status.Roles, 2)
}

func TestStatusReportsTheErrorTextWithoutACode(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces:    []agenttypes.AgentSpace{namedSpace("space-1", "on")},
		operatorAppErr: errors.New("connection reset"),
	}

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, newFakeIAM()), "")
	require.NoError(t, err)

	assert.Contains(t, status.AgentSpaces[0].OperatorAppError, "connection reset")
}

// status and setup show the URL that AWS sends.
func TestStatusUsesTheOperatorAppURLFromAWS(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{namedSpace("space-1", "on")},
		operatorApps: map[string]*devopsagent.GetOperatorAppOutput{
			"space-1": {Iam: &agenttypes.IamAuthConfiguration{}, OperatorAppUrl: aws.String("https://app.example.aws")},
		},
	}

	status, err := awsdevops.GetStatus(context.Background(), newTestClients(agent, newFakeIAM()), "")
	require.NoError(t, err)

	assert.Equal(t, "https://app.example.aws", status.AgentSpaces[0].OperatorAppURL)
}

func TestAgentSpaceReadsTheTagOfOneSpace(t *testing.T) {
	agent := &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{
			{AgentSpaceId: aws.String("space-1"), Name: aws.String("launchdarkly"), Description: aws.String("Managed by the LaunchDarkly CLI")},
		},
		tags: map[string]map[string]string{space1ARN: spaceTags},
	}

	space, err := awsdevops.AgentSpace(context.Background(), newTestClients(agent, newFakeIAM()), "space-1")
	require.NoError(t, err)

	assert.True(t, space.ManagedByLDCLI)
	assert.Equal(t, "launchdarkly", space.Name)
	assert.NotContains(t, agent.calls, "ListAgentSpaces")
}

func namedSpace(id, name string) agenttypes.AgentSpace {
	return agenttypes.AgentSpace{AgentSpaceId: aws.String(id), Name: aws.String(name)}
}
