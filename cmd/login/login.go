package login

import (
	"fmt"
	"os"
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

		v, err := getViperWithConfigFile()
		if err != nil {
			return err
		}
		err = writeConfig2(conf, v, func(key string, value interface{}, v *viper.Viper) {
			v.Set(key, value)
		})
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Your token has been written to the configuration file")

		return nil
	}
}

func getViperWithConfigFile() (*viper.Viper, error) {
	v := viper.GetViper()
	if err := v.ReadInConfig(); err != nil {
		newViper := viper.New()
		configFile := config.GetConfigFile()
		newViper.SetConfigType("yml")
		newViper.SetConfigFile(configFile)

		err = newViper.WriteConfig()
		if err != nil {
			return nil, err
		}

		return newViper, nil
	}

	return v, nil
}

func writeConfig2(
	conf config.Config,
	v *viper.Viper,
	filterFn func(key string, value interface{}, v *viper.Viper),
) error {
	// create a new viper instance since the existing one has the command name and flags already set,
	// and these will get written to the config file.
	newViper := viper.New()
	newViper.SetConfigFile(v.ConfigFileUsed())

	for key, value := range conf.RawConfig {
		filterFn(key, value, newViper)
	}

	err := newViper.WriteConfig()
	if err != nil {
		return err
	}

	return nil
}
