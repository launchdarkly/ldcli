package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/onboarding"
)

func NewOnboardCmd(
	analyticsTrackerFn analytics.TrackerFn,
) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Generate an AI-agent-compatible onboarding plan for integrating the LaunchDarkly SDK into your project. The output is a structured workflow of agent-skills that guide detection, installation, validation, and first flag creation.",
		Short: "Generate an AI-agent onboarding plan for SDK integration",
		Use:   "onboard",
		PreRun: func(cmd *cobra.Command, args []string) {
			analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "onboard", nil))
		},
		RunE: runOnboard(),
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initOnboardFlags(cmd)

	return cmd
}

func runOnboard() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		project := viper.GetString(cliflags.ProjectFlag)
		environment := viper.GetString(cliflags.EnvironmentFlag)

		plan := onboarding.BuildPlan(baseURI, project, environment)

		outputKind := cliflags.GetOutputKind(cmd)
		if outputKind == "json" {
			data, err := plan.ToJSON()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), plan.Plaintext())
		return nil
	}
}

func initOnboardFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.ProjectFlag, "default", "The project key")
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(cliflags.EnvironmentFlag, "test", "The environment key")
	_ = cmd.Flags().SetAnnotation(cliflags.EnvironmentFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
}
