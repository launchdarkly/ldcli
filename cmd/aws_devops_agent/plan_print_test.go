package awsdevopsagent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

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

// A terminal alone does not allow a prompt.
func TestTokenPromptSilentWhenNotInteractive(t *testing.T) {
	old := canPrompt
	canPrompt = func() bool { return true }
	t.Cleanup(func() { canPrompt = old })
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients()

	require.NoError(t, setupWith(cmd, clients, noTokenOptions(), true, false))

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
	assert.Empty(t, w.calls)
}
