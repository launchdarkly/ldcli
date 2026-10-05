package observability

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	o11y "github.com/launchdarkly/ldcli/internal/observability"
	"github.com/launchdarkly/ldcli/internal/resources"
)

// NewObservabilityCmd returns the `observability` command, which queries and manages
// LaunchDarkly observability data: logs, traces, errors, sessions, metrics, dashboards, and alerts.
func NewObservabilityCmd(client resources.Client, analyticsTrackerFn analytics.TrackerFn, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "observability",
		Aliases: []string{"o11y"},
		Short:   "Query logs, traces, errors, and sessions, and manage dashboards and alerts",
		Long: "Query LaunchDarkly observability data (logs, traces, error groups, sessions, and aggregated metrics)\n" +
			"and manage observability dashboards and alerts.\n\n" +
			"Time ranges default to the last 24 hours. --start-date and --end-date accept RFC 3339 timestamps,\n" +
			"dates, or durations before now such as 30m, 24h, or 7d.",
		Args: cobra.MinimumNArgs(1),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"observability",
				map[string]interface{}{
					"action": strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" observability "),
				}))
		},
	}

	cmd.PersistentFlags().String(backendURLFlag, o11y.DefaultBackendURL, "Observability API URL, for non-production LaunchDarkly instances")

	d := deps{client: client, version: version}
	cmd.AddCommand(
		newLogsCmd(d),
		newTracesCmd(d),
		newErrorGroupsCmd(d),
		newSessionsCmd(d),
		newAggregationsCmd(d),
		newKeysCmd(d),
		newServiceMapCmd(d),
		newDashboardsCmd(d),
		newAlertsCmd(d),
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func newGroupCmd(use, short string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
	}
	cmd.AddCommand(children...)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	return cmd
}
