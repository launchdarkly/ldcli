package login

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	errs "github.com/launchdarkly/ldcli/internal/errors"
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
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		var b strings.Builder
		b.WriteString(fmt.Sprintf("Your code is %s\n", deviceAuthorization.UserCode))
		b.WriteString("This code verifies your authentication with LaunchDarkly.\n")
		b.WriteString(
			fmt.Sprintf(
				"Press Enter to open the browser or visit %s/%s (^C to quit)\n",
				viper.GetString(cliflags.BaseURIFlag),
				deviceAuthorization.VerificationURI,
			),
		)
		fmt.Fprintln(cmd.OutOrStdout(), b.String())

		ticker := time.NewTicker(2000 * time.Millisecond)
		var attempts int
		maxAttempts := 3
		var deviceAuthorizationToken login.DeviceAuthorizationToken
		for {
			if attempts >= maxAttempts {
				return errors.New("request timed-out after too many attempts")
			}
			<-ticker.C
			deviceAuthorizationToken, err = login.FetchToken(
				client,
				deviceAuthorization.DeviceCode,
				viper.GetString(cliflags.BaseURIFlag),
			)
			if err == nil {
				break
			}
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			err := json.Unmarshal([]byte(err.Error()), &e)
			if err != nil {
				return errs.NewErrorWrapped("error reading response", err)
			}
			switch e.Message {
			case "authorization_pending":
				attempts += 1
			case "access_denied":
				return errs.NewError("Your request has been denied. Please try logging in again.")
			case "expired_token":
				return errs.NewError("Your request has expired. Please try logging in again.")
			default:
				return errs.NewErrorWrapped("We cannot complete your request", err)
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Your token is %s\n", deviceAuthorizationToken.AccessToken)

		return nil
	}
}
