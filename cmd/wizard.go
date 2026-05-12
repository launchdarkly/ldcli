package cmd

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/wizard"
)

func NewWizardCmd(
	analyticsTrackerFn analytics.TrackerFn,
	environmentsClient environments.Client,
	flagsClient flags.Client,
) *cobra.Command {
	return &cobra.Command{
		Args: validators.Validate(),
		Long: "",
		PreRun: func(cmd *cobra.Command, args []string) {
			analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "quickstart", nil))
		},
		RunE:  runWizard(analyticsTrackerFn, environmentsClient, flagsClient),
		Short: "Connect LaunchDarkly to your existing project",
		Use:   "quickstart",
	}
}

func runWizard(
	analyticsTrackerFn analytics.TrackerFn,
	environmentsClient environments.Client,
	flagsClient flags.Client,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		workDir, err := os.Getwd()
		if err != nil {
			workDir = "."
		}

		_, err = tea.NewProgram(
			wizard.NewContainerModel(
				environmentsClient,
				flagsClient,
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				workDir,
			),
			tea.WithAltScreen(),
		).Run()
		return err
	}
}
