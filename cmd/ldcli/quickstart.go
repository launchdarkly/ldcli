package cmd

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/ldcli/analytics"
	"github.com/launchdarkly/ldcli/cmd/ldcli/cliflags"
	"github.com/launchdarkly/ldcli/cmd/ldcli/validators"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/quickstart"
)

func NewQuickStartCmd(
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
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "setup", nil))
		},
		RunE:  runQuickStart(analyticsTrackerFn, environmentsClient, flagsClient),
		Short: "Setup guide to create your first feature flag",
		Use:   "setup",
	}
}

func runQuickStart(
	analyticsTrackerFn analytics.TrackerFn,
	environmentsClient environments.Client,
	flagsClient flags.Client,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		f, err := tea.LogToFile("debug.log", "")
		if err != nil {
			fmt.Println("could not open file for debugging:", err)
			os.Exit(1)
		}
		defer f.Close()

		analyticsTracker := analyticsTrackerFn(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetBool(cliflags.AnalyticsOptOut),
		)
		_, err = tea.NewProgram(
			quickstart.NewContainerModel(
				analyticsTracker,
				environmentsClient,
				flagsClient,
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
			),
			tea.WithAltScreen(),
		).Run()
		if err != nil {
			log.Fatal(err)
		}

		return nil
	}
}
