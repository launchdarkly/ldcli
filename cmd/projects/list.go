package projects

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/errors"
	"ldcli/internal/output"
	"ldcli/internal/projects"
)

func NewListCmd(client projects.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Return a list of projects",
		RunE:  runList(client),
		Short: "Return a list of projects",
		Use:   "list",
	}

	return cmd
}

func runList(client projects.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		response, err := client.List(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
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

		output, err := output.CmdOutput("list", viper.GetString(cliflags.OutputFlag), response)
		if err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

		return nil
	}
}
