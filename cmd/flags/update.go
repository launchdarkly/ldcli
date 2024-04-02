package flags

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/flags"
)

func NewUpdateCmd(client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Update a flag",
		RunE:  runUpdate(client),
		Short: "Update a flag",
		Use:   "update",
	}

	cmd.Flags().StringP(cliflags.DataFlag, "d", "", "Input data in JSON")
	err := cmd.MarkFlagRequired(cliflags.DataFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.DataFlag, cmd.Flags().Lookup(cliflags.DataFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String(cliflags.FlagFlag, "", "Flag key")
	err = cmd.MarkFlagRequired(cliflags.FlagFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String(cliflags.ProjectFlag, "", "Project key")
	err = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func NewToggleOnUpdateCmd(client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Turn a flag on",
		RunE:  runUpdate(client),
		Short: "Turn a flag on",
		Use:   "toggle-on",
	}

	return setToggleCommandFlags(cmd)
}

func NewToggleOffUpdateCmd(client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Turn a flag off",
		RunE:  runUpdate(client),
		Short: "Turn a flag off",
		Use:   "toggle-off",
	}

	return setToggleCommandFlags(cmd)
}

func setToggleCommandFlags(cmd *cobra.Command) (*cobra.Command, error) {
	cmd.Flags().StringP(cliflags.EnvironmentFlag, "e", "", "Environment key")
	err := cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String(cliflags.FlagFlag, "", "Flag key")
	err = cmd.MarkFlagRequired(cliflags.FlagFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String(cliflags.ProjectFlag, "", "Project key")
	err = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func runUpdate(client flags.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// rebind flags used in other subcommands
		_ = viper.BindPFlag(cliflags.DataFlag, cmd.Flags().Lookup(cliflags.DataFlag))
		_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
		_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

		var patch []flags.UpdateInput
		if cmd.CalledAs() == "toggle-on" || cmd.CalledAs() == "toggle-off" {
			_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
			err := json.Unmarshal([]byte(buildPatch(viper.GetString(cliflags.EnvironmentFlag), cmd.CalledAs() == "toggle-on")), &patch)
			if err != nil {
				return err
			}
		} else {
			err := json.Unmarshal([]byte(viper.GetString(cliflags.DataFlag)), &patch)
			if err != nil {
				return err
			}
		}

		response, err := client.Update(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetString(cliflags.FlagFlag),
			viper.GetString(cliflags.ProjectFlag),
			patch,
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}

func buildPatch(envKey string, toggleValue bool) string {
	return fmt.Sprintf(`[{"op": "replace", "path": "/environments/%s/on", "value": %t}]`, envKey, toggleValue)
}
