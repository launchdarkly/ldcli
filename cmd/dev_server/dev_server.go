package dev_server

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/internal/analytics"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewDevServerCmd(client resources.Client, analyticsTrackerFn analytics.TrackerFn, ldClient dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev-server",
		Short: "Development server",
		Long:  "Start and use a local development server for overriding flag values.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {

			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"dev-server",
				map[string]interface{}{
					"action": cmd.Name(),
				}))
		},
	}

	cmd.PersistentFlags().String(
		cliflags.DevStreamURIFlag,
		cliflags.DevStreamURIDefault,
		cliflags.DevStreamURIDescription,
	)
	_ = viper.BindPFlag(cliflags.DevStreamURIFlag, cmd.PersistentFlags().Lookup(cliflags.DevStreamURIFlag))

	cmd.PersistentFlags().String(
		cliflags.PortFlag,
		cliflags.PortDefault,
		cliflags.PortFlagDescription,
	)

	_ = viper.BindPFlag(cliflags.PortFlag, cmd.PersistentFlags().Lookup(cliflags.PortFlag))

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

func getDevServerUrl() string {
	return fmt.Sprintf("http://localhost:%s", viper.GetString(cliflags.PortFlag))
}
