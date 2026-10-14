package awsdevopsagent

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func renderStatus(status awsdevops.Status) string {
	var out bytes.Buffer
	printStatus(&out, status)

	return out.String()
}

// spaceSection returns the lines under one agent space.
func spaceSection(t *testing.T, out, spaceName string) []string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.Contains(line, spaceName) {
			var section []string
			for _, next := range lines[i+1:] {
				if strings.Contains(next, "console") {
					break
				}
				section = append(section, next)
			}

			return section
		}
	}
	t.Fatalf("output does not name agent space %q:\n%s", spaceName, out)

	return nil
}

func lineWith(t *testing.T, lines []string, needle string) string {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line contains %q in:\n%s", needle, strings.Join(lines, "\n"))

	return ""
}

func TestStatusAssociationNamesService(t *testing.T) {
	out := renderStatus(realisticStatus())
	section := spaceSection(t, out, "Production")

	mcp := lineWith(t, section, "LaunchDarkly")
	assert.Contains(t, mcp, "MCP server")
	assert.Contains(t, strings.Join(section, "\n"), "AWS account")
}

func TestStatusAssociationShowsPlainState(t *testing.T) {
	status := realisticStatus()
	status.AgentSpaces[0].Associations[1].Status = "pending-confirmation"
	section := spaceSection(t, renderStatus(status), "Production")

	assert.Contains(t, lineWith(t, section, "AWS account"), "ready")
	mcp := lineWith(t, section, "LaunchDarkly")
	assert.NotContains(t, mcp, "pending-confirmation")
	assert.NotContains(t, mcp, "ready")
}

func TestStatusPlaintextHasNoARN(t *testing.T) {
	assert.NotContains(t, renderStatus(realisticStatus()), "arn:aws:")
}

func TestStatusRoleShowsState(t *testing.T) {
	status := realisticStatus()
	status.Roles[1] = awsdevops.RoleStatus{Name: awsdevops.OperatorAppRoleName}
	lines := strings.Split(renderStatus(status), "\n")

	assert.Contains(t, lineWith(t, lines, awsdevops.AgentSpaceRoleName), "ready")
	assert.Contains(t, lineWith(t, lines, awsdevops.OperatorAppRoleName), "missing")
}

func TestStatusUnknownServiceShowsID(t *testing.T) {
	status := realisticStatus()
	status.AgentSpaces[0].Associations[1].ServiceID = "s-gone99"
	section := spaceSection(t, renderStatus(status), "Production")

	line := lineWith(t, section, "s-gone99")
	assert.NotContains(t, line, "()")
	assert.NotContains(t, line, "LaunchDarkly")
}

func TestStatusBlankServiceShowsID(t *testing.T) {
	status := realisticStatus()
	status.Services[0] = awsdevops.ServiceStatus{ServiceID: "s-ld01"}
	section := spaceSection(t, renderStatus(status), "Production")

	assert.Contains(t, strings.Join(section, "\n"), "s-ld01")
}

func TestStatusNamesAccountRegionConsole(t *testing.T) {
	out := renderStatus(realisticStatus())

	assert.Contains(t, out, "123456789012")
	assert.Contains(t, out, "us-east-1")
	assert.Contains(t, out, "https://as-prod01.aidevops.global.app.aws")
	assert.Contains(t, out, "as-prod01")
	trimmed := strings.TrimSpace(out)
	assert.True(t, strings.HasSuffix(trimmed, awsdevops.ConsoleURL("us-east-1")), "output must end with the console URL")
}

func TestStatusEmptyNamesSetupCommand(t *testing.T) {
	empty := awsdevops.Status{
		AccountID: "123456789012",
		Region:    "us-east-1",
		Roles: []awsdevops.RoleStatus{
			{Name: awsdevops.AgentSpaceRoleName},
			{Name: awsdevops.OperatorAppRoleName},
		},
	}
	out := renderStatus(empty)

	assert.Contains(t, out, "ldcli aws-devops-agent setup")
	assert.Contains(t, out, "123456789012")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(out), awsdevops.ConsoleURL("us-east-1")))
}

func TestStatusNoSpacesNamesSetupCommand(t *testing.T) {
	status := realisticStatus()
	status.AgentSpaces = nil

	assert.Contains(t, renderStatus(status), "ldcli aws-devops-agent setup")
}

func TestStatusFullSetupOmitsSetupHint(t *testing.T) {
	assert.NotContains(t, renderStatus(realisticStatus()), "ldcli aws-devops-agent setup")
}

func TestStatusJSONUnchanged(t *testing.T) {
	want, err := os.ReadFile("testdata/status.json")
	require.NoError(t, err)

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, printJSON(cmd, realisticStatus()))

	assert.JSONEq(t, string(want), out.String())
}
