package login

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	configcmd "github.com/launchdarkly/ldcli/cmd/config"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/login"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewLoginCmd(
	analyticsTrackerFn analytics.TrackerFn,
	client resources.UnauthenticatedClient,
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

func run(client resources.UnauthenticatedClient) func(*cobra.Command, []string) error {
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

		fullURL, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			deviceAuthorization.VerificationURI,
		)
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Your code is %s\n", deviceAuthorization.UserCode))
		b.WriteString("This code verifies your authentication with LaunchDarkly.\n")
		b.WriteString(fmt.Sprintf("Opening your browser to %s to finish verifying your login.\n", fullURL))
		b.WriteString("If your browser does not open automatically, you can paste the above URL into your browser.")
		fmt.Fprintln(cmd.OutOrStdout(), b.String())

		_ = browser.OpenURL(fullURL)

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

		conf, err := config.New(viper.ConfigFileUsed(), os.ReadFile)
		if err != nil {
			return err
		}
		conf, _, err = conf.Update([]string{cliflags.AccessTokenFlag, deviceAuthorizationToken})
		if err != nil {
			return err
		}

		err = configcmd.Write(conf, configcmd.SetKey)
		if err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Your token has been written to the configuration file")

		return nil
	}
}
