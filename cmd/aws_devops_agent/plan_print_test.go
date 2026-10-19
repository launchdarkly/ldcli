package awsdevopsagent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func TestSetupWithYesFlagStillPrintsPlan(t *testing.T) {
	cmd, out := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients()

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.Contains(t, out.String(), testAccountID)
	assert.Contains(t, out.String(), "ld-devops-agent")
	assert.NotContains(t, out.String(), "Continue?")
	assert.Contains(t, w.calls, "CreateAgentSpace")
}

func TestSetupWithoutTerminalStillPrintsPlan(t *testing.T) {
	cmd, out := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, _ := countingClients()

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, false))

	assert.Contains(t, out.String(), testAccountID)
	assert.Contains(t, out.String(), "ld-devops-agent")
	assert.NotContains(t, out.String(), "Continue?")
}

// A JSON run shows the plan on stderr. Stdout must stay parseable.
func TestSetupJSONPrintsPlanToStderr(t *testing.T) {
	cmd, errOut := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, _ := countingClients()

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), false, false))

	assert.True(t, json.Valid([]byte(stdoutOf(cmd))), "stdout is not JSON: %s", stdoutOf(cmd))
	assert.Contains(t, errOut.String(), "Setup plan")
	assert.Contains(t, errOut.String(), testAccountID)
	assert.NotContains(t, errOut.String(), "Continue?")
}

func TestSetupPlanNamesRolesAndOperatorApp(t *testing.T) {
	cmd, out := setupTestCmd(t, "n\n")
	clients, _ := countingClients()

	_ = setupWith(cmd, clients, newSpaceOptions(), true, true)

	assert.Contains(t, out.String(), awsdevops.AgentSpaceRoleName)
	assert.Contains(t, out.String(), awsdevops.OperatorAppRoleName)
	assert.Contains(t, out.String(), "operator app")
}

func noTokenOptions() awsdevops.SetupOptions {
	opts := newSpaceOptions()
	opts.LDAccessToken = ""

	return opts
}

// The token prompt follows the run's interactive value.
func TestTokenPromptAsksWhenInteractive(t *testing.T) {
	cmd, _ := setupTestCmd(t, "y\napi-token\n")
	clients, w := countingClients()

	require.NoError(t, setupWith(cmd, clients, noTokenOptions(), true, true))

	assert.Contains(t, w.calls, "RegisterService")
}

// No token, no prompt: stop first.
func TestSetupNeedsATokenItCannotAsk(t *testing.T) {
	cases := map[string]struct {
		plaintext, interactive, yes bool
	}{
		"no terminal":     {plaintext: true},
		"json":            {},
		"yes in terminal": {plaintext: true, interactive: true, yes: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cmd, out := setupTestCmd(t, "")
			cmd.SetIn(noReadStdin{t})
			if c.yes {
				require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
			}
			clients, w := countingClients()

			err := setupWith(cmd, clients, noTokenOptions(), c.plaintext, c.interactive)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "--access-token")
			assert.Contains(t, err.Error(), awsdevops.AccessTokenURL(""))
			assert.Empty(t, w.calls)
			assert.NotContains(t, out.String(), "Setup plan")
		})
	}
}

// A registered server needs no token.
func TestSetupWithoutTokenReusesServer(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients()
	agent := clients.Agent.(countingAgent)
	agent.services = []agenttypes.RegisteredService{{
		ServiceId:   aws.String("mcp-1"),
		ServiceType: agenttypes.ServiceMcpServer,
		AdditionalServiceDetails: &agenttypes.AdditionalServiceDetailsMemberMcpserver{
			Value: agenttypes.RegisteredMCPServerDetails{Endpoint: aws.String(awsdevops.MCPServerEndpoint)},
		},
	}}
	clients.Agent = agent

	require.NoError(t, setupWith(cmd, clients, noTokenOptions(), true, false))

	assert.NotContains(t, w.calls, "RegisterService")
	assert.Contains(t, w.calls, "AssociateService")
}

func TestSetupPlanNamesAccountAssociation(t *testing.T) {
	cmd, out := setupTestCmd(t, "n\n")
	clients, _ := countingClients()

	_ = setupWith(cmd, clients, newSpaceOptions(), true, true)

	assert.Contains(t, strings.ToLower(out.String()), "associate aws account "+testAccountID)
}

