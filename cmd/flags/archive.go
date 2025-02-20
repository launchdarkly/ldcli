package flags

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

func NewArchiveCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Archive a flag across all environments. It will only appear in your list when filtered for.",
		RunE:  makeArchiveRequest(client),
		Short: "Archive a feature flag",
		Use:   "archive",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initArchiveFlags(cmd)

	return cmd
}

func makeArchiveRequest(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/flags",
			viper.GetString(cliflags.ProjectFlag),
			viper.GetString(cliflags.FlagFlag),
		)
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"PATCH",
			path,
			"application/json",
			nil,
			[]byte(`[{"op": "replace", "path": "/archived", "value": true}]`),
			false,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		output, err := output.CmdOutput("update", viper.GetString(cliflags.OutputFlag), res)
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprint(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}

func initArchiveFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.FlagFlag, "", "The feature flag key. The key identifies the flag in your code.")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
}
