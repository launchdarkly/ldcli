package sync

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewSyncCmd(client resources.Client, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize local resources with LaunchDarkly",
		Args:  cobra.MinimumNArgs(1),
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"sync",
				map[string]interface{}{"action": cmd.Name()},
			))
		},
	}

	cmd.AddCommand(NewPromptCmd(client))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}
