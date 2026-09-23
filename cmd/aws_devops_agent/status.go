package awsdevopsagent

import (
	"fmt"

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
	cmd.Flags().String(agentSpaceIDFlag, "", "Inspect a single agent space instead of every agent space")

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	clients, err := awsdevops.NewClients(cmd.Context(), mustString(cmd, regionFlag))
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

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Account %s in %s\n", status.AccountID, status.Region)
	for _, role := range status.Roles {
		if role.Exists {
			_, _ = fmt.Fprintf(out, "  role %s: %s\n", role.Name, role.ARN)

			continue
		}
		_, _ = fmt.Fprintf(out, "  role %s: missing\n", role.Name)
	}
	for _, space := range status.AgentSpaces {
		_, _ = fmt.Fprintf(out, "  agent space %s (%s)\n", space.Name, space.AgentSpaceID)
		for _, association := range space.Associations {
			_, _ = fmt.Fprintf(out, "    association %s: %s %s\n", association.AssociationID, association.ServiceID, association.Status)
		}
		for _, asset := range space.Assets {
			_, _ = fmt.Fprintf(out, "    asset %s: %s\n", asset.AssetID, asset.AssetType)
		}
	}

	return nil
}
