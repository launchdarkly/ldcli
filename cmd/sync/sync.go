package sync

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
)

// NewSyncCmd creates the parent command for synchronization workflows.
// The cliVersion is the ldcli version that each LaunchDarkly request names.
func NewSyncCmd(cliVersion string, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
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

	cmd.AddCommand(NewPromptCmd(cliVersion))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}
