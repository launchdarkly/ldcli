package awsdevopsagent

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func NewStatusCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "status",
		Short:  "Show what the AWS DevOps Agent setup has provisioned",
		Args:   cobra.NoArgs,
		PreRun: trackRun(analyticsTrackerFn),
		RunE:   runStatus,
	}

	cmd.Flags().String(regionFlag, "", "AWS region to inspect. Defaults to the region of the current AWS session")
	cmd.Flags().String(profileFlag, "", "Named AWS profile to use. Overrides AWS_PROFILE for this command")
	cmd.Flags().String(agentSpaceIDFlag, "", "Inspect a single agent space instead of every agent space")

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	clients, err := loadClients(cmd)
	if err != nil {
		return err
	}

	status, err := awsdevops.GetStatus(cmd.Context(), clients, mustString(cmd, agentSpaceIDFlag))
	if err != nil {
		return err
	}

	if cliflags.GetOutputKind(cmd) == "json" {
		return printJSON(cmd, status)
	}

	printStatus(cmd.OutOrStdout(), status)

	return nil
}

const setupHint = "Run ldcli aws-devops-agent setup."

func printStatus(out io.Writer, status awsdevops.Status) {
	_, _ = fmt.Fprintf(out, "AWS account %s in %s\n", status.AccountID, status.Region)

	anyRole := false
	for _, role := range status.Roles {
		anyRole = anyRole || role.Exists
	}
	if !anyRole && len(status.Services) == 0 && len(status.AgentSpaces) == 0 {
		_, _ = fmt.Fprintf(out, "\nNothing is set up. %s\n", setupHint)
		printConsoleURL(out, status.Region)

		return
	}

	_, _ = fmt.Fprintln(out, "\nIAM roles:")
	for _, role := range status.Roles {
		state := "missing"
		if role.Exists {
			state = "ready"
		}
		_, _ = fmt.Fprintf(out, "  %s: %s\n", role.Name, state)
	}

	if len(status.Services) > 0 {
		_, _ = fmt.Fprintln(out, "\nServices:")
		for _, service := range status.Services {
			label := serviceLabel(service)
			if label == "" {
				label = "Service"
			}
			_, _ = fmt.Fprintf(out, "  %s (%s)\n", label, service.ServiceID)
		}
	}

	if len(status.AgentSpaces) == 0 {
		_, _ = fmt.Fprintf(out, "\nNo agent spaces. %s\n", setupHint)
	}
	for _, space := range status.AgentSpaces {
		_, _ = fmt.Fprintf(out, "\nAgent space %s (%s)\n", space.Name, space.AgentSpaceID)
		app := space.OperatorAppURL
		switch {
		case space.OperatorAppError != "":
			app = "unknown (" + space.OperatorAppError + ")"
		case !space.OperatorAppEnabled:
			app = "off"
		}
		_, _ = fmt.Fprintf(out, "  Operator app: %s\n", app)
		if len(space.Associations) > 0 {
			_, _ = fmt.Fprintln(out, "  Connected to:")
		}
		for _, association := range space.Associations {
			_, _ = fmt.Fprintf(out, "    %s: %s\n",
				associationLabel(status, association), associationState(association))
		}
		if space.Skill != nil {
			_, _ = fmt.Fprintf(out, "  Skill %s: %s\n",
				assetLabel(awsdevops.ExperimentSkillName, *space.Skill),
				assetLine(*space.Skill, "SKILL.md", "--replace-skill"))
		}
		if space.Agent != nil {
			_, _ = fmt.Fprintf(out, "  Custom agent %s: %s\n",
				assetLabel(awsdevops.ExperimentAgentName, space.Agent.AssetStatus), agentLine(*space.Agent))
		}
	}

	printConsoleURL(out, status.Region)
}

// assetLabel is the name of the skill or the agent, with its asset ID.
func assetLabel(name string, asset awsdevops.AssetStatus) string {
	if asset.AssetID == "" {
		return name
	}

	return name + " (" + asset.AssetID + ")"
}

