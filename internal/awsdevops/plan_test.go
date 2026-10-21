package awsdevops_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

var planRoles = []string{awsdevops.AgentSpaceRoleName, awsdevops.OperatorAppRoleName}

func planWith(t *testing.T, iamClient *fakeIAM, provision bool) awsdevops.SetupPlan {
	t.Helper()
	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(&fakeAgent{}, iamClient), awsdevops.SetupOptions{
		AgentSpaceName: "launchdarkly",
		AgentSpaceID:   map[bool]string{true: "", false: "space-2"}[provision],
	}, provision)
	require.NoError(t, err)

	return plan
}

// roleLine returns the plan line naming role.
func roleLine(t *testing.T, plan awsdevops.SetupPlan, role string) string {
	t.Helper()
	for _, line := range plan.Text().Roles {
		if strings.Contains(line, role+" ") || strings.HasSuffix(line, role) {
			return strings.ToLower(line)
		}
	}
	t.Fatalf("no plan line names role %s in %q", role, plan.Text().Roles)

	return ""
}

func TestPlanSetupCreatesMissingRoles(t *testing.T) {
	plan := planWith(t, newFakeIAM(), true)

	for _, role := range planRoles {
		assert.Contains(t, roleLine(t, plan, role), "create")
	}
}

func TestPlanSetupUpdatesManagedRoles(t *testing.T) {
	iamClient := newFakeIAM()
	for _, role := range planRoles {
		iamClient.addManagedRole(role)
	}

	plan := planWith(t, iamClient, true)

	for _, role := range planRoles {
		line := roleLine(t, plan, role)
		assert.Contains(t, line, "update")
		assert.NotContains(t, line, "create")
	}
}

// A tag with the right key but wrong value is not ours.
func TestPlanSetupLeavesUnmanagedRoles(t *testing.T) {
	iamClient := newFakeIAM()
	for _, role := range planRoles {
		iamClient.existingRoles[role] = true
		iamClient.roleTags[role] = []iamtypes.Tag{{
			Key:   aws.String(awsdevops.ManagedTagKey),
			Value: aws.String("something-else"),
		}}
	}

	plan := planWith(t, iamClient, true)

	for _, role := range planRoles {
		line := roleLine(t, plan, role)
		assert.Contains(t, line, "unchanged")
		assert.NotContains(t, line, "update")
		assert.NotContains(t, line, "create")
	}
}

func TestPlanSetupEnablesTheOperatorApp(t *testing.T) {
	plan := planWith(t, newFakeIAM(), true)

	assert.Contains(t, strings.ToLower(plan.Text().OperatorApp), "enable")
}

// Connecting never touches roles or the app.
func TestPlanConnectLeavesRolesAndAppAlone(t *testing.T) {
	plan := planWith(t, newFakeIAM(), false)

	require.NotEmpty(t, plan.Text().Roles)
	for _, line := range plan.Text().Roles {
		lower := strings.ToLower(line)
		assert.NotContains(t, lower, "create")
		assert.NotContains(t, lower, "update")
	}
	require.NotEmpty(t, plan.Text().OperatorApp)
	assert.NotContains(t, strings.ToLower(plan.Text().OperatorApp), "enable")
}

func TestPlanSetupMakesNoIAMWrites(t *testing.T) {
	iamClient := newFakeIAM()
	iamClient.addManagedRole(awsdevops.AgentSpaceRoleName)

	_ = planWith(t, iamClient, true)

	assert.Empty(t, iamClient.createdRoles)
	assert.Empty(t, iamClient.trustPolicies)
	assert.Empty(t, iamClient.inlinePolicies)
	assert.Empty(t, iamClient.attachedPolicy)
}

func TestPlanNamesTheAccountAssociation(t *testing.T) {
	for name, links := range map[string][]agenttypes.Association{"keep": awsLink(), "associate": nil} {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{
				agentSpaces:  []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")},
				associations: links,
			}

			plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), runPlanOptions(), true)

			require.NoError(t, err)
			assert.Contains(t, strings.ToLower(plan.Text().Association), name)
			assert.Contains(t, plan.Text().Association, testAccountID)
		})
	}
}

func TestPlanConnectChangesNothingElse(t *testing.T) {
	agent := reuseWithApp(idcApp())
	opts := runPlanOptions()
	opts.AgentSpaceID = "space-7"

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, false)

	require.NoError(t, err)
	assert.Equal(t, "No change", plan.Text().Association)
	assert.Equal(t, "No change", plan.Text().OperatorApp)
	assert.Contains(t, agent.calls, "GetOperatorApp")
	assert.NotContains(t, agent.calls, "EnableOperatorApp")
}

