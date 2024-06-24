package dev_server

import (
	"github.com/spf13/cobra"

	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewDevServerCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev-server",
		Short: "Development server",
	}

	// Add subcommands here
	cmd.AddCommand(NewStartServerCmd())
	cmd.AddCommand(NewListProjectsCmd(client))
	cmd.AddCommand(NewGetProjectCmd(client))

	cmd.SetUsageTemplate(resourcecmd.SubcommandUsageTemplate())

	return cmd
}
