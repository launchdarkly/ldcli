package setup

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/setup"
	"github.com/launchdarkly/ldcli/internal/setup/agent"
)

// NewSetupCmd creates the top-level setup command and registers its hidden subcommands.
func NewSetupCmd(
	analyticsTrackerFn analytics.TrackerFn,
	clients setup.Clients,
	detector setup.Detector,
	installer setup.Installer,
) *cobra.Command {
	svc := setup.Service{
		Clients:     clients,
		Detector:    detector,
		Installer:   installer,
		Initializer: setup.Initializer{},
	}
	runner := agent.NewOpenCode()
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up LaunchDarkly in your project",
		Long: `Guided setup to integrate LaunchDarkly into your codebase.

Detects your project's language and framework, installs the correct SDK,
initializes it with your environment's SDK key, creates a feature flag,
and verifies the connection.

The setup agent adds the initialization code. It needs OpenCode. To skip the
project and environment screens, pass --project and --environment.`,
		PreRun: func(cmd *cobra.Command, args []string) {
			// Dim the notice and set it off with a blank line so it reads as a
			// transitional notice, visually distinct from command output.
			notice := mutedStyle.Render(
				"Notice: 'ldcli setup' now runs the new guided setup wizard (project detection, SDK installation, and initialization).\n" +
					"The previous quickstart wizard is still available via 'ldcli quickstart' during the transition period.")
			fmt.Fprintf(cmd.ErrOrStderr(), "%s\n\n", notice)
			analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "setup", nil))
		},
		RunE: runSetupWizard(analyticsTrackerFn, svc, runner),
	}
	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key. Setup skips the project screen.")
	cmd.Flags().String(cliflags.EnvironmentFlag, "", "The environment key. Setup skips the environment screen.")

	cmd.AddCommand(newDetectCmd(svc))
	cmd.AddCommand(newInstallCmd(svc))
	cmd.AddCommand(newInitCmd(svc))
	cmd.AddCommand(newAgentCmd(svc, runner))

	return cmd
}