// An app with no sign-in is off.
func TestPlanEnablesAppWithNoSignIn(t *testing.T) {
	agent := reuseWithApp(&devopsagent.GetOperatorAppOutput{})

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), runPlanOptions(), true)

	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(plan.Text().OperatorApp), "enable")
}

// The same flow is not a change.
func TestPlanSameFlowIsNotAChange(t *testing.T) {
	agent := reuseWithApp(&devopsagent.GetOperatorAppOutput{
		Iam:            &agenttypes.IamAuthConfiguration{},
		OperatorAppUrl: aws.String("https://space-7.aidevops.global.app.aws"),
	})
	opts := runPlanOptions()
	opts.AuthFlowSet = true

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(plan.Text().OperatorApp), "change")
	assert.Contains(t, plan.Text().OperatorApp, "IAM")
}

// Same IdC or IdP flow changes settings.
func TestPlanSameIdcFlowSaysUpdate(t *testing.T) {
	apps := map[string]*devopsagent.GetOperatorAppOutput{
		"idc": idcApp(),
		"idp": {Idp: &agenttypes.IdpAuthConfiguration{}, OperatorAppUrl: aws.String("https://space-7.aidevops.global.app.aws")},
	}
	for flow, app := range apps {
		t.Run(flow, func(t *testing.T) {
			agent := reuseWithApp(app)
			opts := runPlanOptions()
			opts.AuthFlow, opts.AuthFlowSet = flow, true

			plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

			require.NoError(t, err)
			assert.NotContains(t, plan.Text().OperatorApp, "again")
			assert.Contains(t, strings.ToLower(plan.Text().OperatorApp), "update")
		})
	}
}

func TestPlanAcceptsRegisteredMCPServiceID(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{launchDarklyMCPService("mcp-1")}}
	opts := runPlanOptions()
	opts.MCPServiceID = "mcp-1"

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
}

// An ID of a server at another endpoint is not a LaunchDarkly MCP server.
func TestPlanStopsOnAnMCPServiceIDAtAnotherEndpoint(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{
		mcpService("mcp-9", "Datadog", "https://mcp.datadoghq.com/mcp"),
	}}
	opts := runPlanOptions()
	opts.MCPServiceID = "mcp-9"

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.Error(t, err)
	assert.ErrorContains(t, err, "MCP server mcp-9 serves https://mcp.datadoghq.com/mcp")
	assert.ErrorContains(t, err, "--mcp-endpoint https://mcp.datadoghq.com/mcp")
}

// A LaunchDarkly MCP server that the customer hosts needs --mcp-endpoint.
func TestPlanAcceptsAnMCPServiceIDWithItsEndpoint(t *testing.T) {
	agent := &fakeAgent{services: []agenttypes.RegisteredService{
		mcpService("mcp-9", "LaunchDarkly", "https://mcp.example.internal/mcp"),
	}}
	opts := runPlanOptions()
	opts.MCPServiceID = "mcp-9"
	opts.MCPEndpoint = "https://mcp.example.internal/mcp"

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
	assert.Equal(t, "mcp-9", plan.MCP.ServiceID)
}

// The text of a connect plan comes from its steps.
func TestPlanTextOfAConnectPlan(t *testing.T) {
	for name, tc := range map[string]struct {
		flow string
		want string
	}{
		"app on":  {flow: "iam", want: "No change"},
		"app off": {want: "No change. The operator app is off"},
	} {
		t.Run(name, func(t *testing.T) {
			plan := awsdevops.SetupPlan{
				AccountID: testAccountID,
				Space:     awsdevops.SpaceStep{Action: awsdevops.SpaceConnect, ID: "space-7", Name: "platform"},
				App:       awsdevops.AppStep{Action: awsdevops.AppKeep, Flow: tc.flow},
				MCP:       awsdevops.MCPStep{Action: awsdevops.MCPRegister, Endpoint: awsdevops.MCPServerEndpoint},
			}

			text := plan.Text()

			assert.Equal(t, "Connect to agent space platform (space-7)", text.AgentSpace)
			assert.Equal(t, "No change", text.Association)
			assert.Equal(t, []string{"No change"}, text.Roles)
			assert.Equal(t, tc.want, text.OperatorApp)
			assert.Equal(t, "Register MCP server "+awsdevops.MCPServerName+" at "+awsdevops.MCPServerEndpoint, text.MCPServer)
		})
	}
}

func TestPlanTextOfASkippedMCPServer(t *testing.T) {
	plan := awsdevops.SetupPlan{MCP: awsdevops.MCPStep{Action: awsdevops.MCPSkip}}

	assert.Equal(t, "Skip MCP server "+awsdevops.MCPServerName+", because no token was given", plan.Text().MCPServer)
}

