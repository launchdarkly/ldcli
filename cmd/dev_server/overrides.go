package dev_server

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/output"
)

func NewAddOverrideCmd(client dev_server.Client) *cobra.Command {
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

	cmd.Flags().String(cliflags.FlagFlag, "", "The flag key")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	cmd.Flags().String(cliflags.DataFlag, "", "flag value to override flag with. The json representation of the variation value")
	_ = cmd.MarkFlagRequired(cliflags.DataFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.DataFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.DataFlag, cmd.Flags().Lookup(cliflags.DataFlag))

	return cmd
}

func addOverride(client dev_server.Client) func(*cobra.Command, []string) error {
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

		path := fmt.Sprintf("%s/dev/projects/%s/overrides/%s", DEV_SERVER, viper.GetString(cliflags.ProjectFlag), viper.GetString(cliflags.FlagFlag))
		res, err := client.MakeRequest(
			"PUT",
			path,
			jsonData,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewRemoveOverrideCmd(client dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "remove override for flag",
		RunE:  removeOverride(client),
		Short: "remove override",
		Use:   "remove-override",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(cliflags.FlagFlag, "", "The flag key")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	return cmd
}

func removeOverride(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		path := fmt.Sprintf("%s/dev/projects/%s/overrides/%s", DEV_SERVER, viper.GetString(cliflags.ProjectFlag), viper.GetString(cliflags.FlagFlag))
		res, err := client.MakeRequest(
			"DELETE",
			path,
			nil,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}
