package login

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/login"
	"github.com/launchdarkly/ldcli/internal/output"
)

func NewLoginCmd(
	analyticsTrackerFn analytics.TrackerFn,
	client login.Client,
) *cobra.Command {
	cmd := cobra.Command{
		Long: "",
		PreRun: func(cmd *cobra.Command, args []string) {
			analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "login", nil))
		},
		RunE:  run(client),
		Short: "Log in to your LaunchDarkly account to set up the CLI",
		Use:   "login",
	}

	helpFun := cmd.HelpFunc()
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		analyticsTrackerFn(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetBool(cliflags.AnalyticsOptOut),
		).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
			cmd,
			"login",
			map[string]interface{}{
				"action": "help",
			},
		))

		helpFun(cmd, args)
	})

	return &cmd
}

func run(client login.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		_, err := login.FetchDeviceAuthorization(
			client,
			login.ClientID,
			login.GetDeviceName(),
			viper.GetString(cliflags.BaseURIFlag),
		)
		if err != nil {
			return errors.NewError(output.CmdOutputError(viper.GetString(cliflags.OutputFlag), err))
		}

		return nil
	}
}
