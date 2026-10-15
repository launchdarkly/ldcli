package awsdevopsagent

import (
	"bytes"
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

// JSON stdout must stay parseable.
func TestSetupJSONPrintsNoPlan(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	clients, _ := countingClients()

	require.NoError(t, setupWith(cmd, clients, newSpaceOptions(), false, false))

	assert.True(t, json.Valid(stdout.Bytes()), "stdout is not JSON: %s", stdout.String())
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
			old := canPrompt
			canPrompt = func() bool { return true }
			t.Cleanup(func() { canPrompt = old })
			cmd, out := setupTestCmd(t, "")
			cmd.SetIn(noReadStdin{t})
			if c.yes {
				require.NoError(t, cmd.Flags().Set(yesFlag, "true"))
			}
			clients, w := countingClients()

			err := setupWith(cmd, clients, noTokenOptions(), c.plaintext, c.interactive)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "--access-token")
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
