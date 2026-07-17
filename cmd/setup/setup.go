package setup

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/resources"
	"github.com/launchdarkly/ldcli/internal/setup"
)

// NewSetupCmd creates the top-level setup command and registers its hidden subcommands.
func NewSetupCmd(
	analyticsTrackerFn analytics.TrackerFn,
	resourcesClient resources.Client,
	flagsClient flags.Client,
	detector setup.Detector,
	installer setup.Installer,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up LaunchDarkly in your project",
		Long: `Guided setup to integrate LaunchDarkly into your codebase.

Detects your project's language and framework, installs the correct SDK,
initializes it with your environment's SDK key, creates a feature flag,
and verifies the connection.`,
		PreRun: func(cmd *cobra.Command, args []string) {
			// Dim the notice and set it off with a blank line so it reads as a
			// transitional notice, visually distinct from command output.
			notice := lipgloss.NewStyle().Faint(true).Render(
				"Notice: 'ldcli setup' now runs the new guided setup wizard (project detection, SDK installation, and initialization).\n" +
					"The previous quickstart wizard is still available via 'ldcli quickstart' during the transition period.")
			fmt.Fprintf(cmd.ErrOrStderr(), "%s\n\n", notice)
			analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "setup", nil))
		},
		RunE: runSetupWizard(analyticsTrackerFn, resourcesClient, flagsClient, detector, installer),
	}

	cmd.AddCommand(newDetectCmd(detector))
	cmd.AddCommand(newInstallCmd(installer))
	cmd.AddCommand(newInitCmd())

	return cmd
}
