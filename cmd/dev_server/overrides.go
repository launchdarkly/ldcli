package dev_server

import (
	"encoding/json"
	"fmt"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewAddOverrideCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "override flag value with value provided in the body",
		RunE:  addOverride(client),
		Short: "override flag value",
		Use:   "add-override",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(cliflags.DataFlag, "", "flag value to override flag with. The json representation of the variation value")
	_ = cmd.MarkFlagRequired(cliflags.DataFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.DataFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.DataFlag, cmd.Flags().Lookup(cliflags.DataFlag))

	cmd.MarkFlagsRequiredTogether("context-kind", "context-key")

	return cmd
}

func addOverride(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		var data interface{}
		err := json.Unmarshal([]byte(viper.GetString(cliflags.DataFlag)), &data)
		if err != nil {
			return err
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}

		path := DEV_SERVER + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"PUT",
			path,
			"application/json",
			nil,
			jsonData,
			false,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}
