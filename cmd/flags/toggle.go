package flags

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	resourcescmd "ldcli/cmd/resources"
	"ldcli/cmd/validators"
	"ldcli/internal/errors"
	"ldcli/internal/output"
	"ldcli/internal/resources"
)

func NewToggleOnCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Turn a feature flag on",
		RunE:  runE(client),
		Short: "Turn a feature flag on",
		Use:   "toggle-on",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initFlags(cmd)

	return cmd
}

func NewToggleOffCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Turn a feature flag off",
		RunE:  runE(client),
		Short: "Turn a feature flag off",
		Use:   "toggle-off",
	}

	initFlags(cmd)

	return cmd
}

func runE(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		var toggleOn bool
		switch cmd.CalledAs() {
		case "toggle-on":
			toggleOn = true
		case "toggle-off":
			toggleOn = false
		}

		path := fmt.Sprintf(
			"%s/api/v2/flags/%s/%s",
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetString(cliflags.ProjectFlag),
			viper.GetString(cliflags.FlagFlag),
		)
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"PATCH",
			path,
			"application/json",
			nil,
			[]byte(buildPatch(viper.GetString("environment"), toggleOn)),
		)
		if err != nil {
			return errors.NewError(output.CmdOutputError(viper.GetString(cliflags.OutputFlag), err))
		}

		output, err := output.CmdOutput("update", viper.GetString(cliflags.OutputFlag), res)
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}

func initFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.EnvironmentFlag, "", "The environment key")
	_ = cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.EnvironmentFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))

	cmd.Flags().String(cliflags.FlagFlag, "", "The feature flag key")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
}

func buildPatch(envKey string, toggleValue bool) string {
	return fmt.Sprintf(`[{"op": "replace", "path": "/environments/%s/on", "value": %t}]`, envKey, toggleValue)
}
