package environments

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/environments"
	"ldcli/internal/errors"
	"ldcli/internal/output"
)

func NewGetCmd(
	client environments.Client,
) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Return an environment",
		RunE:  runGet(client),
		Short: "Return an environment",
		Use:   "get",
	}

	cmd.Flags().StringP(cliflags.EnvironmentFlag, "e", "", "Environment key")
	err := cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	if err != nil {
		return nil, err
	}

	err = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().StringP(cliflags.ProjectFlag, "p", "", "Project key")
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

func runGet(
	client environments.Client,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
		_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

		response, err := client.Get(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetString(cliflags.EnvironmentFlag),
			viper.GetString(cliflags.ProjectFlag),
		)
		if err != nil {
			output, err := output.CmdOutputSingular(
				viper.GetString(cliflags.OutputFlag),
				[]byte(err.Error()),
				output.ErrorPlaintextOutputFn,
			)
			if err != nil {
				return errors.NewError(err.Error())
			}

			return errors.NewError(output)
		}

		output, err := output.CmdOutputSingular(
			viper.GetString(cliflags.OutputFlag),
			response,
			output.SingularPlaintextOutputFn,
		)
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}
