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
	assert.Contains(t, mcp, "waiting for confirmation")
	assert.NotContains(t, mcp, "pending-confirmation")
}

func TestStatusPlainStateNamesEachState(t *testing.T) {
	cases := map[string]string{
		"valid":                "ready",
		"invalid":              "not valid",
		"pending-confirmation": "waiting for confirmation",
		"":                     "state unknown",
		"new-state":            "new-state",
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			assert.Equal(t, want, plainState(raw))
		})
	}
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

	assert.Contains(t, out, "Nothing is set up. Run ldcli aws-devops-agent setup.")
	assert.NotContains(t, out, "IAM roles:")
	assert.Contains(t, out, "123456789012")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(out), awsdevops.ConsoleURL("us-east-1")))
}

// A registered MCP server is not "nothing".
func TestStatusOnlyServiceShowsService(t *testing.T) {
	status := awsdevops.Status{
		AccountID: "123456789012",
		Region:    "us-east-1",
		Roles:     []awsdevops.RoleStatus{{Name: awsdevops.AgentSpaceRoleName}},
		Services:  []awsdevops.ServiceStatus{{ServiceID: "mcp-1", ServiceType: "mcpserver", Name: "LaunchDarkly"}},
	}

	out := renderStatus(status)

	assert.NotContains(t, out, "Nothing is set up")
	assert.Contains(t, out, "LaunchDarkly MCP server (mcp-1)")
	assert.Contains(t, out, "No agent spaces")
}

// One ready role is a setup that did not finish, not "nothing".
func TestStatusOneRoleShowsTheRoles(t *testing.T) {
	status := awsdevops.Status{
		AccountID: "123456789012",
		Region:    "us-east-1",
		Roles: []awsdevops.RoleStatus{
			{Name: awsdevops.AgentSpaceRoleName, Exists: true},
			{Name: awsdevops.OperatorAppRoleName},
		},
	}
	out := renderStatus(status)
	lines := strings.Split(out, "\n")

	assert.NotContains(t, out, "Nothing is set up")
	assert.Contains(t, lineWith(t, lines, awsdevops.AgentSpaceRoleName), "ready")
	assert.Contains(t, lineWith(t, lines, awsdevops.OperatorAppRoleName), "missing")
	assert.Contains(t, out, "No agent spaces")
}

// Each MCP server type of a vendor is an MCP server.
func TestStatusVendorMCPServerHasPlainLabel(t *testing.T) {
	status := realisticStatus()
	status.Services[0].ServiceType = "mcpserverdatadog"
	status.Services[0].Name = "Datadog"
	out := renderStatus(status)

	assert.Contains(t, out, "Datadog MCP server (s-ld01)")
	assert.NotContains(t, out, "mcpserverdatadog")
}

// Each AWS account association has the service ID "aws". The label names
// the account of the association, not the account that runs the command.
func TestStatusNamesEachAWSAccount(t *testing.T) {
	status := realisticStatus()
	space := &status.AgentSpaces[0]
	space.Associations = append(space.Associations,
		awsdevops.AssociationStatus{
			AssociationID: "a-003", ServiceID: "aws", Status: "valid",
			AccountID: "210987654321", AccountType: "source",
		},
		awsdevops.AssociationStatus{
			AssociationID: "a-004", ServiceID: "aws", Status: "pending-confirmation",
			AccountID: "333333333333", AccountType: "source",
		},
	)
	section := spaceSection(t, renderStatus(status), "Production")

	assert.Equal(t, "    AWS account 123456789012: ready", lineWith(t, section, "AWS account 123456789012"))
	assert.Equal(t, "    Source AWS account 210987654321: ready", lineWith(t, section, "210987654321"))
	assert.Equal(t, "    Source AWS account 333333333333: waiting for confirmation", lineWith(t, section, "333333333333"))
}

// An AWS association without a known configuration names the account of
// the command, as before.
func TestStatusAWSAssociationWithoutAccountUsesTheCallerAccount(t *testing.T) {
	status := realisticStatus()
	status.AgentSpaces[0].Associations[0].AccountID = ""
	status.AgentSpaces[0].Associations[0].AccountType = ""
	section := spaceSection(t, renderStatus(status), "Production")

	assert.Contains(t, lineWith(t, section, "AWS account"), "AWS account 123456789012")
}

func TestStatusServiceTypesHavePlainLabels(t *testing.T) {
	cases := map[string]string{
		"github":           "acme GitHub",
		"pagerduty":        "acme PagerDuty",
		"remoteagentsigv4": "acme remote agent",
		"mcpserversigv4":   "acme MCP server",
		"newtype":          "acme newtype",
	}
	for kind, want := range cases {
		t.Run(kind, func(t *testing.T) {
			assert.Equal(t, want, serviceLabel(awsdevops.ServiceStatus{ServiceType: kind, Name: "acme"}))
		})
	}
}

func TestStatusNoSpacesNamesSetupCommand(t *testing.T) {
	status := realisticStatus()
	status.AgentSpaces = nil

	assert.Contains(t, renderStatus(status), "ldcli aws-devops-agent setup")
}

func TestStatusFullSetupOmitsSetupHint(t *testing.T) {
	assert.NotContains(t, renderStatus(realisticStatus()), "ldcli aws-devops-agent setup")
}

func TestStatusShowsAnUnknownOperatorApp(t *testing.T) {
	status := realisticStatus()
	space := &status.AgentSpaces[0]
	space.OperatorAppEnabled = false
	space.OperatorAppURL = ""
	space.OperatorAppError = "AccessDeniedException"
	section := spaceSection(t, renderStatus(status), "Production")

	assert.Equal(t, "  Operator app: unknown (AccessDeniedException)", lineWith(t, section, "Operator app"))
	assert.Contains(t, strings.Join(section, "\n"), "LaunchDarkly MCP server: ready")
}

func TestStatusJSONUnchanged(t *testing.T) {
	cases := map[string]awsdevops.Status{
		"testdata/status.json":         realisticStatus(),
		"testdata/status-partial.json": partialStatus(),
	}
	for path, status := range cases {
		t.Run(path, func(t *testing.T) {
			want, err := os.ReadFile(path)
			require.NoError(t, err)

			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, printJSON(cmd, status))

			assert.JSONEq(t, string(want), out.String())
		})
	}
}

// A nameless service still gets a label.
func TestStatusServicesListLabelsBlankService(t *testing.T) {
	status := realisticStatus()
	status.Services[0] = awsdevops.ServiceStatus{ServiceID: "s-ld01"}

	for _, line := range strings.Split(renderStatus(status), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "s-ld01") {
			assert.False(t, strings.HasPrefix(trimmed, "("), "line has no label: %q", line)
		}
	}
}

// An app that is off has no working URL.
func TestStatusShowsAnOperatorAppThatIsOff(t *testing.T) {
	status := realisticStatus()
	status.AgentSpaces[0].OperatorAppEnabled = false
	status.AgentSpaces[0].OperatorAppURL = ""

	out := renderStatus(status)

	assert.Contains(t, out, "Operator app: off")
	assert.NotContains(t, out, "aidevops.global.app.aws")
}
