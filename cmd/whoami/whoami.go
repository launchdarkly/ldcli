package whoami

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewWhoAmICmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  cobra.NoArgs,
		Long:  "Show information about the identity associated with the current access token.",
		RunE:  makeRequest(client),
		Short: "Show current caller identity",
		Use:   "whoami",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	// Hide flags that don't apply to whoami from its help output.
	// Access token and base URI are read from config; analytics opt-out is not relevant.
	hiddenInHelp := []string{
		cliflags.AccessTokenFlag,
		cliflags.BaseURIFlag,
		cliflags.AnalyticsOptOut,
	}
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		for _, name := range hiddenInHelp {
			if f := c.Root().PersistentFlags().Lookup(name); f != nil {
				f.Hidden = true
			}
		}
		defaultHelp(c, args)
		for _, name := range hiddenInHelp {
			if f := c.Root().PersistentFlags().Lookup(name); f != nil {
				f.Hidden = false
			}
		}
	})

	return cmd
}

func makeRequest(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		if accessToken == "" {
			return errors.NewError("no access token configured. Run `ldcli login` or set LD_ACCESS_TOKEN")
		}

		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/caller-identity",
		)
		res, err := client.MakeRequest(
			accessToken,
			"GET",
			path,
			"application/json",
			nil,
			nil,
			false,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		out, err := output.CmdOutputSingular(viper.GetString(cliflags.OutputFlag), res, output.ConfigPlaintextOutputFn)
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprint(cmd.OutOrStdout(), out+"\n")

		return nil
	}
}
