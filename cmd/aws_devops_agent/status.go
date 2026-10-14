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
	clients, err := awsdevops.NewClients(cmd.Context(), mustString(cmd, regionFlag), mustString(cmd, profileFlag))
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
	if !anyRole && len(status.AgentSpaces) == 0 {
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
			_, _ = fmt.Fprintf(out, "  %s (%s)\n", serviceLabel(service), service.ServiceID)
		}
	}

	if len(status.AgentSpaces) == 0 {
		_, _ = fmt.Fprintf(out, "\nNo agent spaces. %s\n", setupHint)
	}
	for _, space := range status.AgentSpaces {
		_, _ = fmt.Fprintf(out, "\nAgent space %s (%s)\n", space.Name, space.AgentSpaceID)
		_, _ = fmt.Fprintf(out, "  Operator app: %s\n", space.OperatorAppURL)
		if len(space.Associations) > 0 {
			_, _ = fmt.Fprintln(out, "  Connected to:")
		}
		for _, association := range space.Associations {
			_, _ = fmt.Fprintf(out, "    %s: %s\n",
				associationLabel(status, association.ServiceID), plainState(association.Status))
		}
	}

	printConsoleURL(out, status.Region)
}

func printConsoleURL(out io.Writer, region string) {
	_, _ = fmt.Fprintf(out, "\nAWS DevOps Agent console: %s\n", awsdevops.ConsoleURL(region))
}

// associationLabel names the service by ID.
func associationLabel(status awsdevops.Status, serviceID string) string {
	if serviceID == awsdevops.AWSServiceID {
		return "AWS account " + status.AccountID
	}
	for _, service := range status.Services {
		if service.ServiceID == serviceID && serviceLabel(service) != "" {
			return serviceLabel(service)
		}
	}

	return "Service " + serviceID
}

func serviceLabel(service awsdevops.ServiceStatus) string {
	kind := service.ServiceType
	if kind == "mcpserver" {
		kind = "MCP server"
	}

	return strings.TrimSpace(service.Name + " " + kind)
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
