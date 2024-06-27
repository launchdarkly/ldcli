package login

import (
	"fmt"
	"strings"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/login"
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
		if ok, err := config.AccessTokenIsSet(viper.GetViper().ConfigFileUsed()); !ok {
			return err
		}

		deviceAuthorization, err := login.FetchDeviceAuthorization(
			client,
			login.ClientID,
			login.GetDeviceName(),
			viper.GetString(cliflags.BaseURIFlag),
		)
		if err != nil {
			return err
		}

		fullURL := fmt.Sprintf("%s/%s", viper.GetString(cliflags.BaseURIFlag), deviceAuthorization.VerificationURI)
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Your code is %s\n", deviceAuthorization.UserCode))
		b.WriteString("This code verifies your authentication with LaunchDarkly.\n")
		b.WriteString(fmt.Sprintf("If your browser did not open to confirm your login, visit %s\n", fullURL))
		fmt.Fprintln(cmd.OutOrStdout(), b.String())

		err = browser.OpenURL(fullURL)
		if err != nil {
			return err
		}

		deviceAuthorizationToken, err := login.FetchToken(
			client,
			deviceAuthorization.DeviceCode,
			viper.GetString(cliflags.BaseURIFlag),
			login.TokenInterval,
			login.MaxFetchTokenAttempts,
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Your token is %s\n", deviceAuthorizationToken.AccessToken)

		return nil
	}
}
