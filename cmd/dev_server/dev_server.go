package dev_server

import (
	"github.com/spf13/cobra"

	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/dev_server"
)

func NewDevServerCmd(localClient dev_server.LocalClient, ldClient dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev-server",
		Short: "Development server",
		Long:  "Start and use a local development server for overriding flag values.",
	}

	// Add subcommands here
	cmd.AddGroup(&cobra.Group{ID: "projects", Title: "Project commands:"})
	cmd.AddCommand(NewListProjectsCmd(localClient))
	cmd.AddCommand(NewGetProjectCmd(localClient))
	cmd.AddCommand(NewSyncProjectCmd(localClient))
	cmd.AddCommand(NewRemoveProjectCmd(localClient))
	cmd.AddCommand(NewAddProjectCmd(localClient))
	cmd.AddGroup(&cobra.Group{ID: "overrides", Title: "Override commands:"})
	cmd.AddCommand(NewAddOverrideCmd(localClient))
	cmd.AddCommand(NewRemoveOverrideCmd(localClient))
	cmd.AddGroup(&cobra.Group{ID: "server", Title: "Server commands:"})
	cmd.AddCommand(NewStartServerCmd(ldClient))

	cmd.SetUsageTemplate(resourcecmd.SubcommandUsageTemplate())

	return cmd
}
