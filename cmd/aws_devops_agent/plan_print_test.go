package awsdevopsagent

import (
	"encoding/json"
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

func TestTokenPromptSilentWhenNotInteractive(t *testing.T) {
	cmd, _ := setupTestCmd(t, "")
	cmd.SetIn(noReadStdin{t})
	clients, w := countingClients()

	_ = setupWith(cmd, clients, noTokenOptions(), true, false)

	assert.NotContains(t, w.calls, "RegisterService")
}
