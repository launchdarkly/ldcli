package dev_server

import (
	"github.com/spf13/cobra"

	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/dev_server"
)

func NewDevServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev-server",
		Short: "Development server",
		Long:  "Start and use a local development server for overriding flag values.",
	}

	client := dev_server.NewClient()

	// Add subcommands here
	cmd.AddCommand(NewStartServerCmd())
	cmd.AddCommand(NewListProjectsCmd(client))
	cmd.AddCommand(NewGetProjectCmd(client))
	cmd.AddCommand(NewRemoveProjectCmd(client))
	cmd.AddCommand(NewAddProjectCmd(client))
	cmd.AddCommand(NewAddOverrideCmd(client))
	cmd.AddCommand(NewRemoveOverrideCmd(client))

	cmd.SetUsageTemplate(resourcecmd.SubcommandUsageTemplate())

	return cmd
}
