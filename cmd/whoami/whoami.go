package whoami

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewWhoAmICmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Show information about the identity associated with the current access token.",
		RunE:  makeRequest(client),
		Short: "Show current caller identity",
		Use:   "whoami",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func makeRequest(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/caller-identity",
		)
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
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
