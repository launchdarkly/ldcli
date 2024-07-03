package dev_server

import (
	"github.com/launchdarkly/ldcli/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/dev_server"
)

func NewDevServerCmd(client resources.Client, ldClient dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev-server",
		Short: "Development server",
		Long:  "Start and use a local development server for overriding flag values.",
	}

	cmd.PersistentFlags().String(
		cliflags.DevStreamURIFlag,
		cliflags.DevStreamURIDefault,
		cliflags.DevStreamURIDescription,
	)
	_ = viper.BindPFlag(cliflags.DevStreamURIFlag, cmd.PersistentFlags().Lookup(cliflags.DevStreamURIFlag))

	// Add subcommands here
	cmd.AddGroup(&cobra.Group{ID: "projects", Title: "Project commands:"})
	cmd.AddCommand(NewListProjectsCmd(client))
	cmd.AddCommand(NewGetProjectCmd(client))
	cmd.AddCommand(NewSyncProjectCmd(client))
	cmd.AddCommand(NewRemoveProjectCmd(client))
	cmd.AddCommand(NewAddProjectCmd(client))

	cmd.AddGroup(&cobra.Group{ID: "overrides", Title: "Override commands:"})
	cmd.AddCommand(NewAddOverrideCmd(client))
	cmd.AddCommand(NewRemoveOverrideCmd(client))
	cmd.AddGroup(&cobra.Group{ID: "server", Title: "Server commands:"})

	cmd.AddCommand(NewStartServerCmd(ldClient))
	cmd.AddCommand(NewUICmd())

	cmd.SetUsageTemplate(resourcecmd.SubcommandUsageTemplate())

	return cmd
}
