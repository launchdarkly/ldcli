package awsdevopsagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/devopsagent"
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
	assert.Contains(t, out.String(), "Skill:         Create skill "+awsdevops.ExperimentSkillName)
}

// A canceled plan does not create the skill.
func TestSetupCanceledCreatesNoSkill(t *testing.T) {
	cmd, _ := setupTestCmd(t, "n\n")
	clients, w := countingClients()

	require.Error(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.NotContains(t, w.calls, "CreateAsset")
}

func noTokenOptions() awsdevops.SetupOptions {
	opts := newSpaceOptions()
	opts.LDAccessToken = ""

	return opts
}

// The token prompt comes before the plan question.
func TestTokenPromptAsksWhenInteractive(t *testing.T) {
	cmd, _ := setupTestCmd(t, "api-token\ny\n")
	clients, w := countingClients()

	require.NoError(t, setupWith(cmd, clients, noTokenOptions(), true, true))

	assert.Contains(t, w.calls, "RegisterService")
	assert.Less(t, strings.Index(stderrOf(cmd), "Paste the token"), strings.Index(stderrOf(cmd), "Setup plan:"))
	// The plan is built again with the token, so it is current.
	assert.Contains(t, stderrOf(cmd), "Register a new MCP server for this agent space at")
	assert.NotContains(t, stderrOf(cmd), "Ask for a token")
}

// A rejected token stops setup before any AWS write.
func TestSetupChecksAPastedTokenFirst(t *testing.T) {
	saved := awsdevops.CheckToken
	t.Cleanup(func() { awsdevops.CheckToken = saved })
	awsdevops.CheckToken = func(context.Context, string, string) error { return awsdevops.ErrInvalidAccessToken }
	cmd, _ := setupTestCmd(t, "expired-token\ny\n")
	clients, w := countingClients()

	err := setupWith(cmd, clients, noTokenOptions(), true, true)

	require.ErrorIs(t, err, awsdevops.ErrInvalidAccessToken)
	assert.Empty(t, w.calls)
	assert.NotContains(t, stderrOf(cmd), "Setup plan:")
}

// Enter skips the MCP server, and the end names the next step.
func TestSetupSkippedTokenNamesTheNextStep(t *testing.T) {
	cmd, _ := setupTestCmd(t, "\ny\n")
	clients, w := countingClients()

	require.NoError(t, setupWith(cmd, clients, noTokenOptions(), true, true))

	assert.NotContains(t, w.calls, "RegisterService")
	assert.Contains(t, stderrOf(cmd), "Skip MCP server LaunchDarkly")
	assert.NotContains(t, stderrOf(cmd), "Ask for a token")
	assert.Contains(t, stdoutOf(cmd), "The LaunchDarkly MCP server is not connected")
}

// Without a token, a connect run has nothing to do.
func TestSetupConnectWithoutATokenStops(t *testing.T) {
	cmd, _ := setupTestCmd(t, "2\n\n")
	clients, w := twoSimilarSpaces()

	err := setupWith(cmd, clients, noTokenOptions(), true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nothing to do on agent space LaunchDarkly (space-2)")
	assert.Empty(t, w.calls)
	assert.NotContains(t, stderrOf(cmd), "Continue?")
}

// Progress goes to stderr, so a json run shows it, and stdout stays JSON.
func TestSetupJSONShowsProgressOnStderr(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, _ := countingClients()

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), false, false))

	assert.Contains(t, stderrOf(cmd), "Created agent space")
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdoutOf(cmd)), &result))
}

func TestPlanNamesTheAWSDefaultProfile(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	t.Setenv("AWS_DEFAULT_PROFILE", "corp")

	assert.Equal(t, "corp", profileLabel(cmd))
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

// The server of the agent space needs no token.
func TestSetupWithoutTokenReusesServer(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("platform")})
	agent := clients.Agent.(countingAgent)
	agent.associations = []agenttypes.Association{{AssociationId: aws.String("assoc-1"), ServiceId: aws.String("mcp-1")}}
	agent.services = []agenttypes.RegisteredService{{
		ServiceId:   aws.String("mcp-1"),
		ServiceType: agenttypes.ServiceMcpServer,
		AdditionalServiceDetails: &agenttypes.AdditionalServiceDetailsMemberMcpserver{
			Value: agenttypes.RegisteredMCPServerDetails{Endpoint: aws.String(awsdevops.MCPServerEndpoint)},
		},
	}}
	clients.Agent = agent

	opts := noTokenOptions()
	opts.AgentSpaceName, opts.AgentSpaceID = "", "space-1"

	require.NoError(t, setupWith(cmd, clients, opts, true, false))

	assert.NotContains(t, w.calls, "RegisterService")
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
	opts := newSpaceOptions()
	opts.AgentSpaceName = ""

	err := setupWith(cmd, clients, opts, true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--agent-space-id")
	assert.Contains(t, err.Error(), "--agent-space-name")
	assert.Empty(t, w.calls)
}

// The same command works on the first run and on every later run.
func TestSetupWithYesCreatesTheNamedSpaceWhenNoSpaceHasTheName(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("platform")})

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.Contains(t, stderrOf(cmd), "Create agent space ld-devops-agent")
	assert.Contains(t, w.calls, "CreateAgentSpace")
}

func TestSetupWithYesStopsOnTwoSpacesWithTheName(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients(
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("ld-devops-agent")},
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("LD-DevOps-Agent")},
	)

	err := setupWith(cmd, clients, newSpaceOptions(), true, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "space-1")
	assert.Contains(t, err.Error(), "space-2")
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

