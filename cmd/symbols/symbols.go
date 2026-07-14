package symbols

import (
	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewSymbolsCmd(client resources.Client, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "symbols",
		Short: "Manage symbol files",
		Long:  "Manage symbol files (for example, React Native sourcemaps) for LaunchDarkly error monitoring",
		Args:  cobra.MinimumNArgs(1),
	}

	cmd.AddCommand(NewUploadCmd(client, analyticsTrackerFn))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}