// The plan names the server by its name in AWS, which the customer can change.
func TestPlanNamesTheReusedMCPServerByItsName(t *testing.T) {
	for name, id := range map[string]string{"found by endpoint": "", "given by ID": "mcp-1"} {
		t.Run(name, func(t *testing.T) {
			agent := &fakeAgent{services: []agenttypes.RegisteredService{
				mcpService("mcp-1", "LaunchDarkly (prod)", awsdevops.MCPServerEndpoint),
			}}
			opts := runPlanOptions()
			opts.MCPServiceID = id

			plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, true)

			require.NoError(t, err)
			assert.Equal(t, "Reuse MCP server LaunchDarkly (prod) (mcp-1)", plan.Text().MCPServer)
		})
	}
}

// A connect plan never creates a space, so a used name is not a clash.
func TestPlanConnectIgnoresNewAgentSpace(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "platform")}}
	opts := runPlanOptions()
	opts.AgentSpaceName = "platform"
	opts.NewAgentSpace = true

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, false)

	require.NoError(t, err)
	assert.Equal(t, awsdevops.SpaceStep{Action: awsdevops.SpaceConnect, ID: "space-7", Name: "platform"}, plan.Space)
}

func TestPlanConnectWithoutANameAsksForTheID(t *testing.T) {
	opts := runPlanOptions()
	opts.AgentSpaceName = ""

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(&fakeAgent{}, newFakeIAM()), opts, false)

	assert.EqualError(t, err, "no agent space to connect to. Pass --agent-space-id")
}

// The replace plan names touched spaces.
func TestPlanReplaceNamesEachSpace(t *testing.T) {
	opts := runPlanOptions()
	opts.ReplaceMCPToken = true

	plan, err := awsdevops.PlanSetup(context.Background(), newTestClients(replaceAcrossSpaces(), newFakeIAM()), opts, true)

	require.NoError(t, err)
	assert.Contains(t, plan.Text().MCPServer, "space-1")
	assert.Contains(t, plan.Text().MCPServer, "space-3")
	assert.NotContains(t, plan.Text().MCPServer, "space-2")
}

// Two spaces with one name leave no safe choice.
func TestPlanStopsOnTwoSpacesWithTheName(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{
		{AgentSpaceId: aws.String("space-1"), Name: aws.String("launchdarkly")},
		{AgentSpaceId: aws.String("space-2"), Name: aws.String("LaunchDarkly")},
	}}

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), runPlanOptions(), true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--agent-space-id")
	assert.Contains(t, err.Error(), "space-1, space-2")
}

func awsLink() []agenttypes.Association {
	return []agenttypes.Association{{AssociationId: aws.String("assoc-aws"), ServiceId: aws.String("aws")}}
}

func runPlanOptions() awsdevops.SetupOptions {
	return awsdevops.SetupOptions{AgentSpaceName: "launchdarkly", AuthFlow: "iam", LDAccessToken: "api-token"}
}

func reuseWithApp(app *devopsagent.GetOperatorAppOutput) *fakeAgent {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-7", "launchdarkly")}}
	if app != nil {
		agent.operatorApps = map[string]*devopsagent.GetOperatorAppOutput{"space-7": app}
	}

	return agent
}

func idcApp() *devopsagent.GetOperatorAppOutput {
	return &devopsagent.GetOperatorAppOutput{
		Idc:            &agenttypes.IdcAuthConfiguration{},
		OperatorAppUrl: aws.String("https://space-7.aidevops.global.app.aws"),
	}
}

func replaceAcrossSpaces() *fakeAgent {
	return &fakeAgent{
		agentSpaces: []agenttypes.AgentSpace{
			namedSpace("space-1", "launchdarkly"), namedSpace("space-2", "other"), namedSpace("space-3", "third"),
		},
		services:   []agenttypes.RegisteredService{launchDarklyMCPService("mcp-old")},
		spaceLinks: map[string][]agenttypes.Association{"space-1": mcpLink("mcp-old"), "space-3": mcpLink("mcp-old")},
	}
}

func mcpLink(serviceID string) []agenttypes.Association {
	return []agenttypes.Association{{AssociationId: aws.String("assoc-" + serviceID), ServiceId: aws.String(serviceID)}}
}

// A connect plan needs a space, and it never plans to create one.
func TestPlanConnectWithoutASpaceStops(t *testing.T) {
	agent := &fakeAgent{agentSpaces: []agenttypes.AgentSpace{namedSpace("space-1", "other")}}
	opts := runPlanOptions()
	opts.AgentSpaceName = "missing"

	_, err := awsdevops.PlanSetup(context.Background(), newTestClients(agent, newFakeIAM()), opts, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no agent space named missing")
	assert.NotContains(t, agent.calls, "GetOperatorApp")
}