// A named space that setup did not make only gets the MCP server.
func TestSetupWithYesConnectsToANamedSpaceItDidNotMake(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
	clients, w := countingClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-2"), Name: aws.String("ld-devops-agent")})

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.Contains(t, stderrOf(cmd), "Connect to agent space ld-devops-agent (space-2)")
	assert.Contains(t, w.calls, "AssociateService")
	assert.NotContains(t, w.calls, "CreateRole")
	assert.NotContains(t, w.calls, "EnableOperatorApp")
	assert.NotContains(t, w.calls, "CreateAgentSpace")
}

// With a name, Enter picks the space with that name, which setup made.
func TestSetupEnterGivesFullSetupOnTheSpaceSetupMade(t *testing.T) {
	cmd, out := setupTestCmd(t, "\ny\n")
	clients, w := countingClients(
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("platform")},
		ldcliSpace("space-2", "ld-devops-agent"),
	)

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, true))

	assert.Contains(t, out.String(), "start 2\n")
	assert.Contains(t, out.String(), "Reuse agent space ld-devops-agent (space-2)")
	assert.Contains(t, w.calls, "EnableOperatorApp")
	assert.NotContains(t, w.calls, "CreateAgentSpace")
}

// Without a name, Enter does what an unattended run does: it picks the one
// space that setup made.
func TestSetupEnterWithoutANamePicksTheSpaceSetupMade(t *testing.T) {
	cmd, out := setupTestCmd(t, "\ny\n")
	clients, w := countingClients(
		agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("platform")},
		ldcliSpace("space-2", "ld-devops-agent"),
	)
	opts := newSpaceOptions()
	opts.AgentSpaceName = ""

	require.NoError(t, setupWith(cmd, clients, opts, true, true))

	assert.Contains(t, out.String(), "start 2\n")
	assert.Contains(t, out.String(), "Reuse agent space ld-devops-agent (space-2)")
	assert.Contains(t, w.calls, "EnableOperatorApp")
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
	assert.Contains(t, stdoutOf(cmd), "use the AWS console")
	assert.NotContains(t, stdoutOf(cmd), "--auth-flow")
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

// failingAssociate fails every association, after the space exists.
type failingAssociate struct{ countingAgent }

func (failingAssociate) AssociateService(context.Context, *devopsagent.AssociateServiceInput, ...func(*devopsagent.Options)) (*devopsagent.AssociateServiceOutput, error) {
	return nil, errors.New("ValidationException: role not ready")
}

// A failed run names the space that it made.
func TestSetupFailureNamesTheNewSpace(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients()
	clients.Agent = failingAssociate{clients.Agent.(countingAgent)}

	err := setupWith(cmd, clients, newSpaceOptions(), false, false)

	require.Error(t, err)
	assert.Contains(t, w.calls, "CreateAgentSpace")
	assert.Contains(t, err.Error(), "Setup created agent space ld-devops-agent (space-new)")
	// The same flags stop on the name of the new space, so give the ID.
	assert.Contains(t, err.Error(), "To use it, run setup again with --agent-space-id space-new")
}

// A server that the space gets between the two plans is reused, and setup
// says that it does not use the pasted token.
func TestSetupSaysWhenTheSecondPlanReusesAServer(t *testing.T) {
	cmd, _ := setupTestCmd(t, "api-token\n")
	clients, _ := countingClients(agenttypes.AgentSpace{AgentSpaceId: aws.String("space-1"), Name: aws.String("platform")})
	agent := clients.Agent.(countingAgent)
	agent.associations = []agenttypes.Association{{AssociationId: aws.String("assoc-7"), ServiceId: aws.String("mcp-7")}}
	agent.services = []agenttypes.RegisteredService{{
		ServiceId:   aws.String("mcp-7"),
		ServiceType: agenttypes.ServiceMcpServer,
		AdditionalServiceDetails: &agenttypes.AdditionalServiceDetailsMemberMcpserver{
			Value: agenttypes.RegisteredMCPServerDetails{Name: aws.String("Prod LD"), Endpoint: aws.String(awsdevops.MCPServerEndpoint)},
		},
	}}
	clients.Agent = agent
	opts := noTokenOptions()
	opts.AgentSpaceName, opts.AgentSpaceID = "", "space-1"
	first := awsdevops.SetupPlan{MCP: awsdevops.MCPStep{Action: awsdevops.MCPAsk}}

	plan, err := planWithToken(cmd, clients, &opts, first, true)

	require.NoError(t, err)
	assert.Equal(t, awsdevops.MCPReuse, plan.MCP.Action)
	assert.Contains(t, stderrOf(cmd), "MCP server Prod LD (mcp-7) was registered after the first plan")
	assert.Contains(t, stderrOf(cmd), "--access-token and --replace-mcp-token")
}

type failingSkillAgent struct{ countingAgent }

func (failingSkillAgent) CreateAsset(context.Context, *devopsagent.CreateAssetInput, ...func(*devopsagent.Options)) (*devopsagent.CreateAssetOutput, error) {
	return nil, errors.New("AccessDeniedException: no aidevops:CreateAsset")
}

// A failed skill step warns at the end, and setup still prints the app URL.
func TestSetupWarnsWhenSkillFails(t *testing.T) {
	cmd, errOut := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients()
	clients.Agent = failingSkillAgent{clients.Agent.(countingAgent)}

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), true, false))

	assert.Contains(t, w.calls, "RegisterService")
	assert.Contains(t, errOut.String(), "Warning: unable to create skill "+awsdevops.ExperimentSkillName)
	assert.Contains(t, stdoutOf(cmd), "The "+awsdevops.ExperimentSkillName+" skill is not set up")
	assert.Contains(t, stdoutOf(cmd), "Open the DevOps Agent at:")
}
