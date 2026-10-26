package awsdevops_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func collectLogs(opts *awsdevops.SetupOptions) *[]string {
	var logged []string
	opts.Logf = func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}

	return &logged
}

func TestSetupProgressLinesHaveNoARNs(t *testing.T) {
	unmanaged := newFakeIAM()
	unmanaged.existingRoles[awsdevops.AgentSpaceRoleName] = true
	unmanaged.existingRoles[awsdevops.OperatorAppRoleName] = true

	scenarios := map[string]struct {
		agent *fakeAgent
		iam   *fakeIAM
		opts  awsdevops.SetupOptions
	}{
		"new space with a slow role": {
			agent: &fakeAgent{associateFailures: 1},
			iam:   newFakeIAM(),
			opts:  awsdevops.SetupOptions{AgentSpaceName: "launchdarkly", AuthFlow: "iam", LDAccessToken: "api-token"},
		},
		"roles ldcli did not create": {
			agent: &fakeAgent{},
			iam:   unmanaged,
			opts:  awsdevops.SetupOptions{AgentSpaceID: "space-1", AuthFlow: "iam"},
		},
		"token replacement": {
			agent: &fakeAgent{
				services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-old")},
				associations: []agenttypes.Association{{
					AssociationId: aws.String("assoc-old"),
					ServiceId:     aws.String("mcp-old"),
				}},
			},
			iam: newFakeIAM(),
			opts: awsdevops.SetupOptions{
				AgentSpaceName:  "launchdarkly",
				AuthFlow:        "iam",
				LDAccessToken:   "api-token",
				ReplaceMCPToken: true,
				LDBaseURI:       stubLaunchDarkly(t, http.StatusOK),
			},
		},
	}
	for name, s := range scenarios {
		t.Run(name, func(t *testing.T) {
			logged := collectLogs(&s.opts)

			_, err := awsdevops.Setup(context.Background(), newTestClients(s.agent, s.iam), s.opts)
			require.NoError(t, err)

			require.NotEmpty(t, *logged)
			for _, line := range *logged {
				assert.NotContains(t, line, "arn:aws:", "progress line shows an ARN")
				assert.NotContains(t, line, ";", "progress line has a semicolon")
			}
		})
	}
}

func TestConnectExistingProgressHasNoARNs(t *testing.T) {
	opts := awsdevops.SetupOptions{AgentSpaceID: "space-2", LDAccessToken: "api-token"}
	logged := collectLogs(&opts)

	agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")}}

	_, err := awsdevops.ConnectExisting(context.Background(), newTestClients(agent, newFakeIAM()), opts)
	require.NoError(t, err)

	for _, line := range *logged {
		assert.NotContains(t, line, "arn:aws:")
		assert.NotContains(t, line, ";")
	}
}

func TestSetupReuseLineNamesTheAgentSpace(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{{
		AgentSpaceId: aws.String("space-existing"),
		Name:         aws.String("launchdarkly"),
	}}}
	opts := awsdevops.SetupOptions{AgentSpaceName: "launchdarkly", AuthFlow: "iam"}
	logged := collectLogs(&opts)

	_, err := awsdevops.Setup(context.Background(), newTestClients(agent, newFakeIAM()), opts)
	require.NoError(t, err)

	assert.Contains(t, strings.Join(*logged, "\n"), "agent space launchdarkly (space-existing)")
}

func TestPlanSetupMakesNoWrites(t *testing.T) {
	agent := &fakeAgent{}
	iamClient := newFakeIAM()

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, iamClient), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		LDAccessToken:  "api-token",
	}, true)
	require.NoError(t, err)

	assert.Empty(t, iamClient.createdRoles)
	for _, call := range agent.calls {
		assert.True(t, strings.HasPrefix(call, "List"), "plan made a write: %s", call)
	}
}

