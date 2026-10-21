package sync

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

// NewSyncCmd creates the parent command for synchronization workflows.
// The transport sends each LaunchDarkly request of the sync commands.
func NewSyncCmd(transport syncapi.Transport, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize local resources with LaunchDarkly",
		Example: `  # Synchronize prompt variations
  ldcli sync prompts

  # Preview prompt synchronization changes
  ldcli sync prompts --dry-run`,
		Args: cobra.MinimumNArgs(1),
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

	cmd.AddCommand(NewPromptCmd(transport))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}