func TestSetupRejectsUnknownMCPServiceID(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients()
	opts := newSpaceOptions()
	opts.MCPServiceID = "mcp-9"

	err := setupWith(cmd, clients, opts, true, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mcp-9")
	assert.NotErrorIs(t, err, awsdevops.ErrPlanChanged)
	assert.Empty(t, w.calls)
}

// --yes asks no question at all.
func TestSetupWithYesSkipsSpaceChoice(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")})

	err := setupWith(cmd, clients, newSpaceOptions(), true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--agent-space-id")
	assert.Empty(t, w.calls)
}

func TestSetupWithYesReusesMatchingSpace(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients(ldcliSpace("space-2", "ld-devops-agent"))

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.NotContains(t, w.calls, "CreateAgentSpace")
	assert.Contains(t, w.calls, "EnableOperatorApp")
}

func ldcliSpace(id, name string) agenttypes.AgentSpace {
	return agenttypes.AgentSpace{
		AgentSpaceId: aws.String(id),
		Name:         aws.String(name),
		Description:  aws.String("Managed by the LaunchDarkly CLI"),
	}
}

// The name alone does not make a space ours.
func TestSetupWithYesStopsOnASameNameSpaceItDidNotMake(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("ld-devops-agent")})

	err := setupWith(cmd, clients, newSpaceOptions(), true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--agent-space-id")
	assert.Empty(t, w.calls)
}

// Enter and an unattended run do the same thing on the space setup made.
func TestSetupEnterGivesFullSetupOnTheSpaceSetupMade(t *testing.T) {
	cmd, out := setupTestCmd(t, "\ny\n")
	clients, w := countingClients(
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("platform")},
		ldcliSpace("space-2", "ld-devops-agent"),
	)

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.Contains(t, out.String(), "connect to? [2] ")
	assert.Contains(t, out.String(), "Reuse agent space ld-devops-agent (space-2)")
	assert.Contains(t, w.calls, "EnableOperatorApp")
	assert.NotContains(t, w.calls, "CreateAgentSpace")
}

func TestSetupAgentSpaceIDGivesFullSetupOnTheSpaceSetupMade(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients(ldcliSpace("space-2", "ld-devops-agent"))
	opts := newSpaceOptions()
	opts.AgentSpaceID = "space-2"

	require.NoError(t, setupWith(cmd, clients, opts, true, false))

	assert.Contains(t, w.calls, "EnableOperatorApp")
}

func twoSimilarSpaces() (awsdevops.Clients, *writes) {
	return countingClients(
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("launchdarkly")},
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("LaunchDarkly")},
	)
}

// --auth-flow runs full setup on choice.
func TestSetupChosenSpaceWithAuthFlow(t *testing.T) {
	cmd, out := setupTestCmd(t, "2\ny\n")
	require.NoError(t, cmd.Flags().Set(authFlowFlag, "iam"))
	clients, w := twoSimilarSpaces()
	opts, err := setupOptions(cmd)
	require.NoError(t, err)
	opts.LDAccessToken = "api-token"

	require.NoError(t, setupWith(cmd, clients, opts, true, true))

	assert.Contains(t, out.String(), "Reuse agent space LaunchDarkly (space-2)")
	assert.Contains(t, w.calls, "EnableOperatorApp")
	assert.NotContains(t, w.calls, "CreateAgentSpace")
	assert.Contains(t, out.String(), "Reused agent space LaunchDarkly (space-2)")
	assert.NotContains(t, out.String(), "agent space launchdarkly (space-1)")
}

func TestSetupChosenSpaceWithoutAuthFlowConnects(t *testing.T) {
	cmd, _ := setupTestCmd(t, "2\ny\n")
	clients, w := twoSimilarSpaces()

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.NotContains(t, w.calls, "EnableOperatorApp")
	assert.NotContains(t, w.calls, "CreateRole")
	assert.Contains(t, stderrOf(cmd), "Using agent space LaunchDarkly (space-2)")
	assert.Contains(t, stderrOf(cmd), "No change. The operator app is off")
	assert.Contains(t, stdoutOf(cmd), "The operator app of agent space space-2 is off")
	assert.NotContains(t, stdoutOf(cmd), "Open the DevOps Agent at")
}

// A new space from the list asks for a name until no space uses it.
func TestSetupNewSpaceChoiceAsksForAnUnusedName(t *testing.T) {
	cmd, out := setupTestCmd(t, "2\nld-devops-agent\nfresh\ny\n")
	clients, w := countingClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("ld-devops-agent")})

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.Contains(t, out.String(), "An agent space named ld-devops-agent already exists")
	assert.Contains(t, out.String(), "Create agent space fresh")
	assert.Contains(t, w.calls, "CreateAgentSpace")
}
