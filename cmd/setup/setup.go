package setup

import (
	"context"
	"fmt"
	"time"

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
	// With LDCLI_SETUP_AGENT_SDK_KEY set, the agent reads its instructions and
	// model from the setup-agent AI Config. Without it, the built-in
	// configuration applies.
	source := agent.ConfigSourceFromEnv()
	runner.Config = source
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up LaunchDarkly in your project",
		Long: `Guided setup to integrate LaunchDarkly into your codebase.

Detects your project's language and framework, installs the correct SDK,
initializes it with your environment's SDK key, creates a feature flag,
and verifies the connection.

With --agent=auto (the default), setup uses the setup agent to add the
initialization code if OpenCode is installed, and the built-in templates if not.`,
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
		RunE: closingSource(source, runSetupWizard(analyticsTrackerFn, svc, runner)),
	}
	cmd.Flags().String(agentFlag, "auto", "Use the setup agent to add initialization code: auto, on, or off")

	cmd.AddCommand(newDetectCmd(svc))
	cmd.AddCommand(newInstallCmd(svc))
	cmd.AddCommand(newInitCmd(svc))
	agentCmd := newAgentCmd(svc, runner)
	agentCmd.RunE = closingSource(source, agentCmd.RunE)
	cmd.AddCommand(agentCmd)

	return cmd
}

// closingSource closes the config source after the command runs, so the AI
// SDK sends its events before ldcli exits.
func closingSource(source agent.ConfigSource, run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	closer, ok := source.(interface{ Close(context.Context) error })
	if !ok {
		return run
	}
	return func(cmd *cobra.Command, args []string) error {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = closer.Close(ctx)
		}()
		return run(cmd, args)
	}
}
