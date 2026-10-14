package awsdevopsagent

import (
	"bytes"
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
