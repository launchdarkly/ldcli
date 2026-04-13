package signup

import (
	"fmt"
	"net/url"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
)

func NewSignupCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Long: "Open your browser to create a new LaunchDarkly account",
		PreRun: func(cmd *cobra.Command, args []string) {
			analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, "signup", nil))
		},
		RunE:  run,
		Short: "Create a new LaunchDarkly account",
		Use:   "signup",
	}

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	signupURL, err := url.JoinPath(
		viper.GetString(cliflags.BaseURIFlag),
		"/signup",
	)
	if err != nil {
		return fmt.Errorf("failed to construct signup URL: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Opening your browser to %s to create a new LaunchDarkly account.\n", signupURL)
	fmt.Fprintln(cmd.OutOrStdout(), "If your browser does not open automatically, you can paste the above URL into your browser.")

	err = browser.OpenURL(signupURL)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to open browser automatically: %v\n", err)
	}

	return nil
}