// assetLine gives the state of the skill or the agent, and what setup does
// with it. file is the file that setup writes, and flag is the setup flag that
// overwrites an edited one.
func assetLine(asset awsdevops.AssetStatus, file, flag string) string {
	switch asset.State {
	case awsdevops.AssetCurrent:
		return "this ldcli version"
	case awsdevops.AssetOutdated:
		return "an older ldcli version. Setup updates it"
	case awsdevops.AssetEdited:
		return "edited. Setup leaves it unless you pass " + flag
	case awsdevops.AssetNoFile:
		return "has no " + file + ". Setup leaves it unless you pass " + flag
	case awsdevops.AssetMissing:
		return "missing. " + setupHint
	}

	return "unknown (" + asset.Error + ")"
}

func agentLine(agent awsdevops.AgentStatus) string {
	line := assetLine(agent.AssetStatus, awsdevops.ExperimentAgentName+".md", "--replace-agent")
	if agent.State == awsdevops.AssetMissing || agent.AssetID == "" {
		return line
	}
	line += fmt.Sprintf(". It has %d tools", len(agent.Tools))
	if agent.SkillAttached != nil && !*agent.SkillAttached {
		line += " and not skill " + awsdevops.ExperimentSkillName
	}

	return line
}

func printConsoleURL(out io.Writer, region string) {
	_, _ = fmt.Fprintf(out, "\nAWS DevOps Agent console: %s\n", awsdevops.ConsoleURL(region))
}

// associationLabel names the service of an association.
func associationLabel(status awsdevops.Status, association awsdevops.AssociationStatus) string {
	if association.ServiceID == awsdevops.AWSServiceID {
		// An agent space can read other AWS accounts. Each of them is an
		// association with the service ID "aws" and its own account ID.
		accountID := association.AccountID
		if accountID == "" {
			accountID = status.AccountID
		}
		if association.AccountType == "source" {
			return "Source AWS account " + accountID
		}

		return "AWS account " + accountID
	}
	for _, service := range status.Services {
		if service.ServiceID == association.ServiceID && serviceLabel(service) != "" {
			return serviceLabel(service)
		}
	}

	return "Service " + association.ServiceID
}

// serviceKinds gives a plain name for each SDK service type. A type that is
// not in the map shows as AWS sends it.
var serviceKinds = map[string]string{
	"github":           "GitHub",
	"gitlab":           "GitLab",
	"slack":            "Slack",
	"azure":            "Azure",
	"azureidentity":    "Azure",
	"azuredevops":      "Azure DevOps",
	"dynatrace":        "Dynatrace",
	"servicenow":       "ServiceNow",
	"pagerduty":        "PagerDuty",
	"eventChannel":     "event channel",
	"remoteagent":      "remote agent",
	"remoteagentsigv4": "remote agent",
}

func serviceLabel(service awsdevops.ServiceStatus) string {
	kind := service.ServiceType
	// The SDK has one type for each MCP server vendor, for example
	// mcpserverdatadog. The name tells the vendor, so all of them are
	// "MCP server".
	if strings.HasPrefix(kind, "mcpserver") {
		kind = "MCP server"
	} else if plain, ok := serviceKinds[kind]; ok {
		kind = plain
	}

	return strings.TrimSpace(service.Name + " " + kind)
}

// associationState gives the state of an association and its MCP tools, if
// it has tools. AWS does not send a state for an MCP server association, so
// the tools tell that the association exists.
func associationState(association awsdevops.AssociationStatus) string {
	if len(association.Tools) == 0 {
		return plainState(association.Status)
	}
	state := "connected"
	if association.Status != "" {
		state = plainState(association.Status)
	}

	return state + " (tools: " + strings.Join(association.Tools, ", ") + ")"
}

func plainState(status string) string {
	switch status {
	case "valid":
		return "ready"
	case "invalid":
		return "not valid"
	case "pending-confirmation":
		return "waiting for confirmation"
	case "":
		return "state unknown"
	}

	return status
}