func TestPlanSetupNamesTheAccountAndRegion(t *testing.T) {
	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(&fakeAgent{}, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
	}, true)
	require.NoError(t, err)

	assert.Equal(t, testAccountID, plan.AccountID)
	assert.Equal(t, "us-east-1", plan.Region)
}

func TestPlanSetupReusesTheSpaceWithTheSameName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{{
		AgentSpaceId: aws.String("space-existing"),
		Name:         aws.String("launchdarkly"),
	}}}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
	}, true)
	require.NoError(t, err)

	assert.Contains(t, plan.Text().AgentSpace, "launchdarkly (space-existing)")
	assert.Contains(t, strings.ToLower(plan.Text().AgentSpace), "reuse")
}

// The name matches, but the user asked for a new one.
func TestPlanSetupRefusesANewSpaceWithAUsedName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{{
		AgentSpaceId: aws.String("space-existing"),
		Name:         aws.String("launchdarkly"),
	}}}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		NewAgentSpace:  true,
	}, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "an agent space named launchdarkly already exists (space-existing)")
	assert.Equal(t, awsdevops.SpaceStep{}, plan.Space)
}

// A name that only starts the same is not a match.
func TestPlanSetupCreatesOnANearMissName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{{
		AgentSpaceId: aws.String("space-existing"),
		Name:         aws.String("launchdarkly-old"),
	}}}

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
	}, true)
	require.NoError(t, err)

	assert.Contains(t, strings.ToLower(plan.Text().AgentSpace), "create")
	assert.Contains(t, plan.Text().AgentSpace, "launchdarkly")
	assert.NotContains(t, plan.Text().AgentSpace, "space-existing")
}

func TestPlanSetupNamesTheSpaceItConnectsTo(t *testing.T) {
	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(&fakeAgent{}, newFakeIAM()), awsdevops.SetupOptions{
		AgentSpaceID: "space-2",
	}, false)
	require.NoError(t, err)

	// Unlisted spaces are named launchdarkly.
	assert.Contains(t, plan.Text().AgentSpace, "launchdarkly (space-2)")
	assert.Contains(t, strings.ToLower(plan.Text().AgentSpace), "connect")
}

func TestPlanSetupNamesTheMCPServerAction(t *testing.T) {
	cases := map[string]struct {
		services []agenttypes.RegisteredService
		opts     awsdevops.SetupOptions
		want     []string
	}{
		"reuse without a token": {
			services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
			want:     []string{"reuse", "mcp-existing"},
		},
		"reuse when replace has no token": {
			services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
			opts:     awsdevops.SetupOptions{ReplaceMCPToken: true},
			want:     []string{"reuse", "mcp-existing"},
		},
		"replace the token": {
			services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
			opts:     awsdevops.SetupOptions{ReplaceMCPToken: true, LDAccessToken: "api-token"},
			want:     []string{"replace", "mcp-existing"},
		},
		"register with a token": {
			opts: awsdevops.SetupOptions{LDAccessToken: "api-token"},
			want: []string{"register", awsdevops.MCPServerEndpoint},
		},
		"register at another endpoint": {
			services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-existing")},
			opts:     awsdevops.SetupOptions{LDAccessToken: "api-token", MCPEndpoint: "https://mcp.example.com/mcp"},
			want:     []string{"register", "https://mcp.example.com/mcp"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			c.opts.AgentSpaceID = "space-1"
			agent := &fakeAgent{services: c.services, associations: mcpLink("mcp-existing")}

			plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), c.opts, true)
			require.NoError(t, err)

			for _, want := range c.want {
				assert.Contains(t, strings.ToLower(plan.Text().MCPServer), strings.ToLower(want))
			}
		})
	}
}

func TestPlanSetupNeedsANameForANewSpace(t *testing.T) {
	agent := &fakeAgent{}

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), awsdevops.SetupOptions{
		NewAgentSpace: true,
	}, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--agent-space-name")
	assert.NotContains(t, agent.calls, "CreateAgentSpace")
}
